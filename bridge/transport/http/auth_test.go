package httptransport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/storage"
)

func TestLoginRateLimitIgnoresEmptySecretProbe(t *testing.T) {
	authService, handler := newAuthTestHandler(t)
	const remoteAddr = "192.0.2.1:1234"

	for range maxLoginProbeCount {
		result := requestLogin(t, handler, remoteAddr, "")
		if result.Flag || result.Data != "Invalid secret" {
			t.Fatalf("unexpected empty-secret result: %#v", result)
		}
	}
	if authService.IsLoginRateLimited(remoteAddr) {
		t.Fatal("empty-secret probes must not trigger login rate limiting")
	}
	if result := requestLogin(t, handler, remoteAddr, "correct"); !result.Flag {
		t.Fatalf("valid login after probes failed: %#v", result)
	}
}

func TestLoginRateLimitStillCountsNonEmptyFailures(t *testing.T) {
	authService, handler := newAuthTestHandler(t)
	const remoteAddr = "192.0.2.2:1234"

	for range 5 {
		result := requestLogin(t, handler, remoteAddr, "wrong")
		if result.Flag || result.Data != "Invalid secret" {
			t.Fatalf("unexpected failed-login result: %#v", result)
		}
	}
	if !authService.IsLoginRateLimited(remoteAddr) {
		t.Fatal("five non-empty failures should trigger login rate limiting")
	}
	result := requestLogin(t, handler, remoteAddr, "correct")
	if result.Flag || result.Data != "Too many failed attempts, try again later" {
		t.Fatalf("rate-limited login was not rejected: %#v", result)
	}
}

func TestSuccessfulLoginClearsFailures(t *testing.T) {
	authService, handler := newAuthTestHandler(t)
	const remoteAddr = "192.0.2.3:1234"

	for range 4 {
		requestLogin(t, handler, remoteAddr, "wrong")
	}
	if result := requestLogin(t, handler, remoteAddr, "correct"); !result.Flag {
		t.Fatalf("valid login failed: %#v", result)
	}
	for range 4 {
		requestLogin(t, handler, remoteAddr, "wrong")
	}
	if authService.IsLoginRateLimited(remoteAddr) {
		t.Fatal("successful login did not clear prior failures")
	}
}

const maxLoginProbeCount = 6

func newAuthTestHandler(t *testing.T) (*auth.Service, http.Handler) {
	t.Helper()
	authService := auth.NewService(storage.NewPaths(t.TempDir()))
	if err := authService.SetSecret("correct"); err != nil {
		t.Fatalf("set auth secret: %v", err)
	}
	mux := http.NewServeMux()
	registerAPIRoutes(mux, authService)
	return authService, mux
}

func requestLogin(t *testing.T, handler http.Handler, remoteAddr, secret string) FlagResult {
	t.Helper()
	body, err := json.Marshal(map[string]any{"args": []any{secret}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = remoteAddr
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login returned status %d: %s", recorder.Code, recorder.Body.String())
	}
	var result FlagResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode login result: %v", err)
	}
	return result
}
