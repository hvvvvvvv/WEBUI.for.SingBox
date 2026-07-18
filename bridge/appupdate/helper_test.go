package appupdate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUpdateHelperArguments(t *testing.T) {
	args := updateHelperArguments(
		"/tmp/update.zip",
		"/opt/webui",
		42,
		`["--addr","127.0.0.1:8080"]`,
		"/opt",
		true,
	)
	want := []string{
		"__updater",
		"--archive-path", "/tmp/update.zip",
		"--target-path", "/opt/webui",
		"--parent-pid", "42",
		"--restart-args", `["--addr","127.0.0.1:8080"]`,
		"--working-dir", "/opt",
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
	waitForUpdateParent = func(int, time.Duration) error {
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
	want := []string{"stop", "wait", "extract", "replace", "start"}
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
	waitForUpdateParent = func(int, time.Duration) error {
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
			waitForUpdateParent = func(int, time.Duration) error { return fail("wait") }
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
	originalWait := waitForUpdateParent
	originalExtract := extractUpdateArchive
	originalReplace := replaceUpdatedApplication
	originalControl := controlUpdatedService
	originalStart := startUpdatedApplication
	originalDelay := serviceControlDelay
	return func() {
		waitForUpdateParent = originalWait
		extractUpdateArchive = originalExtract
		replaceUpdatedApplication = originalReplace
		controlUpdatedService = originalControl
		startUpdatedApplication = originalStart
		serviceControlDelay = originalDelay
	}
}
