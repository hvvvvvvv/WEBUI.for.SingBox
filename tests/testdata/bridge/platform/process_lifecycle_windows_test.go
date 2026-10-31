//go:build windows

package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/windows"
	"guiforcores/bridge/storage"
)

func missingWindowsPID(t *testing.T) int {
	t.Helper()
	// Confirm absence through the OS rather than assuming a fixed PID is unused.
	for pid := 1<<30 - 1; pid > 1<<30-1024; pid-- {
		p, err := os.FindProcess(pid)
		if p != nil {
			_ = p.Release()
		}
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return pid
		}
	}
	t.Fatal("could not find a confirmed absent PID")
	return 0
}

func TestWindowsMissingProcessDoesNotBlockLifecycleRecovery(t *testing.T) {
	pid := missingWindowsPID(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	identity := ProcessIdentity{PID: pid, Created: 1, Executable: executable}
	if alive, err := identityAlive(identity); err != nil || alive {
		t.Fatalf("confirmed absent process: alive=%v error=%v", alive, err)
	}
	if err := WaitProcessExit(context.Background(), identity); err != nil {
		t.Fatalf("updater blocked by an exited process: %v", err)
	}
	if err := terminateIdentity(context.Background(), identity, false); err != nil {
		t.Fatalf("cleanup of an exited process failed: %v", err)
	}
	if alive, err := IsPIDAlive(pid); err != nil || alive {
		t.Fatalf("startup liveness: alive=%v error=%v", alive, err)
	}

	base := t.TempDir()
	service := NewService(storage.NewPaths(base), nil, Environment{})
	if result := service.KillProcess(pid, 1); !result.Flag {
		t.Fatalf("startup cleanup of an exited process failed: %s", result.Data)
	}
	owner := coreOwner{Session: "previous-run", Backend: identity, Guard: identity, Core: identity}
	if err := writeCoreOwner(base, owner); err != nil {
		t.Fatal(err)
	}
	if err := WaitCoreShutdown(context.Background(), base); err != nil {
		t.Fatalf("updater blocked by exited guard/core: %v", err)
	}
	if err := PrepareCore(context.Background(), base); err != nil {
		t.Fatalf("restart blocked by stale ownership: %v", err)
	}
	for _, path := range []string{coreOwnerPath, corePIDPath} {
		if _, err := os.Stat(filepath.Join(base, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale record %s remains: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, corePIDPath), []byte(strconv.Itoa(pid)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareCore(context.Background(), base); err != nil {
		t.Fatalf("legacy PID blocked restart: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, corePIDPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy PID record remains: %v", err)
	}
}

func TestWindowsProcessAbsenceDoesNotHidePermissionFailures(t *testing.T) {
	if !isProcessMissing(os.NewSyscallError("OpenProcess", windows.ERROR_INVALID_PARAMETER)) {
		t.Fatal("did not recognize a wrapped absent-process error")
	}
	for _, err := range []error{nil, windows.ERROR_ACCESS_DENIED, os.NewSyscallError("OpenProcess", windows.ERROR_ACCESS_DENIED), windows.ERROR_NOT_ENOUGH_MEMORY} {
		if isProcessMissing(err) {
			t.Fatalf("treated an unconfirmed process as absent: %v", err)
		}
	}
}
