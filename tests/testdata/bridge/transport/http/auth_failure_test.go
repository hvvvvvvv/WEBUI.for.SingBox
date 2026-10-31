package httptransport

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/storage"
)

func TestSecretReadFailureCannotIssueAnAuthenticatedSession(t *testing.T) {
	for _, damaged := range []string{"directory", "whitespace", "invalid hash"} {
		t.Run(damaged, func(t *testing.T) {
			paths := storage.NewPaths(t.TempDir())
			authService := auth.NewService(paths)
			if err := authService.SetSecret("correct"); err != nil {
				t.Fatal(err)
			}
			path := paths.Resolve("data/secret.key")
			if damaged == "directory" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				content := []byte("not a valid hash")
				if damaged == "whitespace" {
					content = []byte("\r\n")
				}
				if err := os.WriteFile(path, content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			mux := http.NewServeMux()
			registerAPIRoutes(mux, authService)
			server := &Server{options: Options{Auth: authService}}
			handler := server.buildRootHandler(http.NotFoundHandler(), mux)
			for _, secret := range []string{"", "wrong", "correct"} {
				result := requestLogin(t, handler, "192.0.2.20:1234", secret)
				if result.Flag {
					t.Fatalf("unreadable/corrupt key issued a session: %#v", result)
				}
			}
			request := httptest.NewRequest(http.MethodPost, "/api/auth/session", nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated session check returned %d", recorder.Code)
			}
		})
	}
}
