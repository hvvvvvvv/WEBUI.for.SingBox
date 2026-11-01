package config

import (
	"bufio"
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

	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

// This opt-in integration test uses only loopback listeners. The synthetic IP
// destination is either overridden to the local HTTP server or rejected; there
// are no TUN interfaces, system route changes, or external network requests.
func TestRouterRemoteRulesCacheAndSniffWithCore(t *testing.T) {
	corePath := os.Getenv("SING_BOX_TEST_BINARY")
	if corePath == "" {
		t.Skip("set SING_BOX_TEST_BINARY to a sing-box binary to run the isolated router rule checks")
	}
	root := t.TempDir()
	cachePath := filepath.Join(root, "cache.db")
	var downloads, targetRequests atomic.Int64
	rulesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":3,"rules":[{"domain":["router-check.test"]}]}`)
	}))
	t.Cleanup(rulesServer.Close)
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		if r.Host != "router-check.test" {
			http.Error(w, "unexpected Host", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "matched cached domain rule")
	}))
	t.Cleanup(targetServer.Close)
	_, targetPortString, err := net.SplitHostPort(targetServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	targetPort, err := strconv.Atoi(targetPortString)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyAddress := listener.Addr().String()
	proxyPort := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	profile := &profilev1.Profile{
		Experimental: &profilev1.Experimental{CacheFile: &profilev1.CacheFileExperimental{
			Enabled: true, Path: cachePath, CacheId: "router-rules-test",
		}},
		Inbounds: []*profilev1.Inbound{{
			Id: "mixed", Tag: "test-mixed", Type: profilev1.InboundType_INBOUND_TYPE_MIXED, Enable: true,
			Mixed: &profilev1.InboundUserConfig{Listen: &profilev1.InboundListen{
				Listen: "127.0.0.1", ListenPort: int32(proxyPort),
			}},
		}},
		Outbounds: []*profilev1.Outbound{{Id: "direct", Tag: "direct", Type: profilev1.OutboundType_OUTBOUND_TYPE_DIRECT}},
		Route: &profilev1.Route{
			Final: "direct",
			RuleSet: []*profilev1.RuleSet{{
				Id: "remote-domain", Tag: "remote-domain", Type: profilev1.RulesetType_RULESET_TYPE_REMOTE,
				Format: profilev1.RulesetFormat_RULESET_FORMAT_SOURCE, Url: rulesServer.URL + "/rules.json",
				DownloadDetour: "direct", UpdateInterval: "24h",
			}},
			Rules: []*profilev1.RouteRule{
				{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_SNIFF},
				{
					Enable: true, RuleSet: []string{"remote-domain"}, Action: profilev1.RuleAction_RULE_ACTION_ROUTE,
					ActionOptions: &profilev1.ActionOptions{
						Outbound: "direct", OverrideAddress: "127.0.0.1", OverridePort: uint32(targetPort),
					},
				},
				{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_REJECT},
			},
		},
	}
	writeConfig := func() string {
		generated, err := NewService(storage.NewPaths(root), "test").Generate(profile, nil)
		if err != nil {
			t.Fatal(err)
		}
		// The native API is irrelevant here; do not bind its managed fixed port.
		delete(generated, "services")
		generated["log"] = map[string]any{"level": "debug", "timestamp": false}
		data, err := json.MarshalIndent(generated, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(root, "config.json")
		if err := os.WriteFile(configPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return configPath
	}
	configPath := writeConfig()

	stop, logs := startRouterRulesCore(t, corePath, configPath)
	if err := requestRouterRulesTarget(proxyAddress, "router-check.test"); err != nil {
		t.Fatalf("downloaded domain rule did not route the sniffed IP connection: %v\n%s", err, logs.String())
	}
	if downloads.Load() == 0 || targetRequests.Load() != 1 {
		t.Fatalf("expected a remote download and one routed request, got downloads=%d targets=%d", downloads.Load(), targetRequests.Load())
	}
	// A different HTTP Host must not reach the target even with sniff enabled.
	if err := requestRouterRulesTarget(proxyAddress, "unmatched.test"); err == nil || targetRequests.Load() != 1 {
		t.Fatalf("unmatched domain bypassed the reject rule: error=%v targets=%d\n%s", err, targetRequests.Load(), logs.String())
	}
	stop()
	if info, err := os.Stat(cachePath); err != nil || info.Size() == 0 {
		t.Fatalf("core did not persist its rule-set cache: %v", err)
	}
	rulesServer.Close()
	downloadCount := downloads.Load()
	t.Log("downloaded remote source rules and routed an IP-only CONNECT using its HTTP Host")

	stop, logs = startRouterRulesCore(t, corePath, configPath)
	if err := requestRouterRulesTarget(proxyAddress, "router-check.test"); err != nil {
		t.Fatalf("cached rule set failed after its HTTP origin was stopped: %v\n%s", err, logs.String())
	}
	if downloads.Load() != downloadCount || targetRequests.Load() != 2 {
		t.Fatalf("expected one cached route without a fresh download, got downloads=%d targets=%d", downloads.Load(), targetRequests.Load())
	}
	stop()
	t.Log("restarted with the rule-set origin stopped and routed using the persisted cache")

	profile.Route.Rules[0].Enable = false
	stop, logs = startRouterRulesCore(t, corePath, writeConfig())
	if err := requestRouterRulesTarget(proxyAddress, "router-check.test"); err == nil || targetRequests.Load() != 2 {
		t.Fatalf("IP destination unexpectedly matched a domain rule without sniff: error=%v targets=%d\n%s", err, targetRequests.Load(), logs.String())
	}
	stop()
	t.Log("the same IP-only CONNECT was rejected when sniff was disabled")
}

func startRouterRulesCore(t *testing.T, corePath, configPath string) (func(), *httpClientCoreLog) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	var stopped sync.Once
	stop := func() {
		stopped.Do(func() {
			cancel()
			<-done
		})
	}
	t.Cleanup(stop)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			t.Fatalf("isolated core exited before startup: %v\n%s", waitErr, logs.String())
		case <-ctx.Done():
			stop()
			t.Fatalf("isolated core timed out during startup:\n%s", logs.String())
		case <-ticker.C:
			if strings.Contains(logs.String(), "sing-box started") {
				return stop, logs
			}
		}
	}
}

func requestRouterRulesTarget(proxyAddress, host string) error {
	connection, err := net.DialTimeout("tcp", proxyAddress, 3*time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if _, err := io.WriteString(connection, "CONNECT 198.18.0.2:80 HTTP/1.1\r\nHost: 198.18.0.2:80\r\n\r\n"); err != nil {
		return err
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return fmt.Errorf("CONNECT response: %s", response.Status)
	}
	if _, err := fmt.Fprintf(connection, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host); err != nil {
		return err
	}
	response, err = http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || string(body) != "matched cached domain rule" {
		return fmt.Errorf("target response: %s, body=%q", response.Status, body)
	}
	return nil
}
