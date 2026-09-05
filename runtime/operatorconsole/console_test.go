package operatorconsole

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleServesEmbeddedShellWithoutSecrets(t *testing.T) {
	api := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		http.Error(res, "unexpected API call", http.StatusInternalServerError)
	})
	h, err := NewHandler(api)
	if err != nil {
		t.Fatal(err)
	}

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("root status = %d body=%s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, forbidden := range []string{"super-secret-token", "/srv/private/roms", "api.openai.com"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("console shell contains deployment secret %q", forbidden)
		}
	}
	if !strings.Contains(body, `/app.js`) || !strings.Contains(body, `/styles.css`) {
		t.Fatalf("console shell missing embedded assets: %s", body)
	}
	if got := res.Header().Get("Content-Security-Policy"); !strings.Contains(got, "connect-src 'self'") {
		t.Fatalf("CSP = %q", got)
	}
	if got := res.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("root Cache-Control = %q", got)
	}
}

func TestConsoleAssetsAndUnknownRoutes(t *testing.T) {
	h, err := NewHandler(http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path        string
		contentType string
	}{
		{"/app.js", "text/javascript"},
		{"/styles.css", "text/css"},
	} {
		res := httptest.NewRecorder()
		h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if res.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", tc.path, res.Code, res.Body.String())
		}
		if got := res.Header().Get("Content-Type"); !strings.Contains(got, tc.contentType) {
			t.Fatalf("%s Content-Type = %q", tc.path, got)
		}
	}

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/not-a-route", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404", res.Code)
	}
}

func TestConsoleDelegatesV1RequestsToOperatorAPI(t *testing.T) {
	api := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/config" {
			t.Fatalf("delegated path = %q", req.URL.Path)
		}
		if req.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization header was not preserved")
		}
		res.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(res, `{"ok":true}`)
	})
	h, err := NewHandler(api)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	req.Header.Set("Authorization", "Bearer token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != `{"ok":true}` {
		t.Fatalf("delegated response = %d %s", res.Code, res.Body.String())
	}
}

func TestConsoleRequiresAPIHandler(t *testing.T) {
	if _, err := NewHandler(nil); err == nil {
		t.Fatal("NewHandler(nil) succeeded")
	}
}
