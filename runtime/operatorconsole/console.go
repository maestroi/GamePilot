// Package operatorconsole serves the private browser shell used to operate
// GamePilot live sessions. It deliberately delegates every /v1 request to the
// authenticated operator API; the embedded browser assets contain no ROM paths,
// model credentials, operator token, or other deployment secrets.
package operatorconsole

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
)

//go:embed static/*
var assets embed.FS

// NewHandler combines the embedded operator console with an existing private
// operator API handler. Browser assets are served without deployment data; all
// session/config access remains authenticated by api.
func NewHandler(api http.Handler) (http.Handler, error) {
	if api == nil {
		return nil, fmt.Errorf("operatorconsole: operator API handler is required")
	}
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("operatorconsole: load embedded assets: %w", err)
	}

	files := http.FileServer(http.FS(staticFS))
	mux := http.NewServeMux()
	mux.Handle("/v1/", api)
	mux.HandleFunc("GET /", func(res http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/" {
			http.NotFound(res, req)
			return
		}
		serveAsset(res, req, files, "/index.html", "no-store")
	})
	mux.HandleFunc("GET /app.js", func(res http.ResponseWriter, req *http.Request) {
		serveAsset(res, req, files, "/app.js", "no-cache")
	})
	mux.HandleFunc("GET /styles.css", func(res http.ResponseWriter, req *http.Request) {
		serveAsset(res, req, files, "/styles.css", "no-cache")
	})

	return securityHeaders(mux), nil
}

func serveAsset(res http.ResponseWriter, req *http.Request, files http.Handler, path, cacheControl string) {
	clone := req.Clone(req.Context())
	urlCopy := *req.URL
	urlCopy.Path = path
	clone.URL = &urlCopy
	res.Header().Set("Cache-Control", cacheControl)
	files.ServeHTTP(res, clone)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob: data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		res.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		res.Header().Set("Referrer-Policy", "no-referrer")
		res.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(res, req)
	})
}
