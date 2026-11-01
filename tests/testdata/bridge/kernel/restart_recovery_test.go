package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	appv1 "guiforcores/gen/app/v1"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

type recoveryGenerator struct {
	mu         sync.Mutex
	calls      int
	writes     [][]byte
	generated  map[string]any
	onGenerate func(int)
}

func (g *recoveryGenerator) Generate(profile *profilev1.Profile, _ *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	g.mu.Lock()
	g.calls++
	call := g.calls
	generated := map[string]any{
		"fixture":  map[string]any{"profile": profile.GetName(), "revision": call},
		"inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": 10000 + call}},
	}
	g.generated = generated
	g.mu.Unlock()
	if g.onGenerate != nil {
		g.onGenerate(call)
	}
	return generated, nil
}

func (g *recoveryGenerator) WriteGeneratedConfig(generated map[string]any) error {
	data, err := json.Marshal(generated)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.writes = append(g.writes, data)
	g.mu.Unlock()
	return nil
}

type recoveryLaunch struct {
	path string
	args []string
	env  map[string]string
}

type recoveryProcesses struct {
	fakeProcesses
	mu       sync.Mutex
	launches []recoveryLaunch
	kills    []int
	exits    map[int]func(int, error)
	onLaunch func(int, platform.ExecOptions) *platform.Result
	onKill   func(int) *platform.Result
}

func (p *recoveryProcesses) ExecBackground(path string, args []string, _ string, options platform.ExecOptions) platform.Result {
	p.mu.Lock()
	runtime := cloneKernelRuntime(kernelRuntimeConfig{Args: args, Env: options.Env})
	p.launches = append(p.launches, recoveryLaunch{path: path, args: runtime.Args, env: runtime.Env})
	call := len(p.launches)
	pid := 100 + call
	p.exits[pid] = options.OnExit
	p.mu.Unlock()
	if p.onLaunch != nil {
		if result := p.onLaunch(call, options); result != nil {
			return *result
		}
	}
	options.OnStarted(pid)
	return platform.Result{Flag: true, Data: strconv.Itoa(pid)}
}

func (p *recoveryProcesses) KillProcess(pid int, _ int) platform.Result {
	p.mu.Lock()
	p.kills = append(p.kills, pid)
	onExit := p.exits[pid]
	p.mu.Unlock()
	if p.onKill != nil {
		if result := p.onKill(pid); result != nil {
			return *result
		}
	}
	if onExit != nil {
		onExit(pid, nil)
	}
	return platform.Result{Flag: true}
}

func (p *recoveryProcesses) launchCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.launches)
}

