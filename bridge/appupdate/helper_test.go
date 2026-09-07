package appupdate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"guiforcores/bridge/logging"
	"guiforcores/bridge/platform"
)

func TestUpdateHelperArguments(t *testing.T) {
	args := updateHelperArguments(
		"/tmp/update.zip",
		"/opt/webui",
		platform.ProcessIdentity{PID: 42, Created: 123456, Executable: "/opt/webui"},
		`["--addr","127.0.0.1:8080"]`,
		"/opt",
		true,
		logging.LevelWarn,
		14,
	)
	want := []string{
		"__updater",
		"--archive-path", "/tmp/update.zip",
		"--target-path", "/opt/webui",
		"--parent-pid", "42",
		"--parent-created", "123456",
		"--parent-executable", "/opt/webui",
		"--restart-args", `["--addr","127.0.0.1:8080"]`,
		"--working-dir", "/opt",
		"--log-level", "warn",
		"--log-days", "14",
		"--service-mode",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("helper args = %#v, want %#v", args, want)
	}
}

func TestRunHelperServiceModeOrder(t *testing.T) {
	restore := replaceHelperRuntimeForTest(t)
	defer restore()

	archivePath := filepath.Join(t.TempDir(), "update.zip")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controlUpdatedService = func(_, _ string, action string) error {
		calls = append(calls, action)
		return nil
	}
	waitForUpdateParent = func(platform.ProcessIdentity, time.Duration) error {
		calls = append(calls, "wait")
		return nil
	}
	waitForUpdateCore = func(string) error { calls = append(calls, "wait-core"); return nil }
	extractUpdateArchive = func(_, _ string) error {
		calls = append(calls, "extract")
		return nil
	}
	replaceUpdatedApplication = func(_, _ string) error {
		calls = append(calls, "replace")
		return nil
	}
	startUpdatedApplication = func(string, []string, string) error {
		calls = append(calls, "direct-start")
		return nil
	}
	serviceControlDelay = 0

	err := RunHelper(HelperOptions{
		ArchivePath: archivePath,
		TargetPath:  "/opt/webui",
		ParentPID:   42,
		WorkingDir:  "/opt",
		ServiceMode: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"stop", "wait", "wait-core", "extract", "replace", "start"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("archive was not removed after replacement: %v", err)
	}
}

func TestRunHelperForegroundMode(t *testing.T) {
	restore := replaceHelperRuntimeForTest(t)
	defer restore()

	archivePath := filepath.Join(t.TempDir(), "update.zip")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controlUpdatedService = func(_, _, action string) error {
		calls = append(calls, action)
		return nil
	}
	waitForUpdateParent = func(platform.ProcessIdentity, time.Duration) error {
		calls = append(calls, "wait")
		return nil
	}
	extractUpdateArchive = func(_, _ string) error {
		calls = append(calls, "extract")
		return nil
	}
	replaceUpdatedApplication = func(_, _ string) error {
		calls = append(calls, "replace")
		return nil
	}
	startUpdatedApplication = func(target string, args []string, workingDir string) error {
		calls = append(calls, "direct-start:"+target+":"+strings.Join(args, ",")+":"+workingDir)
		return nil
	}

	err := RunHelper(HelperOptions{
		ArchivePath: archivePath,
		TargetPath:  "/opt/webui",
		ParentPID:   42,
		RestartArgs: []string{"--addr", "localhost:9090"},
		WorkingDir:  "/opt",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"wait", "extract", "replace", "direct-start:/opt/webui:--addr,localhost:9090:/opt"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunHelperServiceModeStopsAfterFailure(t *testing.T) {
	tests := []struct {
		name      string
		failAt    string
		wantCalls []string
	}{
		{name: "stop", failAt: "stop", wantCalls: []string{"stop"}},
		{name: "wait", failAt: "wait", wantCalls: []string{"stop", "wait"}},
		{name: "extract", failAt: "extract", wantCalls: []string{"stop", "wait", "extract"}},
		{name: "replace", failAt: "replace", wantCalls: []string{"stop", "wait", "extract", "replace"}},
		{name: "start", failAt: "start", wantCalls: []string{"stop", "wait", "extract", "replace", "start"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restore := replaceHelperRuntimeForTest(t)
			defer restore()

			archivePath := filepath.Join(t.TempDir(), "update.zip")
			if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
				t.Fatal(err)
			}
			var calls []string
			fail := func(step string) error {
				calls = append(calls, step)
				if test.failAt == step {
					return errors.New("failed at " + step)
				}
				return nil
			}
			controlUpdatedService = func(_, _ string, action string) error { return fail(action) }
			waitForUpdateParent = func(platform.ProcessIdentity, time.Duration) error { return fail("wait") }
			extractUpdateArchive = func(_, _ string) error { return fail("extract") }
			replaceUpdatedApplication = func(_, _ string) error { return fail("replace") }
			serviceControlDelay = 0

			err := RunHelper(HelperOptions{
				ArchivePath: archivePath,
				TargetPath:  "/opt/webui",
				ParentPID:   42,
				ServiceMode: true,
			})
			if err == nil || !strings.Contains(err.Error(), "failed at "+test.failAt) {
				t.Fatalf("RunHelper error = %v", err)
			}
			if !reflect.DeepEqual(calls, test.wantCalls) {
				t.Fatalf("calls = %#v, want %#v", calls, test.wantCalls)
			}
		})
	}
}

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
