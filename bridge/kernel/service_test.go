package kernel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	killResult        *platform.Result
	resolveBase       string
}

func (fakeProcesses) Exec(string, []string, platform.ExecOptions) platform.Result {
	return platform.Result{Flag: true}
}
func (fakeProcesses) ExecBackground(string, []string, string, platform.ExecOptions) platform.Result {
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

func (f fakeProcesses) KillProcess(int, int) platform.Result {
	if f.killResult != nil {
		return *f.killResult
	}
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
	generateErr       error
}

func (f *fakeGenerator) Generate(profile *profilev1.Profile, _ *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	f.generatedProfiles = append(f.generatedProfiles, profile)
	if f.generateErr != nil {
		return nil, f.generateErr
	}
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

type publishedEvent struct {
	name string
	data []any
}

type recordingEvents struct {
	mu     sync.Mutex
	events []publishedEvent
}

func (e *recordingEvents) Publish(name string, data ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, publishedEvent{name: name, data: data})
}

func (e *recordingEvents) coreStates() []map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	states := make([]map[string]any, 0)
	for _, event := range e.events {
		if event.name != kernelStateChangedEvent || len(event.data) == 0 {
			continue
		}
		if state, ok := event.data[0].(map[string]any); ok {
			states = append(states, state)
		}
	}
	return states
}

func (e *recordingEvents) named(name string) []publishedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	events := make([]publishedEvent, 0)
	for _, item := range e.events {
		if item.name == name {
			events = append(events, item)
		}
	}
	return events
}

type exitCallbackProcesses struct {
	fakeProcesses
	mu       sync.Mutex
	onExit   func(int, error)
	execPID  int
	killExit bool
}

func (p *exitCallbackProcesses) ExecBackground(_ string, _ []string, _ string, options platform.ExecOptions) platform.Result {
	p.mu.Lock()
	p.onExit = options.OnExit
	pid := p.execPID
	p.mu.Unlock()
	if pid <= 0 {
		pid = 1
	}
	return platform.Result{Flag: true, Data: strconv.Itoa(pid)}
}

func (p *exitCallbackProcesses) KillProcess(pid int, _ int) platform.Result {
	p.mu.Lock()
	onExit := p.onExit
	killExit := p.killExit
	p.mu.Unlock()
	if killExit && onExit != nil {
		onExit(pid, nil)
	}
	return platform.Result{Flag: true}
}

func (p *exitCallbackProcesses) exit(pid int, err error) {
	p.mu.Lock()
	onExit := p.onExit
	p.mu.Unlock()
	if onExit != nil {
		onExit(pid, err)
	}
}

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

func assertCoreStateEvent(t *testing.T, state map[string]any, status kernelv1.CoreStatus, pid int) {
	t.Helper()
	if state["status"] != status || state["pid"] != pid {
		t.Fatalf("core state event = %#v, want status %v and pid %d", state, status, pid)
	}
}

func TestCoreStateEventsFollowStartAndStop(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return nil
	}
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	processes := &exitCallbackProcesses{execPID: 7, killExit: true}
	events := &recordingEvents{}
	service := NewService(processes, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, events)

	if _, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "profile"})); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil {
		t.Fatal(err)
	}

	states := events.coreStates()
	if len(states) != 4 {
		t.Fatalf("core state event count = %d, want 4: %#v", len(states), states)
	}
	assertCoreStateEvent(t, states[0], kernelv1.CoreStatus_CORE_STATUS_STARTING, -1)
	assertCoreStateEvent(t, states[1], kernelv1.CoreStatus_CORE_STATUS_RUNNING, 7)
	assertCoreStateEvent(t, states[2], kernelv1.CoreStatus_CORE_STATUS_STOPPING, -1)
	assertCoreStateEvent(t, states[3], kernelv1.CoreStatus_CORE_STATUS_STOPPED, -1)
}

