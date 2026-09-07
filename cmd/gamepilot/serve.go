package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	openaiplanner "github.com/maestroi/GamePilot/planner/openai"
	"github.com/maestroi/GamePilot/profiles/tetris"
	"github.com/maestroi/GamePilot/runtime/operatorapi"
	"github.com/maestroi/GamePilot/runtime/sessions"
	"github.com/maestroi/GamePilot/runtime/websurfaces"
)

type serveOptions struct {
	PublicAddr    string
	PrivateAddr   string
	OperatorToken string
	ROMPath       string
	ROMAlias      string
	ROMLabel      string
	LLMBaseURL    string
	LLMModel      string
	LLMAPIKey     string
	LLMThinking   string
	LLMMaxTokens  int
	LLMTimeout    time.Duration
}

func (o serveOptions) validate() error {
	if strings.TrimSpace(o.OperatorToken) == "" {
		return errors.New("operator token is required")
	}
	if strings.TrimSpace(o.ROMPath) == "" {
		return errors.New("ROM path is required")
	}
	if o.PublicAddr != "" && o.PublicAddr == o.PrivateAddr {
		return errors.New("public and private addresses must differ")
	}
	if strings.TrimSpace(o.LLMModel) != "" && strings.TrimSpace(o.LLMBaseURL) == "" {
		return errors.New("LLM base URL is required when a model is configured")
	}
	return nil
}

func (o serveOptions) extraPlanners() map[string]sessions.TetrisPlannerFactory {
	if strings.TrimSpace(o.LLMModel) == "" {
		return nil
	}
	client := openaiplanner.NewClient(o.LLMBaseURL, o.LLMModel, o.LLMAPIKey)
	if o.LLMTimeout > 0 {
		client.HTTPClient.Timeout = o.LLMTimeout
	}
	client.MaxTokens = o.LLMMaxTokens
	switch o.LLMThinking {
	case "off":
		value := false
		client.Thinking = &value
	case "on":
		value := true
		client.Thinking = &value
	default:
		client.Thinking = nil
	}
	return map[string]sessions.TetrisPlannerFactory{
		"llm": sessions.LLMPlannerFactory(func(sessions.LaunchConfig) (tetris.JSONCompleter, error) {
			return client, nil
		}),
	}
}

func (o serveOptions) operatorAPI(manager *sessions.Manager) operatorapi.Options {
	alias := strings.TrimSpace(o.ROMAlias)
	if alias == "" {
		alias = "tetris-rev1"
	}
	label := strings.TrimSpace(o.ROMLabel)
	if label == "" {
		label = "Tetris Rev 1"
	}
	planners := []operatorapi.PlannerOption{
		{ID: "heuristic"},
		{ID: "lookahead"},
	}
	var models []operatorapi.ModelOption
	if model := strings.TrimSpace(o.LLMModel); model != "" {
		planners = append(planners, operatorapi.PlannerOption{ID: "llm", RequiresModel: true})
		models = []operatorapi.ModelOption{{Alias: model, Label: model}}
	}
	return operatorapi.Options{
		Manager:       manager,
		OperatorToken: o.OperatorToken,
		ROMs: []operatorapi.ROMOption{{
			Alias:   alias,
			Label:   label,
			Profile: tetris.ProfileID,
			Path:    o.ROMPath,
		}},
		Profiles: []operatorapi.ProfileOption{{
			ID:       tetris.ProfileID,
			Planners: planners,
		}},
		Models: models,
	}
}

func (o serveOptions) servers(manager *sessions.Manager) (websurfaces.Servers, error) {
	if err := o.validate(); err != nil {
		return websurfaces.Servers{}, err
	}
	return websurfaces.NewServers(websurfaces.Options{
		Manager:     manager,
		Operator:    o.operatorAPI(manager),
		PublicAddr:  o.PublicAddr,
		PrivateAddr: o.PrivateAddr,
	})
}

func runServe(opts serveOptions) error {
	manager := sessions.NewTetrisManager(opts.extraPlanners())
	defer func() { _ = manager.Close(context.Background()) }()

	servers, err := opts.servers(manager)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 2)
	go func() { errc <- listen("public", servers.Public) }()
	go func() { errc <- listen("private", servers.Private) }()

	select {
	case err := <-errc:
		shutdownServers(servers)
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownServers(servers)
		return nil
	}
}

func listen(name string, server *http.Server) error {
	err := server.ListenAndServe()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("%s listener: %w", name, err)
}

func shutdownServers(servers websurfaces.Servers) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = servers.Public.Shutdown(ctx)
	_ = servers.Private.Shutdown(ctx)
}
