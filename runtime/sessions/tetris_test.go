package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/maestroi/GamePilot/profiles/tetris"
)

type fixedPlanner struct {
	plan TetrisPlan
	err  error
}

func (p fixedPlanner) Plan(context.Context, tetris.Observation) (TetrisPlan, error) {
	return p.plan, p.err
}

type blockingPlanner struct {
	started chan struct{}
	once    sync.Once
}

func (p *blockingPlanner) Plan(ctx context.Context, _ tetris.Observation) (TetrisPlan, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return TetrisPlan{}, ctx.Err()
}

type fakeTetrisRuntime struct {
	hash       string
	title      string
	initial    tetris.Observation
	after      tetris.Observation
	startCalls int
	execCalls  int
	closeCalls int
	placement  tetris.Placement
}

func (f *fakeTetrisRuntime) ROMHash() string { return f.hash }
func (f *fakeTetrisRuntime) CartridgeTitle() string { return f.title }
func (f *fakeTetrisRuntime) Start(ctx context.Context) error {
	f.startCalls++
	return ctx.Err()
}
func (f *fakeTetrisRuntime) Observe() (tetris.Observation, error) { return f.initial, nil }
func (f *fakeTetrisRuntime) Execute(ctx context.Context, placement tetris.Placement) (tetris.Observation, error) {
	if err := ctx.Err(); err != nil {
		return tetris.Observation{}, err
	}
	f.execCalls++
	f.placement = placement
	return f.after, nil
}
func (f *fakeTetrisRuntime) Close() error {
	f.closeCalls++
	return nil
}

func TestTetrisRunnerExecutesAndFinalizesReplay(t *testing.T) {
	initial := liveReadyObservation(100, tetris.PieceT, tetris.PieceJ)
	after := liveReadyObservation(180, tetris.PieceJ, tetris.PieceO)
	after.Lines = 1
	placement := tetris.Placement{Rotation: 2, TargetColumn: 3}
	runtime := &fakeTetrisRuntime{
		hash:    tetris.Rev1SHA256,
		title:   "TETRIS",
		initial: initial,
		after:   after,
	}
	factory := &tetrisRunnerFactory{
		open: func(path string) (tetrisRuntime, error) {
			if path != "/roms/tetris.gb" {
				t.Fatalf("ROM path = %q", path)
			}
			return runtime, nil
		},
		planners: map[string]TetrisPlannerFactory{
			"fixed": func(LaunchConfig) (TetrisPlanner, error) {
				return fixedPlanner{plan: TetrisPlan{
					Placement: placement,
					Decision:  json.RawMessage(`{"placement":{"rotation":2,"target_column":3}}`),
				}}, nil
			},
		},
	}

	runner, err := factory.New(LaunchConfig{
		ROMPath:      "/roms/tetris.gb",
		Profile:      tetris.ProfileID,
		Planner:      "fixed",
		MoveLimit:    1,
		RecordReplay: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var updates []Update
	result, err := runner.Run(context.Background(), func(update Update) {
		updates = append(updates, update)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "move_limit" {
		t.Fatalf("reason = %q", result.Reason)
	}
	if runtime.startCalls != 1 || runtime.execCalls != 1 || runtime.closeCalls != 1 {
		t.Fatalf("runtime calls start=%d execute=%d close=%d", runtime.startCalls, runtime.execCalls, runtime.closeCalls)
	}
	if runtime.placement != placement {
		t.Fatalf("executed placement = %+v, want %+v", runtime.placement, placement)
	}
	if len(updates) != 3 {
		t.Fatalf("updates = %d, want initial + decision + after", len(updates))
	}
	if updates[0].Frame != initial.Frame || updates[0].Moves != 0 || updates[0].PlannerActivity != "planning" {
		t.Fatalf("initial update = %+v", updates[0])
	}
	if updates[1].PlannerActivity != "executing" || string(updates[1].Decision) == "" {
		t.Fatalf("decision update = %+v", updates[1])
	}
	if updates[2].Frame != after.Frame || updates[2].Moves != 1 || updates[2].PlannerActivity != "planning" {
		t.Fatalf("after update = %+v", updates[2])
	}

	replay, err := tetris.ReadReplay(bytes.NewReader(result.Replay))
	if err != nil {
		t.Fatalf("read finalized replay: %v", err)
	}
	if len(replay.Moves) != 1 || replay.Moves[0].Placement != placement {
		t.Fatalf("replay moves = %+v", replay.Moves)
	}
	if replay.ROMSHA256 != tetris.Rev1SHA256 {
		t.Fatalf("replay ROM hash = %q", replay.ROMSHA256)
	}
}

func TestTetrisRunnerCancellationFinalizesPartialReplayAndClosesOnce(t *testing.T) {
	initial := liveReadyObservation(100, tetris.PieceT, tetris.PieceJ)
	runtime := &fakeTetrisRuntime{hash: tetris.Rev1SHA256, title: "TETRIS", initial: initial}
	planner := &blockingPlanner{started: make(chan struct{})}
	factory := &tetrisRunnerFactory{
		open: func(string) (tetrisRuntime, error) { return runtime, nil },
		planners: map[string]TetrisPlannerFactory{
			"blocking": func(LaunchConfig) (TetrisPlanner, error) { return planner, nil },
		},
	}
	runner, err := factory.New(LaunchConfig{ROMPath: "x", Profile: tetris.ProfileID, Planner: "blocking", RecordReplay: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		result Result
		err    error
	}
	out := make(chan outcome, 1)
	go func() {
		result, err := runner.Run(ctx, func(Update) {})
		out <- outcome{result: result, err: err}
	}()
	select {
	case <-planner.started:
	case <-time.After(time.Second):
		t.Fatal("planner did not start")
	}
	cancel()
	select {
	case got := <-out:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", got.err)
		}
		if got.result.Reason != "stopped" {
			t.Fatalf("reason = %q", got.result.Reason)
		}
		replay, err := tetris.ReadReplay(bytes.NewReader(got.result.Replay))
		if err != nil {
			t.Fatalf("read partial replay: %v", err)
		}
		if len(replay.Moves) != 0 {
			t.Fatalf("partial replay moves = %d, want 0", len(replay.Moves))
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
	if runtime.closeCalls != 1 {
		t.Fatalf("close calls = %d, want exactly 1", runtime.closeCalls)
	}
}

func TestTetrisRunnerFactoryValidatesProfileAndPlanner(t *testing.T) {
	factory := NewTetrisRunnerFactory(nil)
	if _, err := factory.New(LaunchConfig{ROMPath: "x", Profile: "other", Planner: "heuristic"}); err == nil {
		t.Fatal("expected unsupported profile error")
	}
	if _, err := factory.New(LaunchConfig{ROMPath: "x", Profile: tetris.ProfileID, Planner: "missing"}); err == nil {
		t.Fatal("expected unsupported planner error")
	}
}

func liveReadyObservation(frame uint64, current, next tetris.PieceKind) tetris.Observation {
	return tetris.Observation{
		Frame: frame,
		CurrentPiece: tetris.Piece{
			Kind:     current,
			Rotation: 0,
			AnchorX:  63,
			AnchorY:  24,
			Visible:  true,
		},
		NextPiece: tetris.Piece{Kind: next, Rotation: 0},
		Ready:     true,
	}
}