func TestUnexpectedCoreExitPublishesCrashedState(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return nil
	}
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	processes := &exitCallbackProcesses{execPID: 7}
	events := &recordingEvents{}
	service := NewService(processes, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, events)
	if _, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "profile"})); err != nil {
		t.Fatal(err)
	}

	processes.exit(7, errors.New("unexpected exit"))
	response, err := service.GetCoreStatus(context.Background(), connect.NewRequest(&kernelv1.GetCoreStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetStatus() != kernelv1.CoreStatus_CORE_STATUS_CRASHED || response.Msg.GetPid() != -1 {
		t.Fatalf("core status after exit = %v, pid %d", response.Msg.GetStatus(), response.Msg.GetPid())
	}
	states := events.coreStates()
	assertCoreStateEvent(t, states[len(states)-1], kernelv1.CoreStatus_CORE_STATUS_CRASHED, -1)
	crashes := events.named("kernelCrashed")
	if len(crashes) != 1 || len(crashes[0].data) != 1 {
		t.Fatalf("kernel crash events = %#v, want one", crashes)
	}
	payload, ok := crashes[0].data[0].(map[string]any)
	if !ok || payload["pid"] != 7 || payload["reason"] != "unexpected exit" || payload["phase"] != "runtime" {
		t.Fatalf("unexpected kernel crash payload: %#v", crashes[0].data[0])
	}
}

func TestCoreCrashReasonIsSanitizedAndLimited(t *testing.T) {
	reason := "request https://user:secret@example.com/config?token=secret failed " + strings.Repeat("x", 600)
	sanitized := sanitizeCoreCrashReason(reason)
	if strings.Contains(sanitized, "secret") || strings.Contains(sanitized, "token=") {
		t.Fatalf("sanitized reason leaked credentials: %q", sanitized)
	}
	if !strings.Contains(sanitized, "https://example.com/config") {
		t.Fatalf("sanitized reason lost safe URL context: %q", sanitized)
	}
	if len([]rune(sanitized)) != maxCoreCrashReasonRunes || !strings.HasSuffix(sanitized, "…") {
		t.Fatalf("sanitized reason was not limited: %d runes", len([]rune(sanitized)))
	}
}

func TestStartFailurePublishesStartupCrash(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return errors.New("request https://user:secret@example.com/config?token=secret failed")
	}
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	events := &recordingEvents{}
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, events)
	_, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "profile"}))
	if err == nil {
		t.Fatal("expected start failure")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeUnavailable {
		t.Fatalf("expected unavailable connect error, got %v", err)
	}
	if connectErr.Meta().Get(coreErrorReasonHeader) != coreAPIUnavailable {
		t.Fatalf("missing core API unavailable metadata: %#v", connectErr.Meta())
	}
	crashes := events.named("kernelCrashed")
	if len(crashes) != 1 {
		t.Fatalf("kernel crash event count = %d, want 1", len(crashes))
	}
	payload := crashes[0].data[0].(map[string]any)
	reason, _ := payload["reason"].(string)
	if payload["phase"] != "startup" || strings.Contains(reason, "secret") || strings.Contains(reason, "token=") {
		t.Fatalf("unexpected startup crash payload: %#v", payload)
	}
}

func TestCoreExitDuringStartupDoesNotSetAPIUnavailableMetadata(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	processes := &exitCallbackProcesses{execPID: 7}
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		processes.exit(7, errors.New("process exited"))
		return nil
	}
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	service := NewService(processes, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	_, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "profile"}))
	if err == nil {
		t.Fatal("expected start failure")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeUnavailable {
		t.Fatalf("expected unavailable connect error, got %v", err)
	}
	if connectErr.Meta().Get(coreErrorReasonHeader) != "" {
		t.Fatalf("process exit was marked as API unavailable: %#v", connectErr.Meta())
	}
}

