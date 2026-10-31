package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"guiforcores/bridge/appupdate"
)

func TestUpdaterCommandPassesRestrictedPreparedUpdateName(t *testing.T) {
	previous := runUpdateHelper
	t.Cleanup(func() { runUpdateHelper = previous })
	var received appupdate.HelperOptions
	runUpdateHelper = func(options appupdate.HelperOptions) error { received = options; return nil }
	target := filepath.Join(t.TempDir(), "webui")
	archive := filepath.Join(filepath.Dir(target), "data", ".cache", "gui-update.zip")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"__updater", "--archive-path", archive, "--prepared-update", "gui-update-123abc",
		"--target-path", target, "--parent-pid", "123", "--parent-created", "456",
		"--parent-executable", target, "--log-days", "0",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("updater command failed: code=%d stderr=%s", code, stderr.String())
	}
	if received.PreparedUpdate != "gui-update-123abc" || received.ArchivePath != archive || received.TargetPath != target {
		t.Fatalf("prepared handoff options lost: %+v", received)
	}
}
