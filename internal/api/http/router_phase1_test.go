package apihttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/touken928/wirehub/internal/config"
	"github.com/touken928/wirehub/internal/repo"
	"github.com/touken928/wirehub/internal/service"
)

func TestRoutesCharacterization_PublicStatusAndBodyMatrix(t *testing.T) {
	srv := phase1HTTPServer(t)
	tests := []struct {
		name, method, target, body string
		code                       int
	}{
		{name: "setup status", method: http.MethodGet, target: "/api/setup/status", code: http.StatusForbidden, body: `{"error":"setup token required; check server logs for the first-run setup token"}`},
		{name: "setup", method: http.MethodPost, target: "/api/setup", code: http.StatusForbidden, body: `{"error":"setup token required; check server logs for the first-run setup token"}`},
		{name: "import", method: http.MethodPost, target: "/api/setup/import", code: http.StatusForbidden, body: `{"error":"setup token required; check server logs for the first-run setup token"}`},
		{name: "login before setup", method: http.MethodPost, target: "/api/auth/login", code: http.StatusServiceUnavailable, body: `{"error":"setup required"}`},
		{name: "status websocket missing token", method: http.MethodGet, target: "/api/ws/status", code: http.StatusUnauthorized, body: `{"error":"missing token"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := gin.New()
			RegisterRoutes(r, srv)
			r.ServeHTTP(w, httptest.NewRequest(tt.method, tt.target, nil))
			if w.Code != tt.code || w.Body.String() != tt.body {
				t.Fatalf("got %d %q, want %d %q", w.Code, w.Body.String(), tt.code, tt.body)
			}
		})
	}
}

func TestRoutesCharacterization_ProtectedMissingAuthMatrix(t *testing.T) {
	srv := phase1HTTPServer(t)
	paths := []struct{ method, path string }{
		{http.MethodPost, "/api/admin/reset"}, {http.MethodGet, "/api/settings"}, {http.MethodPut, "/api/settings"}, {http.MethodPut, "/api/settings/password"}, {http.MethodGet, "/api/settings/export"},
		{http.MethodGet, "/api/groups"}, {http.MethodPost, "/api/groups"}, {http.MethodPut, "/api/groups/1"}, {http.MethodDelete, "/api/groups/1"}, {http.MethodGet, "/api/groups/graph"}, {http.MethodPost, "/api/groups/links"}, {http.MethodDelete, "/api/groups/links"}, {http.MethodPut, "/api/groups/layout"},
		{http.MethodGet, "/api/forwards"}, {http.MethodPost, "/api/forwards"}, {http.MethodPut, "/api/forwards/1"}, {http.MethodDelete, "/api/forwards/1"},
		{http.MethodGet, "/api/maps"}, {http.MethodPost, "/api/maps"}, {http.MethodPut, "/api/maps/1"}, {http.MethodDelete, "/api/maps/1"},
		{http.MethodGet, "/api/peers"}, {http.MethodPost, "/api/peers"}, {http.MethodPut, "/api/peers/1"}, {http.MethodDelete, "/api/peers/1"}, {http.MethodPost, "/api/peers/1/toggle"}, {http.MethodGet, "/api/peers/1/config"},
	}
	for _, route := range paths {
		route := route
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := gin.New()
			RegisterRoutes(r, srv)
			r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
			if w.Code != http.StatusUnauthorized || w.Body.String() != `{"error":"missing authorization"}` {
				t.Fatalf("got %d %q, want 401 missing authorization", w.Code, w.Body.String())
			}
		})
	}
}

func phase1HTTPServer(t *testing.T) *Server {
	t.Helper()
	st, err := repo.New(&config.RuntimeConfig{DatabasePath: filepath.Join(t.TempDir(), "wirehub.db")})
	if err != nil {
		t.Fatal(err)
	}
	return New(service.NewApp(st, func() (string, string, error) { return "private", "public", nil }), "test-secret", "test-setup-token", "localhost:8080")
}
