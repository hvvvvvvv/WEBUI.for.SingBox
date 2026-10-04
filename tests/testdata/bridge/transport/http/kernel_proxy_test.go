package httptransport

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/storage"
)

func grpcWebFrame(flags byte, body string) []byte {
	frame := make([]byte, 5+len(body))
	frame[0] = flags
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(body)))
	copy(frame[5:], body)
	return frame
}

func newNativeProxyTestServer(t *testing.T, upstream http.Handler, secret string, valid func() bool) (*httptest.Server, *httptest.Server) {
	t.Helper()
	core := httptest.NewServer(upstream)
	target, err := url.Parse(core.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyNativeAPI(&responseRecorder{ResponseWriter: w, status: http.StatusOK}, r, target, secret, valid)
	}))
	t.Cleanup(proxy.Close)
	t.Cleanup(core.Close)
	return proxy, core
}

func nativeProxyRequest(t *testing.T, proxy *httptest.Server, ctx context.Context) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxy.URL+"/api/kernel/daemon.StartedService/SubscribeStatus", bytes.NewReader(grpcWebFrame(0, "request")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/grpc-web+proto")
	req.Header.Set("Authorization", "Bearer browser-token")
	req.Header.Set("X-Grpc-Web", "1")
	response, err := proxy.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func TestNativeAPIProxyPreservesFramesAndInjectsInstanceSecret(t *testing.T) {
	for _, secret := range []string{"first-instance", "replacement-instance"} {
		t.Run(secret, func(t *testing.T) {
			first := grpcWebFrame(0, "first")
			last := grpcWebFrame(128, "grpc-status: 16\r\ngrpc-message: denied\r\n")
			release := make(chan struct{})
			proxy, _ := newNativeProxyTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/daemon.StartedService/SubscribeStatus" {
					t.Errorf("upstream path = %q", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer "+secret || r.Header.Get("X-Grpc-Web") != "1" {
					t.Errorf("incorrect upstream headers: %v", r.Header)
				}
				body, _ := io.ReadAll(r.Body)
				if !bytes.Equal(body, grpcWebFrame(0, "request")) {
					t.Errorf("upstream body changed: %x", body)
				}
				w.Header().Set("Content-Type", "application/grpc-web+proto")
				w.Write(first)
				w.(http.Flusher).Flush()
				select {
				case <-release:
					w.Write(last)
				case <-r.Context().Done():
				}
			}), secret, func() bool { return true })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			response := nativeProxyRequest(t, proxy, ctx)
			got := make([]byte, len(first))
			if _, err := io.ReadFull(response.Body, got); err != nil || !bytes.Equal(got, first) {
				t.Fatalf("first frame was not flushed: %x, %v", got, err)
			}
			close(release)
			got, err := io.ReadAll(response.Body)
			if err != nil || !bytes.Equal(got, last) || response.StatusCode != http.StatusOK {
				t.Fatalf("native error tail changed: %x, status=%d, err=%v", got, response.StatusCode, err)
			}
		})
	}
}

func TestNativeAPIProxyCancellation(t *testing.T) {
	for _, reason := range []string{"browser", "expired-session"} {
		t.Run(reason, func(t *testing.T) {
			var valid atomic.Bool
			valid.Store(true)
			upstreamCanceled := make(chan struct{})
			proxy, _ := newNativeProxyTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/grpc-web+proto")
				w.Write(grpcWebFrame(0, "live"))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(upstreamCanceled)
			}), "native-secret", valid.Load)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			response := nativeProxyRequest(t, proxy, ctx)
			if reason == "expired-session" {
				valid.Store(false)
			} else if reason == "browser" {
				response.Body.Close()
			}
			select {
			case <-upstreamCanceled:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream stream did not observe cancellation")
			}
		})
	}
}

func TestNativeAPIProxyDoesNotChallengeApplicationLoginForUpstreamHTTP401(t *testing.T) {
	proxy, _ := newNativeProxyTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "native authentication failed", http.StatusUnauthorized)
	}), "native-secret", func() bool { return true })
	response := nativeProxyRequest(t, proxy, context.Background())
	if response.StatusCode != http.StatusBadGateway || response.Header.Get("WWW-Authenticate") != "" {
		t.Fatalf("native auth became application auth: %d, %v", response.StatusCode, response.Header)
	}
}

func TestNativeAPIProxyLongStream(t *testing.T) {
	if testing.Short() {
		t.Skip("verifies an actual stream beyond the old 60-second timeout")
	}
	proxy, _ := newNativeProxyTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc-web+proto")
		w.Write(grpcWebFrame(0, "initial"))
		w.(http.Flusher).Flush()
		select {
		case <-time.After(61 * time.Second):
			w.Write(grpcWebFrame(128, "grpc-status: 0\r\n"))
		case <-r.Context().Done():
		}
	}), "native-secret", func() bool { return true })
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	response := nativeProxyRequest(t, proxy, ctx)
	body, err := io.ReadAll(response.Body)
	if err != nil || !bytes.HasSuffix(body, grpcWebFrame(128, "grpc-status: 0\r\n")) {
		t.Fatalf("long stream did not finish: %x, %v", body, err)
	}
}

func TestNativeKernelRouteRequiresApplicationSession(t *testing.T) {
	authService := auth.NewService(storage.NewPaths(t.TempDir()))
	if err := authService.SetSecret("application-secret"); err != nil {
		t.Fatal(err)
	}
	authService.AddSession("valid-session")
	server := &Server{options: Options{Auth: authService}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/kernel/", server.handleKernelProxy)
	handler := server.buildRootHandler(http.NotFoundHandler(), mux)
	for _, token := range []string{"", "invalid-session", "valid-session"} {
		request := httptest.NewRequest(http.MethodPost, "/api/kernel/daemon.StartedService/GetVersion", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusUnauthorized
		if token == "valid-session" {
			want = http.StatusServiceUnavailable
		}
		if response.Code != want {
			t.Errorf("token %q: status=%d, want=%d", token, response.Code, want)
		}
	}
}
