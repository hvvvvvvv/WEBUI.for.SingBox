package appupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"
)

func TestApplyUpdateCoordinatesShutdownOnlyAfterHelperStarts(t *testing.T) {
	for _, mode := range []string{"interactive", "service", "helper_failure", "helper_canceled"} {
		t.Run(mode, func(t *testing.T) {
			previous := launchUpdateHelper
			defer func() { launchUpdateHelper = previous }()
			paths := storage.NewPaths(t.TempDir())
			archive := paths.Resolve(appUpdateCacheFilePath)
			_ = os.MkdirAll(filepath.Dir(archive), 0755)
			writeHelperArchive(t, archive, appTitle+executableSuffix(), "new binary")
			service := NewService(platform.NewService(paths, nil, platform.Environment{}), nil, nil, "1", mode == "service", logging.LevelWarn, 14)
			service.updatedVersion = "2"
			var started, shutdown bool
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			launchUpdateHelper = func(_ context.Context, path, preparedName string, serviceMode bool, level logging.Level, days int) error {
				if path != archive || serviceMode != (mode == "service") || level != logging.LevelWarn || days != 14 {
					t.Error("helper options lost")
				}
				if filepath.Base(preparedName) != preparedName || !strings.HasPrefix(preparedName, preparedUpdatePrefix) {
					t.Error("helper did not receive a restricted directory name")
				}
				if err := validateUpdateCandidate(filepath.Join(filepath.Dir(archive), preparedName)); err != nil {
					t.Errorf("helper launched before preparation finished: %v", err)
				}
				if mode == "helper_failure" {
					return errors.New("helper failed")
				}
				if mode == "helper_canceled" {
					cancel()
					return ctx.Err()
				}
				started = true
				return nil
			}
			service.SetShutdownHandler(func() {
				if !started {
					t.Error("shutdown requested before helper ready")
				}
				shutdown = true
			})
			_, err := service.ApplyAppUpdate(ctx, connect.NewRequest(&appv1.ApplyAppUpdateRequest{}))
			failed := mode == "helper_failure" || mode == "helper_canceled"
			if (err != nil) != failed {
				t.Fatal(err)
			}
			if shutdown != (mode == "interactive") {
				t.Fatalf("shutdown=%t in %s mode", shutdown, mode)
			}
			if mode == "helper_canceled" && connect.CodeOf(err) != connect.CodeCanceled {
				t.Fatalf("wrong helper cancellation error: %v", err)
			}
			if failed {
				assertNoPreparedUpdates(t, filepath.Dir(archive))
				if service.applying {
					t.Fatal("helper startup failure prevented a retry")
				}
			}
		})
	}
}

func assertNoPreparedUpdates(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), preparedUpdatePrefix) {
			t.Fatalf("prepared update leaked: %s", entry.Name())
		}
	}
}

