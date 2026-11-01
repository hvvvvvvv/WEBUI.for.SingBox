package config

import (
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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

const remoteUpdateOldDomain = "old.rule-update.test"
const remoteUpdateNewDomain = "new.rule-update.test"

// These integration tests intentionally use a one-second update interval, not
// the application's 24h default. All downloads and routed connections stay on
// loopback; an unmatched synthetic destination is rejected before dialing.
func TestRemoteRuleSetUpdateFailuresRetainCacheWithCore(t *testing.T) {
	corePath := remoteUpdateCorePath(t)
	for _, failure := range []string{"http-503", "malformed-json", "invalid-json-rule", "invalid-srs", "http-504"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			binary := failure == "invalid-srs"
			oldRules := remoteUpdateRules(t, corePath, root, remoteUpdateOldDomain, binary)
			newRules := remoteUpdateRules(t, corePath, root, remoteUpdateNewDomain, binary)
			var phase, requests atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch phase.Load() {
				case 0:
					_, _ = w.Write(oldRules)
				case 1:
					remoteUpdateFailure(w, failure)
				case 2:
					_, _ = w.Write(newRules)
				}
			}))
			t.Cleanup(origin.Close)
			configPath, proxyAddress, cachePath := remoteUpdateConfig(t, root, origin.URL, binary)

			// First populate the real on-disk core cache, then start a new core
			// while the remote service is failing. In-memory retention alone is
			// insufficient: a failed fetch must not poison the persisted copy.
			stop, logs := startRouterRulesCore(t, corePath, configPath)
			remoteUpdateAssertRoutes(t, proxyAddress, remoteUpdateOldDomain, remoteUpdateNewDomain, logs)
			stop()
			if info, err := os.Stat(cachePath); err != nil || info.Size() == 0 {
				t.Fatalf("valid rule-set cache was not saved: %v", err)
			}
			phase.Store(1)
			beforeFailure := requests.Load()
			stop, logs = startRouterRulesCore(t, corePath, configPath)
			remoteUpdateWait(t, logs, "failed scheduled update", func() bool {
				return requests.Load() > beforeFailure && strings.Contains(logs.String(), "fetch rule-set update-test:")
			})
			remoteUpdateAssertRoutes(t, proxyAddress, remoteUpdateOldDomain, remoteUpdateNewDomain, logs)
			stop()
			stop, logs = startRouterRulesCore(t, corePath, configPath)
			remoteUpdateAssertRoutes(t, proxyAddress, remoteUpdateOldDomain, remoteUpdateNewDomain, logs)

			beforeRecovery := requests.Load()
			phase.Store(2)
			remoteUpdateWait(t, logs, "successful scheduled retry", func() bool {
				return requests.Load() > beforeRecovery && strings.Contains(logs.String(), "updated rule-set update-test")
			})
			// Both directions are asserted: a successful refresh replaces the
			// old match rather than merely retaining/unioning both rule sets.
			remoteUpdateAssertRoutes(t, proxyAddress, remoteUpdateNewDomain, remoteUpdateOldDomain, logs)
			stop()
			phase.Store(1)
			stop, logs = startRouterRulesCore(t, corePath, configPath)
			remoteUpdateAssertRoutes(t, proxyAddress, remoteUpdateNewDomain, remoteUpdateOldDomain, logs)
			stop()
			t.Log("failed update retained old routing and disk cache; next successful retry replaced routing and persisted it")
		})
	}
}

func TestRemoteRuleSetFirstDownloadFailureDoesNotStartWithCore(t *testing.T) {
	corePath := remoteUpdateCorePath(t)
	for _, failure := range []string{"http-503", "malformed-json", "invalid-json-rule", "invalid-srs", "http-504"} {
		t.Run(failure, func(t *testing.T) {
			var requests atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				remoteUpdateFailure(w, failure)
			}))
			t.Cleanup(origin.Close)
			configPath, _, _ := remoteUpdateConfig(t, t.TempDir(), origin.URL, failure == "invalid-srs")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, corePath, "run", "--disable-color", "-c", configPath)
			command.Env = httpClientCoreEnvironment()
			platform.SetCmdWindowHidden(command)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil || err == nil || requests.Load() == 0 {
				t.Fatalf("initial download did not explicitly fail: error=%v deadline=%v requests=%d\n%s", err, ctx.Err(), requests.Load(), output)
			}
			if strings.Contains(string(output), "sing-box started") || !strings.Contains(string(output), "initial rule-set: update-test") {
				t.Fatalf("missing rule set was silently accepted or failure lacks its tag:\n%s", output)
			}
		})
	}
}