func newRecoveryService(t *testing.T) (*Service, *recoveryGenerator, *recoveryProcesses, *recordingRuntimeConfig) {
	t.Helper()
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error { return nil }
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })
	processes := &recoveryProcesses{fakeProcesses: fakeProcesses{resolveBase: t.TempDir()}, exits: make(map[int]func(int, error))}
	generator := &recoveryGenerator{}
	app := &recordingRuntimeConfig{value: config.AppConfig{Branch: "main", Main: config.CoreRuntimeConfig{
		Args: []string{"run", "-c", processes.ResolvePath(coreConfigRelativePath)}, Env: map[string]string{"FIXTURE_BRANCH": "old"},
	}}}
	service := NewService(processes, generator, app, &fakeProfiles{}, &recordingEvents{})
	t.Cleanup(service.lifecycleCancel)
	_, err := service.StartCoreWithProfile(context.Background(), connect.NewRequest(&kernelv1.StartCoreWithProfileRequest{
		Profile: &profilev1.Profile{Id: "old", Name: "running-old-profile"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return service, generator, processes, app
}

func recoveryRestart(service *Service, ctx context.Context) error {
	_, err := service.RestartCore(ctx, connect.NewRequest(&kernelv1.RestartCoreRequest{
		Profile: &profilev1.Profile{Id: "new", Name: "new-profile"},
	}))
	return err
}

func TestRestartFailureRestoresExactRunningSnapshot(t *testing.T) {
	for _, failure := range []string{"launch", "native-api", "request-canceled"} {
		t.Run(failure, func(t *testing.T) {
			service, generator, processes, app := newRecoveryService(t)
			oldNativeCtx, _, oldSecret, stopNative, err := service.NativeAPIContext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer stopNative()
			oldProxy, err := service.CoreRequestProxy()
			if err != nil {
				t.Fatal(err)
			}
			oldWrite := string(generator.writes[0])
			// Neither mutable generator output nor later app/profile state may
			// alter the restoration snapshot (or trigger a third Generate).
			generator.generated["fixture"].(map[string]any)["profile"] = "mutated-after-start"
			app.value.Branch = "alpha"
			app.value.Alpha = config.CoreRuntimeConfig{Args: []string{"run", "--new-args"}, Env: map[string]string{"FIXTURE_BRANCH": "new"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			processes.onLaunch = func(call int, options platform.ExecOptions) *platform.Result {
				if call != 2 || failure == "native-api" {
					if call == 3 && options.Context.Err() != nil {
						t.Error("recovery inherited the disconnected request's cancellation")
					}
					return nil
				}
				if failure == "request-canceled" {
					cancel()
					return &platform.Result{Data: "request canceled"}
				}
				return &platform.Result{Data: "fixture launch failure"}
			}
			if failure == "native-api" {
				waitKernelAPIReadyFunc = func(_ context.Context, _, _ string, pid int, _ time.Duration) error {
					if pid == 102 {
						return errors.New("fixture API failure")
					}
					return nil
				}
			}
			err = recoveryRestart(service, ctx)
			if err == nil || !strings.Contains(err.Error(), "previous running configuration was restored") {
				t.Fatalf("restart lost its failure/recovery result: %v", err)
			}
			if failure == "request-canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("original cancellation cause lost: %v", err)
			}
			if failure == "native-api" {
				var rpcErr *connect.Error
				if !errors.As(err, &rpcErr) || rpcErr.Meta().Get(coreErrorReasonHeader) != coreAPIUnavailable {
					t.Fatalf("original API readiness error metadata lost: %v", err)
				}
			}
			if generator.calls != 2 || len(generator.writes) != 3 || string(generator.writes[2]) != oldWrite {
				t.Fatalf("restoration regenerated or changed old configuration: generates=%d writes=%d", generator.calls, len(generator.writes))
			}
			if !reflect.DeepEqual(processes.launches[0], processes.launches[2]) || reflect.DeepEqual(processes.launches[0], processes.launches[1]) {
				t.Fatalf("restoration did not preserve original binary/args/env: %#v", processes.launches)
			}
			profile, err := service.GetCurrentProfile(context.Background(), connect.NewRequest(&kernelv1.GetCurrentProfileRequest{}))
			if err != nil || profile.Msg.GetProfile().GetId() != "old" || profile.Msg.GetProfile().GetName() != "running-old-profile" {
				t.Fatalf("restoration lost current profile: %v %v", profile, err)
			}
			_, _, restoredSecret, finishNative, err := service.NativeAPIContext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer finishNative()
			if restoredSecret != oldSecret || !service.restartRequired || service.corePID != 103 {
				t.Fatal("restored instance lost its secret, PID, or pending restart state")
			}
			select {
			case <-oldNativeCtx.Done():
			case <-time.After(time.Second):
				t.Fatal("old native requests remained valid across restored instances")
			}
			if restoredProxy, err := service.CoreRequestProxy(); err != nil || restoredProxy != oldProxy {
				t.Fatalf("restored core request proxy differs from original running configuration: %q %v", restoredProxy, err)
			}
		})
	}
}

func TestCoreUpgradeUsesInternalStopAndHonorsConcurrentStop(t *testing.T) {
	for _, concurrentStop := range []bool{false, true} {
		t.Run(strconv.FormatBool(concurrentStop), func(t *testing.T) {
			service, _, processes, _ := newRecoveryService(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			result := make(chan error, 1)
			go func() {
				result <- service.runWithCoreRestartIfCurrent(context.Background(), appv1.KernelBranch_KERNEL_BRANCH_MAIN, "fixture-candidate", func() error {
					close(entered)
					<-release
					return nil
				})
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("upgrade deadlocked before its action")
			}
			stopped := make(chan error, 1)
			if concurrentStop {
				go func() {
					_, err := service.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{}))
					stopped <- err
				}()
				deadline := time.After(3 * time.Second)
				for {
					service.mu.Lock()
					requested := service.stopRequested
					service.mu.Unlock()
					if requested {
						break
					}
					select {
					case <-deadline:
						t.Fatal("stop intent was not registered during upgrade")
					case <-time.After(time.Millisecond):
					}
				}
			}
			unblock()
			select {
			case err := <-result:
				if (err != nil) != concurrentStop {
					t.Fatalf("unexpected upgrade result: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("upgrade/stop deadlocked")
			}
			if concurrentStop {
				select {
				case err := <-stopped:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("stop did not finish after upgrade")
				}
				if service.corePID > 0 || processes.launchCount() != 1 {
					t.Fatal("upgrade started a core after an explicit stop")
				}
			} else {
				if service.stopRequested || service.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || processes.launchCount() != 2 {
					t.Fatal("internal upgrade stop suppressed future automatic restarts")
				}
			}
		})
	}
}

func TestRestartRecoveryFailureAndUnconfirmedExit(t *testing.T) {
	for _, failure := range []string{"restore-launch", "cleanup", "guard-unconfirmed"} {
		t.Run(failure, func(t *testing.T) {
			service, _, processes, _ := newRecoveryService(t)
			processes.onLaunch = func(call int, options platform.ExecOptions) *platform.Result {
				if call == 2 && failure == "guard-unconfirmed" {
					options.OnStarted(102)
					return &platform.Result{Data: platform.ErrCoreExitUnconfirmed.Error()}
				}
				if failure == "restore-launch" && call >= 2 {
					return &platform.Result{Data: "fixture launch failure " + strconv.Itoa(call)}
				}
				return nil
			}
			if failure == "cleanup" {
				waitKernelAPIReadyFunc = func(_ context.Context, _, _ string, pid int, _ time.Duration) error {
					if pid == 102 {
						return errors.New("fixture API failure")
					}
					return nil
				}
				processes.onKill = func(pid int) *platform.Result {
					if pid == 102 {
						return &platform.Result{Data: "fixture termination unconfirmed"}
					}
					return nil
				}
			}
			err := recoveryRestart(service, context.Background())
			if err == nil {
				t.Fatal("failed restart succeeded")
			}
			if failure == "restore-launch" {
				if processes.launchCount() != 3 || !strings.Contains(err.Error(), "restore failed") || !strings.Contains(err.Error(), "failure 2") || !strings.Contains(err.Error(), "failure 3") {
					t.Fatalf("lost original or recovery failure: %v", err)
				}
			} else if processes.launchCount() != 2 || service.corePID != 102 || !strings.Contains(err.Error(), "exit was not confirmed") {
				t.Fatalf("replacement started before failed process exit was confirmed: %v", err)
			}
			if failure != "restore-launch" {
				// Cleanup can fail transiently. Once the runner can confirm exit,
				// an explicit stop must reclaim the retained PID without another
				// launch, even though its status is STOPPING or CRASHED.
				previousKills := len(processes.kills)
				processes.onKill = nil
				if _, err := service.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil {
					t.Fatalf("cannot retry cleanup of retained PID: %v", err)
				}
				if len(processes.kills) != previousKills+1 || processes.kills[previousKills] != 102 {
					t.Fatalf("retry did not clean up exactly the retained PID: %v", processes.kills)
				}
				if service.corePID > 0 || service.status != kernelv1.CoreStatus_CORE_STATUS_STOPPED || processes.launchCount() != 2 {
					t.Fatalf("retry left a process or launched a replacement: status=%v pid=%d launches=%d", service.status, service.corePID, processes.launchCount())
				}
				if _, err := service.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil || len(processes.kills) != previousKills+1 {
					t.Fatalf("repeated stop retried an already cleared PID: %v kills=%v", err, processes.kills)
				}
			}
		})
	}
}

func TestStopRejectsMissingPIDAfterFailedCleanup(t *testing.T) {
	for _, status := range []kernelv1.CoreStatus{
		kernelv1.CoreStatus_CORE_STATUS_CRASHED,
		kernelv1.CoreStatus_CORE_STATUS_STOPPING,
		kernelv1.CoreStatus_CORE_STATUS_RUNNING,
	} {
		for _, pid := range []int{-1, 0} {
			t.Run(status.String()+"/"+strconv.Itoa(pid), func(t *testing.T) {
				processes := &recoveryProcesses{}
				service := NewService(processes, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
				t.Cleanup(service.lifecycleCancel)
				service.status, service.corePID = status, pid
				_, err := service.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{}))
				if connect.CodeOf(err) != connect.CodeFailedPrecondition || len(processes.kills) != 0 {
					t.Fatalf("stop attempted cleanup without a registered PID: err=%v kills=%v", err, processes.kills)
				}
			})
		}
	}
}

func TestStopAndShutdownPreemptRestartRecovery(t *testing.T) {
	for _, when := range []string{"preflight", "launch", "recovery", "shutdown"} {
		t.Run(when, func(t *testing.T) {
			service, generator, processes, _ := newRecoveryService(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			if when == "preflight" {
				generator.onGenerate = func(call int) {
					if call == 2 {
						close(entered)
						<-release
					}
				}
			} else {
				processes.onLaunch = func(call int, options platform.ExecOptions) *platform.Result {
					if call == 2 && when == "recovery" {
						return &platform.Result{Data: "fixture first launch failure"}
					}
					if (call == 2 && when != "recovery") || (call == 3 && when == "recovery") {
						close(entered)
						<-options.Context.Done()
						return &platform.Result{Data: options.Context.Err().Error()}
					}
					return nil
				}
			}
			result := make(chan error, 1)
			go func() { result <- recoveryRestart(service, context.Background()) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("restart did not reach controlled phase")
			}
			if when == "shutdown" {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := service.Close(ctx); err != nil {
					t.Fatal(err)
				}
			} else if _, err := service.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil {
				t.Fatal(err)
			}
			unblock()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("canceled restart succeeded")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stop/restart deadlocked")
			}
			wantStarts := 2
			if when == "preflight" {
				wantStarts = 1
			} else if when == "recovery" {
				wantStarts = 3
			}
			if processes.launchCount() != wantStarts || service.corePID > 0 || service.status != kernelv1.CoreStatus_CORE_STATUS_STOPPED {
				t.Fatalf("stop/shutdown resurrected a core: starts=%d status=%v pid=%d", processes.launchCount(), service.status, service.corePID)
			}
			if when != "shutdown" {
				// Even an already-dequeued automatic restart cannot override stop.
				if _, err := service.restartCoreOnce(context.Background(), "old"); err == nil || processes.launchCount() != wantStarts {
					t.Fatal("queued automatic restart overrode explicit stop")
				}
			}
		})
	}
}
