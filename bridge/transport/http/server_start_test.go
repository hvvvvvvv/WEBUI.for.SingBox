package httptransport

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/storage"
)

func TestAppStartTimeRouteIsPublic(t *testing.T) {
	authService := auth.NewService(storage.NewPaths(t.TempDir()))
	authService.AddSession("test-token")
	server := &Server{
		options:            Options{Auth: authService},
		startedAtUnixMilli: 1_753_689_600_000,
	}
	mux := http.NewServeMux()
	server.registerAppRoutes(mux)
	mux.HandleFunc("/api/private", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := server.buildRootHandler(http.NotFoundHandler(), mux)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/app/start-time", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("start time status = %d, want %d", response.Code, http.StatusOK)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	var body struct {
		StartedAt int64 `json:"started_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode start time response: %v", err)
	}
	if body.StartedAt != server.startedAtUnixMilli {
		t.Fatalf("started_at = %d, want %d", body.StartedAt, server.startedAtUnixMilli)
	}

	protectedResponse := httptest.NewRecorder()
	handler.ServeHTTP(protectedResponse, httptest.NewRequest(http.MethodGet, "/api/private", nil))
	if protectedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("protected route status = %d, want %d", protectedResponse.Code, http.StatusUnauthorized)
	}
}

func TestAPIRequestLoggingAndContext(t *testing.T) {
	previousLogger := slog.Default()
	defer slog.SetDefault(previousLogger)

	authService := auth.NewService(storage.NewPaths(t.TempDir()))
	authService.AddSession("test-token")
	server := &Server{options: Options{Auth: authService}, startedAtUnixMilli: 1234}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		logging.FromContext(r.Context()).InfoContext(r.Context(), "business operation", "component", "test", "operation", "read", "result", "success")
		w.WriteHeader(http.StatusNoContent)
	})

	var debugOutput bytes.Buffer
	slog.SetDefault(logging.New(&debugOutput, logging.LevelDebug))
	handler := server.buildRootHandler(http.NotFoundHandler(), mux)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(response, request)
	output := debugOutput.String()
	for _, fragment := range []string{
		`component=test`,
		`msg="business operation"`,
		`component=http`,
		`msg="request completed"`,
		`request_id="4d2-1"`,
		`status=204`,
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("debug output %q does not contain %q", output, fragment)
		}
	}

	var infoOutput bytes.Buffer
	slog.SetDefault(logging.New(&infoOutput, logging.LevelInfo))
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/test", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(response, request)
	if strings.Contains(infoOutput.String(), `msg="request completed"`) {
		t.Fatalf("info output contains debug access log: %q", infoOutput.String())
	}
}

func TestStaticRequestDoesNotProduceAccessLog(t *testing.T) {
	previousLogger := slog.Default()
	defer slog.SetDefault(previousLogger)
	var output bytes.Buffer
	slog.SetDefault(logging.New(&output, logging.LevelDebug))

	server := &Server{options: Options{Auth: auth.NewService(storage.NewPaths(t.TempDir()))}}
	handler := server.buildRootHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("static"))
	}), http.NewServeMux())
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if output.Len() != 0 {
		t.Fatalf("static request logged: %q", output.String())
	}
}

func TestAppStartTimeRouteOnlyAllowsGet(t *testing.T) {
	authService := auth.NewService(storage.NewPaths(t.TempDir()))
	server := &Server{
		options:            Options{Auth: authService},
		startedAtUnixMilli: 1,
	}
	mux := http.NewServeMux()
	server.registerAppRoutes(mux)
	handler := server.buildRootHandler(http.NotFoundHandler(), mux)

	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/api/app/start-time", strings.NewReader("{}")),
	)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST start time status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
