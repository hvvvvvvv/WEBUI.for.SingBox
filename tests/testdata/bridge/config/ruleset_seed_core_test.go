package config

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/storage"
)

// Google is a real member of the bundled GeoLocation-!CN snapshot. The actual
// connection is overridden to a loopback HTTP fixture after sniffing the Host;
// an unmatched domain is rejected before dialing. No TUN, DNS or public service
// is used. All six unmodified default rule sources are identified by production
// bootstrap code before the test replaces their download URLs with loopback.
func TestBundledRuleSetBootstrapFailuresAndCacheWithCore(t *testing.T) {
	corePath := remoteUpdateCorePath(t)
	const seedDomain = "google.com"
	const updatedDomain = "updated.seed-rules.test"
	const exercisedTag = "GeoLocation-!CN"
	for _, failure := range []string{"http-503", "invalid-srs"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			updatedRules := remoteUpdateRules(t, corePath, root, updatedDomain, true)
			var phase, requests atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if phase.Load() == 1 && r.URL.Path == "/"+exercisedTag+".srs" {
					_, _ = w.Write(updatedRules)
					return
				}
				remoteUpdateFailure(w, failure)
			}))
			t.Cleanup(origin.Close)
			configPath, proxyAddress, cachePath := bundledRuleSetRuntime(t, root, origin.URL, exercisedTag)
			if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
				t.Fatalf("first startup must have no core cache: %v", err)
			}

			stop, logs := startRouterRulesCore(t, corePath, configPath)
			remoteUpdateWait(t, logs, "failed background update with bundled rules active", func() bool {
				return requests.Load() >= 6 && strings.Contains(logs.String(), "fetch rule-set "+exercisedTag+":")
			})
			if strings.Contains(logs.String(), "load initial rule-set from") {
				t.Fatalf("a bundled rule set could not be read:\n%s", logs.String())
			}
			remoteUpdateAssertRoutes(t, proxyAddress, seedDomain, updatedDomain, logs)

			beforeRecovery := requests.Load()
			phase.Store(1)
			remoteUpdateWait(t, logs, "successful background update after bootstrap", func() bool {
				return requests.Load() > beforeRecovery && strings.Contains(logs.String(), "updated rule-set "+exercisedTag)
			})
			remoteUpdateAssertRoutes(t, proxyAddress, updatedDomain, seedDomain, logs)
			stop()
			if info, err := os.Stat(cachePath); err != nil || info.Size() == 0 {
				t.Fatalf("successful update did not persist its cache: %v", err)
			}

			// A newer cached download must win over the older bundled snapshot,
			// including when the update origin is unavailable on the next boot.
			phase.Store(0)
			beforeFailure := requests.Load()
			stop, logs = startRouterRulesCore(t, corePath, configPath)
			remoteUpdateWait(t, logs, "failed refresh after loading newer cache", func() bool {
				return requests.Load() > beforeFailure && strings.Contains(logs.String(), "fetch rule-set "+exercisedTag+":")
			})
			remoteUpdateAssertRoutes(t, proxyAddress, updatedDomain, seedDomain, logs)
			stop()

			// Removing only this disposable core cache returns to the bundled
			// rules, without requiring a successful first network download.
			if err := os.Remove(cachePath); err != nil {
				t.Fatal(err)
			}
			stop, logs = startRouterRulesCore(t, corePath, configPath)
			remoteUpdateAssertRoutes(t, proxyAddress, seedDomain, updatedDomain, logs)
			stop()
			t.Log("all six embedded rule sets booted with a failed origin; updates retained active rules, newer disk cache won after restart, and a cleared cache booted from seeds")
		})
	}
}

