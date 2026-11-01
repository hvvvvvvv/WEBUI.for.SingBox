package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/platform"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

type bootstrapPreparationGenerator struct {
	fakeGenerator
	err     error
	called  bool
	initial string
}

func (g *bootstrapPreparationGenerator) Generate(*profilev1.Profile, *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	return map[string]any{"route": map[string]any{"rule_set": []any{map[string]any{
		"tag": "default-rule", "type": "remote", "format": "binary", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs",
	}}}}, nil
}

func (g *bootstrapPreparationGenerator) PrepareRuleSetBootstrap(ctx context.Context, root map[string]any) error {
	g.called = true
	if g.err != nil {
		return g.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rule := root["route"].(map[string]any)["rule_set"].([]any)[0].(map[string]any)
	rule["initial_path"] = g.initial
	return nil
}

type bootstrapCheckingProcesses struct {
	preflightProcesses
	checked map[string]any
	readErr error
}

func (p *bootstrapCheckingProcesses) Exec(path string, args []string, options platform.ExecOptions) platform.Result {
	if args[0] == "check" {
		var data []byte
		data, p.readErr = os.ReadFile(args[3])
		if p.readErr == nil {
			p.readErr = json.Unmarshal(data, &p.checked)
		}
	}
	return p.preflightProcesses.Exec(path, args, options)
}

func TestPreparedCoreChecksInstalledRuleSeedPaths(t *testing.T) {
	processes := &bootstrapCheckingProcesses{preflightProcesses: preflightProcesses{
		fakeProcesses: fakeProcesses{resolveBase: t.TempDir()}, version: "1.14.4",
	}}
	generator := &bootstrapPreparationGenerator{initial: processes.ResolvePath("data/bootstrap/cn.srs")}
	service := NewService(processes, generator, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	defer service.lifecycleCancel()
	prepared, err := service.prepareCoreStart(context.Background(), &profilev1.Profile{}, kernelRuntimeConfig{}, "core")
	if err != nil || processes.readErr != nil {
		t.Fatalf("preparation failed: %v; reading the checked config: %v", err, processes.readErr)
	}
	if !generator.called || processes.checks != 1 {
		t.Fatalf("bootstrap/check not run: bootstrap=%v checks=%d", generator.called, processes.checks)
	}
	for _, root := range []map[string]any{processes.checked, prepared.config} {
		rule := root["route"].(map[string]any)["rule_set"].([]any)[0].(map[string]any)
		if rule["initial_path"] != generator.initial {
			t.Fatalf("check or launch config lost installed initial_path: %#v", rule)
		}
	}
}

func TestRuleSeedPreparationFailureKeepsRunningCore(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error { return nil }
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })
	processes := &preflightProcesses{fakeProcesses: fakeProcesses{resolveBase: t.TempDir()}, version: "1.14.4"}
	generator := &bootstrapPreparationGenerator{err: os.ErrPermission}
	service := NewService(processes, generator, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	defer service.lifecycleCancel()
	startNativeTestInstance(t, service, 11, "running-secret")
	_, err := service.RestartCore(context.Background(), connect.NewRequest(&kernelv1.RestartCoreRequest{ProfileId: "profile"}))
	if !errors.Is(err, os.ErrPermission) || !generator.called {
		t.Fatalf("disk preparation failure was lost: called=%v err=%v", generator.called, err)
	}
	if processes.checks != 0 || processes.starts != 0 || processes.kills != 0 {
		t.Fatalf("failed seed preparation checked/stopped/replaced the core: %#v", processes)
	}
	_, _, secret, cancel, err := service.NativeAPIContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if secret != "running-secret" || service.corePID != 11 {
		t.Fatal("failed seed preparation changed the running instance")
	}
}
