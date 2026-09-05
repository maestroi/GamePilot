package spectator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maestroi/GamePilot/runtime/sessions"
)

type readerStub struct{}

func (readerStub) List() []sessions.Snapshot { return nil }
func (readerStub) Snapshot(string) (sessions.Snapshot, error) { return sessions.Snapshot{}, sessions.ErrSessionNotFound }
func (readerStub) Frame(string) (sessions.Frame, error) { return sessions.Frame{}, sessions.ErrFrameUnavailable }

func TestSpectatorServesEmbeddedShellWithoutOperatorSecrets(t *testing.T) {
	h, err := NewHandler(readerStub{})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path string
		ct   string
	}{
		{"/", "text/html"},
		{"/app.js", "text/javascript"},
		{"/styles.css", "text/css"},
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", tc.path, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Header().Get("Content-Type"), tc.ct) {
			t.Errorf("GET %s content-type=%q", tc.path, rr.Header().Get("Content-Type"))
		}
		if rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("Referrer-Policy") != "no-referrer" || rr.Header().Get("X-Content-Type-Options") != "nosniff" || rr.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("GET %s missing security headers", tc.path)
		}
		body := rr.Body.String()
		for _, forbidden := range []string{"Authorization", "Bearer ", "operator.token", "sessionStorage", "localStorage", "OPENAI_API_KEY", "/v1/config", "POST /v1/sessions"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("GET %s contains private/operator marker %q", tc.path, forbidden)
			}
		}
	}
}

func TestSpectatorDoesNotDelegatePrivateRoutes(t *testing.T) {
	h, err := NewHandler(readerStub{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/v1/sessions", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/v1/sessions/abc", http.StatusMethodNotAllowed},
		{http.MethodGet, "/v1/config", http.StatusNotFound},
		{http.MethodGet, "/v1/sessions", http.StatusNotFound},
		{http.MethodPost, "/v1/watch", http.StatusMethodNotAllowed},
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
		if rr.Code != tc.want {
			t.Errorf("%s %s status=%d want=%d body=%s", tc.method, tc.path, rr.Code, tc.want, rr.Body.String())
		}
	}
}

func TestSpectatorHealthAndReadinessRevealNoConfig(t *testing.T) {
	h, err := NewHandler(readerStub{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rr.Code, rr.Body.String())
		}
		body := strings.ToLower(rr.Body.String())
		if strings.Contains(body, "rom") || strings.Contains(body, "token") || strings.Contains(body, "model") || strings.Contains(body, "path") {
			t.Fatalf("GET %s leaked config-shaped data: %q", path, rr.Body.String())
		}
	}

	unavailable, err := NewHandler(nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	unavailable.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable || strings.TrimSpace(rr.Body.String()) != "unavailable" {
		t.Fatalf("unavailable readiness status=%d body=%q", rr.Code, rr.Body.String())
	}
}
