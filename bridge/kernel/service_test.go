package kernel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
)

type fakeProcesses struct {
	memoryResult      *platform.Result
	processInfoResult *platform.Result
	resolveBase       string
}

func (fakeProcesses) Exec(string, []string, platform.ExecOptions) platform.Result {
	return platform.Result{Flag: true}
}
func (fakeProcesses) ExecBackground(string, []string, string, string, platform.ExecOptions) platform.Result {
	return platform.Result{Flag: true, Data: "1"}
}
func (f fakeProcesses) ProcessInfo(int32) platform.Result {
	if f.processInfoResult != nil {
		return *f.processInfoResult
	}
	return platform.Result{Flag: false}
}
func (f fakeProcesses) ProcessMemory(int32) platform.Result {
	if f.memoryResult != nil {
		return *f.memoryResult
	}
	return platform.Result{Flag: true, Data: "1024"}
}
func (fakeProcesses) KillProcess(int, int) platform.Result {
	return platform.Result{Flag: true}
}
func (f fakeProcesses) ResolvePath(path string) string {
	if f.resolveBase != "" {
		return filepath.Join(f.resolveBase, path)
	}
	return path
}
func (fakeProcesses) BaseDir() string { return "/tmp/app" }

type fakeGenerator struct {
	generatedConfig   map[string]any
	generatedProfiles []*profilev1.Profile
}

func (f *fakeGenerator) Generate(profile *profilev1.Profile, _ *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	f.generatedProfiles = append(f.generatedProfiles, profile)
	if f.generatedConfig != nil {
		return f.generatedConfig, nil
	}
	return map[string]any{}, nil
}
func (*fakeGenerator) WriteGeneratedConfig(map[string]any) error { return nil }
func (*fakeGenerator) ReadGeneratedSecret() string               { return "" }

type fakeConfig struct{ value config.AppConfig }

func (f fakeConfig) Current() config.AppConfig { return f.value }

type fakeProfiles struct {
	findCalls int
	profile   *profilev1.Profile
}

func (f *fakeProfiles) FindByID(string) (*profilev1.Profile, error) {
	f.findCalls++
	if f.profile != nil {
		return f.profile, nil
	}
	return &profilev1.Profile{Id: "profile"}, nil
}

type fakeEvents struct{}

func (fakeEvents) Publish(string, ...any) {}

func TestStartCoreRejectsMissingProfileID(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	_, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{}))
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	status, _ := service.Status()
	if status != kernelv1.CoreStatus_CORE_STATUS_STOPPED {
		t.Fatalf("expected stopped status, got %v", status)
	}
}

func TestAutoStartHonorsDisabledConfig(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{
		value: config.AppConfig{AutoStartKernel: false, Profile: "profile"},
	}, &fakeProfiles{}, fakeEvents{})
	service.AutoStart(context.Background())
	status, _ := service.Status()
	if status != kernelv1.CoreStatus_CORE_STATUS_STOPPED {
		t.Fatalf("expected auto start to remain stopped, got %v", status)
	}
}

func TestGetCurrentProfileRejectsWhenStopped(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})

	_, err := service.GetCurrentProfile(context.Background(), connect.NewRequest(&kernelv1.GetCurrentProfileRequest{}))
	if err == nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected failed precondition, got %v", err)
	}
}

func TestGetCurrentCoreMemoryRejectsWhenStopped(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})

	_, err := service.GetCurrentCoreMemory(context.Background(), connect.NewRequest(&kernelv1.GetCurrentCoreMemoryRequest{}))
	if err == nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected failed precondition, got %v", err)
	}
}

func TestGetCurrentCoreMemoryReturnsRSS(t *testing.T) {
	service := NewService(fakeProcesses{
		memoryResult: &platform.Result{Flag: true, Data: "2048"},
	}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.corePID = 123
	service.mu.Unlock()

	resp, err := service.GetCurrentCoreMemory(context.Background(), connect.NewRequest(&kernelv1.GetCurrentCoreMemoryRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetRss() != 2048 {
		t.Fatalf("expected rss 2048, got %d", resp.Msg.GetRss())
	}
}

func TestGetCurrentCoreMemoryReturnsProcessError(t *testing.T) {
	service := NewService(fakeProcesses{
		memoryResult: &platform.Result{Flag: false, Data: "process not found"},
	}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.corePID = 123
	service.mu.Unlock()

	_, err := service.GetCurrentCoreMemory(context.Background(), connect.NewRequest(&kernelv1.GetCurrentCoreMemoryRequest{}))
	if err == nil || connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("expected internal error, got %v", err)
	}
}

func TestGetCoreStatusReturnsStoppedWithInvalidPID(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})

	resp, err := service.GetCoreStatus(context.Background(), connect.NewRequest(&kernelv1.GetCoreStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetStatus() != kernelv1.CoreStatus_CORE_STATUS_STOPPED {
		t.Fatalf("expected stopped status, got %v", resp.Msg.GetStatus())
	}
	if resp.Msg.GetPid() != -1 {
		t.Fatalf("expected invalid pid -1, got %d", resp.Msg.GetPid())
	}
}

func TestGetCoreStatusReturnsRunningPID(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.corePID = 123
	service.mu.Unlock()

	resp, err := service.GetCoreStatus(context.Background(), connect.NewRequest(&kernelv1.GetCoreStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetStatus() != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		t.Fatalf("expected running status, got %v", resp.Msg.GetStatus())
	}
	if resp.Msg.GetPid() != 123 {
		t.Fatalf("expected running pid 123, got %d", resp.Msg.GetPid())
	}
}

func TestStartCoreReturnsPID(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return nil
	}
	t.Cleanup(func() {
		waitKernelAPIReadyFunc = previousWait
	})

	profiles := &fakeProfiles{profile: &profilev1.Profile{Id: "profile", Name: "persisted"}}
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, profiles, fakeEvents{})

	resp, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{
		ProfileId: "profile",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetPid() != 1 {
		t.Fatalf("expected pid 1, got %d", resp.Msg.GetPid())
	}

	profileResp, err := service.GetCurrentProfile(context.Background(), connect.NewRequest(&kernelv1.GetCurrentProfileRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if profileResp.Msg.GetProfile().GetName() != "persisted" {
		t.Fatalf("expected current persisted profile, got %#v", profileResp.Msg.GetProfile())
	}
	if profiles.findCalls != 1 {
		t.Fatalf("expected current profile to use cached profile without extra lookup, got %d find calls", profiles.findCalls)
	}
}

func TestStartCoreWithProfileRejectsMissingProfile(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})

	_, err := service.StartCoreWithProfile(context.Background(), connect.NewRequest(&kernelv1.StartCoreWithProfileRequest{}))
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}

	_, err = service.StartCoreWithProfile(context.Background(), connect.NewRequest(&kernelv1.StartCoreWithProfileRequest{
		Profile: &profilev1.Profile{},
	}))
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument for missing profile id, got %v", err)
	}
}

func TestStartCoreWithProfileRejectsWhenRunning(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.mu.Unlock()

	_, err := service.StartCoreWithProfile(context.Background(), connect.NewRequest(&kernelv1.StartCoreWithProfileRequest{
		Profile: &profilev1.Profile{Id: "temporary"},
	}))
	if err == nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected failed precondition, got %v", err)
	}
}

