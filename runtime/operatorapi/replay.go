package operatorapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/maestroi/GamePilot/runtime/sessions"
)

// NewHandlerWithReplay builds the normal private operator API and adds the
// terminal replay download route used by the browser console. NewHandler keeps
// its existing route set for callers that do not want to expose artifacts.
func NewHandlerWithReplay(opts Options) (http.Handler, error) {
	api, err := NewHandler(opts)
	if err != nil {
		return nil, err
	}
	if opts.OperatorToken == "" {
		return api, nil
	}
	if opts.Manager == nil {
		return nil, fmt.Errorf("operatorapi: session manager is required")
	}

	h := &handler{manager: opts.Manager, token: opts.OperatorToken}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions/{id}/replay", h.requireAuth(h.getReplay))
	mux.Handle("/", api)
	return mux, nil
}

func (h *handler) getReplay(res http.ResponseWriter, req *http.Request) {
	replay, err := h.manager.Replay(req.PathValue("id"))
	switch {
	case err == nil:
		res.Header().Set("Content-Type", "application/json")
		res.Header().Set("Content-Disposition", `attachment; filename="gamepilot-replay.json"`)
		res.Header().Set("Cache-Control", "no-store")
		res.WriteHeader(http.StatusOK)
		_, _ = res.Write(replay)
	case errors.Is(err, sessions.ErrSessionNotFound):
		writeError(res, http.StatusNotFound, "session_not_found", "session not found")
	case errors.Is(err, sessions.ErrReplayUnavailable):
		writeError(res, http.StatusNotFound, "replay_not_found", "replay not available")
	default:
		writeError(res, http.StatusInternalServerError, "read_failed", "replay read failed")
	}
}
