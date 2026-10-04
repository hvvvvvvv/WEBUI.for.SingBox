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

type recordingRuntimeConfig struct {
	value        config.AppConfig
	currentCalls int
}

func (c *recordingRuntimeConfig) Current() config.AppConfig {
	c.currentCalls++
	return c.value
}

type recordingRuntimeGenerator struct {
	fakeGenerator
	options    *kernelv1.GenerateConfigOptions
	onGenerate func()
}

func (g *recordingRuntimeGenerator) Generate(profile *profilev1.Profile, options *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	g.options = options
	if g.onGenerate != nil {
		g.onGenerate()
	}
	return g.fakeGenerator.Generate(profile, options)
}

type recordingRuntimeProcesses struct {
	fakeProcesses
	startCalls int
	path       string
	args       []string
	env        map[string]string
}

func (p *recordingRuntimeProcesses) ExecBackground(path string, args []string, event string, options platform.ExecOptions) platform.Result {
	p.startCalls++
	p.path = path
	p.args = args
	p.env = options.Env
	return p.fakeProcesses.ExecBackground(path, args, event, options)
}

func TestCoreStartUsesRuntimeSnapshotForAlphaAdaptation(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		return nil
	}
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	for _, entrypoint := range []string{"StartCore", "StartCoreWithProfile", "RestartCore", "AutoStart"} {
		for _, branch := range []string{"main", "alpha"} {
			t.Run(entrypoint+"/"+branch, func(t *testing.T) {
				profile := &profilev1.Profile{Id: "profile"}
				appConfig := &recordingRuntimeConfig{
					value: config.AppConfig{
						Branch:          branch,
						AutoStartKernel: true,
						Profile:         profile.GetId(),
						Main: config.CoreRuntimeConfig{
							Args: []string{"run", "--branch=main", "-c", "$APP_BASE_PATH/$CORE_BASE_PATH/main.json"},
							Env:  map[string]string{"BRANCH": "main", "CORE_PATH": "$APP_BASE_PATH/$CORE_BASE_PATH"},
						},
						Alpha: config.CoreRuntimeConfig{
							Args: []string{"run", "--branch=alpha", "-c", "$APP_BASE_PATH/$CORE_BASE_PATH/alpha.json"},
							Env:  map[string]string{"BRANCH": "alpha", "CORE_PATH": "$APP_BASE_PATH/$CORE_BASE_PATH"},
						},
					},
				}
				wantCurrentCalls := 1
				if entrypoint == "AutoStart" {
					// AutoStart first reads whether startup is enabled and which profile to use.
					wantCurrentCalls++
				}
				generator := &recordingRuntimeGenerator{}
				processes := &recordingRuntimeProcesses{}
				service := NewService(processes, generator, appConfig, &fakeProfiles{profile: profile}, fakeEvents{})
				t.Cleanup(service.lifecycleCancel)
				generator.onGenerate = func() {
					if appConfig.currentCalls != wantCurrentCalls {
						t.Fatalf("configuration reads before generation = %d, want %d", appConfig.currentCalls, wantCurrentCalls)
					}
					status, _ := service.Status()
					wantStatus := kernelv1.CoreStatus_CORE_STATUS_STARTING
					if entrypoint == "RestartCore" {
						wantStatus = kernelv1.CoreStatus_CORE_STATUS_RUNNING
					}
					if status != wantStatus {
						t.Fatalf("status during generation = %v, want %v", status, wantStatus)
					}
					if branch == "main" {
						appConfig.value.Branch = "alpha"
					} else {
						appConfig.value.Branch = "main"
					}
					// A concurrent settings update must not change this startup's parameters.
					appConfig.value.Main.Args[0] = "changed-main"
					appConfig.value.Main.Env["BRANCH"] = "changed-main"
					appConfig.value.Alpha.Args[0] = "changed-alpha"
					appConfig.value.Alpha.Env["BRANCH"] = "changed-alpha"
				}

				switch entrypoint {
				case "StartCore":
					if _, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: profile.GetId()})); err != nil {
						t.Fatal(err)
					}
				case "StartCoreWithProfile":
					if _, err := service.StartCoreWithProfile(context.Background(), connect.NewRequest(&kernelv1.StartCoreWithProfileRequest{Profile: profile})); err != nil {
						t.Fatal(err)
					}
				case "RestartCore":
					service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
					service.corePID = 123
					service.activeProfileID = profile.GetId()
					if _, err := service.RestartCore(context.Background(), connect.NewRequest(&kernelv1.RestartCoreRequest{})); err != nil {
						t.Fatal(err)
					}
				case "AutoStart":
					service.AutoStart(context.Background())
				}

				if appConfig.currentCalls != wantCurrentCalls {
					t.Fatalf("configuration reads after startup = %d, want %d", appConfig.currentCalls, wantCurrentCalls)
				}
				if generator.options == nil {
					t.Fatal("generator did not receive options")
				}
				if generator.options.GetEnableAlphaConfigAdaptation() != (branch == "alpha") {
					t.Fatalf("alpha adaptation = %v for initial branch %q", generator.options.GetEnableAlphaConfigAdaptation(), branch)
				}
				if !generator.options.GetEnableMixinProcessing() || !generator.options.GetEnableScriptProcessing() {
					t.Fatalf("startup must enable mixin and script processing, got %v", generator.options)
				}
				if processes.startCalls != 1 {
					t.Fatalf("process starts = %d, want 1", processes.startCalls)
				}
				wantPath := coreWorkingDirectory + "/" + getKernelFileName(branch == "alpha")
				if processes.path != wantPath {
					t.Fatalf("core executable = %q, want %q", processes.path, wantPath)
				}
				wantArgs := []string{"run", "--branch=" + branch, "-c", "/tmp/app/" + coreWorkingDirectory + "/" + branch + ".json"}
				if !reflect.DeepEqual(processes.args, wantArgs) {
					t.Fatalf("core arguments = %#v, want %#v", processes.args, wantArgs)
				}
				wantEnv := map[string]string{"BRANCH": branch, "CORE_PATH": "/tmp/app/" + coreWorkingDirectory}
				if !reflect.DeepEqual(processes.env, wantEnv) {
					t.Fatalf("core environment = %#v, want %#v", processes.env, wantEnv)
				}
				status, _ := service.Status()
				if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
					t.Fatalf("status after startup = %v, want running", status)
				}
			})
		}
	}
}
