package network

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRequestEnvironmentModeUsesServiceEnvironment(t *testing.T) {
	if os.Getenv("WEBUI_TEST_ENV_PROXY") == "1" {
		client, err := NewClient(Options{Mode: ModeEnvironment}, time.Second, false)
		if err != nil {
			t.Fatal(err)
		}
		defer client.CloseIdleConnections()
		response, err := client.Get("http://unresolvable.invalid/from-backend-environment")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 204 {
			t.Fatal(response.StatusCode)
		}
		return
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "http://unresolvable.invalid/from-backend-environment" {
			t.Errorf("wrong target: %s", r.URL)
		}
		w.WriteHeader(204)
	}))
	defer proxy.Close()
	// The standard library caches proxy variables. A fresh process verifies the
	// backend's real environment without depending on other tests' first request.
	t.Setenv("WEBUI_TEST_ENV_PROXY", "1")
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("REQUEST_METHOD", "")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestRequestEnvironmentModeUsesServiceEnvironment$")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("environment proxy failed: %v\n%s", err, output)
	}
}

func TestRequestModesActuallyUseConfiguredProxy(t *testing.T) {
	for _, mode := range []string{ModeCustom, ModeCore} {
		t.Run(mode, func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.String() != "http://unresolvable.invalid/asset" || r.Header.Get("User-Agent") != "router-webui/test" {
					t.Errorf("wrong proxied target or UA: %s %q", r.URL, r.Header.Get("User-Agent"))
				}
				want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:password"))
				if r.Header.Get("Proxy-Authorization") != want {
					t.Error("proxy credentials not used")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer proxy.Close()
			proxyURL, _ := url.Parse(proxy.URL)
			proxyURL.User = url.UserPassword("alice", "password")
			options := Options{Mode: mode, ProxyURL: proxyURL.String(), UserAgent: "router-webui/test", CoreProxy: func() (string, error) { return proxyURL.String(), nil }}
			client, err := NewClient(options, time.Second, false)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			request, _ := http.NewRequest(http.MethodGet, "http://unresolvable.invalid/asset", nil)
			if err := PrepareRequest(request, options, false); err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatal(response.StatusCode)
			}
		})
	}
	for _, options := range []Options{{Mode: ModeCore}, {Mode: ModeCore, CoreProxy: func() (string, error) { return "", errors.New("core stopped") }}, {Mode: ModeCustom, ProxyURL: "http://user:secret@invalid:bad"}} {
		if _, err := NewClient(options, time.Second, false); err == nil {
			t.Fatal("unavailable proxy silently fell back")
		}
	}
	// None must bypass even an explicitly supplied proxy; the environment mode
	// uses the standard environment resolver rather than the browser's settings.
	client, _ := NewClient(Options{Mode: ModeNone, ProxyURL: "http://127.0.0.1:1"}, time.Second, false)
	defer client.CloseIdleConnections()
	if client.Transport.(privateTransport).Proxy != nil {
		t.Fatal("none still resolves a proxy")
	}
	environment, _ := NewClient(Options{Mode: ModeEnvironment}, time.Second, false)
	defer environment.CloseIdleConnections()
	if environment.Transport.(privateTransport).Proxy == nil {
		t.Fatal("environment proxy resolver missing")
	}
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer direct.Close()
	response, err := client.Get(direct.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestHTTPSAndSOCKSProxiesReachDestination(t *testing.T) {
	t.Run("https", func(t *testing.T) {
		proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Host != "unresolvable.invalid" {
				t.Errorf("wrong destination: %s", r.URL)
			}
			w.WriteHeader(204)
		}))
		defer proxy.Close()
		client, err := NewClient(Options{Mode: ModeCustom, ProxyURL: proxy.URL}, time.Second, true)
		if err != nil {
			t.Fatal(err)
		}
		defer client.CloseIdleConnections()
		response, err := client.Get("http://unresolvable.invalid/asset")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 204 {
			t.Fatal(response.StatusCode)
		}
	})
	t.Run("socks5", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		done := make(chan error, 1)
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				done <- err
				return
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			reader := bufio.NewReader(conn)
			head := make([]byte, 2)
			if _, err = io.ReadFull(reader, head); err != nil {
				done <- err
				return
			}
			methods := make([]byte, int(head[1]))
			if _, err = io.ReadFull(reader, methods); err != nil {
				done <- err
				return
			}
			conn.Write([]byte{5, 0})
			connect := make([]byte, 5)
			if _, err = io.ReadFull(reader, connect); err != nil {
				done <- err
				return
			}
			if connect[0] != 5 || connect[1] != 1 || connect[3] != 3 {
				done <- errors.New("expected SOCKS5 domain connect")
				return
			}
			destination := make([]byte, int(connect[4])+2)
			if _, err = io.ReadFull(reader, destination); err != nil {
				done <- err
				return
			}
			if string(destination[:len(destination)-2]) != "unresolvable.invalid" {
				done <- errors.New("destination DNS was not delegated to SOCKS")
				return
			}
			conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
			request, err := http.ReadRequest(reader)
			if err == nil && request.URL.Path != "/asset" {
				err = errors.New("wrong HTTP request")
			}
			if err == nil {
				_, err = io.WriteString(conn, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
			}
			done <- err
		}()
		client, err := NewClient(Options{Mode: ModeCustom, ProxyURL: "socks5://" + listener.Addr().String()}, 2*time.Second, false)
		if err != nil {
			t.Fatal(err)
		}
		defer client.CloseIdleConnections()
		response, err := client.Get("http://unresolvable.invalid/asset")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if err = <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestGitHubTokenAndMirrorTrustBoundaries(t *testing.T) {
	options := Options{UserAgent: "webui", GitHubToken: "private-token", GitHubMirror: "https://mirror.invalid/?url={url}"}
	for _, target := range []string{"https://api.github.com/repos/test/repo", "http://api.github.com/repos/test/repo", "https://api.github.com:8443/repos/test/repo", "https://sub.api.github.com/path", "https://raw.githubusercontent.com/file", "https://github.com/owner/repo/releases/download/v1/a.zip?x=1&y=2"} {
		t.Run(target, func(t *testing.T) {
			request, _ := http.NewRequest(http.MethodGet, target, nil)
			if err := PrepareRequest(request, options, true); err != nil {
				t.Fatal(err)
			}
			if target == "https://api.github.com/repos/test/repo" {
				if request.Header.Get("Authorization") != "Bearer private-token" || request.URL.String() != target {
					t.Fatal("API token or URL wrong")
				}
			} else if request.Header.Get("Authorization") != "" {
				t.Fatal("token leaked outside exact API origin")
			}
			if strings.Contains(target, "/releases/download/") && (request.URL.Host != "mirror.invalid" || request.URL.Query().Get("url") != target) {
				t.Fatalf("incorrect mirror escaping: %s", request.URL)
			}
		})
	}
	request, _ := http.NewRequest(http.MethodGet, "https://github.com/owner/repo/releases/download/v1/a.zip", nil)
	request.Header.Set("Authorization", "Bearer supplied")
	request.Header.Set("Cookie", "session=private")
	if err := PrepareRequest(request, options, true); err != nil {
		t.Fatal(err)
	}
	if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
		t.Fatal("mirror inherited sensitive headers")
	}
	client, err := NewClient(options, time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	original, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/test/repo", nil)
	for _, target := range []string{"https://sub.api.github.com/x", "http://api.github.com/x", "https://api.github.com:8443/x", "https://mirror.invalid/x"} {
		redirected, _ := http.NewRequest(http.MethodGet, target, nil)
		redirected.Header.Set("Authorization", "Bearer private-token")
		if err := client.CheckRedirect(redirected, []*http.Request{original}); err != nil {
			t.Fatal(err)
		}
		if redirected.Header.Get("Authorization") != "" {
			t.Fatalf("redirect leaked token to %s", target)
		}
	}
	for _, mirror := range []string{"http://mirror.invalid/{url}", "https://user:secret@mirror.invalid/{url}", "https://{url}", "https://mirror.invalid/{url}#{url}", "https://mirror.invalid/"} {
		if ValidateMirror(mirror) == nil {
			t.Errorf("invalid mirror accepted: %s", mirror)
		}
	}
}

func TestProxyErrorsDoNotRevealCredentialsAndKeepCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	client, err := NewClient(Options{Mode: ModeCustom, ProxyURL: "http://private-user:secret-password@" + address}, time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	_, err = client.Get("http://unresolvable.invalid/")
	if err == nil || strings.Contains(err.Error(), "private-user") || strings.Contains(err.Error(), "secret-password") {
		t.Fatalf("unsafe error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://unresolvable.invalid/", nil)
	_, err = client.Do(request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation identity: %v", err)
	}
}
