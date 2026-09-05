package spectatorapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maestroi/GamePilot/runtime/sessions"
)

type fakeReader struct {
	list      []sessions.Snapshot
	frames    map[string]sessions.Frame
	frameErrs map[string]error
}

func (f *fakeReader) List() []sessions.Snapshot { return append([]sessions.Snapshot(nil), f.list...) }
func (f *fakeReader) Snapshot(id string) (sessions.Snapshot, error) {
	for _, snap := range f.list {
		if snap.ID == id {
			return snap, nil
		}
	}
	return sessions.Snapshot{}, sessions.ErrSessionNotFound
}
func (f *fakeReader) Frame(id string) (sessions.Frame, error) {
	if err := f.frameErrs[id]; err != nil {
		return sessions.Frame{}, err
	}
	frame, ok := f.frames[id]
	if !ok {
		return sessions.Frame{}, sessions.ErrFrameUnavailable
	}
	return frame, nil
}

func TestWatchProjectsExplicitPublicDTOAndRedactsInternals(t *testing.T) {
	now := time.Date(2026, 9, 5, 18, 0, 0, 0, time.UTC)
	obs := json.RawMessage(`{"frame":123,"board":[[1,0,0,0,0,0,0,0,0,0]],"current_piece":{"kind":"T","rotation":2,"anchor_x":99,"anchor_y":88},"next_piece":{"kind":"I","rotation":1},"score":1200,"lines":4,"level":0,"ready":true,"game_over":false,"game_state":99,"secret":"observation-secret"}`)
	decision := json.RawMessage(`{"placement":{"rotation":2,"target_column":4},"analysis":"planner-secret"}`)
	reader := &fakeReader{list: []sessions.Snapshot{{
		ID:     "abc123",
		Status: sessions.StatusRunning,
		Config: sessions.LaunchConfig{
			ROMPath:  "/srv/private/roms/tetris.gb",
			Profile:  "tetris",
			Planner:  "llm",
			MoveLimit: 99,
			PlannerOptions: sessions.PlannerOptions{
				Model: "https://secret.invalid/v1?key=do-not-leak",
			},
		},
		Profile:          "tetris",
		ROMSHA256:        "private-rom-hash",
		CartridgeTitle:   "PRIVATE CART TITLE",
		Frame:            123,
		Moves:            7,
		Observation:      obs,
		Decision:         decision,
		PlannerActivity:  "planning",
		PlannerLatencyMS: 42,
		Sequence:         9,
		FrameSequence:    8,
		FrameAvailable:   true,
		Reason:           "private reason",
		Error:            "private runtime error",
		CreatedAt:        now.Add(-20 * time.Second),
		StartedAt:        now.Add(-15 * time.Second),
		UpdatedAt:        now,
	}}}

	h := &handler{reader: reader, now: func() time.Time { return now }}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/watch", h.getWatch)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/watch", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, secret := range []string{"/srv/private", "private-rom-hash", "PRIVATE CART TITLE", "do-not-leak", "secret.invalid", "private reason", "private runtime error", "observation-secret", "planner-secret", "anchor_x", "game_state"} {
		if strings.Contains(body, secret) {
			t.Fatalf("public JSON leaked %q: %s", secret, body)
		}
	}

	var got WatchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Session == nil || got.Session.ID != "abc123" {
		t.Fatalf("unexpected selected session: %#v", got.Session)
	}
	if got.Session.ModelLabel != "configured" {
		t.Fatalf("unsafe model label was not sanitized: %q", got.Session.ModelLabel)
	}
	if got.Session.Tetris == nil || got.Session.Tetris.Score != 1200 || got.Session.Tetris.Lines != 4 {
		t.Fatalf("unexpected public tetris state: %#v", got.Session.Tetris)
	}
	if got.Session.LatestPlacement == nil || got.Session.LatestPlacement.Rotation != 2 || got.Session.LatestPlacement.TargetColumn != 4 {
		t.Fatalf("unexpected placement: %#v", got.Session.LatestPlacement)
	}
	if got.Session.ElapsedSeconds != 15 {
		t.Fatalf("elapsed=%d want 15", got.Session.ElapsedSeconds)
	}
}

