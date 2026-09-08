package sessions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/maestroi/GamePilot/profiles/boxxle"
)

type fakeBoxxleRuntime struct {
	hash       string
	title      string
	initial    boxxle.Observation
	after      boxxle.Observation
	startCalls int
	execCalls  int
	closeCalls int
	move       boxxle.Move
}

func (f *fakeBoxxleRuntime) ROMHash() string        { return f.hash }
func (f *fakeBoxxleRuntime) CartridgeTitle() string { return f.title }
func (f *fakeBoxxleRuntime) Start(ctx context.Context) error {
	f.startCalls++
	return ctx.Err()
}
func (f *fakeBoxxleRuntime) Observe() (boxxle.Observation, error) { return f.initial, nil }
func (f *fakeBoxxleRuntime) Execute(ctx context.Context, move boxxle.Move) (boxxle.Observation, error) {
	if err := ctx.Err(); err != nil {
		return boxxle.Observation{}, err
	}
	f.execCalls++
	f.move = move
	return f.after, nil
}
func (f *fakeBoxxleRuntime) Close() error {
	f.closeCalls++
	return nil
}

type fixedBoxxlePlanner struct {
	plan BoxxlePlan
	err  error
}

func (p fixedBoxxlePlanner) Plan(context.Context, boxxle.Observation) (BoxxlePlan, error) {
	return p.plan, p.err
}

func TestBoxxleRunnerExecutesUntilMoveLimit(t *testing.T) {
	initial := boxxleReadyObservation(100, 1, 1)
	after := boxxleReadyObservation(140, 2, 1)
	after.Moves = 1
	runtime := &fakeBoxxleRuntime{
		hash:    boxxle.USAEuropeSHA256,
		title:   "BOXXLE",
		initial: initial,
		after:   after,
	}
	move := boxxle.Move{Direction: boxxle.DirRight}
	factory := &boxxleRunnerFactory{
		open: func(path string) (boxxleRuntime, error) {
			if path != "/roms/boxxle.gb" {
				t.Fatalf("ROM path = %q", path)
			}
			return runtime, nil
		},
		planners: map[string]BoxxlePlannerFactory{
			"fixed": func(LaunchConfig) (BoxxlePlanner, error) {
				return fixedBoxxlePlanner{plan: BoxxlePlan{
					Move:     move,
					Decision: json.RawMessage(`{"move":{"direction":"right"}}`),
				}}, nil
			},
		},
	}

	runner, err := factory.New(LaunchConfig{
		ROMPath:   "/roms/boxxle.gb",
		Profile:   boxxle.ProfileID,
		Planner:   "fixed",
		MoveLimit: 1,
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
	if runtime.move != move {
		t.Fatalf("executed move = %+v, want %+v", runtime.move, move)
	}
	if len(updates) != 3 {
		t.Fatalf("updates = %d, want initial + decision + after", len(updates))
	}
	if updates[0].Profile != boxxle.ProfileID || updates[0].PlannerActivity != "planning" {
		t.Fatalf("initial update = %+v", updates[0])
	}
	if updates[2].Moves != 1 || updates[2].Frame != after.Frame {
		t.Fatalf("after update = %+v", updates[2])
	}
}

func TestBoxxleRunnerStopsWhenSolved(t *testing.T) {
	initial := boxxleReadyObservation(100, 1, 1)
	after := boxxleReadyObservation(140, 2, 1)
	after.Solved = true
	runtime := &fakeBoxxleRuntime{hash: boxxle.USAEuropeSHA256, title: "BOXXLE", initial: initial, after: after}
	factory := &boxxleRunnerFactory{
		open: func(string) (boxxleRuntime, error) { return runtime, nil },
		planners: map[string]BoxxlePlannerFactory{
			"fixed": func(LaunchConfig) (BoxxlePlanner, error) {
				return fixedBoxxlePlanner{plan: BoxxlePlan{Move: boxxle.Move{Direction: boxxle.DirRight}, Decision: json.RawMessage(`{}`)}}, nil
			},
		},
	}
	runner, err := factory.New(LaunchConfig{ROMPath: "x", Profile: boxxle.ProfileID, Planner: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), func(Update) {})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "solved" {
		t.Fatalf("reason = %q", result.Reason)
	}
}

func TestBoxxleRunnerFactoryValidatesProfileAndPlanner(t *testing.T) {
	factory := NewBoxxleRunnerFactory()
	if _, err := factory.New(LaunchConfig{ROMPath: "x", Profile: "tetris", Planner: "heuristic"}); err == nil {
		t.Fatal("expected unsupported profile error")
	}
	if _, err := factory.New(LaunchConfig{ROMPath: "x", Profile: boxxle.ProfileID, Planner: "lookahead"}); err == nil {
		t.Fatal("expected unsupported planner error")
	}
}

func TestLiveRunnerFactoryAcceptsBoxxleAndTetris(t *testing.T) {
	factory := NewLiveRunnerFactory(nil)
	if _, err := factory.New(LaunchConfig{ROMPath: "x", Profile: boxxle.ProfileID, Planner: "heuristic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.New(LaunchConfig{ROMPath: "x", Profile: "tetris", Planner: "heuristic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.New(LaunchConfig{ROMPath: "x", Profile: "pong", Planner: "heuristic"}); err == nil {
		t.Fatal("expected unsupported profile")
	}
}

func boxxleReadyObservation(frame uint64, x, y int) boxxle.Observation {
	var obs boxxle.Observation
	obs.Frame = frame
	obs.PlayerX = x
	obs.PlayerY = y
	obs.Ready = true
	obs.Playing = true
	obs.Grid[0][0] = boxxle.Wall
	obs.Grid[y][x] = boxxle.Player
	return obs
}
