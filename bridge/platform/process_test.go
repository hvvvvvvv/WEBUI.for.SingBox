package platform

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/storage"
)

func TestExecBackgroundCallsOnExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command is Unix-specific")
	}

	type exitResult struct {
		pid int
		err error
	}
	exited := make(chan exitResult, 1)
	service := NewService(storage.NewPaths(t.TempDir()), nil, Environment{})
	result := service.ExecBackground("/bin/sh", []string{"-c", "exit 7"}, "", ExecOptions{
		OnExit: func(pid int, err error) {
			exited <- exitResult{pid: pid, err: err}
		},
	})
	if !result.Flag {
		t.Fatalf("start background process: %s", result.Data)
	}
	pid, err := strconv.Atoi(result.Data)
	if err != nil {
		t.Fatalf("parse background pid: %v", err)
	}

	select {
	case exit := <-exited:
		if exit.pid != pid {
			t.Fatalf("exit callback pid = %d, want %d", exit.pid, pid)
		}
		if exit.err == nil {
			t.Fatal("expected non-zero process exit error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for background exit callback")
	}
}

func TestWaitForExitNotification(t *testing.T) {
	t.Run("exit before timeout", func(t *testing.T) {
		exited := make(chan struct{})
		close(exited)
		killCalls := 0

		err := waitForExitNotification(exited, time.Hour, func() error {
			killCalls++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if killCalls != 0 {
			t.Fatalf("force kill calls = %d, want 0", killCalls)
		}
	})

	t.Run("timeout forces kill", func(t *testing.T) {
		killCalls := 0
		err := waitForExitNotification(make(chan struct{}), 0, func() error {
			killCalls++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if killCalls != 1 {
			t.Fatalf("force kill calls = %d, want 1", killCalls)
		}
	})

	t.Run("already exited is success", func(t *testing.T) {
		err := waitForExitNotification(make(chan struct{}), 0, func() error {
			return os.ErrProcessDone
		})
		if err != nil {
			t.Fatalf("already exited process returned an error: %v", err)
		}
	})

	t.Run("kill error is returned", func(t *testing.T) {
		killErr := errors.New("permission denied")
		err := waitForExitNotification(make(chan struct{}), 0, func() error {
			return killErr
		})
		if !errors.Is(err, killErr) {
			t.Fatalf("wait error = %v, want %v", err, killErr)
		}
	})
}

func TestKillProcessUsesManagedExitNotification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command is Unix-specific")
	}

	service := NewService(storage.NewPaths(t.TempDir()), nil, Environment{})
	callbackStarted := make(chan struct{}, 1)
	releaseCallback := make(chan struct{})
	callbackFinished := make(chan struct{})
	var callbackCalls atomic.Int32

	result := service.ExecBackground("/bin/sh", []string{"-c", "trap 'exit 0' INT; while :; do :; done"}, "", ExecOptions{
		OnExit: func(int, error) {
			callbackCalls.Add(1)
			callbackStarted <- struct{}{}
			<-releaseCallback
			close(callbackFinished)
		},
	})
	if !result.Flag {
		t.Fatalf("start background process: %s", result.Data)
	}
	pid, err := strconv.Atoi(result.Data)
	if err != nil {
		t.Fatalf("parse background pid: %v", err)
	}

	killed := make(chan Result, 1)
	go func() {
		killed <- service.KillProcess(pid, 2)
	}()

	select {
	case result := <-killed:
		if !result.Flag {
			close(releaseCallback)
			t.Fatalf("kill managed process: %s", result.Data)
		}
	case <-time.After(3 * time.Second):
		close(releaseCallback)
		t.Fatal("KillProcess blocked on the OnExit callback")
	}

	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		close(releaseCallback)
		t.Fatal("OnExit callback was not called")
	}
	close(releaseCallback)
	select {
	case <-callbackFinished:
	case <-time.After(time.Second):
		t.Fatal("OnExit callback did not finish")
	}

	if calls := callbackCalls.Load(); calls != 1 {
		t.Fatalf("OnExit callback calls = %d, want 1", calls)
	}
	if service.trackedProcess(pid) != nil {
		t.Fatal("exited process is still tracked")
	}
}

func TestKillProcessSupportsUntrackedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command is Unix-specific")
	}

	cmd := exec.Command("/bin/sh", "-c", "trap 'exit 0' INT; while :; do :; done")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	service := NewService(storage.NewPaths(t.TempDir()), nil, Environment{})
	result := service.KillProcess(cmd.Process.Pid, 2)
	if !result.Flag {
		_ = cmd.Process.Kill()
		t.Fatalf("kill untracked process: %s", result.Data)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("untracked process did not exit")
	}
}