// sing-box 1.14.2 has no response deadline on RemoteRuleSet's HTTP client.
// Do not misrepresent an HTTP 504 as a client timeout: a stalled HTTP response
// keeps the update pending while cached routing continues. Releasing that
// response with a gateway timeout permits the next scheduled retry to succeed.
func TestRemoteRuleSetStalledUpdateKeepsCachedRoutesWithCore(t *testing.T) {
	corePath := remoteUpdateCorePath(t)
	root := t.TempDir()
	oldRules := remoteUpdateRules(t, corePath, root, remoteUpdateOldDomain, false)
	newRules := remoteUpdateRules(t, corePath, root, remoteUpdateNewDomain, false)
	var phase, requests atomic.Int64
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch phase.Load() {
		case 0:
			_, _ = w.Write(oldRules)
		case 1:
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
				http.Error(w, "fixture gateway timeout", http.StatusGatewayTimeout)
			case <-r.Context().Done():
			}
		case 2:
			_, _ = w.Write(newRules)
		}
	}))
	t.Cleanup(origin.Close)
	t.Cleanup(unblock)
	configPath, proxyAddress, _ := remoteUpdateConfig(t, root, origin.URL, false)
	stop, _ := startRouterRulesCore(t, corePath, configPath)
	stop()
	phase.Store(1)
	stop, logs := startRouterRulesCore(t, corePath, configPath)
	select {
	case <-entered:
	case <-time.After(8 * time.Second):
		t.Fatalf("scheduled request never reached the stalled origin:\n%s", logs.String())
	}
	before := requests.Load()
	remoteUpdateAssertRoutes(t, proxyAddress, remoteUpdateOldDomain, remoteUpdateNewDomain, logs)
	// More than two scheduled intervals pass. The assertion concerns this
	// bounded observation window, not an inferred unlimited wait from timing.
	timer := time.NewTimer(2200 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	remoteUpdateAssertRoutes(t, proxyAddress, remoteUpdateOldDomain, remoteUpdateNewDomain, logs)
	// Record the actual retry behavior, but do not require the core to lack
	// a timeout: adding a response deadline in a later release is valid.
	t.Logf("stalled response observation: requests=%d, retry requests=%d, failure logged=%v", before,
		requests.Load()-before, strings.Contains(logs.String(), "fetch rule-set update-test:"))
	beforeRecovery := requests.Load()
	phase.Store(2)
	unblock()
	remoteUpdateWait(t, logs, "recovery after stalled update", func() bool {
		return requests.Load() > beforeRecovery && strings.Contains(logs.String(), "updated rule-set update-test")
	})
	remoteUpdateAssertRoutes(t, proxyAddress, remoteUpdateNewDomain, remoteUpdateOldDomain, logs)
	stop()
	t.Log("cached routes remained live during a stalled response; after the server returned 504, the next retry applied new rules")
}

func TestRemoteRuleSetStalledFirstDownloadDoesNotStartWithCore(t *testing.T) {
	corePath := remoteUpdateCorePath(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			http.Error(w, "fixture gateway timeout", http.StatusGatewayTimeout)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(origin.Close)
	t.Cleanup(unblock)
	configPath, _, _ := remoteUpdateConfig(t, t.TempDir(), origin.URL, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	command := exec.CommandContext(ctx, corePath, "run", "--disable-color", "-c", configPath)
	command.Env = httpClientCoreEnvironment()
	command.WaitDelay = time.Second
	platform.SetCmdWindowHidden(command)
	logs := &httpClientCoreLog{}
	command.Stdout, command.Stderr = logs, logs
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = command.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	select {
	case <-entered:
	case <-done:
		t.Fatalf("core failed before making its initial request: %v\n%s", waitErr, logs.String())
	case <-ctx.Done():
		t.Fatalf("initial request did not reach the origin:\n%s", logs.String())
	}
	select {
	case <-done:
		// A future core may apply its own response deadline. An explicit
		// initial-download failure is valid; starting with no rules is not.
	case <-time.After(2200 * time.Millisecond):
	}
	if strings.Contains(logs.String(), "sing-box started") {
		t.Fatalf("core started without any rule-set data:\n%s", logs.String())
	}
	unblock()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("initial HTTP failure did not stop startup:\n%s", logs.String())
	}
	if ctx.Err() != nil || waitErr == nil || strings.Contains(logs.String(), "sing-box started") || !strings.Contains(logs.String(), "initial rule-set: update-test") {
		t.Fatalf("stalled first download was not rejected after the origin returned 504: %v\n%s", waitErr, logs.String())
	}
}

func remoteUpdateCorePath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("SING_BOX_TEST_BINARY")
	if path == "" {
		t.Skip("set SING_BOX_TEST_BINARY to run isolated real-core rule update tests")
	}
	return path
}

