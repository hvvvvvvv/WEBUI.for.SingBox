//go:build !windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func SetCmdWindowHidden(cmd *exec.Cmd) {
}

func configureCoreGuard(cmd *exec.Cmd)   { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func equalPlatformPath(a, b string) bool { return a == b }

// Writes to descriptor 1 normally terminate a Go process on SIGPIPE. The guard
// must instead observe the write error and clean up its child first.
func prepareCoreGuardIO() { signal.Ignore(syscall.SIGPIPE) }

func isProcessMissing(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone)
}

func SendExitSignal(p *os.Process) error {
	return p.Signal(syscall.SIGINT)
}

func IsProcessAlive(p *os.Process) (bool, error) {
	err := p.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false, nil
	}
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case syscall.ESRCH:
			return false, nil
		case syscall.EPERM:
			return true, nil
		}
	}
	return false, fmt.Errorf("failed to check process %d: %w", p.Pid, err)
}

func processExitedDuringIdentityRead(p *os.Process) bool {
	// Linux can remove /proc/PID/exe before the process becomes a zombie, and
	// signal 0 still succeeds for zombies until their parent reaps them. Allow
	// that short teardown window, but never treat a metadata error alone as exit.
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		alive, err := IsProcessAlive(p)
		if err == nil && !alive {
			return true
		}
		if processIsZombie(p.Pid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func IsPrivileged() (bool, error) {
	return os.Geteuid() == 0, nil
}
