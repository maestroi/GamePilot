// Package websurfaces composes GamePilot's public spectator and private operator
// into separate HTTP servers. The two trust surfaces receive different handlers
// and can bind different addresses/ports; the public handler never receives the
// operator API.
package websurfaces

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/maestroi/GamePilot/runtime/operatorapi"
	"github.com/maestroi/GamePilot/runtime/operatorconsole"
	"github.com/maestroi/GamePilot/runtime/sessions"
	"github.com/maestroi/GamePilot/runtime/spectator"
)

const (
	defaultPublicAddr       = ":8080"
	defaultPrivateAddr      = "127.0.0.1:8081"
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 15 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultMaxHeaderBytes    = 1 << 20
)

// Options configures the separated public/private web deployment shape.
type Options struct {
	Manager  *sessions.Manager
	Operator operatorapi.Options

	PublicAddr  string
	PrivateAddr string

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
}

// Servers contains two independently bindable HTTP servers. Public is safe to
// place behind an internet-facing proxy; Private still requires network policy
// in addition to its Bearer token.
type Servers struct {
	Public  *http.Server
	Private *http.Server
}

func NewServers(opts Options) (Servers, error) {
	if opts.Manager == nil {
		return Servers{}, fmt.Errorf("websurfaces: session manager is required")
	}
	if strings.TrimSpace(opts.Operator.OperatorToken) == "" {
		return Servers{}, fmt.Errorf("websurfaces: operator token is required for the private surface")
	}
	applyDefaults(&opts)
	if opts.PublicAddr == opts.PrivateAddr {
		return Servers{}, fmt.Errorf("websurfaces: public and private addresses must differ")
	}

	public, err := spectator.NewHandler(opts.Manager)
	if err != nil {
		return Servers{}, err
	}

	operatorOpts := opts.Operator
	operatorOpts.Manager = opts.Manager
	api, err := operatorapi.NewHandlerWithReplay(operatorOpts)
	if err != nil {
		return Servers{}, err
	}
	private, err := operatorconsole.NewHandler(api)
	if err != nil {
		return Servers{}, err
	}
	private = privateHealth(private)

	return Servers{
		Public:  newServer(opts.PublicAddr, public, opts),
		Private: newServer(opts.PrivateAddr, private, opts),
	}, nil
}

func applyDefaults(opts *Options) {
	if strings.TrimSpace(opts.PublicAddr) == "" {
		opts.PublicAddr = defaultPublicAddr
	}
	if strings.TrimSpace(opts.PrivateAddr) == "" {
		opts.PrivateAddr = defaultPrivateAddr
	}
	if opts.ReadHeaderTimeout <= 0 {
		opts.ReadHeaderTimeout = defaultReadHeaderTimeout
	}
	if opts.ReadTimeout <= 0 {
		opts.ReadTimeout = defaultReadTimeout
	}
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = defaultWriteTimeout
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = defaultIdleTimeout
	}
	if opts.MaxHeaderBytes <= 0 {
		opts.MaxHeaderBytes = defaultMaxHeaderBytes
	}
}

func newServer(addr string, handler http.Handler, opts Options) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
		ReadTimeout:       opts.ReadTimeout,
		WriteTimeout:      opts.WriteTimeout,
		IdleTimeout:       opts.IdleTimeout,
		MaxHeaderBytes:    opts.MaxHeaderBytes,
	}
}

func privateHealth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Cache-Control", "no-store")
		res.Header().Set("Referrer-Policy", "no-referrer")
		res.Header().Set("X-Content-Type-Options", "nosniff")
		res.Header().Set("X-Frame-Options", "DENY")
		switch req.URL.Path {
		case "/healthz":
			if req.Method != http.MethodGet {
				res.Header().Set("Allow", http.MethodGet)
				http.Error(res, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			res.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = res.Write([]byte("ok\n"))
			return
		case "/readyz":
			if req.Method != http.MethodGet {
				res.Header().Set("Allow", http.MethodGet)
				http.Error(res, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			res.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = res.Write([]byte("ready\n"))
			return
		default:
			next.ServeHTTP(res, req)
		}
	})
}
