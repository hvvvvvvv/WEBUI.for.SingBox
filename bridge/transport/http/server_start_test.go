package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/storage"
)

func TestAppStartTimeRouteIsPublic(t *testing.T) {
	authService := auth.NewService(storage.NewPaths(t.TempDir()))
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
