package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"guiforcores/bridge/storage"
)

const CoreLockPath = "data/sing-box/core.lock"
const coreOwnerPath = "data/sing-box/process.json"
const corePIDPath = "data/sing-box/pid.txt"

var ErrCoreExitUnconfirmed = errors.New("core exit could not be confirmed")

type ProcessIdentity struct {
	PID        int    `json:"pid"`
	Created    int64  `json:"created"`
	Executable string `json:"executable"`
}

type coreOwner struct {
	Session string          `json:"session"`
	Backend ProcessIdentity `json:"backend"`
	Guard   ProcessIdentity `json:"guard"`
	Core    ProcessIdentity `json:"core"`
}

func IdentifyProcess(pid int) (ProcessIdentity, error) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return ProcessIdentity{}, err
	}
	created, err := p.CreateTime()
	if err != nil {
		return ProcessIdentity{}, err
	}
	executable, err := p.Exe()
	if err != nil {
		return ProcessIdentity{}, err
	}
	return ProcessIdentity{PID: pid, Created: created, Executable: filepath.Clean(executable)}, nil
}

func identityAlive(identity ProcessIdentity) (bool, error) {
	if identity.PID <= 0 {
		return false, nil
	}
	p, err := os.FindProcess(identity.PID)
	if err != nil {
		return false, err
	}
	defer p.Release()
	alive, err := IsProcessAlive(p)
	if err != nil || !alive {
		return alive, err
	}
	if proc, err := process.NewProcess(int32(identity.PID)); err == nil {
		if states, err := proc.Status(); err == nil {
			for _, state := range states {
				if state == "Z" || state == "zombie" {
					return false, nil
				}
			}
		}
	}
	current, err := IdentifyProcess(identity.PID)
	if err != nil {
		// The process may exit between the liveness probe and /proc/handle
		// identity reads. Only a confirmed disappearance makes this safe.
		if errors.Is(err, process.ErrorProcessNotRunning) {
			return false, nil
		}
		if stillAlive, checkErr := IsProcessAlive(p); checkErr == nil && !stillAlive {
			return false, nil
		}
		return false, fmt.Errorf("verify process %d: %w", identity.PID, err)
	}
	if current.Created != identity.Created || !samePath(current.Executable, identity.Executable) {
		return false, fmt.Errorf("process %d identity changed; refusing to signal it", identity.PID)
	}
	return true, nil
}

func samePath(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	return equalPlatformPath(filepath.Clean(a), filepath.Clean(b))
}

func writeCoreOwner(base string, owner coreOwner) error {
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	if err := storage.AtomicWriteFile(filepath.Join(base, coreOwnerPath), data, 0600); err != nil {
		return err
	}
	return storage.AtomicWriteFile(filepath.Join(base, corePIDPath), []byte(strconv.Itoa(owner.Core.PID)), 0644)
}

func readCoreOwner(base string) (coreOwner, error) {
	var owner coreOwner
	data, err := os.ReadFile(filepath.Join(base, coreOwnerPath))
	if err != nil {
		return owner, err
	}
	err = json.Unmarshal(data, &owner)
	if err == nil && (owner.Session == "" || !validIdentity(owner.Guard) || !validIdentity(owner.Backend) || !validIdentity(owner.Core)) {
		err = errors.New("invalid core ownership record")
	}
	return owner, err
}

func validIdentity(identity ProcessIdentity) bool {
	return identity.PID > 0 && identity.Created > 0 && filepath.IsAbs(identity.Executable)
}