func TestStopFailurePublishesShutdownCrash(t *testing.T) {
	events := &recordingEvents{}
	service := NewService(fakeProcesses{
		killResult: &platform.Result{Flag: false, Data: "permission denied"},
	}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, events)
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.corePID = 7
	service.mu.Unlock()

	_, err := service.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{}))
	if err == nil {
		t.Fatal("expected stop failure")
	}
	crashes := events.named("kernelCrashed")
	if len(crashes) != 1 {
		t.Fatalf("kernel crash event count = %d, want 1", len(crashes))
	}
	payload := crashes[0].data[0].(map[string]any)
	if payload["phase"] != "shutdown" || payload["reason"] != "permission denied" {
		t.Fatalf("unexpected shutdown crash payload: %#v", payload)
	}
}

func TestStaleCoreExitDoesNotOverrideNewProcess(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return nil
	}
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	processes := &exitCallbackProcesses{execPID: 7}
	events := &recordingEvents{}
	service := NewService(processes, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, events)
	if _, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "profile"})); err != nil {
		t.Fatal(err)
	}
	service.setRunning(8, "profile", &profilev1.Profile{Id: "profile"})
	stateCount := len(events.coreStates())

	processes.exit(7, errors.New("old process exited"))
	response, err := service.GetCoreStatus(context.Background(), connect.NewRequest(&kernelv1.GetCoreStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetStatus() != kernelv1.CoreStatus_CORE_STATUS_RUNNING || response.Msg.GetPid() != 8 {
		t.Fatalf("stale exit changed core state to %v, pid %d", response.Msg.GetStatus(), response.Msg.GetPid())
	}
	if len(events.coreStates()) != stateCount {
		t.Fatal("stale process exit published a state change")
	}
}

func TestRestartStartFailureEndsInStoppedState(t *testing.T) {
	events := &recordingEvents{}
	service := NewService(
		fakeProcesses{},
		&fakeGenerator{generateErr: errors.New("generation failed")},
		fakeConfig{},
		&fakeProfiles{},
		events,
	)
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.corePID = 9
	service.activeProfileID = "profile"
	service.mu.Unlock()

	_, err := service.RestartCore(context.Background(), connect.NewRequest(&kernelv1.RestartCoreRequest{ProfileId: "profile"}))
	if err == nil {
		t.Fatal("expected restart failure")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Meta().Get(coreErrorReasonHeader) != "" {
		t.Fatalf("non-API-ready failure was marked as API unavailable: %#v", connectErr.Meta())
	}
	response, statusErr := service.GetCoreStatus(context.Background(), connect.NewRequest(&kernelv1.GetCoreStatusRequest{}))
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if response.Msg.GetStatus() != kernelv1.CoreStatus_CORE_STATUS_STOPPED || response.Msg.GetPid() != -1 {
		t.Fatalf("core status after failed restart = %v, pid %d", response.Msg.GetStatus(), response.Msg.GetPid())
	}
	states := events.coreStates()
	wantStatuses := []kernelv1.CoreStatus{
		kernelv1.CoreStatus_CORE_STATUS_STOPPING,
		kernelv1.CoreStatus_CORE_STATUS_STOPPED,
		kernelv1.CoreStatus_CORE_STATUS_STARTING,
		kernelv1.CoreStatus_CORE_STATUS_STOPPED,
	}
	if len(states) != len(wantStatuses) {
		t.Fatalf("core state event count = %d, want %d: %#v", len(states), len(wantStatuses), states)
	}
	for index, status := range wantStatuses {
		assertCoreStateEvent(t, states[index], status, -1)
	}
}

func TestRestartAPIReadyFailurePreservesMetadata(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return errors.New("API timeout")
	}
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.corePID = 9
	service.activeProfileID = "profile"
	service.mu.Unlock()

	_, err := service.RestartCore(context.Background(), connect.NewRequest(&kernelv1.RestartCoreRequest{ProfileId: "profile"}))
	if err == nil {
		t.Fatal("expected restart failure")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeUnavailable {
		t.Fatalf("expected unavailable connect error, got %v", err)
	}
	if connectErr.Meta().Get(coreErrorReasonHeader) != coreAPIUnavailable {
		t.Fatalf("restart lost core API unavailable metadata: %#v", connectErr.Meta())
	}
}
