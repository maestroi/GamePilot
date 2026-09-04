package operatorapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maestroi/GamePilot/runtime/sessions"
)

type testRunnerFunc func(context.Context, func(sessions.Update)) (sessions.Result, error)

func (f testRunnerFunc) Run(ctx context.Context, publish func(sessions.Update)) (sessions.Result, error) {
	return f(ctx, publish)
}

func TestAuthDisabledMountsNoOperatorRoutes(t *testing.T) {
	h, err := NewHandler(Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/config"},
		{http.MethodGet, "/v1/sessions"},
		{http.MethodPost, "/v1/sessions"},
	} {
		res := httptest.NewRecorder()
		h.ServeHTTP(res, httptest.NewRequest(tc.method, tc.path, nil))
		if res.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404", tc.method, tc.path, res.Code)
		}
	}
}

func TestOperatorRoutesRequireBearerAndConfigRedactsSecrets(t *testing.T) {
	manager := sessions.NewManager(sessions.RunnerFactoryFunc(func(sessions.LaunchConfig) (sessions.Runner, error) {
		return testRunnerFunc(func(context.Context, func(sessions.Update)) (sessions.Result, error) {
			return sessions.Result{Reason: "completed"}, nil
		}), nil
	}))
	h := mustHandler(t, Options{
		Manager:       manager,
		OperatorToken: "super-secret-token",
		ROMs: []ROMOption{{Alias: "tetris-rev1", Label: "Tetris Rev 1", Profile: "tetris", Path: "/srv/private/roms/tetris.gb"}},
		Profiles: []ProfileOption{{ID: "tetris", Planners: []PlannerOption{{ID: "lookahead"}, {ID: "llm", RequiresModel: true}}}},
		Models: []ModelOption{{Alias: "qwen-local", Label: "Qwen local"}},
	})

	unauth := httptest.NewRecorder()
	h.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauth.Code)
	}
	if got := unauth.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q", got)
	}

	wrong := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(wrong, req)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d, want 401", wrong.Code)
	}

	res := doAuthed(h, http.MethodGet, "/v1/config", "super-secret-token", "")
	if res.Code != http.StatusOK {
		t.Fatalf("config status = %d body=%s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, secret := range []string{"/srv/private/roms/tetris.gb", "super-secret-token"} {
		if strings.Contains(body, secret) {
			t.Fatalf("config leaked %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, `"alias":"tetris-rev1"`) || !strings.Contains(body, `"alias":"qwen-local"`) {
		t.Fatalf("config missing safe aliases: %s", body)
	}
}

func TestCreateResolvesAliasAndDefaultsRealtime(t *testing.T) {
	configured := make(chan sessions.LaunchConfig, 1)
	manager := sessions.NewManager(sessions.RunnerFactoryFunc(func(config sessions.LaunchConfig) (sessions.Runner, error) {
		configured <- config
		return testRunnerFunc(func(ctx context.Context, publish func(sessions.Update)) (sessions.Result, error) {
			publish(sessions.Update{Profile: "tetris", Frame: 100, PlannerActivity: "planning"})
			<-ctx.Done()
			return sessions.Result{Reason: "stopped"}, ctx.Err()
		}), nil
	}))
	h := standardHandler(t, manager, 60)

	res := doAuthed(h, http.MethodPost, "/v1/sessions", "token", `{
		"rom_alias":"tetris-rev1",
		"profile":"tetris",
		"planner":"lookahead",
		"move_limit":25,
		"record_replay":true
	}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", res.Code, res.Body.String())
	}
	var snap sessions.Snapshot
	if err := json.Unmarshal(res.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.ID == "" || res.Header().Get("Location") != "/v1/sessions/"+snap.ID {
		t.Fatalf("create response id/location = %q / %q", snap.ID, res.Header().Get("Location"))
	}

	select {
	case cfg := <-configured:
		if cfg.ROMPath != "/private/tetris.gb" {
			t.Fatalf("resolved ROM path = %q", cfg.ROMPath)
		}
		if cfg.Pacing != sessions.PacingRealtime {
			t.Fatalf("pacing = %q, want realtime", cfg.Pacing)
		}
		if cfg.Profile != "tetris" || cfg.Planner != "lookahead" || cfg.MoveLimit != 25 || !cfg.RecordReplay {
			t.Fatalf("resolved config = %+v", cfg)
		}
	case <-time.After(time.Second):
		t.Fatal("runner factory did not receive launch config")
	}

	encoded := res.Body.String()
	if strings.Contains(encoded, "/private/tetris.gb") {
		t.Fatalf("create response leaked ROM path: %s", encoded)
	}
	if err := manager.Stop(snap.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := manager.Wait(ctx, snap.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRejectsUnallowlistedAndInvalidConfig(t *testing.T) {
	manager := sessions.NewManager(sessions.RunnerFactoryFunc(func(sessions.LaunchConfig) (sessions.Runner, error) {
		t.Fatal("runner factory must not be reached for invalid API config")
		return nil, errors.New("unreachable")
	}))
	h := standardHandler(t, manager, 60)

	cases := []struct {
		name string
		body string
		code string
	}{
		{"unknown ROM", `{"rom_alias":"../../secret.gb","profile":"tetris","planner":"lookahead"}`, "invalid_rom_alias"},
		{"profile mismatch", `{"rom_alias":"tetris-rev1","profile":"other","planner":"lookahead"}`, "invalid_profile"},
		{"unknown planner", `{"rom_alias":"tetris-rev1","profile":"tetris","planner":"shell"}`, "invalid_planner"},
		{"model on deterministic", `{"rom_alias":"tetris-rev1","profile":"tetris","planner":"lookahead","model":"qwen-local"}`, "invalid_planner_options"},
		{"unknown model", `{"rom_alias":"tetris-rev1","profile":"tetris","planner":"llm","model":"not-configured"}`, "invalid_model"},
		{"too many moves", `{"rom_alias":"tetris-rev1","profile":"tetris","planner":"lookahead","move_limit":100001}`, "invalid_move_limit"},
		{"raw path field", `{"rom_alias":"tetris-rev1","profile":"tetris","planner":"lookahead","rom_path":"/tmp/x.gb"}`, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := doAuthed(h, http.MethodPost, "/v1/sessions", "token", tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("body = %s, want code %s", res.Body.String(), tc.code)
			}
		})
	}
}

func TestCreateStopDeleteLifecycle(t *testing.T) {
	started := make(chan struct{}, 1)
	manager := sessions.NewManager(sessions.RunnerFactoryFunc(func(sessions.LaunchConfig) (sessions.Runner, error) {
		return testRunnerFunc(func(ctx context.Context, publish func(sessions.Update)) (sessions.Result, error) {
			publish(sessions.Update{Profile: "tetris", Frame: 10})
			started <- struct{}{}
			<-ctx.Done()
			return sessions.Result{Reason: "stopped", Replay: []byte("replay")}, ctx.Err()
		}), nil
	}))
	h := standardHandler(t, manager, 60)

	create := doAuthed(h, http.MethodPost, "/v1/sessions", "token", `{"rom_alias":"tetris-rev1","profile":"tetris","planner":"lookahead"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var snap sessions.Snapshot
	if err := json.Unmarshal(create.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("session did not start")
	}

	activeDelete := doAuthed(h, http.MethodDelete, "/v1/sessions/"+snap.ID, "token", "")
	if activeDelete.Code != http.StatusConflict || !strings.Contains(activeDelete.Body.String(), "session_not_terminal") {
		t.Fatalf("active delete = %d %s", activeDelete.Code, activeDelete.Body.String())
	}

	stop := doAuthed(h, http.MethodPost, "/v1/sessions/"+snap.ID+"/stop", "token", "")
	if stop.Code != http.StatusOK {
		t.Fatalf("stop status = %d body=%s", stop.Code, stop.Body.String())
	}
	// Stop is idempotent, including after the runner reaches terminal state.
	stopAgain := doAuthed(h, http.MethodPost, "/v1/sessions/"+snap.ID+"/stop", "token", "")
	if stopAgain.Code != http.StatusOK {
		t.Fatalf("second stop status = %d body=%s", stopAgain.Code, stopAgain.Body.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	terminal, err := manager.Wait(ctx, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Status != sessions.StatusDone {
		t.Fatalf("terminal status = %s", terminal.Status)
	}

	deleteRes := doAuthed(h, http.MethodDelete, "/v1/sessions/"+snap.ID, "token", "")
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", deleteRes.Code, deleteRes.Body.String())
	}
	get := doAuthed(h, http.MethodGet, "/v1/sessions/"+snap.ID, "token", "")
	if get.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d body=%s", get.Code, get.Body.String())
	}
}

func TestMutationRateAndBodyLimits(t *testing.T) {
	manager := sessions.NewManager(sessions.RunnerFactoryFunc(func(sessions.LaunchConfig) (sessions.Runner, error) {
		return testRunnerFunc(func(context.Context, func(sessions.Update)) (sessions.Result, error) {
			return sessions.Result{}, nil
		}), nil
	}))
	h := mustHandler(t, Options{
		Manager:       manager,
		OperatorToken: "token",
		ROMs:          []ROMOption{{Alias: "tetris-rev1", Profile: "tetris", Path: "/private/tetris.gb"}},
		Profiles:      []ProfileOption{{ID: "tetris", Planners: []PlannerOption{{ID: "lookahead"}}}},
		MutationLimit: 1,
		MutationWindow: time.Minute,
		MaxBodyBytes:  64,
	})

	first := doAuthed(h, http.MethodPost, "/v1/sessions", "token", strings.Repeat("x", 100))
	if first.Code != http.StatusBadRequest {
		t.Fatalf("oversize body status = %d body=%s", first.Code, first.Body.String())
	}
	second := doAuthed(h, http.MethodPost, "/v1/sessions/missing/stop", "token", "")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited status = %d body=%s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("rate-limited response missing Retry-After")
	}
}

func standardHandler(t *testing.T, manager *sessions.Manager, mutationLimit int) http.Handler {
	t.Helper()
	return mustHandler(t, Options{
		Manager:       manager,
		OperatorToken: "token",
		ROMs:          []ROMOption{{Alias: "tetris-rev1", Label: "Tetris Rev 1", Profile: "tetris", Path: "/private/tetris.gb"}},
		Profiles: []ProfileOption{{ID: "tetris", Planners: []PlannerOption{
			{ID: "heuristic"},
			{ID: "lookahead"},
			{ID: "llm", RequiresModel: true},
		}}},
		Models:        []ModelOption{{Alias: "qwen-local"}},
		MutationLimit: mutationLimit,
	})
}

func mustHandler(t *testing.T, opts Options) http.Handler {
	t.Helper()
	h, err := NewHandler(opts)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func doAuthed(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	reader = strings.NewReader(body)
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res
}
