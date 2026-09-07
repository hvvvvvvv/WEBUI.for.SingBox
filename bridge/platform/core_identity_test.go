package platform

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"guiforcores/bridge/storage"
)

func TestPrepareCoreRefusesForeignPID(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, corePIDPath)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
	if err := PrepareCore(context.Background(), base); err == nil {
		t.Fatal("accepted a foreign process as core")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("unverified PID record removed", err)
	}
}

func TestPrepareCoreRefusesActiveOwner(t *testing.T) {
	base := t.TempDir()
	identity, err := IdentifyProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	owner := coreOwner{Session: "test", Backend: identity, Guard: identity, Core: identity}
	if err := writeCoreOwner(base, owner); err != nil {
		t.Fatal(err)
	}
	if err := PrepareCore(context.Background(), base); err == nil {
		t.Fatal("accepted an active owner")
	}
}

func TestIdentityRejectsReusedPID(t *testing.T) {
	identity, err := IdentifyProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	identity.Created--
	if _, err := identityAlive(identity); err == nil {
		t.Fatal("creation time mismatch was ignored")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := terminateIdentity(ctx, identity, false); err == nil {
		t.Fatal("termination did not reject mismatched identity")
	}
}

func TestCoreRecordRemovalMatchesSessionAndPID(t *testing.T) {
	base := t.TempDir()
	identity, err := IdentifyProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	owner := coreOwner{Session: "new", Backend: identity, Guard: identity, Core: identity}
	if err := writeCoreOwner(base, owner); err != nil {
		t.Fatal(err)
	}
	if err := removeCoreOwner(base, "old"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(base, coreOwnerPath))
	if err != nil {
		t.Fatal(err)
	}
	var saved coreOwner
	_ = json.Unmarshal(data, &saved)
	if saved.Session != "new" {
		t.Fatal("new session was overwritten")
	}
	if err := os.WriteFile(filepath.Join(base, corePIDPath), []byte("999"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeCoreOwner(base, "new"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(base, corePIDPath))
	if err != nil || string(data) != "999" {
		t.Fatal("newer PID record was removed")
	}
}

func TestPrepareCoreWaitsForGuardLock(t *testing.T) {
	base := t.TempDir()
	lock, err := storage.LockFile(filepath.Join(base, CoreLockPath))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := PrepareCore(ctx, base); err == nil {
		t.Fatal("ignored active core lock")
	}
}

func TestPrepareCoreStopsVerifiedLegacyProcess(t *testing.T) {
	base := t.TempDir()
	name := "sing-box"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(base, "data/sing-box", name)
	_ = os.MkdirAll(filepath.Dir(binary), 0755)
	source, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination, err := os.OpenFile(binary, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(destination, source)
	_ = destination.Close()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestCoreGuardHelper$", "--", "-c", filepath.Join(base, "data/sing-box/config.json"))
	SetCmdWindowHidden(cmd)
	cmd.Env = append(os.Environ(), "WEBUI_GUARD_TEST_ROLE=core")
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	buffer := make([]byte, 128)
	if _, err := output.Read(buffer); err != nil {
		t.Fatal(err)
	}
	identity, err := IdentifyProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, corePIDPath), []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Second)
	defer cancel()
	if err := PrepareCore(ctx, base); err != nil {
		t.Fatal(err)
	}
	if alive, err := identityAlive(identity); alive || err != nil {
		t.Fatalf("legacy core survived cleanup: %v %v", alive, err)
	}
	if _, err := os.Stat(filepath.Join(base, corePIDPath)); !os.IsNotExist(err) {
		t.Fatal("legacy PID record remains")
	}
}

func TestPrepareCoreRemovesDeadPID(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestCoreGuardHelper$", "--", "check")
	cmd.Env = append(os.Environ(), "WEBUI_GUARD_TEST_ROLE=core")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	path := filepath.Join(base, corePIDPath)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	if err := PrepareCore(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dead PID remains")
	}
}
