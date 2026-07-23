//go:build linux

package appupdate

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestStartUpdateProcessSelectsSystemdOnlyForSystemService(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		serviceMode bool
		wantSystemd bool
	}{
		{name: "systemd service", platform: "linux-systemd", serviceMode: true, wantSystemd: true},
		{name: "systemd foreground", platform: "linux-systemd", serviceMode: false, wantSystemd: false},
		{name: "openrc service", platform: "linux-openrc", serviceMode: true, wantSystemd: false},
		{name: "systemv service", platform: "unix-systemv", serviceMode: true, wantSystemd: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restoreUpdateProcessRuntimeForTest(t)
			currentUpdateServicePlatform = func() string { return test.platform }
			var directCalls, systemdCalls int
			startDirectUpdateProcess = func(string, []string, string) error {
				directCalls++
				return nil
			}
			launchSystemdUpdateProcess = func(string, []string, string) error {
				systemdCalls++
				return nil
			}

			if err := startUpdateProcess("/tmp/updater", []string{"__updater"}, "/opt/app", test.serviceMode); err != nil {
				t.Fatal(err)
			}
			if test.wantSystemd {
				if systemdCalls != 1 || directCalls != 0 {
					t.Fatalf("systemd/direct calls = %d/%d, want 1/0", systemdCalls, directCalls)
				}
			} else if systemdCalls != 0 || directCalls != 1 {
				t.Fatalf("systemd/direct calls = %d/%d, want 0/1", systemdCalls, directCalls)
			}
		})
	}
}

func TestRunSystemdUpdateProcessBuildsDetachedUnitCommand(t *testing.T) {
	restoreUpdateProcessRuntimeForTest(t)
	nextSystemdUpdateUnitName = func() string { return "webui-for-singbox-update-test" }
	var gotArgs []string
	var gotWorkingDir string
	executeSystemdRun = func(args []string, workingDir string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		gotWorkingDir = workingDir
		return nil, nil
	}

	err := runSystemdUpdateProcess(
		"/tmp/webui$helper",
		[]string{"__updater", "--archive-path", "/opt/app/$release.zip", "--restart-args", `["$VALUE"]`},
		"/opt/app",
	)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{
		"--quiet",
		"--unit", "webui-for-singbox-update-test",
		"--description", systemdUpdateDescription,
		"--service-type=exec",
		"/tmp/webui$$helper",
		"__updater", "--archive-path", "/opt/app/$$release.zip", "--restart-args", `["$$VALUE"]`,
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("systemd-run args = %#v, want %#v", gotArgs, wantArgs)
	}
	if gotWorkingDir != "/opt/app" {
		t.Fatalf("systemd-run working directory = %q", gotWorkingDir)
	}
}

func TestRunSystemdUpdateProcessReturnsCommandOutput(t *testing.T) {
	restoreUpdateProcessRuntimeForTest(t)
	nextSystemdUpdateUnitName = func() string { return "webui-for-singbox-update-test" }
	executeSystemdRun = func([]string, string) ([]byte, error) {
		return []byte("Failed to start transient service: Access denied\n"), errors.New("exit status 1")
	}

	err := runSystemdUpdateProcess("/tmp/updater", []string{"__updater"}, "/opt/app")
	if err == nil {
		t.Fatal("expected systemd-run failure")
	}
	if !strings.Contains(err.Error(), "start updater with systemd-run") ||
		!strings.Contains(err.Error(), "Access denied") ||
		!strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("unexpected systemd-run error: %v", err)
	}
}

func TestStartUpdateProcessDoesNotFallbackAfterSystemdFailure(t *testing.T) {
	restoreUpdateProcessRuntimeForTest(t)
	currentUpdateServicePlatform = func() string { return "linux-systemd" }
	var directCalls int
	startDirectUpdateProcess = func(string, []string, string) error {
		directCalls++
		return nil
	}
	wantErr := errors.New("systemd-run unavailable")
	launchSystemdUpdateProcess = func(string, []string, string) error {
		return wantErr
	}

	err := startUpdateProcess("/tmp/updater", []string{"__updater"}, "/opt/app", true)
	if !errors.Is(err, wantErr) {
		t.Fatalf("start update error = %v, want %v", err, wantErr)
	}
	if directCalls != 0 {
		t.Fatalf("direct updater started %d times after systemd failure", directCalls)
	}
}

func TestEscapeSystemdUpdateArgument(t *testing.T) {
	if got, want := escapeSystemdUpdateArgument(`$HOME/${BUILD}/$$/plain`), `$$HOME/$${BUILD}/$$$$/plain`; got != want {
		t.Fatalf("escaped argument = %q, want %q", got, want)
	}
}

func restoreUpdateProcessRuntimeForTest(t *testing.T) {
	t.Helper()
	originalPlatform := currentUpdateServicePlatform
	originalDirect := startDirectUpdateProcess
	originalSystemd := launchSystemdUpdateProcess
	originalUnitName := nextSystemdUpdateUnitName
	originalExecute := executeSystemdRun
	t.Cleanup(func() {
		currentUpdateServicePlatform = originalPlatform
		startDirectUpdateProcess = originalDirect
		launchSystemdUpdateProcess = originalSystemd
		nextSystemdUpdateUnitName = originalUnitName
		executeSystemdRun = originalExecute
	})
}
