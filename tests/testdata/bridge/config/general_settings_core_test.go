package config

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	"guiforcores/gen/native/daemon"
	"guiforcores/gen/native/daemon/daemonconnect"
	profilev1 "guiforcores/gen/profile/v1"
)

// All listeners, cache and log files are temporary. No TUN, DNS query, remote
// rule download or public network access is needed to exercise the real API.
func TestGeneralNativeDefaultModesAndLoggingWithCore(t *testing.T) {
	core := os.Getenv("SING_BOX_TEST_BINARY")
	if core == "" {
		t.Skip("set SING_BOX_TEST_BINARY to run isolated native API integration")
	}
	core, err := filepath.Abs(core)
	if err != nil {
		t.Fatal(err)
	}
	oldController := CoreAPIController
	t.Cleanup(func() { CoreAPIController = oldController })
	for _, mode := range []string{"", "global", "rule", "direct"} {
		t.Run("default-"+mode, func(t *testing.T) {
			CoreAPIController = defaultProfileUnusedAddress(t)
			root := t.TempDir()
			profile := generalSettingsProfile(mode)
			profile.Experimental.CacheFile.Enabled = true
			config := generalSettingsGenerated(t, root, profile)
			client, stop, _ := startGeneralSettingsCore(t, core, root, config)
			want := mode
			if want == "" {
				want = "rule"
			}
			assertGeneralMode(t, client, config, want)
			if mode == "global" {
				setGeneralMode(t, client, config, "direct")
				stop()
				client, stop, _ = startGeneralSettingsCore(t, core, root, config)
				assertGeneralMode(t, client, config, "direct")
				profile.Experimental.CacheFile.Enabled = false
				stop()
				config = generalSettingsGenerated(t, root, profile)
				client, stop, _ = startGeneralSettingsCore(t, core, root, config)
				assertGeneralMode(t, client, config, "global")
			}
			stop()
		})
	}
	for _, tc := range []struct {
		name     string
		disabled bool
		level    profilev1.LogLevel
		native   daemon.LogLevel
	}{
		{"file-info", false, profilev1.LogLevel_LOG_LEVEL_INFO, daemon.LogLevel_INFO},
		{"file-warn", false, profilev1.LogLevel_LOG_LEVEL_WARN, daemon.LogLevel_WARN},
		{"disabled", true, profilev1.LogLevel_LOG_LEVEL_INFO, daemon.LogLevel_TRACE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			CoreAPIController = defaultProfileUnusedAddress(t)
			root := t.TempDir()
			profile := generalSettingsProfile("rule")
			profile.Log = &profilev1.Log{Disabled: tc.disabled, Level: tc.level, Output: "chosen.log", Timestamp: true}
			config := generalSettingsGenerated(t, root, profile)
			client, stop, console := startGeneralSettingsCore(t, core, root, config)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			level, err := client.GetDefaultLogLevel(ctx, defaultProfileNativeRequest(&emptypb.Empty{}, NativeAPISecret(config)))
			if err != nil || level.Msg.GetLevel() != tc.native {
				t.Fatalf("actual log level=%v err=%v", level, err)
			}
			setGeneralMode(t, client, config, "direct")
			assertGeneralMode(t, client, config, "direct")
			stream, err := client.SubscribeLog(ctx, defaultProfileNativeRequest(&emptypb.Empty{}, NativeAPISecret(config)))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if !stream.Receive() {
				t.Fatalf("native log snapshot unavailable: %v", stream.Err())
			}
			found := false
			for _, entry := range stream.Msg().GetMessages() {
				if strings.Contains(entry.GetMessage(), "updated mode: direct") {
					found = true
				}
			}
			if found == tc.disabled {
				t.Fatalf("native log visibility: found=%v disabled=%v snapshot=%v", found, tc.disabled, stream.Msg())
			}
			stop()
			data, err := os.ReadFile(filepath.Join(root, "chosen.log"))
			if tc.disabled {
				if !os.IsNotExist(err) || console.String() != "" {
					t.Fatalf("disabled log produced output: err=%v console=%q", err, console.String())
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				logged := strings.Contains(string(data), "updated mode: direct")
				if logged != (tc.level == profilev1.LogLevel_LOG_LEVEL_INFO) {
					t.Fatalf("console level ignored: %s", data)
				}
				if strings.Contains(console.String(), "updated mode: direct") {
					t.Fatal("file output was redirected to console")
				}
			}
		})
	}
}

