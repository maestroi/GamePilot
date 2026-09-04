package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/maestroi/GamePilot/profiles/tetris"
)

type observedRuntimeFake struct {
	initial      tetris.Observation
	intermediate []tetris.Observation
	after        tetris.Observation
	currentFrame uint64
	captures     int
	execCalls    int
	observedCalls int
}

func (f *observedRuntimeFake) ROMHash() string       { return tetris.Rev1SHA256 }
func (f *observedRuntimeFake) CartridgeTitle() string { return "TETRIS" }
func (f *observedRuntimeFake) Start(context.Context) error {
	f.currentFrame = f.initial.Frame
	return nil
}
func (f *observedRuntimeFake) Observe() (tetris.Observation, error) { return f.initial, nil }
func (f *observedRuntimeFake) Execute(ctx context.Context, _ tetris.Placement) (tetris.Observation, error) {
	if err := ctx.Err(); err != nil {
		return tetris.Observation{}, err
	}
	f.execCalls++
	f.currentFrame = f.after.Frame
	return f.after, nil
}
func (f *observedRuntimeFake) ExecuteObserved(ctx context.Context, _ tetris.Placement, observer tetris.PlacementFrameObserver) (tetris.Observation, error) {
	f.observedCalls++
	for _, obs := range f.intermediate {
		if err := ctx.Err(); err != nil {
			return tetris.Observation{}, err
		}
		f.currentFrame = obs.Frame
		if err := observer(obs); err != nil {
			return tetris.Observation{}, err
		}
	}
	f.currentFrame = f.after.Frame
	return f.after, nil
}
func (f *observedRuntimeFake) CaptureFrame() (Frame, error) {
	f.captures++
	return Frame{
		EmulatorFrame: f.currentFrame,
		Width:         160,
		Height:        144,
		ContentType:   "image/png",
		Data:          []byte{byte(f.currentFrame)},
	}, nil
}
func (f *observedRuntimeFake) Close() error { return nil }

type recordingPacer struct {
	frames []uint64
}

func (p *recordingPacer) Wait(ctx context.Context, frame uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.frames = append(p.frames, frame)
	return nil
}

type advancingPlanner struct {
	placement tetris.Placement
	now       *time.Time
	advance   time.Duration
}

func (p advancingPlanner) Plan(context.Context, tetris.Observation) (TetrisPlan, error) {
	*p.now = p.now.Add(p.advance)
	payload, _ := json.Marshal(struct {
		Placement tetris.Placement `json:"placement"`
	}{Placement: p.placement})
	return TetrisPlan{Placement: p.placement, Decision: payload}, nil
}

