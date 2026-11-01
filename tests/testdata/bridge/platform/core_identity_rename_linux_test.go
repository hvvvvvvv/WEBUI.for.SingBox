//go:build linux

package platform

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A harmless sleep process proves why core updates must not rename the live
// executable. No service, TUN, network namespace or root permission is needed.
func TestRenamingRunningExecutableInvalidatesRecordedIdentity(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep executable unavailable")
	}
	source, err := os.Open(sleep)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	// Keep the applet name when /bin/sleep is supplied by BusyBox (OpenWrt).
	path := filepath.Join(t.TempDir(), "sleep")
	target, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(target, source)
	closeErr := target.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("copy helper: %v %v", err, closeErr)
	}
	cmd := exec.Command(path, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	identity, err := IdentifyProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".bak"); err != nil {
		t.Fatal(err)
	}
	current, err := IdentifyProcess(cmd.Process.Pid)
	if err != nil || current.Executable != path+".bak" {
		t.Fatalf("unexpected /proc executable after rename: %#v %v", current, err)
	}
	if _, err := identityAlive(identity); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("renamed executable no longer rejected: %v", err)
	}
}
