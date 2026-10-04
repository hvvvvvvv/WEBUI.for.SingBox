package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

// SING_BOX_114_PATH enables actual rule-set downloads against the fixed 1.14.0
// core. Ordinary unit tests neither download nor require an external binary.
func TestRuleSetHTTPClientsWithSingBox114(t *testing.T) {
	corePath := os.Getenv("SING_BOX_114_PATH")
	if corePath == "" {
		t.Skip("set SING_BOX_114_PATH to a sing-box 1.14.0 binary to run core checks")
	}
	version, err := runHTTPClientCoreCommand(corePath, "version")
	if err != nil || strings.SplitN(string(version), "\n", 2)[0] != "sing-box version 1.14.0" {
		t.Fatalf("expected sing-box 1.14.0, got %s (error=%v)", version, err)
	}

	tests := []struct {
		name       string
		detour     string
		final      string
		firstProxy bool
		noOutbound bool
		unnamed    bool
		directOpts bool
		shared     bool
		sharedBare bool
		inline     bool
		script     bool
		wantProxy  bool
	}{
		{name: "explicit-direct", detour: "direct", final: "proxy"},
		{name: "explicit-configured-direct", detour: "direct", directOpts: true, final: "proxy"},
		{name: "explicit-proxy", detour: "proxy", final: "direct", wantProxy: true},
		{name: "explicit-selector", detour: "selector", final: "direct", wantProxy: true},
		{name: "default-final-selector", final: "selector", wantProxy: true},
		{name: "default-final-direct", final: "direct"},
		{name: "default-first-outbound", firstProxy: true, wantProxy: true},
		{name: "default-unnamed-outbound", unnamed: true, wantProxy: true},
		{name: "default-no-outbound", noOutbound: true},
		{name: "explicit-direct-fallback-reference", detour: "direct", noOutbound: true},
		{name: "existing-shared-client", shared: true, final: "direct", wantProxy: true},
		{name: "existing-unnamed-shared-client", sharedBare: true, final: "direct", wantProxy: true},
		{name: "inline-client-precedes-legacy", detour: "direct", inline: true, wantProxy: true},
		{name: "mixin-and-script-final", script: true, wantProxy: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var originRequests atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originRequests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"version":3,"rules":[{"domain_suffix":["http-client.example"]}]}`)
			}))
			defer origin.Close()
			originAddr := strings.TrimPrefix(origin.URL, "http://")
			proxy, proxyRequests := newHTTPClientCoreProxy(t, originAddr)
			defer proxy.Close()
			proxyHost, proxyPort, err := net.SplitHostPort(strings.TrimPrefix(proxy.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(proxyPort)
			if err != nil {
				t.Fatal(err)
			}
			direct := map[string]any{"type": "direct", "tag": "direct"}
			if tc.directOpts {
				direct["inet4_bind_address"] = "127.0.0.1"
				direct["connect_timeout"] = "5s"
			}
			proxyOutbound := map[string]any{"type": "http", "tag": "proxy", "server": proxyHost, "server_port": port}
			selector := map[string]any{"type": "selector", "tag": "selector", "outbounds": []any{"proxy", "direct"}, "default": "proxy"}
			outbounds := []any{direct, proxyOutbound, selector}
			if tc.firstProxy {
				outbounds = []any{proxyOutbound, direct, selector}
			}
			if tc.noOutbound {
				outbounds = nil
			}
			if tc.unnamed {
				delete(proxyOutbound, "tag")
				outbounds = []any{proxyOutbound}
			}
			ruleSet := map[string]any{"type": "remote", "tag": "downloaded", "format": "source", "url": origin.URL + "/rules.json"}
			if tc.detour != "" {
				ruleSet["download_detour"] = tc.detour
			}
			if tc.inline {
				ruleSet["http_client"] = map[string]any{"detour": "proxy"}
			}
			route := map[string]any{"rule_set": []any{ruleSet}}
			if tc.final != "" {
				route["final"] = tc.final
			}
			config := map[string]any{
				"log":       map[string]any{"level": "debug", "timestamp": false},
				"outbounds": outbounds,
				"route":     route,
			}
			if tc.shared {
				config["http_clients"] = []any{map[string]any{"tag": "custom", "detour": "proxy"}}
				route["default_http_client"] = "custom"
			}
			if tc.sharedBare {
				config["http_clients"] = []any{map[string]any{"detour": "proxy"}}
			}
			if tc.script {
				// The mixin creates the remote rule set and the script changes its
				// default outbound. Normalization must run after both operations.
				route["final"] = "direct"
				mixin, err := json.Marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				service := NewService(storage.NewPaths(t.TempDir()), "test")
				config, err = service.Generate(&profilev1.Profile{
					Mixin:  &profilev1.Mixin{Priority: profilev1.MixinPriority_MIXIN_PRIORITY_MIXIN, Config: string(mixin)},
					Script: &profilev1.Script{Code: `function onGenerate(config) { config.route.final = "proxy"; return config; }`},
				}, &kernelv1.GenerateConfigOptions{EnableMixinProcessing: true, EnableScriptProcessing: true})
				if err != nil {
					t.Fatal(err)
				}
			} else if err := normalizeRuleSetHTTPClients(config); err != nil {
				t.Fatal(err)
			}
			data, err := json.MarshalIndent(config, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte(`"download_detour"`)) {
				t.Fatalf("generated config contains legacy download_detour: %s", data)
			}
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			output, err := runHTTPClientCoreCommand(corePath, "check", "-c", configPath)
			if err != nil {
				t.Fatalf("core rejected migrated config: error=%v, output=%s, config=%s", err, output, data)
			}
			runOutput := runHTTPClientCoreUntilStarted(t, corePath, configPath)
			if originRequests.Load() == 0 {
				t.Fatalf("core started without downloading the remote rule set: %s", runOutput)
			}
			if got := proxyRequests.Load() > 0; got != tc.wantProxy {
				t.Fatalf("download used proxy = %v, want %v (requests=%d): %s", got, tc.wantProxy, proxyRequests.Load(), runOutput)
			}
			logs := strings.ToLower(string(output) + runOutput)
			for _, deprecated := range []string{"download_detour", "deprecated", "implicit default http"} {
				if strings.Contains(logs, deprecated) {
					t.Fatalf("core emitted legacy HTTP client warning %q: %s", deprecated, logs)
				}
			}
		})
	}
}

func httpClientCoreEnvironment() []string {
	var environment []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "ENABLE_DEPRECATED_") {
			environment = append(environment, value)
		}
	}
	return environment
}

func runHTTPClientCoreCommand(corePath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, corePath, args...)
	command.Env = httpClientCoreEnvironment()
	return command.CombinedOutput()
}

type httpClientCoreLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *httpClientCoreLog) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *httpClientCoreLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func runHTTPClientCoreUntilStarted(t *testing.T, corePath, configPath string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, corePath, "run", "-c", configPath)
	command.Env = httpClientCoreEnvironment()
	var logs httpClientCoreLog
	command.Stdout, command.Stderr = &logs, &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("core exited before startup: error=%v, output=%s", err, logs.String())
		case <-ticker.C:
			if strings.Contains(logs.String(), "sing-box started") {
				cancel()
				<-done
				return logs.String()
			}
		case <-ctx.Done():
			<-done
			t.Fatalf("core did not finish downloading and start: %s", logs.String())
		}
	}
}

func newHTTPClientCoreProxy(t *testing.T, originAddr string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	requests := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != originAddr {
			http.Error(w, "unexpected CONNECT destination", http.StatusBadRequest)
			return
		}
		requests.Add(1)
		upstream, err := net.DialTimeout("tcp", originAddr, 5*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		client, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		if _, err := fmt.Fprint(buffered, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err := buffered.Flush(); err != nil {
			return
		}
		go func() {
			_, _ = io.Copy(upstream, buffered)
			_ = upstream.Close()
			_ = client.Close()
		}()
		_, _ = io.Copy(client, upstream)
	}))
	return server, requests
}
