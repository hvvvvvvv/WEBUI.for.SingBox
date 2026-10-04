package platform

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/storage"
)

func TestExecHonorsContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command is Unix-specific")
	}
	service := NewService(storage.NewPaths(t.TempDir()), nil, Environment{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	result := service.Exec("/bin/sh", []string{"-c", "exec sleep 30"}, ExecOptions{Context: ctx})
	if result.Flag || ctx.Err() == nil {
		t.Fatal("canceled command completed successfully")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("context did not interrupt command")
	}
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
