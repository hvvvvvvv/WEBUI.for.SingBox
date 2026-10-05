package kernel

import (
	"context"
	"reflect"
	"testing"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
)

type recordingRuntimeConfig struct{ value config.AppConfig }

func (c *recordingRuntimeConfig) Current() config.AppConfig { return c.value }

type recordingRuntimeGenerator struct {
	options    *kernelv1.GenerateConfigOptions
	onGenerate func()
}

func (g *recordingRuntimeGenerator) Generate(_ *profilev1.Profile, options *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	g.options = options
	g.onGenerate()
	return map[string]any{}, nil
}

func (*recordingRuntimeGenerator) WriteGeneratedConfig(map[string]any) error { return nil }

type recordingRuntimeProcesses struct {
	fakeProcesses
	path string
	args []string
	env  map[string]string
}

func (p *recordingRuntimeProcesses) ExecBackground(path string, args []string, event string, options platform.ExecOptions) platform.Result {
	p.path, p.args, p.env = path, args, options.Env
	return p.fakeProcesses.ExecBackground(path, args, event, options)
}

func TestCoreStartKeepsRuntimeSnapshotDuringSettingsChange(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error { return nil }
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	for _, branch := range []string{"main", "alpha"} {
		t.Run(branch, func(t *testing.T) {
			appConfig := &recordingRuntimeConfig{value: config.AppConfig{
				Branch: branch,
				Main: config.CoreRuntimeConfig{
					Args: []string{"run", "--branch=main", "-c", "$APP_BASE_PATH/$CORE_BASE_PATH/main.json"},
					Env:  map[string]string{"BRANCH": "main"},
				},
				Alpha: config.CoreRuntimeConfig{
					Args: []string{"run", "--branch=alpha", "-c", "$APP_BASE_PATH/$CORE_BASE_PATH/alpha.json"},
					Env:  map[string]string{"BRANCH": "alpha"},
				},
			}}
			generator := &recordingRuntimeGenerator{}
			generator.onGenerate = func() {
				appConfig.value.Branch = "changed"
				appConfig.value.Main.Args[0], appConfig.value.Alpha.Args[0] = "changed-main", "changed-alpha"
				appConfig.value.Main.Env["BRANCH"], appConfig.value.Alpha.Env["BRANCH"] = "changed-main", "changed-alpha"
			}
			processes := &recordingRuntimeProcesses{}
			service := NewService(processes, generator, appConfig, &fakeProfiles{}, fakeEvents{})
			t.Cleanup(service.lifecycleCancel)
			if _, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "profile"})); err != nil {
				t.Fatal(err)
			}
			if generator.options == nil || !generator.options.GetEnableMixinProcessing() || !generator.options.GetEnableScriptProcessing() {
				t.Fatalf("runtime configuration processing disabled: %v", generator.options)
			}
			wantPath := coreWorkingDirectory + "/" + getKernelFileName(branch == "alpha")
			wantArgs := []string{"run", "--branch=" + branch, "-c", "/tmp/app/" + coreWorkingDirectory + "/" + branch + ".json"}
			if processes.path != wantPath || !reflect.DeepEqual(processes.args, wantArgs) || processes.env["BRANCH"] != branch {
				t.Fatalf("mixed startup snapshots: path=%q args=%v env=%v", processes.path, processes.args, processes.env)
			}
		})
	}
}