func TestBundledRuleSetStartsWhileDownloadProxyStallsWithCore(t *testing.T) {
	corePath := remoteUpdateCorePath(t)
	var originRequests, proxyRequests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests.Add(1)
		http.Error(w, "downloads must use the selected fixture proxy", http.StatusServiceUnavailable)
	}))
	t.Cleanup(origin.Close)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		// Accept the core's HTTP proxy handshake, but never establish an
		// upstream connection. Each rule download remains pending until
		// cleanup closes it, independently of the active data-plane rules.
		select {
		case <-release:
			http.Error(w, "fixture proxy stopped", http.StatusGatewayTimeout)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(proxy.Close)
	t.Cleanup(unblock)
	root := t.TempDir()
	configPath, proxyAddress, cachePath := bundledRuleSetRuntime(t, root, origin.URL, "GeoLocation-!CN")
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("first startup must not use a pre-existing cache: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var generated map[string]any
	if err := json.Unmarshal(data, &generated); err != nil {
		t.Fatal(err)
	}
	generated["outbounds"] = append(generated["outbounds"].([]any), map[string]any{
		"type": "http", "tag": "stalled-download-proxy", "server": "127.0.0.1",
		"server_port": proxy.Listener.Addr().(*net.TCPAddr).Port,
	})
	for _, value := range generated["route"].(map[string]any)["rule_set"].([]any) {
		value.(map[string]any)["http_client"] = map[string]any{"engine": "go", "detour": "stalled-download-proxy"}
	}
	data, err = json.MarshalIndent(generated, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	stop, logs := startRouterRulesCore(t, corePath, configPath)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatalf("no background rule download reached the selected stalled proxy:\n%s", logs.String())
	}
	remoteUpdateAssertRoutes(t, proxyAddress, "google.com", "unmatched.seed-rules.test", logs)
	// Hold the unavailable proxy across more than two test update intervals.
	// This is a bounded liveness observation, not an assertion that a future
	// core must never introduce its own download timeout.
	timer := time.NewTimer(2200 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	remoteUpdateAssertRoutes(t, proxyAddress, "google.com", "unmatched.seed-rules.test", logs)
	if originRequests.Load() != 0 || proxyRequests.Load() == 0 {
		t.Fatalf("rule downloads bypassed the selected proxy: origin=%d proxy=%d", originRequests.Load(), proxyRequests.Load())
	}
	stop()
	unblock()
	t.Log("six embedded rule sets started without cache while the selected download proxy hung; Google-matching loopback traffic remained functional")
}

func bundledRuleSetRuntime(t *testing.T, root, origin, exercisedTag string) (string, string, string) {
	t.Helper()
	defaultProfileSubscription(t, root, map[string]any{
		"type": "http", "tag": "fixture-node", "server": "127.0.0.1", "server_port": 1080,
	})
	service := NewService(storage.NewPaths(root), "test")
	generated, err := service.Generate(defaultQuickStartProfile(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PrepareRuleSetBootstrap(context.Background(), generated); err != nil {
		t.Fatal(err)
	}
	ruleSets := generated["route"].(map[string]any)["rule_set"].([]any)
	if len(ruleSets) != 6 {
		t.Fatalf("expected the six real QuickStart rule sources, got %d", len(ruleSets))
	}
	workingDirectory := filepath.Join(root, "data", "sing-box")
	if err := os.MkdirAll(workingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, value := range ruleSets {
		ruleSet := value.(map[string]any)
		seedPath, ok := ruleSet["initial_path"].(string)
		if !ok || seedPath == "" {
			t.Fatalf("default rule set %v did not receive an embedded seed", ruleSet["tag"])
		}
		if !filepath.IsAbs(seedPath) {
			// The shared process helper runs without changing the caller cwd.
			// Resolve a production relative seed against the managed core dir.
			seedPath = filepath.Join(workingDirectory, filepath.FromSlash(seedPath))
			ruleSet["initial_path"] = seedPath
		}
		if info, err := os.Stat(seedPath); err != nil || info.Size() == 0 {
			t.Fatalf("bundled seed for %v is unavailable: %v", ruleSet["tag"], err)
		}
		if ruleSet["update_interval"] != "24h" {
			t.Fatalf("production update interval changed: %#v", ruleSet)
		}
		// Only the isolated runtime fixture changes update scheduling/network.
		ruleSet["url"] = origin + "/" + ruleSet["tag"].(string) + ".srs"
		ruleSet["http_client"] = map[string]any{"engine": "go"}
		ruleSet["update_interval"] = "1s"
		delete(ruleSet, "download_detour")
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "matched cached domain rule")
	}))
	t.Cleanup(target.Close)
	proxyAddress := defaultProfileUnusedAddress(t)
	proxyAddr, err := net.ResolveTCPAddr("tcp", proxyAddress)
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(root, "cache.db")
	// Retain all generated seed-backed rule descriptors, but intentionally do
	// not run the default profile's TUN, DNS, selectors, API or subscription.
	runtimeConfig := map[string]any{
		"log": map[string]any{"level": "debug", "timestamp": false},
		"inbounds": []any{map[string]any{
			"type": "mixed", "tag": "seed-test", "listen": "127.0.0.1", "listen_port": proxyAddr.Port,
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"experimental": map[string]any{"cache_file": map[string]any{
			"enabled": true, "path": cachePath, "cache_id": "bundled-seed-regression",
		}},
		"route": map[string]any{
			"rule_set": ruleSets,
			"rules": []any{
				map[string]any{"action": "sniff"},
				map[string]any{
					"rule_set": []string{exercisedTag}, "action": "route", "outbound": "direct",
					"override_address": "127.0.0.1", "override_port": target.Listener.Addr().(*net.TCPAddr).Port,
				},
				map[string]any{"action": "reject"},
			},
		},
	}
	data, err := json.MarshalIndent(runtimeConfig, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, proxyAddress, cachePath
}
