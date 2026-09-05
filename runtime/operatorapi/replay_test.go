package operatorapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/maestroi/GamePilot/runtime/sessions"
)

func TestHandlerWithReplayServesRetainedReplay(t *testing.T) {
	manager := sessions.NewManager(sessions.RunnerFactoryFunc(func(sessions.LaunchConfig) (sessions.Runner, error) {
		return testRunnerFunc(func(context.Context, func(sessions.Update)) (sessions.Result, error) {
			return sessions.Result{Reason: "completed", Replay: []byte(`{"format_version":1}`)}, nil
		}), nil
	}))
	h, err := NewHandlerWithReplay(Options{
		Manager:       manager,
		OperatorToken: "token",
		ROMs:          []ROMOption{{Alias: "tetris-rev1", Profile: "tetris", Path: "/private/tetris.gb"}},
		Profiles:      []ProfileOption{{ID: "tetris", Planners: []PlannerOption{{ID: "lookahead"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	create := doAuthed(h, http.MethodPost, "/v1/sessions", "token", `{"rom_alias":"tetris-rev1","profile":"tetris","planner":"lookahead","record_replay":true}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var snap sessions.Snapshot
	if err := json.Unmarshal(create.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := manager.Wait(ctx, snap.ID); err != nil {
		t.Fatal(err)
	}

	unauth := doAuthed(h, http.MethodGet, "/v1/sessions/"+snap.ID+"/replay", "wrong", "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized replay status = %d", unauth.Code)
	}

	replay := doAuthed(h, http.MethodGet, "/v1/sessions/"+snap.ID+"/replay", "token", "")
	if replay.Code != http.StatusOK || replay.Body.String() != `{"format_version":1}` {
		t.Fatalf("replay response = %d %s", replay.Code, replay.Body.String())
	}
	if replay.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("replay Content-Type = %q", replay.Header().Get("Content-Type"))
	}
	if replay.Header().Get("Content-Disposition") == "" {
		t.Fatal("replay response missing Content-Disposition")
	}
}

func TestHandlerWithReplayKeepsAuthDisabledSurfaceUnroutable(t *testing.T) {
	h, err := NewHandlerWithReplay(Options{})
	if err != nil {
		t.Fatal(err)
	}
	res := doAuthed(h, http.MethodGet, "/v1/sessions/missing/replay", "anything", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("auth-disabled replay status = %d, want 404", res.Code)
	}
}