func TestStartCoreWithProfileUsesRequestProfile(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return nil
	}
	t.Cleanup(func() {
		waitKernelAPIReadyFunc = previousWait
	})

	generator := &fakeGenerator{
		generatedConfig: map[string]any{
			"log": map[string]any{"level": "debug"},
		},
	}
	profiles := &fakeProfiles{}
	service := NewService(fakeProcesses{}, generator, fakeConfig{}, profiles, fakeEvents{})

	startResp, err := service.StartCoreWithProfile(context.Background(), connect.NewRequest(&kernelv1.StartCoreWithProfileRequest{
		Profile: &profilev1.Profile{Id: "temporary"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if startResp.Msg.GetPid() != 1 {
		t.Fatalf("expected pid 1, got %d", startResp.Msg.GetPid())
	}
	if profiles.findCalls != 0 {
		t.Fatalf("temporary profile start should not load persisted profile, got %d calls", profiles.findCalls)
	}
	if len(generator.generatedProfiles) != 1 || generator.generatedProfiles[0].GetId() != "temporary" {
		t.Fatalf("expected generator to receive temporary profile, got %#v", generator.generatedProfiles)
	}

	profileResp, err := service.GetCurrentProfile(context.Background(), connect.NewRequest(&kernelv1.GetCurrentProfileRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if profileResp.Msg.GetProfile().GetId() != "temporary" {
		t.Fatalf("expected current temporary profile, got %#v", profileResp.Msg.GetProfile())
	}
	if profiles.findCalls != 0 {
		t.Fatalf("temporary current profile should not load persisted profile, got %d calls", profiles.findCalls)
	}
}

func TestStopCoreClearsCurrentProfile(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return nil
	}
	t.Cleanup(func() {
		waitKernelAPIReadyFunc = previousWait
	})

	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	if _, err := service.StartCoreWithProfile(context.Background(), connect.NewRequest(&kernelv1.StartCoreWithProfileRequest{
		Profile: &profilev1.Profile{Id: "temporary"},
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil {
		t.Fatal(err)
	}

	_, err := service.GetCurrentProfile(context.Background(), connect.NewRequest(&kernelv1.GetCurrentProfileRequest{}))
	if err == nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected failed precondition after stop, got %v", err)
	}
}

func TestGetCurrentProfileFallsBackToAttachedProfileID(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, corePidFilePath)
	if err := os.MkdirAll(filepath.Dir(pidFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidFile, []byte("123"), 0644); err != nil {
		t.Fatal(err)
	}

	profiles := &fakeProfiles{profile: &profilev1.Profile{Id: "profile", Name: "attached"}}
	service := NewService(fakeProcesses{
		processInfoResult: &platform.Result{Flag: true, Data: "sing-box"},
		resolveBase:       tmpDir,
	}, &fakeGenerator{}, fakeConfig{}, profiles, fakeEvents{})

	if !service.attachExistingCoreFromPID("profile") {
		t.Fatal("expected service to attach existing core")
	}

	resp, err := service.GetCurrentProfile(context.Background(), connect.NewRequest(&kernelv1.GetCurrentProfileRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetProfile().GetName() != "attached" {
		t.Fatalf("expected attached profile fallback, got %#v", resp.Msg.GetProfile())
	}
	if profiles.findCalls != 1 {
		t.Fatalf("expected one profile lookup for attached fallback, got %d", profiles.findCalls)
	}
}

func TestRestartCoreReturnsPID(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return nil
	}
	t.Cleanup(func() {
		waitKernelAPIReadyFunc = previousWait
	})

	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.corePID = 1
	service.activeProfileID = "profile"
	service.mu.Unlock()

	resp, err := service.RestartCore(context.Background(), connect.NewRequest(&kernelv1.RestartCoreRequest{
		ProfileId: "profile",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetPid() != 1 {
		t.Fatalf("expected restarted pid 1, got %d", resp.Msg.GetPid())
	}
}
