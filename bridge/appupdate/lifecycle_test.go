package appupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"
)

func TestApplyUpdateCoordinatesShutdownOnlyAfterHelperStarts(t *testing.T) {
	for _, mode := range []string{"interactive", "service", "helper_failure"} {
		t.Run(mode, func(t *testing.T) {
			previous := launchUpdateHelper
			defer func() { launchUpdateHelper = previous }()
			paths := storage.NewPaths(t.TempDir())
			archive := paths.Resolve(appUpdateCacheFilePath)
			_ = os.MkdirAll(filepath.Dir(archive), 0755)
			_ = os.WriteFile(archive, []byte("test"), 0644)
			service := NewService(platform.NewService(paths, nil, platform.Environment{}), nil, nil, "1", mode == "service", logging.LevelWarn, 14)
			service.updatedVersion = "2"
			var started, shutdown bool
			launchUpdateHelper = func(path string, serviceMode bool, level logging.Level, days int) error {
				if path != archive || serviceMode != (mode == "service") || level != logging.LevelWarn || days != 14 {
					t.Error("helper options lost")
				}
				if mode == "helper_failure" {
					return errors.New("helper failed")
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
			_, err := service.ApplyAppUpdate(context.Background(), connect.NewRequest(&appv1.ApplyAppUpdateRequest{}))
			if (err != nil) != (mode == "helper_failure") {
				t.Fatal(err)
			}
			if shutdown != (mode == "interactive") {
				t.Fatalf("shutdown=%t in %s mode", shutdown, mode)
			}
		})
	}
}