func remoteUpdateFailure(w http.ResponseWriter, failure string) {
	switch failure {
	case "http-503":
		http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
	case "http-504":
		http.Error(w, "fixture gateway timeout", http.StatusGatewayTimeout)
	case "malformed-json":
		_, _ = io.WriteString(w, `{"version":3,"rules":[`)
	case "invalid-json-rule":
		// Valid JSON with a valid first rule followed by an invalid rule also
		// checks that partially parsed updates cannot replace live rules.
		_, _ = io.WriteString(w, `{"version":3,"rules":[{"domain":["new.rule-update.test"]},{"ip_cidr":["invalid-prefix"]}]}`)
	case "invalid-srs":
		_, _ = w.Write([]byte{'S', 'R', 'S', 3, 0})
	}
}

func remoteUpdateRules(t *testing.T, corePath, root, domain string, binary bool) []byte {
	t.Helper()
	data := []byte(fmt.Sprintf(`{"version":3,"rules":[{"domain":[%q]}]}`, domain))
	if !binary {
		return data
	}
	sourcePath, binaryPath := filepath.Join(root, domain+".json"), filepath.Join(root, domain+".srs")
	if err := os.WriteFile(sourcePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := runHTTPClientCoreCommand(corePath, "rule-set", "compile", "--output", binaryPath, sourcePath); err != nil {
		t.Fatalf("compile local rule fixture: %v\n%s", err, output)
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func remoteUpdateConfig(t *testing.T, root, origin string, binary bool) (string, string, string) {
	t.Helper()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "matched cached domain rule")
	}))
	t.Cleanup(target.Close)
	proxyAddress := defaultProfileUnusedAddress(t)
	proxyAddr, err := net.ResolveTCPAddr("tcp", proxyAddress)
	if err != nil {
		t.Fatal(err)
	}
	format := profilev1.RulesetFormat_RULESET_FORMAT_SOURCE
	if binary {
		format = profilev1.RulesetFormat_RULESET_FORMAT_BINARY
	}
	cachePath := filepath.Join(root, "cache.db")
	profile := &profilev1.Profile{
		Experimental: &profilev1.Experimental{CacheFile: &profilev1.CacheFileExperimental{Enabled: true, Path: cachePath, CacheId: "update-regression"}},
		Inbounds: []*profilev1.Inbound{{Id: "mixed", Tag: "mixed", Enable: true, Type: profilev1.InboundType_INBOUND_TYPE_MIXED,
			Mixed: &profilev1.InboundUserConfig{Listen: &profilev1.InboundListen{Listen: "127.0.0.1", ListenPort: int32(proxyAddr.Port)}}}},
		Outbounds: []*profilev1.Outbound{{Id: "direct", Tag: "direct", Type: profilev1.OutboundType_OUTBOUND_TYPE_DIRECT}},
		Route: &profilev1.Route{Final: "direct", RuleSet: []*profilev1.RuleSet{{
			Id: "update-test", Tag: "update-test", Type: profilev1.RulesetType_RULESET_TYPE_REMOTE,
			Format: format, Url: origin + "/rules", UpdateInterval: "1s", DownloadDetour: "direct",
		}}, Rules: []*profilev1.RouteRule{
			{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_SNIFF},
			{Enable: true, RuleSet: []string{"update-test"}, Action: profilev1.RuleAction_RULE_ACTION_ROUTE,
				ActionOptions: &profilev1.ActionOptions{Outbound: "direct", OverrideAddress: "127.0.0.1", OverridePort: uint32(target.Listener.Addr().(*net.TCPAddr).Port)}},
			{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_REJECT},
		}},
	}
	generated, err := NewService(storage.NewPaths(root), "test").Generate(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	delete(generated, "services") // no managed/fixed API port in this isolated test
	generated["log"] = map[string]any{"level": "debug", "timestamp": false}
	data, err := json.Marshal(generated)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, proxyAddress, cachePath
}

func remoteUpdateAssertRoutes(t *testing.T, proxyAddress, accepted, rejected string, logs *httpClientCoreLog) {
	t.Helper()
	if err := requestRouterRulesTarget(proxyAddress, accepted); err != nil {
		t.Fatalf("expected active rule %q to route: %v\n%s", accepted, err, logs.String())
	}
	if err := requestRouterRulesTarget(proxyAddress, rejected); err == nil {
		t.Fatalf("inactive rule %q unexpectedly routed\n%s", rejected, logs.String())
	}
}

func remoteUpdateWait(t *testing.T, logs *httpClientCoreLog, description string, ready func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s:\n%s", description, logs.String())
		case <-ticker.C:
		}
	}
}
