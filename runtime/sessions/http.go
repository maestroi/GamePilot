package sessions

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// NewReadHandler exposes only the read side of a Manager. It is intentionally
// unsuitable as the eventual public spectator trust boundary: #8 will marshal a
// narrower public DTO. The private operator API can reuse these routes directly.
func NewReadHandler(manager *Manager) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions/{id}", func(res http.ResponseWriter, req *http.Request) {
		if manager == nil {
			writeReadError(res, http.StatusServiceUnavailable, "session manager unavailable")
			return
		}
		snap, err := manager.Snapshot(req.PathValue("id"))
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				writeReadError(res, http.StatusNotFound, "session not found")
				return
			}
			writeReadError(res, http.StatusInternalServerError, "session read failed")
			return
		}
		res.Header().Set("Content-Type", "application/json")
		res.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(res).Encode(snap)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/frame", func(res http.ResponseWriter, req *http.Request) {
		if manager == nil {
			writeReadError(res, http.StatusServiceUnavailable, "session manager unavailable")
			return
		}
		frame, err := manager.Frame(req.PathValue("id"))
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) || errors.Is(err, ErrFrameUnavailable) {
				http.NotFound(res, req)
				return
			}
			writeReadError(res, http.StatusInternalServerError, "frame read failed")
			return
		}
		res.Header().Set("Content-Type", frame.ContentType)
		res.Header().Set("Cache-Control", "no-store")
		res.Header().Set("X-GamePilot-Sequence", strconv.FormatUint(frame.Sequence, 10))
		res.Header().Set("X-GamePilot-Emulator-Frame", strconv.FormatUint(frame.EmulatorFrame, 10))
		res.WriteHeader(http.StatusOK)
		_, _ = res.Write(frame.Data)
	})
	return mux
}

func writeReadError(res http.ResponseWriter, status int, message string) {
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("Cache-Control", "no-store")
	res.WriteHeader(status)
	_ = json.NewEncoder(res).Encode(map[string]string{"error": message})
}
