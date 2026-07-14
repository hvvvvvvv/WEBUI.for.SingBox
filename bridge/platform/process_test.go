package platform

import (
	"runtime"
	"strconv"
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
	result := service.ExecBackground("/bin/sh", []string{"-c", "exit 7"}, "", "", ExecOptions{
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
