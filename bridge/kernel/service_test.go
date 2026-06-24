package kernel

import (
	"context"
	"testing"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
)

type fakeProcesses struct{}

func (fakeProcesses) Exec(string, []string, platform.ExecOptions) platform.Result {
	return platform.Result{Flag: true}
}
func (fakeProcesses) ExecBackground(string, []string, string, string, platform.ExecOptions) platform.Result {
	return platform.Result{Flag: true, Data: "1"}
}
func (fakeProcesses) ProcessInfo(int32) platform.Result {
	return platform.Result{Flag: false}
}
func (fakeProcesses) KillProcess(int, int) platform.Result {
	return platform.Result{Flag: true}
}
func (fakeProcesses) ResolvePath(path string) string { return path }
func (fakeProcesses) BaseDir() string                { return "/tmp/app" }

type fakeGenerator struct{}

func (fakeGenerator) Generate(*profilev1.Profile, *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	return map[string]any{}, nil
}
func (fakeGenerator) WriteGeneratedConfig(map[string]any) error { return nil }
func (fakeGenerator) ReadGeneratedSecret() string               { return "" }

type fakeConfig struct{ value config.AppConfig }

func (f fakeConfig) Current() config.AppConfig { return f.value }

type fakeProfiles struct{}

func (fakeProfiles) FindByID(string) (*profilev1.Profile, error) {
	return &profilev1.Profile{Id: "profile"}, nil
}

type fakeEvents struct{}

func (fakeEvents) Publish(string, ...any) {}

func TestStartCoreRejectsMissingProfileID(t *testing.T) {
	service := NewService(fakeProcesses{}, fakeGenerator{}, fakeConfig{}, fakeProfiles{}, fakeEvents{})
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
	service := NewService(fakeProcesses{}, fakeGenerator{}, fakeConfig{
		value: config.AppConfig{AutoStartKernel: false, Profile: "profile"},
	}, fakeProfiles{}, fakeEvents{})
	service.AutoStart(context.Background())
	status, _ := service.Status()
	if status != kernelv1.CoreStatus_CORE_STATUS_STOPPED {
		t.Fatalf("expected auto start to remain stopped, got %v", status)
	}
}