// WaitProcessExit validates identity on every poll, including PID reuse. It does
// not send signals, and is also used by the detached application updater.
func WaitProcessExit(ctx context.Context, identity ProcessIdentity) error {
	if !validIdentity(identity) {
		return errors.New("process identity is incomplete")
	}
	for {
		alive, err := identityAlive(identity)
		if err != nil || !alive {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("process %d is still running: %w", identity.PID, ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func removeCoreOwner(base, session string) error {
	owner, err := readCoreOwner(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if owner.Session != session {
		return nil
	}
	pidPath := filepath.Join(base, corePIDPath)
	if data, err := os.ReadFile(pidPath); err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(owner.Core.PID) {
		if err := os.Remove(pidPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Remove(filepath.Join(base, coreOwnerPath))
}

// PrepareCore removes only verified, abandoned processes while holding the core lock.
// The caller must already own the backend directory lock.
func PrepareCore(ctx context.Context, base string) error {
	lock, err := storage.WaitFileLock(ctx, filepath.Join(base, CoreLockPath))
	if err != nil {
		return fmt.Errorf("wait for previous core guard: %w", err)
	}
	defer lock.Close()
	owner, err := readCoreOwner(base)
	if err == nil {
		for _, identity := range []ProcessIdentity{owner.Backend, owner.Guard} {
			alive, checkErr := identityAlive(identity)
			if checkErr != nil {
				return checkErr
			}
			if alive {
				return fmt.Errorf("core is still owned by process %d", identity.PID)
			}
		}
		if alive, err := identityAlive(owner.Core); err != nil {
			return err
		} else if alive {
			if err := terminateIdentity(ctx, owner.Core, true); err != nil {
				return err
			}
		}
		return removeCoreOwner(base, owner.Session)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read core ownership: %w", err)
	}
	pidPath := filepath.Join(base, corePIDPath)
	data, err := os.ReadFile(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid legacy core PID; remove stale PID file after checking running processes")
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	defer p.Release()
	alive, err := IsProcessAlive(p)
	if err != nil {
		return err
	}
	if !alive {
		return os.Remove(pidPath)
	}
	identity, err := IdentifyProcess(pid)
	if err != nil {
		return err
	}
	name := filepath.Base(identity.Executable)
	if (name != "sing-box" && name != "sing-box-latest" && name != "sing-box.exe" && name != "sing-box-latest.exe") || !samePath(filepath.Dir(identity.Executable), filepath.Join(base, "data/sing-box")) {
		return fmt.Errorf("legacy PID %d does not identify this application's core; stop it manually if appropriate", pid)
	}
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return err
	}
	args, err := proc.CmdlineSlice()
	if err != nil {
		return err
	}
	matched := false
	for i, arg := range args {
		var path string
		if (arg == "-c" || arg == "--config") && i+1 < len(args) {
			path = args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			path = strings.TrimPrefix(arg, "--config=")
		}
		if path != "" && filepath.IsAbs(path) && samePath(path, filepath.Join(base, "data/sing-box/config.json")) {
			matched = true
		}
	}
	if !matched {
		return fmt.Errorf("cannot verify legacy core %d configuration path; stop it manually", pid)
	}
	parent, err := proc.Ppid()
	if err != nil {
		return err
	}
	if parent > 1 {
		parentIdentity, err := IdentifyProcess(int(parent))
		if err != nil {
			return fmt.Errorf("cannot verify legacy core parent: %w", err)
		}
		if samePath(filepath.Dir(parentIdentity.Executable), base) {
			return fmt.Errorf("legacy core %d still has an active owner %d", pid, parent)
		}
	}
	if err := terminateIdentity(ctx, identity, true); err != nil {
		return err
	}
	return os.Remove(pidPath)
}

func terminateIdentity(ctx context.Context, identity ProcessIdentity, graceful bool) error {
	alive, err := identityAlive(identity)
	if err != nil || !alive {
		return err
	}
	p, err := os.FindProcess(identity.PID)
	if err != nil {
		return err
	}
	defer p.Release()
	if graceful {
		_ = SendExitSignal(p)
	} else if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	grace := time.NewTimer(10 * time.Second)
	defer grace.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		alive, err := identityAlive(identity)
		if err != nil || !alive {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("process %d has not exited: %w", identity.PID, ctx.Err())
		case <-grace.C:
			if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return err
			}
		case <-tick.C:
		}
	}
}

// WaitCoreShutdown is read-only with respect to processes: updates never kill
// an unverifiable process, and never replace a binary still used by a guard.
func WaitCoreShutdown(ctx context.Context, base string) error {
	lock, err := storage.WaitFileLock(ctx, filepath.Join(base, CoreLockPath))
	if err != nil {
		return err
	}
	defer lock.Close()
	owner, err := readCoreOwner(base)
	if errors.Is(err, os.ErrNotExist) {
		data, e := os.ReadFile(filepath.Join(base, corePIDPath))
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if strings.TrimSpace(string(data)) != "" {
			return errors.New("core PID record remains; refusing application replacement")
		}
		return nil
	}
	if err != nil {
		return err
	}
	for _, identity := range []ProcessIdentity{owner.Core, owner.Guard} {
		if err := WaitProcessExit(ctx, identity); err != nil {
			return err
		}
	}
	return nil
}
