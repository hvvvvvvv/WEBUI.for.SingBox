package kernel

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/network"
	"guiforcores/bridge/platform"
	appv1 "guiforcores/gen/app/v1"
	kernelv1 "guiforcores/gen/kernel/v1"
)

func TestCoreDownloadRejectsRunningActualBranchBeforeNetwork(t *testing.T) {
	service, _, _, app := newRecoveryService(t)
	// The selected branch may already differ from the still-running instance.
	app.value.Branch = "alpha"
	_, err := service.DownloadCore(context.Background(), connect.NewRequest(&kernelv1.DownloadCoreRequest{ProgressEvent: "test", Branch: appv1.KernelBranch_KERNEL_BRANCH_MAIN}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "stop the running core") {
		t.Fatalf("running executable update allowed: %v", err)
	}
}

func TestUnknownCoreIdentityBlocksBothBranchFileChanges(t *testing.T) {
	for _, status := range []kernelv1.CoreStatus{
		kernelv1.CoreStatus_CORE_STATUS_RUNNING,
		kernelv1.CoreStatus_CORE_STATUS_CRASHED,
		kernelv1.CoreStatus_CORE_STATUS_STARTING,
		kernelv1.CoreStatus_CORE_STATUS_STOPPING,
	} {
		t.Run(status.String(), func(t *testing.T) {
			service, _, processes, app := newRecoveryService(t)
			app.value.Branch = "alpha"
			service.mu.Lock()
			service.currentStart = nil
			service.status = status
			// Starting also owns a transition before OnStarted provides a PID.
			if status == kernelv1.CoreStatus_CORE_STATUS_STARTING {
				service.corePID = -1
			}
			service.mu.Unlock()
			for _, branch := range []appv1.KernelBranch{appv1.KernelBranch_KERNEL_BRANCH_MAIN, appv1.KernelBranch_KERNEL_BRANCH_ALPHA} {
				path := processes.ResolvePath(coreFilePathForBranch(branch))
				if err := os.WriteFile(path, []byte("current"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path+".bak", []byte("backup"), 0700); err != nil {
					t.Fatal(err)
				}
				_, err := service.DownloadCore(context.Background(), connect.NewRequest(&kernelv1.DownloadCoreRequest{ProgressEvent: "unknown-owner", Branch: branch}))
				if connect.CodeOf(err) != connect.CodeFailedPrecondition {
					t.Fatalf("unknown %s owner allowed download of %v: %v", status, branch, err)
				}
				_, err = service.RollbackCore(context.Background(), connect.NewRequest(&kernelv1.RollbackCoreRequest{Branch: branch}))
				if connect.CodeOf(err) != connect.CodeFailedPrecondition {
					t.Fatalf("unknown %s owner allowed rollback of %v: %v", status, branch, err)
				}
				current, _ := os.ReadFile(path)
				backup, _ := os.ReadFile(path + ".bak")
				if string(current) != "current" || string(backup) != "backup" {
					t.Fatal("unknown process ownership allowed executable mutation")
				}
			}
			if len(processes.kills) != 0 || processes.launchCount() != 1 {
				t.Fatal("unknown process ownership triggered stop or restart")
			}
		})
	}
}

func TestCoreDownloadCancelCannotReleaseAnotherDownloadSlot(t *testing.T) {
	previous := newRequestClient
	t.Cleanup(func() { newRequestClient = previous })
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	newRequestClient = func(_ network.Options, _ time.Duration, _ bool) (*http.Client, error) {
		return &http.Client{Transport: coreDownloadTransport(func(r *http.Request) (*http.Response, error) {
			close(entered)
			<-r.Context().Done()
			close(canceled)
			<-release
			return nil, r.Context().Err()
		})}, nil
	}
	service := NewService(fakeProcesses{resolveBase: t.TempDir()}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, nil)
	t.Cleanup(service.lifecycleCancel)
	result := make(chan error, 1)
	go func() {
		_, err := service.DownloadCore(context.Background(), connect.NewRequest(&kernelv1.DownloadCoreRequest{ProgressEvent: "same"}))
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("release fetch did not begin")
	}
	if _, err := service.CancelCoreDownload(context.Background(), connect.NewRequest(&kernelv1.CancelCoreDownloadRequest{ProgressEvent: "same"})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("release metadata fetch ignored cancellation")
	}
	for _, event := range []string{"same", "other"} {
		_, err := service.DownloadCore(context.Background(), connect.NewRequest(&kernelv1.DownloadCoreRequest{ProgressEvent: event}))
		if connect.CodeOf(err) != connect.CodeAlreadyExists {
			t.Fatalf("parallel download %q was admitted: %v", event, err)
		}
	}
	unblock()
	select {
	case err := <-result:
		if connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatalf("wrong cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("download did not finish")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.downloads) != 0 {
		t.Fatal("download registration leaked")
	}
}

type stagedInstallProcesses struct {
	fakeProcesses
	checked chan struct{}
}

func (p *stagedInstallProcesses) Exec(_ string, _ []string, _ platform.ExecOptions) platform.Result {
	close(p.checked)
	return platform.Result{Flag: true, Data: "sing-box version 1.14.4"}
}

func TestCoreInstallRechecksRunningBranchUnderTransitionLock(t *testing.T) {
	for _, runningBranch := range []string{"main", "alpha"} {
		t.Run(runningBranch, func(t *testing.T) {
			base := t.TempDir()
			cache := filepath.Join(base, coreDownloadCacheFolder)
			if err := os.MkdirAll(cache, 0700); err != nil {
				t.Fatal(err)
			}
			assetName := getKernelAssetFileName("1.14.2")
			archive := filepath.Join(cache, assetName)
			if err := os.WriteFile(archive, coreDownloadArchive(t, assetName), 0600); err != nil {
				t.Fatal(err)
			}
			corePath := filepath.Join(base, coreFilePathForBranch(appv1.KernelBranch_KERNEL_BRANCH_MAIN))
			if err := os.MkdirAll(filepath.Dir(corePath), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(corePath, []byte("old"), 0700); err != nil {
				t.Fatal(err)
			}
			processes := &stagedInstallProcesses{fakeProcesses: fakeProcesses{resolveBase: base}, checked: make(chan struct{})}
			service := NewService(processes, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, nil)
			t.Cleanup(service.lifecycleCancel)
			service.coreTransitionMu.Lock()
			result := make(chan error, 1)
			go func() {
				result <- service.installCoreArchive(context.Background(), archive, cache, appv1.KernelBranch_KERNEL_BRANCH_MAIN)
			}()
			<-processes.checked
			// A start finishes while extraction/version verification were running.
			service.mu.Lock()
			service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
			service.corePID = 123
			service.currentStart = &coreStartSnapshot{runtime: kernelRuntimeConfig{Branch: runningBranch}}
			service.mu.Unlock()
			service.coreTransitionMu.Unlock()
			err := <-result
			data, _ := os.ReadFile(corePath)
			if runningBranch == "main" {
				if connect.CodeOf(err) != connect.CodeFailedPrecondition || string(data) != "old" {
					t.Fatalf("running branch was replaced: %q %v", data, err)
				}
			} else if err != nil || string(data) != "verified new core" {
				t.Fatalf("inactive branch could not update: %q %v", data, err)
			}
		})
	}
}
