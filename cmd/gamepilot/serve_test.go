package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maestroi/GamePilot/profiles/tetris"
	"github.com/maestroi/GamePilot/runtime/operatorapi"
	"github.com/maestroi/GamePilot/runtime/sessions"
)

func TestServeOptionsRejectMissingTokenAndROM(t *testing.T) {
	opts := serveOptions{
		PublicAddr:    ":8080",
		PrivateAddr:   ":8081",
		OperatorToken: "",
		ROMPath:       "/roms/tetris.gb",
	}
	if err := opts.validate(); err == nil {
		t.Fatal("expected missing operator token to fail")
	}

	opts.OperatorToken = "secret"
	opts.ROMPath = ""
	if err := opts.validate(); err == nil {
		t.Fatal("expected missing ROM path to fail")
	}
}

func TestServeOptionsRejectIdenticalAddresses(t *testing.T) {
	opts := serveOptions{
		PublicAddr:    ":8080",
		PrivateAddr:   ":8080",
		OperatorToken: "secret",
		ROMPath:       "/roms/tetris.gb",
	}
	if err := opts.validate(); err == nil {
		t.Fatal("expected identical public and private addresses to fail")
	}
}

func TestServeOptionsRejectLLMModelWithoutBaseURL(t *testing.T) {
	opts := validServeOptions()
	opts.LLMModel = "qwen-local"
	if err := opts.validate(); err == nil {
		t.Fatal("expected missing LLM base URL to fail")
	}
}

func TestServeCatalogOmitsLLMUntilModelConfigured(t *testing.T) {
	opts := validServeOptions()
	api := opts.operatorAPI(sessions.NewTetrisManager(nil))
	if got := plannerIDs(api); len(got) != 2 || got[0] != "heuristic" || got[1] != "lookahead" {
		t.Fatalf("deterministic catalog=%v", got)
	}
	if len(api.Models) != 0 {
		t.Fatalf("unexpected models=%v", api.Models)
	}
	if _, ok := opts.extraPlanners()["llm"]; ok {
		t.Fatal("llm planner should stay unregistered without a model")
	}

	opts.LLMModel = "qwen-local"
	opts.LLMBaseURL = "http://llm:8002/v1"
	api = opts.operatorAPI(sessions.NewTetrisManager(nil))
	if got := plannerIDs(api); len(got) != 3 || got[2] != "llm" {
		t.Fatalf("llm catalog=%v", got)
	}
	if len(api.Models) != 1 || api.Models[0].Alias != "qwen-local" {
		t.Fatalf("models=%v", api.Models)
	}
	if _, ok := opts.extraPlanners()["llm"]; !ok {
		t.Fatal("llm planner factory missing")
	}
}

func TestServeServersKeepPublicAndPrivateTrustSurfaces(t *testing.T) {
	opts := validServeOptions()
	manager := sessions.NewTetrisManager(opts.extraPlanners())
	servers, err := opts.servers(manager)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	servers.Public.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("public /v1/config status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	servers.Private.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("private /healthz status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+opts.OperatorToken)
	servers.Private.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("private /v1/config status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, needle := range []string{tetris.ProfileID, "tetris-rev1", "lookahead"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("private catalog missing %q: %s", needle, body)
		}
	}
}

func validServeOptions() serveOptions {
	return serveOptions{
		PublicAddr:    ":8080",
		PrivateAddr:   ":8081",
		OperatorToken: "secret",
		ROMPath:       "/roms/tetris.gb",
		ROMAlias:      "tetris-rev1",
	}
}

func plannerIDs(api operatorapi.Options) []string {
	for _, profile := range api.Profiles {
		if profile.ID == tetris.ProfileID {
			ids := make([]string, 0, len(profile.Planners))
			for _, planner := range profile.Planners {
				ids = append(ids, planner.ID)
			}
			return ids
		}
	}
	return nil
}
