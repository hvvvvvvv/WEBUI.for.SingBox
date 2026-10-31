package httptransport

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"guiforcores/bridge/auth"
	"guiforcores/bridge/event"
	"guiforcores/bridge/storage"
)

func protectedAuthHandler(t *testing.T, configured bool) (*auth.Service, *event.Hub, http.Handler) {
	t.Helper()
	service := auth.NewService(storage.NewPaths(t.TempDir()))
	if configured {
		if err := service.SetSecret("correct"); err != nil {
			t.Fatal(err)
		}
	}
	hub := event.NewHub(service)
	t.Cleanup(hub.Close)
	mux := http.NewServeMux()
	registerAPIRoutes(mux, service)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) { hub.ServeWebSocket(w, r, authTokenFromRequest(r)) })
	server := &Server{options: Options{Auth: service, Events: hub}}
	return service, hub, server.buildRootHandler(http.NotFoundHandler(), mux)
}
func authRequest(t *testing.T, handler http.Handler, endpoint, token string, args ...any) (*httptest.ResponseRecorder, FlagResult) {
	t.Helper()
	if args == nil {
		args = []any{}
	}
	data, err := json.Marshal(map[string]any{"args": args})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	req.RemoteAddr = "192.0.2.88:1234"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	var result FlagResult
	if recorder.Code == 200 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	}
	return recorder, result
}
func mustAuthResult(t *testing.T, handler http.Handler, endpoint, token string, args ...any) FlagResult {
	t.Helper()
	response, result := authRequest(t, handler, endpoint, token, args...)
	if response.Code != 200 || !result.Flag {
		t.Fatalf("%s: %d %s", endpoint, response.Code, response.Body.String())
	}
	return result
}
func currentHTTPOTP(t *testing.T, secret string) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(data[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000)
}
func enableHTTPOTP(t *testing.T, handler http.Handler, token string) (auth.Enrollment, auth.RecoveryResult) {
	t.Helper()
	begin := mustAuthResult(t, handler, "/api/auth/totp/setup", token, "correct")
	var pending auth.Enrollment
	if err := json.Unmarshal([]byte(begin.Data), &pending); err != nil {
		t.Fatal(err)
	}
	enable := mustAuthResult(t, handler, "/api/auth/totp/enable", token, currentHTTPOTP(t, pending.Secret))
	var result auth.RecoveryResult
	if err := json.Unmarshal([]byte(enable.Data), &result); err != nil {
		t.Fatal(err)
	}
	return pending, result
}
func TestHTTPAuthBootstrapAndStatusNeverExposeCredentials(t *testing.T) {
	service, _, handler := protectedAuthHandler(t, false)
	response, status := authRequest(t, handler, "/api/auth/status", "")
	if response.Header().Get("Cache-Control") != "no-store" || !status.Flag || status.Data != `{"enabled":false,"totp_enabled":false}` {
		t.Fatalf("status leaked or wrong: %s", response.Body.String())
	}
	login := mustAuthResult(t, handler, "/api/auth/login", "", "")
	replacement := mustAuthResult(t, handler, "/api/auth/setup", login.Data, "a sufficiently long initial password")
	if service.ValidateSession(login.Data) || !service.ValidateSession(replacement.Data) {
		t.Fatal("bootstrap token survived password setup")
	}
	response, _ = authRequest(t, handler, "/api/auth/setup", "", "unauthenticated new password")
	if response.Code != http.StatusUnauthorized {
		t.Fatal("setup became public")
	}
	_, failed := authRequest(t, handler, "/api/auth/setup", replacement.Data, "a sufficiently long replacement password")
	if failed.Flag {
		t.Fatal("password changed without current password")
	}
	_, status = authRequest(t, handler, "/api/auth/status", "")
	if status.Data != `{"enabled":true,"totp_enabled":false}` {
		t.Fatal(status)
	}
}
func TestHTTPSecondFactorRequiredAndAllSessionsRotate(t *testing.T) {
	service, _, handler := protectedAuthHandler(t, true)
	login := mustAuthResult(t, handler, "/api/auth/login", "", "correct")
	other := mustAuthResult(t, handler, "/api/auth/login", "", "correct")
	pending, enabled := enableHTTPOTP(t, handler, login.Data)
	if service.ValidateSession(login.Data) || service.ValidateSession(other.Data) || !service.ValidateSession(enabled.Token) {
		t.Fatal("enable did not revoke old sessions")
	}
	_, status := authRequest(t, handler, "/api/auth/status", "")
	if status.Data != `{"enabled":true,"totp_enabled":true}` || strings.Contains(status.Data, pending.Secret) {
		t.Fatal(status)
	}
	for _, args := range [][]any{{"correct"}, {"correct", currentHTTPOTP(t, pending.Secret)}} {
		_, result := authRequest(t, handler, "/api/auth/login", "", args...)
		if result.Flag {
			t.Fatal("missing/replayed factor signed in")
		}
	}
	recovered := mustAuthResult(t, handler, "/api/auth/login", "", "correct", enabled.RecoveryCodes[0])
	_, replay := authRequest(t, handler, "/api/auth/login", "", "correct", enabled.RecoveryCodes[0])
	if replay.Flag {
		t.Fatal("recovery code replay signed in")
	}
	_, bad := authRequest(t, handler, "/api/auth/totp/disable", recovered.Data, "correct", "")
	if bad.Flag {
		t.Fatal("disable bypassed factor")
	}
	regenerated := mustAuthResult(t, handler, "/api/auth/totp/recovery", recovered.Data, "correct", enabled.RecoveryCodes[1])
	var recovery auth.RecoveryResult
	if err := json.Unmarshal([]byte(regenerated.Data), &recovery); err != nil {
		t.Fatal(err)
	}
	if service.ValidateSession(enabled.Token) || service.ValidateSession(recovered.Data) {
		t.Fatal("regeneration did not revoke sessions")
	}
	_, bad = authRequest(t, handler, "/api/auth/login", "", "correct", enabled.RecoveryCodes[2])
	if bad.Flag {
		t.Fatal("old recovery codes survived")
	}
	disabled := mustAuthResult(t, handler, "/api/auth/totp/disable", recovery.Token, "correct", recovery.RecoveryCodes[0])
	if service.ValidateSession(recovery.Token) || !service.ValidateSession(disabled.Data) {
		t.Fatal("disable did not rotate session")
	}
	mustAuthResult(t, handler, "/api/auth/login", "", "correct")
}
func TestHTTPMalformedAuthCannotClearPasswordOrBypassSession(t *testing.T) {
	service, _, handler := protectedAuthHandler(t, true)
	login := mustAuthResult(t, handler, "/api/auth/login", "", "correct")
	for _, body := range []string{`{}`, `{"args":[]}`, `{"args":[null,"correct"]}`, `{"args":[false,"correct"]}`, `{"args":[{},"correct"]}`, `{"args":["","correct"],"extra":1}`, `{"args":["","correct",null]}`, `{"args":["","correct"]}{}`, `{"args":["ignored"],"args":["","correct"]}`, `{"Args":["","correct"]}`, strings.Repeat(" ", maxAuthBody+1)} {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+login.Data)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != 400 && recorder.Code != 413 {
			t.Fatalf("malformed body accepted (%d): %.80s", recorder.Code, body)
		}
		if !service.VerifySecret("correct") {
			t.Fatal("malformed setup changed secret")
		}
	}
	for _, path := range []string{"/api/auth/setup", "/api/auth/session", "/api/auth/totp/setup", "/api/auth/totp/enable", "/api/auth/totp/disable", "/api/auth/totp/recovery", "/api/auth/totp/cancel", "/ws", "/api/auth/../auth/session"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"args":[]}`))
		req.Header.Set("Authorization", "Bearer invalid")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != 401 {
			t.Fatalf("route %s bypassed session: %d", path, recorder.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"args":["correct"]}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != 403 {
		t.Fatal("cross-site login accepted")
	}
}
func TestTOTPRevocationRejectsWebSocketAndClosesOldClient(t *testing.T) {
	_, hub, handler := protectedAuthHandler(t, true)
	login := mustAuthResult(t, handler, "/api/auth/login", "", "correct")
	server := httptest.NewServer(handler)
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?auth=" + login.Data
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	enableHTTPOTP(t, handler, login.Data)
	hub.Publish("secret-change-event")
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("revoked socket received event")
	}
	if old, response, err := websocket.DefaultDialer.Dial(endpoint, nil); err == nil {
		old.Close()
		t.Fatal("revoked token opened websocket")
	} else if response == nil || response.StatusCode != 401 {
		t.Fatalf("unexpected reject: %v", err)
	}
}