func TestRealtimeTetrisRunnerPublishesIntermediateFramesAndPlannerLatency(t *testing.T) {
	initial := liveReadyObservation(100, tetris.PieceT, tetris.PieceJ)
	step1 := initial
	step1.Frame = 101
	step1.CurrentPiece.AnchorX = 71
	step2 := step1
	step2.Frame = 102
	step2.CurrentPiece.AnchorY = 32
	step3 := step2
	step3.Frame = 103
	step3.CurrentPiece.AnchorY = 40
	step4 := step3
	step4.Frame = 104
	step4.CurrentPiece.Visible = false
	step4.Ready = false
	after := liveReadyObservation(105, tetris.PieceJ, tetris.PieceO)

	runtime := &observedRuntimeFake{
		initial:      initial,
		intermediate: []tetris.Observation{step1, step2, step3, step4, after},
		after:        after,
	}
	clock := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	placement := tetris.Placement{Rotation: 0, TargetColumn: 4}
	pacer := &recordingPacer{}
	factory := &tetrisRunnerFactory{
		open: func(string) (tetrisRuntime, error) { return runtime, nil },
		planners: map[string]TetrisPlannerFactory{
			"fixed": func(LaunchConfig) (TetrisPlanner, error) {
				return advancingPlanner{placement: placement, now: &clock, advance: 125 * time.Millisecond}, nil
			},
		},
		now: func() time.Time { return clock },
		pacer: func(PacingMode) framePacer { return pacer },
	}
	runner, err := factory.New(LaunchConfig{
		ROMPath:      "x",
		Profile:      tetris.ProfileID,
		Planner:      "fixed",
		MoveLimit:    1,
		RecordReplay: true,
		Pacing:       PacingRealtime,
	})
	if err != nil {
		t.Fatal(err)
	}

	var updates []Update
	result, err := runner.Run(context.Background(), func(update Update) { updates = append(updates, update) })
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "move_limit" {
		t.Fatalf("reason = %q", result.Reason)
	}
	if runtime.observedCalls != 1 || runtime.execCalls != 0 {
		t.Fatalf("observed=%d fast=%d, want observed execution only", runtime.observedCalls, runtime.execCalls)
	}
	if len(pacer.frames) != len(runtime.intermediate) {
		t.Fatalf("paced frames = %v, want one wait per controller frame", pacer.frames)
	}
	for i, obs := range runtime.intermediate {
		if pacer.frames[i] != obs.Frame {
			t.Fatalf("paced frame[%d] = %d, want %d", i, pacer.frames[i], obs.Frame)
		}
	}

	var executingFrames []uint64
	var sawPlanningStart bool
	var sawLatency bool
	for _, update := range updates {
		if update.PlannerActivity == "planning" && !update.PlannerStartedAt.IsZero() {
			sawPlanningStart = true
		}
		if update.PlannerActivity == "executing" {
			if update.Image != nil {
				executingFrames = append(executingFrames, update.Frame)
			}
			if update.PlannerLatencyMS == 125 {
				sawLatency = true
			}
		}
	}
	if !sawPlanningStart {
		t.Fatal("planning start timestamp was not published")
	}
	if !sawLatency {
		t.Fatal("planner latency was not surfaced on executing updates")
	}
	if len(executingFrames) < 3 {
		t.Fatalf("intermediate frame publications = %v, want multiple visible execution frames", executingFrames)
	}
}

func TestFastAndRealtimeProduceEquivalentReplayBoundaries(t *testing.T) {
	initial := liveReadyObservation(50, tetris.PieceT, tetris.PieceJ)
	mid := initial
	mid.Frame = 51
	mid.CurrentPiece.AnchorY = 32
	after := liveReadyObservation(55, tetris.PieceJ, tetris.PieceO)
	placement := tetris.Placement{Rotation: 0, TargetColumn: 4}

	run := func(mode PacingMode) []byte {
		runtime := &observedRuntimeFake{
			initial:      initial,
			intermediate: []tetris.Observation{mid, after},
			after:        after,
		}
		factory := &tetrisRunnerFactory{
			open: func(string) (tetrisRuntime, error) { return runtime, nil },
			planners: map[string]TetrisPlannerFactory{
				"fixed": func(LaunchConfig) (TetrisPlanner, error) {
					return fixedPlanner{plan: TetrisPlan{Placement: placement, Decision: json.RawMessage(`{"ok":true}`)}}, nil
				},
			},
			now: time.Now,
			pacer: func(PacingMode) framePacer { return fastFramePacer{} },
		}
		runner, err := factory.New(LaunchConfig{
			ROMPath:      "x",
			Profile:      tetris.ProfileID,
			Planner:      "fixed",
			MoveLimit:    1,
			RecordReplay: true,
			Pacing:       mode,
		})
		if err != nil {
			t.Fatal(err)
		}
		result, err := runner.Run(context.Background(), func(Update) {})
		if err != nil {
			t.Fatal(err)
		}
		return result.Replay
	}

	fast := run(PacingFast)
	realtime := run(PacingRealtime)
	if !bytes.Equal(fast, realtime) {
		t.Fatalf("replay bytes differ between fast and realtime execution\nfast=%s\nrealtime=%s", fast, realtime)
	}
}
