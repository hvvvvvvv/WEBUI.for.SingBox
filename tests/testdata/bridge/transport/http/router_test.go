package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/storage"
)

func TestRouterEndpointsRequireAuthentication(t *testing.T) {
	authService := auth.NewService(storage.NewPaths(t.TempDir()))
	if err := authService.SetSecret("router-test-secret"); err != nil {
		t.Fatal(err)
	}
	authService.AddSession("router-session")
	server := &Server{options: Options{Auth: authService}}
	mux := http.NewServeMux()
	server.registerRouterRoutes(mux)
	handler := server.buildRootHandler(http.NotFoundHandler(), mux)
	for _, path := range []string{"/api/router/plugins", "/api/router/diagnostics"} {
		for _, token := range []string{"", "router-session"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			want := http.StatusOK
			if token == "" {
				want = http.StatusUnauthorized
			}
			if w.Code != want {
				t.Fatalf("%s token=%q: %d %s", path, token, w.Code, w.Body.String())
			}
			if token != "" && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private diagnostics may be cached")
			}
		}
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer router-session")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s allows unexpected method", path)
		}
	}
}
