package kernel

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/platform"
	appv1 "guiforcores/gen/app/v1"
	kernelv1 "guiforcores/gen/kernel/v1"
)

func TestClearCacheForOtherBranchStopsSharedCacheOwner(t *testing.T) {
	service, generator, processes, _ := newRecoveryService(t)
	path := processes.ResolvePath(coreCacheFilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("cache"), 0600); err != nil {
		t.Fatal(err)
	}
	oldConfig := string(generator.writes[0])
	processes.onKill = func(int) *platform.Result {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("cache deleted before its owner stopped: %v", err)
		}
		return nil
	}
	service.updateCoreState(func() { service.restartRequired = true })
	_, err := service.ClearCoreCache(context.Background(), connect.NewRequest(&kernelv1.ClearCoreCacheRequest{Branch: appv1.KernelBranch_KERNEL_BRANCH_ALPHA}))
	if err != nil {
		t.Fatal(err)
	}
	if len(processes.kills) != 1 || processes.launchCount() != 2 {
		t.Fatalf("shared cache owner was not restarted: kills=%v launches=%d", processes.kills, processes.launchCount())
	}
	if generator.calls != 1 || string(generator.writes[1]) != oldConfig {
		t.Fatal("cache clearing regenerated/changed temporary running settings")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cache was not removed: %v", err)
	}
	if !service.restartRequired {
		t.Fatal("pending persisted configuration changes were forgotten")
	}
}

func TestClearCacheExplicitStopPreventsRestart(t *testing.T) {
	service, _, processes, _ := newRecoveryService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	cleared, stopped := make(chan error, 1), make(chan error, 1)
	go func() {
		cleared <- service.clearSharedCoreCache(context.Background(), func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("clear did not reach cache action")
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
			t.Fatal("stop intent was not registered")
		}
		time.Sleep(time.Millisecond)
	}
	unblock()
	select {
	case err := <-cleared:
		if err == nil {
			t.Fatal("clear ignored explicit stop")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("clear did not finish")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not finish")
	}
	if processes.launchCount() != 1 || service.corePID > 0 {
		t.Fatal("cache clearing restarted after explicit stop")
	}
}

func TestClearCacheFailureRestoresRunningSnapshot(t *testing.T) {
	service, generator, processes, _ := newRecoveryService(t)
	path := processes.ResolvePath(coreCacheFilePath)
	// A nonempty directory produces a portable, deterministic remove failure.
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "retained"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	oldConfig := string(generator.writes[0])
	_, err := service.ClearCoreCache(context.Background(), connect.NewRequest(&kernelv1.ClearCoreCacheRequest{}))
	if err == nil {
		t.Fatal("cache removal failure was hidden")
	}
	if service.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || processes.launchCount() != 2 {
		t.Fatalf("failed clear left core offline: status=%v launches=%d error=%v", service.status, processes.launchCount(), err)
	}
	if generator.calls != 1 || string(generator.writes[1]) != oldConfig {
		t.Fatal("restoration lost running snapshot")
	}
}