func generalSettingsProfile(mode string) *profilev1.Profile {
	rules := []*profilev1.RouteRule{}
	for _, value := range []string{"global", "rule", "direct"} {
		rules = append(rules, &profilev1.RouteRule{Id: value, Enable: true, Action: profilev1.RuleAction_RULE_ACTION_ROUTE, ClashMode: value, ActionOptions: &profilev1.ActionOptions{Outbound: "direct"}})
	}
	return &profilev1.Profile{Id: "general-test", Log: &profilev1.Log{Level: profilev1.LogLevel_LOG_LEVEL_INFO}, Experimental: &profilev1.Experimental{DefaultMode: mode, CacheFile: &profilev1.CacheFileExperimental{CacheId: "general-test"}}, Outbounds: []*profilev1.Outbound{{Id: "direct", Tag: "direct", Type: profilev1.OutboundType_OUTBOUND_TYPE_DIRECT}}, Route: &profilev1.Route{Final: "direct", Rules: rules}}
}

func generalSettingsGenerated(t *testing.T, root string, profile *profilev1.Profile) map[string]any {
	t.Helper()
	config, err := NewService(storage.NewPaths(root), "test").Generate(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	FinalizeGeneratedConfig(config)
	return config
}

func assertGeneralMode(t *testing.T, client daemonconnect.StartedServiceClient, config map[string]any, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mode, err := client.GetClashModeStatus(ctx, defaultProfileNativeRequest(&emptypb.Empty{}, NativeAPISecret(config)))
	if err != nil || !strings.EqualFold(mode.Msg.GetCurrentMode(), want) {
		t.Fatalf("mode=%v want=%s err=%v", mode, want, err)
	}
}

func setGeneralMode(t *testing.T, client daemonconnect.StartedServiceClient, config map[string]any, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.SetClashMode(ctx, defaultProfileNativeRequest(&daemon.ClashMode{Mode: mode}, NativeAPISecret(config))); err != nil {
		t.Fatal(err)
	}
}

func startGeneralSettingsCore(t *testing.T, core, root string, config map[string]any) (daemonconnect.StartedServiceClient, func(), *httpClientCoreLog) {
	t.Helper()
	// A conflicting occupied listener proves the removed Clash controller is
	// never bound; merely checking the generated map would not prove runtime behavior.
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reserved.Close() })
	experimental := config["experimental"].(map[string]any)
	legacy, _ := experimental["clash_api"].(map[string]any)
	if legacy == nil {
		legacy = map[string]any{}
		experimental["clash_api"] = legacy
	}
	legacy["external_controller"] = reserved.Addr().String()
	legacy["secret"] = "not-used"
	if err := EnforceNativeAPIConfig(config); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "general.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer checkCancel()
	check := exec.CommandContext(checkCtx, core, "check", "-c", path)
	check.Dir, check.Env = root, httpClientCoreEnvironment()
	platform.SetCmdWindowHidden(check)
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("check: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	command := exec.CommandContext(ctx, core, "run", "--disable-color", "-c", path)
	command.Dir, command.Env, command.WaitDelay = root, httpClientCoreEnvironment(), time.Second
	platform.SetCmdWindowHidden(command)
	console := &httpClientCoreLog{}
	command.Stdout, command.Stderr = console, console
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = command.Wait(); close(done) }()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); <-done; reserved.Close() }) }
	t.Cleanup(stop)
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	client := daemonconnect.NewStartedServiceClient(&http.Client{Transport: transport, Timeout: time.Second}, "http://"+CoreAPIController, connect.WithGRPCWeb())
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("core readiness timed out: %s", console.String())
		case <-done:
			t.Fatalf("core exited before readiness: %s", console.String())
		case <-ticker.C:
			if _, err := client.GetVersion(ctx, defaultProfileNativeRequest(&emptypb.Empty{}, NativeAPISecret(config))); err == nil {
				return client, stop, console
			}
		}
	}
}
