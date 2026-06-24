package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/storage"
)

func TestAuthMiddlewarePreservesPublicAndProtectedPaths(t *testing.T) {
	authService := auth.NewService(storage.NewPaths(t.TempDir()))
	handler := authMiddleware(authService, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	publicRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	publicResponse := httptest.NewRecorder()
	handler.ServeHTTP(publicResponse, publicRequest)
	if publicResponse.Code != http.StatusNoContent {
		t.Fatalf("expected public login route, got %d", publicResponse.Code)
	}

	protectedRequest := httptest.NewRequest(http.MethodPost, "/api/app/env", nil)
	protectedResponse := httptest.NewRecorder()
	handler.ServeHTTP(protectedResponse, protectedRequest)
	if protectedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected protected route to reject missing token, got %d", protectedResponse.Code)
	}

	token, err := authService.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	authService.AddSession(token)
	protectedRequest = httptest.NewRequest(http.MethodPost, "/api/app/env", nil)
	protectedRequest.Header.Set("Authorization", "Bearer "+token)
	protectedResponse = httptest.NewRecorder()
	handler.ServeHTTP(protectedResponse, protectedRequest)
	if protectedResponse.Code != http.StatusNoContent {
		t.Fatalf("expected valid token to pass, got %d", protectedResponse.Code)
	}
}
