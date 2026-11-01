package kernel

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

// Use the production guard protocol when the platform runner relaunches this
// test executable. No backend build or unguarded substitute is needed.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == platform.CoreGuardCommand {
		if err := platform.RunCoreGuard(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type startupCoreProcesses struct {
	*platform.Service
	binary string
}

func (p startupCoreProcesses) Exec(_ string, args []string, options platform.ExecOptions) platform.Result {
	return p.Service.Exec(p.binary, args, options)
}

func (p startupCoreProcesses) ExecBackground(_ string, args []string, event string, options platform.ExecOptions) platform.Result {
	return p.Service.ExecBackground(p.binary, args, event, options)
}

func newRuleDownloadCore(t *testing.T, origin string) (*Service, *recordingEvents) {
	t.Helper()
	binary := os.Getenv("SINGBOX_NATIVE_TEST_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_NATIVE_TEST_BINARY to run isolated real-core startup tests")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	previous := config.CoreAPIController
	config.CoreAPIController = listener.Addr().String()
	listener.Close()
	t.Cleanup(func() { config.CoreAPIController = previous })
	paths := storage.NewPaths(t.TempDir())
	processes := startupCoreProcesses{Service: platform.NewService(paths, nil, platform.Environment{}), binary: binary}
	profile := &profilev1.Profile{
		Id:        "slow-rule-download",
		Outbounds: []*profilev1.Outbound{{Id: "direct", Tag: "direct", Type: profilev1.OutboundType_OUTBOUND_TYPE_DIRECT}},
		Route: &profilev1.Route{Final: "direct", RuleSet: []*profilev1.RuleSet{{
			Id: "bootstrap", Tag: "bootstrap", Type: profilev1.RulesetType_RULESET_TYPE_REMOTE,
			Format: profilev1.RulesetFormat_RULESET_FORMAT_SOURCE, Url: origin, UpdateInterval: "24h", DownloadDetour: "direct",
		}}, Rules: []*profilev1.RouteRule{{Enable: true, RuleSet: []string{"bootstrap"}, Action: profilev1.RuleAction_RULE_ACTION_ROUTE,
			ActionOptions: &profilev1.ActionOptions{Outbound: "direct"}}}},
	}
	app := fakeConfig{value: config.AppConfig{Branch: "main", Main: config.CoreRuntimeConfig{
		Args: []string{"run", "--disable-color", "-c", paths.Resolve(coreConfigRelativePath), "-D", paths.Resolve(coreWorkingDirectory)},
	}}}
	events := &recordingEvents{}
	service := NewService(processes, config.NewService(paths, "test"), app, &fakeProfiles{profile: profile}, events)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := service.Close(ctx); err != nil {
			t.Errorf("close isolated core: %v", err)
		}
	})
	return service, events
}

func TestStartupWaitsForSlowInitialRuleDownloadWithCore(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises a real initial download lasting longer than the former 15-second deadline")
	}
	var downloads atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		timer := time.NewTimer(16 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			fmt.Fprint(w, `{"version":3,"rules":[{"domain":["isolated.test"]}]}`)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(origin.Close)
	service, events := newRuleDownloadCore(t, origin.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	started := time.Now()
	response, err := service.StartCore(ctx, connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "slow-rule-download"}))
	if err != nil {
		t.Fatalf("valid core was killed while downloading its initial rule set: %v", err)
	}
	if time.Since(started) < 16*time.Second || downloads.Load() != 1 || response.Msg.GetPid() <= 0 {
		t.Fatalf("cold download was skipped/retried: elapsed=%s downloads=%d pid=%d", time.Since(started), downloads.Load(), response.Msg.GetPid())
	}
	if status, _ := service.Status(); status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || len(events.named("kernelCrashed")) != 0 {
		t.Fatalf("slow initialization did not reach RUNNING cleanly: %v", status)
	}
}

func TestStopCancelsStalledInitialRuleDownloadWithCore(t *testing.T) {
	entered, disconnected := make(chan struct{}, 1), make(chan struct{}, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-r.Context().Done()
		disconnected <- struct{}{}
	}))
	t.Cleanup(origin.Close)
	service, events := newRuleDownloadCore(t, origin.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := service.StartCore(ctx, connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "slow-rule-download"}))
		result <- err
	}()
	select {
	case <-entered:
	case err := <-result:
		t.Fatalf("start returned before the fixture download: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("core never requested its initial rule set")
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if _, err := service.StopCore(stopCtx, connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatalf("expected canceled startup, got %v", err)
		}
	case <-stopCtx.Done():
		t.Fatal("stop did not interrupt initialization")
	}
	select {
	case <-disconnected:
	case <-stopCtx.Done():
		t.Fatal("stopped core retained its pending download")
	}
	if status, _ := service.Status(); status != kernelv1.CoreStatus_CORE_STATUS_STOPPED || len(events.named("kernelCrashed")) != 0 {
		t.Fatalf("intentional stop reported as a crash or restarted the core: %v", status)
	}
}