func TestApplyPreparationFailureKeepsApplicationRunning(t *testing.T) {
	for _, mode := range []string{"corrupt_archive", "missing_binary", "write_failure", "canceled_before", "canceled_after", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			previousLaunch, previousExtract := launchUpdateHelper, extractUpdateArchive
			t.Cleanup(func() { launchUpdateHelper, extractUpdateArchive = previousLaunch, previousExtract })
			paths := storage.NewPaths(t.TempDir())
			archive := paths.Resolve(appUpdateCacheFilePath)
			if err := os.MkdirAll(filepath.Dir(archive), 0755); err != nil {
				t.Fatal(err)
			}
			name := appTitle + executableSuffix()
			if mode == "missing_binary" {
				name = "README.md"
			}
			writeHelperArchive(t, archive, name, "new binary")
			if mode == "corrupt_archive" {
				if err := os.WriteFile(archive, []byte("broken zip"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "deadline" {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
			}
			if mode == "canceled_before" {
				cancel()
			}
			extractUpdateArchive = func(source, destination string) error {
				if mode == "canceled_before" || mode == "deadline" {
					t.Error("canceled request began extraction")
				}
				if mode == "write_failure" {
					if err := os.WriteFile(filepath.Join(destination, "partial"), []byte("partial"), 0600); err != nil {
						return err
					}
					return syscall.ENOSPC
				}
				err := extractZip(source, destination)
				if mode == "canceled_after" {
					cancel()
				}
				return err
			}
			service := NewService(platform.NewService(paths, nil, platform.Environment{}), nil, nil, "1", false, logging.LevelInfo, 7)
			service.updatedVersion = "2"
			service.SetShutdownHandler(func() { t.Error("preparation failure shut down the application") })
			launchUpdateHelper = func(context.Context, string, string, bool, logging.Level, int) error {
				t.Error("preparation failure launched a helper")
				return nil
			}
			_, err := service.ApplyAppUpdate(ctx, connect.NewRequest(&appv1.ApplyAppUpdateRequest{}))
			if err == nil {
				t.Fatal("preparation failure accepted")
			}
			if strings.HasPrefix(mode, "canceled_") && connect.CodeOf(err) != connect.CodeCanceled {
				t.Fatalf("wrong cancellation error: %v", err)
			}
			if mode == "deadline" && connect.CodeOf(err) != connect.CodeDeadlineExceeded {
				t.Fatalf("wrong deadline error: %v", err)
			}
			if mode == "write_failure" && !errors.Is(err, syscall.ENOSPC) {
				t.Fatalf("write error lost: %v", err)
			}
			assertNoPreparedUpdates(t, filepath.Dir(archive))
			if service.applying {
				t.Fatal("failed apply prevented a retry")
			}
		})
	}
}

func TestApplyRejectsConcurrentAndAlreadyHandedOffUpdates(t *testing.T) {
	previousLaunch, previousExtract := launchUpdateHelper, extractUpdateArchive
	t.Cleanup(func() { launchUpdateHelper, extractUpdateArchive = previousLaunch, previousExtract })
	paths := storage.NewPaths(t.TempDir())
	archive := paths.Resolve(appUpdateCacheFilePath)
	if err := os.MkdirAll(filepath.Dir(archive), 0755); err != nil {
		t.Fatal(err)
	}
	writeHelperArchive(t, archive, appTitle+executableSuffix(), "new binary")
	service := NewService(platform.NewService(paths, nil, platform.Environment{}), nil, nil, "1", false, logging.LevelInfo, 7)
	service.updatedVersion = "2"
	var launches, shutdowns int
	service.SetShutdownHandler(func() { shutdowns++ })
	launchUpdateHelper = func(context.Context, string, string, bool, logging.Level, int) error { launches++; return nil }
	entered, release := make(chan struct{}), make(chan struct{})
	extractUpdateArchive = func(source, destination string) error {
		close(entered)
		<-release
		return extractZip(source, destination)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.ApplyAppUpdate(ctx, connect.NewRequest(&appv1.ApplyAppUpdateRequest{}))
		done <- err
	}()
	<-entered
	_, err := service.ApplyAppUpdate(context.Background(), connect.NewRequest(&appv1.ApplyAppUpdateRequest{}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("concurrent apply returned %v", err)
	}
	_, err = service.DownloadAppUpdate(context.Background(), connect.NewRequest(&appv1.DownloadAppUpdateRequest{ProgressEvent: "during-install"}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("download during apply returned %v", err)
	}
	cancel()
	close(release)
	if err := <-done; connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("canceled apply returned %v", err)
	}
	assertNoPreparedUpdates(t, filepath.Dir(archive))
	extractUpdateArchive = extractZip
	if _, err := service.ApplyAppUpdate(context.Background(), connect.NewRequest(&appv1.ApplyAppUpdateRequest{})); err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyAppUpdate(context.Background(), connect.NewRequest(&appv1.ApplyAppUpdateRequest{}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("handed-off apply accepted a duplicate: %v", err)
	}
	if launches != 1 || shutdowns != 1 {
		t.Fatalf("launches=%d shutdowns=%d", launches, shutdowns)
	}
}

func TestApplyCanRetryAfterHelperFailsToStopService(t *testing.T) {
	defer replaceHelperRuntimeForTest(t)()
	previousLaunch := launchUpdateHelper
	t.Cleanup(func() { launchUpdateHelper = previousLaunch })
	paths := storage.NewPaths(t.TempDir())
	archive := paths.Resolve(appUpdateCacheFilePath)
	if err := os.MkdirAll(filepath.Dir(archive), 0755); err != nil {
		t.Fatal(err)
	}
	target := paths.Resolve(appTitle + executableSuffix())
	if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	writeHelperArchive(t, archive, appTitle+executableSuffix(), "new binary")
	service := NewService(platform.NewService(paths, nil, platform.Environment{}), nil, nil, "1", true, logging.LevelInfo, 7)
	service.updatedVersion = "2"
	serviceControlDelay = 0
	var stops, starts, parentWaits int
	controlUpdatedService = func(_, _, action string) error {
		if action == "stop" {
			stops++
			if stops == 1 {
				return errors.New("service manager refused stop")
			}
		} else if action == "start" {
			starts++
		}
		return nil
	}
	waitForUpdateParent = func(platform.ProcessIdentity, time.Duration) error { parentWaits++; return nil }
	finished := make(chan error, 1)
	launchUpdateHelper = func(_ context.Context, path, name string, serviceMode bool, _ logging.Level, _ int) error {
		go func() {
			finished <- RunHelper(HelperOptions{ArchivePath: path, PreparedUpdate: name, TargetPath: target, ServiceMode: serviceMode})
		}()
		return nil
	}
	if _, err := service.ApplyAppUpdate(context.Background(), connect.NewRequest(&appv1.ApplyAppUpdateRequest{})); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err == nil || !strings.Contains(err.Error(), "refused stop") {
		t.Fatalf("helper failure=%v", err)
	}
	if parentWaits != 0 || starts != 0 {
		t.Fatal("failed stop continued updating")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old binary" {
		t.Fatalf("old application changed: %q %v", data, err)
	}
	assertNoPreparedUpdates(t, filepath.Dir(archive))
	if _, err := service.ApplyAppUpdate(context.Background(), connect.NewRequest(&appv1.ApplyAppUpdateRequest{})); err != nil {
		t.Fatalf("retry rejected after helper cleanup: %v", err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if stops != 2 || starts != 1 || parentWaits != 1 {
		t.Fatalf("stops=%d starts=%d parentWaits=%d", stops, starts, parentWaits)
	}
}
