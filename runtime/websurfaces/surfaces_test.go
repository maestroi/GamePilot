package websurfaces

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maestroi/GamePilot/runtime/operatorapi"
	"github.com/maestroi/GamePilot/runtime/sessions"
)

func TestNewServersSeparatesPublicAndPrivateTrustSurfaces(t *testing.T) {
	manager := sessions.NewManager(nil)
	servers, err := NewServers(Options{
		Manager: manager,
		Operator: operatorapi.Options{
			OperatorToken: "private-token",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if servers.Public.Addr != defaultPublicAddr || servers.Private.Addr != defaultPrivateAddr {
		t.Fatalf("unexpected addresses public=%q private=%q", servers.Public.Addr, servers.Private.Addr)
	}
	if servers.Public.Addr == servers.Private.Addr {
		t.Fatal("public and private servers share an address")
	}

	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/v1/config", http.StatusNotFound},
		{http.MethodGet, "/v1/sessions", http.StatusNotFound},
		{http.MethodPost, "/v1/sessions", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/v1/sessions/abc", http.StatusMethodNotAllowed},
	} {
		rr := httptest.NewRecorder()
		servers.Public.Handler.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
		if rr.Code != tc.want {
			t.Errorf("public %s %s status=%d want=%d body=%s", tc.method, tc.path, rr.Code, tc.want, rr.Body.String())
		}
	}

	rr := httptest.NewRecorder()
	servers.Private.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("private API without token status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	req.Header.Set("Authorization", "Bearer private-token")
	servers.Private.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("private API with token status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestServersHaveLimitsAndNonSensitiveHealth(t *testing.T) {
	servers, err := NewServers(Options{
		Manager: sessions.NewManager(nil),
		Operator: operatorapi.Options{OperatorToken: "private-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, server := range map[string]*http.Server{"public": servers.Public, "private": servers.Private} {
		if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 || server.MaxHeaderBytes <= 0 {
			t.Errorf("%s server is missing HTTP limits: %#v", name, server)
		}
		for _, path := range []string{"/healthz", "/readyz"} {
			rr := httptest.NewRecorder()
			server.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("%s GET %s status=%d body=%s", name, path, rr.Code, rr.Body.String())
			}
			body := strings.ToLower(rr.Body.String())
			for _, forbidden := range []string{"private-token", "rom", "model", "path", "config"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("%s GET %s leaked %q in %q", name, path, forbidden, rr.Body.String())
				}
			}
		}
	}
}

func TestNewServersRejectsUnsafeComposition(t *testing.T) {
	manager := sessions.NewManager(nil)
	if _, err := NewServers(Options{Manager: manager}); err == nil {
		t.Fatal("expected missing operator token to fail")
	}
	if _, err := NewServers(Options{
		Manager: manager,
		Operator: operatorapi.Options{OperatorToken: "private-token"},
		PublicAddr: "127.0.0.1:8080",
		PrivateAddr: "127.0.0.1:8080",
	}); err == nil {
		t.Fatal("expected identical public/private addresses to fail")
	}
}
