// Package spectator serves the embedded public GamePilot watch UI together with
// the deliberately narrow read-only spectator API.
package spectator

import (
	"embed"
	"fmt"
	"net/http"
	"strings"

	"github.com/maestroi/GamePilot/runtime/spectatorapi"
)

//go:embed static/*
var staticFS embed.FS

type handler struct {
	api       http.Handler
	available bool
	index     []byte
	app       []byte
	styles    []byte
}

// NewHandler builds a self-contained public spectator surface. It constructs
// the public API internally instead of accepting an arbitrary handler, which
// prevents accidentally mounting the private operator API behind public assets.
func NewHandler(reader spectatorapi.SessionReader) (http.Handler, error) {
	index, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		return nil, fmt.Errorf("spectator: read index: %w", err)
	}
	app, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		return nil, fmt.Errorf("spectator: read app: %w", err)
	}
	styles, err := staticFS.ReadFile("static/styles.css")
	if err != nil {
		return nil, fmt.Errorf("spectator: read styles: %w", err)
	}
	return securityHeaders(&handler{
		api:       spectatorapi.NewHandler(reader),
		available: reader != nil,
		index:     index,
		app:       app,
		styles:    styles,
	}), nil
}

func (h *handler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	if path == "/v1/watch" || strings.HasPrefix(path, "/v1/frame/") {
		h.api.ServeHTTP(res, req)
		return
	}
	if path == "/healthz" {
		if req.Method != http.MethodGet {
			methodNotAllowed(res)
			return
		}
		writeText(res, http.StatusOK, "ok\n")
		return
	}
	if path == "/readyz" {
		if req.Method != http.MethodGet {
			methodNotAllowed(res)
			return
		}
		if !h.available {
			writeText(res, http.StatusServiceUnavailable, "unavailable\n")
			return
		}
		writeText(res, http.StatusOK, "ready\n")
		return
	}
	if req.Method != http.MethodGet {
		methodNotAllowed(res)
		return
	}
	switch path {
	case "/":
		serveAsset(res, "text/html; charset=utf-8", h.index)
	case "/app.js":
		serveAsset(res, "text/javascript; charset=utf-8", h.app)
	case "/styles.css":
		serveAsset(res, "text/css; charset=utf-8", h.styles)
	default:
		http.NotFound(res, req)
	}
}

func serveAsset(res http.ResponseWriter, contentType string, data []byte) {
	res.Header().Set("Content-Type", contentType)
	res.WriteHeader(http.StatusOK)
	_, _ = res.Write(data)
}

func writeText(res http.ResponseWriter, status int, text string) {
	res.Header().Set("Content-Type", "text/plain; charset=utf-8")
	res.WriteHeader(status)
	_, _ = res.Write([]byte(text))
}

func methodNotAllowed(res http.ResponseWriter) {
	res.Header().Set("Allow", http.MethodGet)
	http.Error(res, "method not allowed", http.StatusMethodNotAllowed)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Cache-Control", "no-store")
		res.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob: data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		res.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		res.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		res.Header().Set("Referrer-Policy", "no-referrer")
		res.Header().Set("X-Content-Type-Options", "nosniff")
		res.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(res, req)
	})
}
