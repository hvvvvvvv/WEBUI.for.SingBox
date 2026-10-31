//go:build linux

package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLinuxIdentityExitHelper(t *testing.T) {
	if os.Getenv("WEBUI_IDENTITY_EXIT_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func TestLinuxZombieConfirmsExitAfterIdentityReadFailure(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLinuxIdentityExitHelper$")
	cmd.Env = append(os.Environ(), "WEBUI_IDENTITY_EXIT_HELPER=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = input.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	identity, err := IdentifyProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if processExitedDuringIdentityRead(cmd.Process) {
		t.Fatal("a live process was mistaken for an exited process")
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}

	// Do not call Wait until cleanup: retain a real zombie whose PID still
	// exists while /proc/PID/exe has already disappeared.
	procPath := filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid))
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := os.ReadFile(filepath.Join(procPath, "status"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(status), "State:\tZ") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child did not become a zombie: %s", status)
		}
		time.Sleep(time.Millisecond)
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("zombie PID should still exist before Wait: %v", err)
	}
	if _, err := IdentifyProcess(cmd.Process.Pid); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing zombie executable, got %v", err)
	}
	if !processExitedDuringIdentityRead(cmd.Process) {
		t.Fatal("identity-read failure did not recognize an unreaped zombie")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := WaitProcessExit(ctx, identity); err != nil {
		t.Fatalf("unreaped zombie blocked lifecycle cleanup: %v", err)
	}
}