func TestWatchSelectsActiveFirstAndBoundsCompletedTail(t *testing.T) {
	now := time.Date(2026, 9, 5, 18, 0, 0, 0, time.UTC)
	reader := &fakeReader{}
	for i := 0; i < recentLimit+3; i++ {
		reader.list = append(reader.list, sessions.Snapshot{
			ID:        "done" + string(rune('a'+i)),
			Status:    sessions.StatusDone,
			Config:    sessions.LaunchConfig{Profile: "tetris", Planner: "heuristic"},
			Profile:   "tetris",
			CreatedAt: now.Add(-time.Duration(i+1) * time.Minute),
			UpdatedAt: now.Add(-time.Duration(i+1) * time.Minute),
			EndedAt:   now.Add(-time.Duration(i+1) * time.Minute),
		})
	}
	reader.list = append(reader.list, sessions.Snapshot{
		ID: "live1", Status: sessions.StatusRunning,
		Config: sessions.LaunchConfig{Profile: "tetris", Planner: "lookahead"},
		Profile: "tetris", CreatedAt: now, UpdatedAt: now,
	})

	h := &handler{reader: reader, now: func() time.Time { return now }}
	rr := httptest.NewRecorder()
	h.getWatch(rr, httptest.NewRequest(http.MethodGet, "/v1/watch", nil))
	var got WatchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Session == nil || got.Session.ID != "live1" {
		t.Fatalf("selected=%#v want live1", got.Session)
	}
	if len(got.Recent) != recentLimit {
		t.Fatalf("recent=%d want %d", len(got.Recent), recentLimit)
	}
}

func TestPublicRoutesAreReadOnlyAndUnknownPrivateRoutesStayAbsent(t *testing.T) {
	h := NewHandler(&fakeReader{})
	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/v1/watch", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/v1/watch", http.StatusMethodNotAllowed},
		{http.MethodPost, "/v1/sessions", http.StatusNotFound},
		{http.MethodDelete, "/v1/sessions/abc123", http.StatusNotFound},
		{http.MethodGet, "/v1/config", http.StatusNotFound},
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
		if rr.Code != tc.want {
			t.Errorf("%s %s: status=%d want=%d body=%s", tc.method, tc.path, rr.Code, tc.want, rr.Body.String())
		}
		if rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("X-Content-Type-Options") != "nosniff" || rr.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s %s missing public security headers", tc.method, tc.path)
		}
	}
}

func TestFrameServesOnlyPNGAndUsesGenericErrors(t *testing.T) {
	reader := &fakeReader{
		frames: map[string]sessions.Frame{
			"live1": {Sequence: 17, ContentType: "image/png", Data: []byte("png-bytes")},
			"html1": {Sequence: 18, ContentType: "text/html", Data: []byte("<script>bad()</script>")},
		},
		frameErrs: map[string]error{"boom1": errors.New("backend path /secret/file")},
	}
	h := NewHandler(reader)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/frame/live1", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "png-bytes" || rr.Header().Get("Content-Type") != "image/png" || rr.Header().Get("X-GamePilot-Sequence") != "17" {
		t.Fatalf("unexpected frame response: status=%d headers=%v body=%q", rr.Code, rr.Header(), rr.Body.String())
	}

	for _, id := range []string{"html1", "boom1", "missing1"} {
		rr = httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/frame/"+id, nil))
		if rr.Code != http.StatusNotFound && rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status=%d body=%s", id, rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), "/secret/file") || strings.Contains(rr.Body.String(), "backend") {
			t.Fatalf("backend error leaked: %s", rr.Body.String())
		}
	}
}

func TestNilBackendFailsClosed(t *testing.T) {
	h := NewHandler(nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/watch", nil))
	if rr.Code != http.StatusServiceUnavailable || strings.TrimSpace(rr.Body.String()) != `{"error":"spectator_unavailable"}` {
		t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
	}
}
