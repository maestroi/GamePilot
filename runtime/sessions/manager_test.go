package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type runnerFunc func(context.Context, func(Update)) (Result, error)

func (f runnerFunc) Run(ctx context.Context, publish func(Update)) (Result, error) {
	return f(ctx, publish)
}

func TestManagerStartSnapshotStopAndReplay(t *testing.T) {
	started := make(chan struct{})
	factory := RunnerFactoryFunc(func(config LaunchConfig) (Runner, error) {
		if config.Planner != "fake" {
			t.Fatalf("planner = %q, want fake", config.Planner)
		}
		return runnerFunc(func(ctx context.Context, publish func(Update)) (Result, error) {
			publish(Update{
				Profile:         "tetris",
				ROMSHA256:       "hash",
				CartridgeTitle:  "TETRIS",
				Frame:           123,
				Moves:           4,
				Observation:     json.RawMessage(`{"board":"snapshot"}`),
				Decision:        json.RawMessage(`{"rotation":2}`),
				PlannerActivity: "planning",
			})
			close(started)
			<-ctx.Done()
			return Result{Reason: "stopped", Replay: []byte("replay-data")}, ctx.Err()
		}), nil
	})

	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	m := newManager(factory, func() time.Time { return now }, func() (string, error) { return "session-1", nil })
	id, err := m.Start(LaunchConfig{ROMPath: "/private/tetris.gb", Profile: "tetris", Planner: "fake", RecordReplay: true})
	if err != nil {
		t.Fatal(err)
	}
	if id != "session-1" {
		t.Fatalf("id = %q", id)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}

	snap, err := m.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != StatusRunning {
		t.Fatalf("status = %s, want running", snap.Status)
	}
	if snap.Frame != 123 || snap.Moves != 4 || snap.ROMSHA256 != "hash" || snap.CartridgeTitle != "TETRIS" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if snap.Config.ROMPath != "/private/tetris.gb" {
		t.Fatalf("internal config ROM path lost: %q", snap.Config.ROMPath)
	}

	// Snapshot payloads are copies: caller mutation must not alter manager state.
	snap.Observation[0] = 'X'
	again, err := m.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if string(again.Observation) != `{"board":"snapshot"}` {
		t.Fatalf("manager observation mutated through snapshot: %q", again.Observation)
	}

	// ROMPath is excluded from accidental JSON serialization at the read-model boundary.
	encoded, err := json.Marshal(again)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || containsString(string(encoded), "/private/tetris.gb") {
		t.Fatalf("serialized snapshot leaked ROM path: %s", encoded)
	}

	if err := m.Stop(id); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(id); err != nil { // idempotent
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	terminal, err := m.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Status != StatusDone || terminal.Reason != "stopped" {
		t.Fatalf("terminal snapshot = %+v", terminal)
	}
	replay, err := m.Replay(id)
	if err != nil {
		t.Fatal(err)
	}
	if string(replay) != "replay-data" {
		t.Fatalf("replay = %q", replay)
	}
	replay[0] = 'X'
	replay2, _ := m.Replay(id)
	if string(replay2) != "replay-data" {
		t.Fatalf("manager replay mutated through caller: %q", replay2)
	}
}

func TestManagerRetainsFailure(t *testing.T) {
	boom := errors.New("boom")
	m := newManager(
		RunnerFactoryFunc(func(LaunchConfig) (Runner, error) {
			return runnerFunc(func(context.Context, func(Update)) (Result, error) {
				return Result{Reason: "runner_failed"}, boom
			}), nil
		}),
		time.Now,
		func() (string, error) { return "failed", nil },
	)
	id, err := m.Start(LaunchConfig{ROMPath: "x", Profile: "tetris", Planner: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snap, err := m.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != StatusFailed || snap.Reason != "runner_failed" || snap.Error != boom.Error() {
		t.Fatalf("failed snapshot = %+v", snap)
	}
	if _, err := m.Replay(id); !errors.Is(err, ErrReplayUnavailable) {
		t.Fatalf("Replay error = %v, want ErrReplayUnavailable", err)
	}
}

func TestManagerMultipleSessionsAndConcurrentReaders(t *testing.T) {
	var mu sync.Mutex
	cancels := make(map[string]chan struct{})
	factory := RunnerFactoryFunc(func(config LaunchConfig) (Runner, error) {
		name := config.PlannerOptions.Model
		ch := make(chan struct{})
		mu.Lock()
		cancels[name] = ch
		mu.Unlock()
		return runnerFunc(func(ctx context.Context, publish func(Update)) (Result, error) {
			publish(Update{Profile: "tetris", Frame: 1, Observation: json.RawMessage(`{"ok":true}`)})
			select {
			case <-ctx.Done():
				return Result{Reason: "stopped"}, ctx.Err()
			case <-ch:
				return Result{Reason: "completed"}, nil
			}
		}), nil
	})
	ids := []string{"a", "b"}
	n := 0
	m := newManager(factory, time.Now, func() (string, error) {
		id := ids[n]
		n++
		return id, nil
	})
	for _, name := range []string{"one", "two"} {
		if _, err := m.Start(LaunchConfig{ROMPath: "x", Profile: "tetris", Planner: "fake", PlannerOptions: PlannerOptions{Model: name}}); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(time.Second)
	for {
		list := m.List()
		if len(list) == 2 && list[0].Frame == 1 && list[1].Frame == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sessions did not publish: %+v", list)
		}
		time.Sleep(time.Millisecond)
	}

	var readers sync.WaitGroup
	for i := 0; i < 20; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 100; j++ {
				_, _ = m.Snapshot("a")
				_ = m.List()
			}
		}()
	}
	readers.Wait()

	if err := m.Stop("a"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := m.Wait(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	second, err := m.Snapshot("b")
	if err != nil {
		t.Fatal(err)
	}
	if terminal(second.Status) {
		t.Fatalf("stopping session a terminated independent session b: %+v", second)
	}

	mu.Lock()
	close(cancels["two"])
	mu.Unlock()
	if _, err := m.Wait(ctx, "b"); err != nil {
		t.Fatal(err)
	}
}

func TestManagerRejectsInvalidConfigAndFactoryError(t *testing.T) {
	factoryErr := errors.New("bad planner")
	m := NewManager(RunnerFactoryFunc(func(LaunchConfig) (Runner, error) { return nil, factoryErr }))
	if _, err := m.Start(LaunchConfig{}); err == nil {
		t.Fatal("expected invalid config error")
	}
	_, err := m.Start(LaunchConfig{ROMPath: "x", Profile: "tetris", Planner: "bad"})
	if !errors.Is(err, factoryErr) {
		t.Fatalf("Start error = %v, want wrapped factory error", err)
	}
	if _, err := m.Snapshot("missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Snapshot error = %v, want ErrSessionNotFound", err)
	}
}

func TestManagerCloseStopsAll(t *testing.T) {
	factory := RunnerFactoryFunc(func(LaunchConfig) (Runner, error) {
		return runnerFunc(func(ctx context.Context, _ func(Update)) (Result, error) {
			<-ctx.Done()
			return Result{Reason: "stopped"}, ctx.Err()
		}), nil
	})
	n := 0
	m := newManager(factory, time.Now, func() (string, error) {
		n++
		return fmt.Sprintf("s%d", n), nil
	})
	for i := 0; i < 3; i++ {
		if _, err := m.Start(LaunchConfig{ROMPath: "x", Profile: "tetris", Planner: "fake"}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	for _, snap := range m.List() {
		if snap.Status != StatusDone {
			t.Fatalf("session %s status = %s, want done", snap.ID, snap.Status)
		}
	}
}

func containsString(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
