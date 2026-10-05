package kernel

import (
	"context"
	"errors"
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
	resolveBase string
}

func (fakeProcesses) Exec(string, []string, platform.ExecOptions) platform.Result {
	return platform.Result{Flag: true, Data: "sing-box version 1.14.2"}
}
func (fakeProcesses) ExecBackground(string, []string, string, platform.ExecOptions) platform.Result {
	return platform.Result{Flag: true, Data: "1"}
}
func (fakeProcesses) ProcessMemory(int32) platform.Result {
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

type fakeGenerator struct{}

func (*fakeGenerator) Generate(*profilev1.Profile, *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	return map[string]any{}, nil
}
func (*fakeGenerator) WriteGeneratedConfig(map[string]any) error { return nil }

type fakeConfig struct{ value config.AppConfig }

func (f fakeConfig) Current() config.AppConfig { return f.value }

type fakeProfiles struct {
	profile *profilev1.Profile
	err     error
}

func (f *fakeProfiles) FindByID(string) (*profilev1.Profile, error) {
	if f.err != nil {
		return nil, f.err
	}
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
