package appupdate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"guiforcores/bridge/platform"
)

func replaceHelperRuntimeForTest(t *testing.T) func() {
	t.Helper()
	originalUpdateLock := acquireUpdateLock
	acquireUpdateLock = func(string) (io.Closer, error) { return io.NopCloser(strings.NewReader("")), nil }
	originalCoreWait := waitForUpdateCore
	waitForUpdateCore = func(string) error { return nil }
	originalWait := waitForUpdateParent
	originalExtract := extractUpdateArchive
	originalReplace := replaceUpdatedApplication
	originalControl := controlUpdatedService
	originalStart := startUpdatedApplication
	originalDelay := serviceControlDelay
	return func() {
		acquireUpdateLock = originalUpdateLock
		waitForUpdateCore = originalCoreWait
		waitForUpdateParent = originalWait
		extractUpdateArchive = originalExtract
		replaceUpdatedApplication = originalReplace
		controlUpdatedService = originalControl
		startUpdatedApplication = originalStart
		serviceControlDelay = originalDelay
	}
}

func TestHelperRefusesReplacementWhenCoreOrLockCannotBeVerified(t *testing.T) {
	for _, failAt := range []string{"lock", "core"} {
		t.Run(failAt, func(t *testing.T) {
			defer replaceHelperRuntimeForTest(t)()
			waitForUpdateParent = func(platform.ProcessIdentity, time.Duration) error { return nil }
			if failAt == "lock" {
				acquireUpdateLock = func(string) (io.Closer, error) { return nil, errors.New("conflict") }
			} else {
				waitForUpdateCore = func(string) error { return errors.New("identity cannot be verified") }
			}
			extractUpdateArchive = func(string, string) error { t.Error("extracted before cleanup confirmed"); return nil }
			replaceUpdatedApplication = func(string, string) error { t.Error("replaced before cleanup confirmed"); return nil }
			startUpdatedApplication = func(string, []string, string) error { t.Error("started before cleanup confirmed"); return nil }
			if err := RunHelper(HelperOptions{TargetPath: filepath.Join(t.TempDir(), "webui")}); err == nil {
				t.Fatal("unsafe update succeeded")
			}
		})
	}
}

func TestUpdateParentIdentityRejectsReuseAndMissingIdentity(t *testing.T) {
	identity, err := platform.IdentifyProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	identity.Created--
	if err := waitForParentExit(identity, time.Millisecond); err == nil {
		t.Fatal("PID reuse was accepted")
	}
	if err := waitForParentExit(platform.ProcessIdentity{PID: os.Getpid()}, time.Millisecond); err == nil {
		t.Fatal("incomplete identity was accepted")
	}
}

func TestReplaceApplicationPreservesYAMLAndLogsOnEveryPlatform(t *testing.T) {
	base, archive := t.TempDir(), t.TempDir()
	target := filepath.Join(base, appTitle+executableSuffix())
	source := filepath.Join(archive, appTitle+executableSuffix())
	if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new binary"), 0755); err != nil {
		t.Fatal(err)
	}
	preserved := map[string]string{
		"data/config.yaml":              "coreLogDays: 7\nautoStartKernel: false\n",
		"data/logs/core/2026-09-07.log": "INFO saved core output\n",
	}
	for name, content := range preserved {
		path := filepath.Join(base, name)
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := replaceApplication(archive, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "new binary" {
		t.Fatalf("replacement failed: %v", err)
	}
	for name, content := range preserved {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil || string(data) != content {
			t.Fatalf("lost %s: %v", name, err)
		}
	}
}
