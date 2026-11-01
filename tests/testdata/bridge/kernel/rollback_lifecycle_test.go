package kernel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/platform"
	appv1 "guiforcores/gen/app/v1"
	kernelv1 "guiforcores/gen/kernel/v1"
)

func TestRollbackUsesActualBranchAndRestoresBinaryOnFailure(t *testing.T) {
	for _, failLaunch := range []bool{false, true} {
		name := "success"
		if failLaunch {
			name = "launch-failure"
		}
		t.Run(name, func(t *testing.T) {
			service, generator, processes, app := newRecoveryService(t)
			app.value.Branch = "alpha"
			service.updateCoreState(func() { service.restartRequired = true })
			path := processes.ResolvePath(coreFilePathForBranch(appv1.KernelBranch_KERNEL_BRANCH_MAIN))
			if err := os.WriteFile(path, []byte("current"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".bak", []byte("previous"), 0700); err != nil {
				t.Fatal(err)
			}
			oldConfig := string(generator.writes[0])
			processes.onKill = func(int) *platform.Result {
				data, _ := os.ReadFile(path)
				if string(data) != "current" {
					t.Error("rollback changed the live executable before stopping it")
				}
				return nil
			}
			processes.onLaunch = func(call int, _ platform.ExecOptions) *platform.Result {
				data, _ := os.ReadFile(path)
				if call == 2 && string(data) != "previous" {
					t.Error("rollback did not install the backup")
				}
				if call == 3 && string(data) != "current" {
					t.Error("failed rollback did not restore original executable before launch")
				}
				if failLaunch && call == 2 {
					return &platform.Result{Data: "fixture candidate failed"}
				}
				return nil
			}
			_, err := service.RollbackCore(context.Background(), connect.NewRequest(&kernelv1.RollbackCoreRequest{Branch: appv1.KernelBranch_KERNEL_BRANCH_MAIN}))
			if (err != nil) != failLaunch {
				t.Fatalf("unexpected result: %v", err)
			}
			if failLaunch && !strings.Contains(err.Error(), "was restored") {
				t.Fatalf("missing recovery result: %v", err)
			}
			wantLaunches := 2
			wantCurrent, wantBackup := "previous", "current"
			if failLaunch {
				wantLaunches = 3
				wantCurrent, wantBackup = "current", "previous"
			}
			if len(processes.kills) != 1 || processes.launchCount() != wantLaunches || service.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
				t.Fatalf("wrong lifecycle: kills=%v launches=%d status=%v", processes.kills, processes.launchCount(), service.status)
			}
			current, _ := os.ReadFile(path)
			backup, _ := os.ReadFile(path + ".bak")
			if string(current) != wantCurrent || string(backup) != wantBackup {
				t.Fatalf("wrong executable pair: %q %q", current, backup)
			}
			if generator.calls != 1 || string(generator.writes[len(generator.writes)-1]) != oldConfig {
				t.Fatal("rollback regenerated or lost temporary settings")
			}
			if !service.restartRequired {
				t.Fatal("rollback forgot the pending branch/configuration change")
			}
		})
	}
}

func TestExplicitStopDuringRollbackRestoresFilesWithoutRelaunch(t *testing.T) {
	service, _, processes, _ := newRecoveryService(t)
	path := processes.ResolvePath(coreFilePathForBranch(appv1.KernelBranch_KERNEL_BRANCH_MAIN))
	for name, content := range map[string]string{path: "current", path + ".bak": "previous"} {
		if err := os.WriteFile(name, []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	stopped := make(chan error, 1)
	processes.onLaunch = func(call int, _ platform.ExecOptions) *platform.Result {
		if call != 2 {
			return nil
		}
		go func() {
			_, err := service.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{}))
			stopped <- err
		}()
		deadline := time.Now().Add(3 * time.Second)
		for {
			service.mu.Lock()
			requested := service.stopRequested
			service.mu.Unlock()
			if requested {
				break
			}
			if time.Now().After(deadline) {
				t.Error("explicit stop was not registered during rollback")
				break
			}
			time.Sleep(time.Millisecond)
		}
		return &platform.Result{Data: "candidate canceled before start"}
	}
	_, err := service.RollbackCore(context.Background(), connect.NewRequest(&kernelv1.RollbackCoreRequest{}))
	if err == nil || !strings.Contains(err.Error(), "restore canceled by stop") {
		t.Fatalf("unexpected rollback cancellation: %v", err)
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not finish")
	}
	if service.corePID > 0 || processes.launchCount() != 2 {
		t.Fatal("rollback recovery relaunched after explicit stop")
	}
	current, _ := os.ReadFile(path)
	backup, _ := os.ReadFile(path + ".bak")
	if string(current) != "current" || string(backup) != "previous" {
		t.Fatalf("canceled rollback lost its original files: %q %q", current, backup)
	}
}

func TestRollbackCanRestoreMissingStoppedExecutable(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, coreFilePathForBranch(appv1.KernelBranch_KERNEL_BRANCH_MAIN))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte("retained backup"), 0700); err != nil {
		t.Fatal(err)
	}
	service := NewService(fakeProcesses{resolveBase: base}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, nil)
	t.Cleanup(service.lifecycleCancel)
	if _, err := service.RollbackCore(context.Background(), connect.NewRequest(&kernelv1.RollbackCoreRequest{})); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "retained backup" {
		t.Fatalf("backup restoration failed: %q %v", data, err)
	}
}

func TestClearCacheRejectsCustomConfigArgumentsWithoutStopping(t *testing.T) {
	service, _, processes, _ := newRecoveryService(t)
	service.currentStart.runtime.Args = []string{"run", "-c", "outside.json"}
	called := false
	err := service.clearSharedCoreCache(context.Background(), func() error { called = true; return nil })
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || called || len(processes.kills) != 0 {
		t.Fatalf("custom cache path was guessed or core stopped: called=%v kills=%v error=%v", called, processes.kills, err)
	}
}
