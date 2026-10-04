package platform

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"guiforcores/bridge/logging"
	"guiforcores/bridge/storage"
)

func guardTestCommand() (*exec.Cmd, error) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestCoreGuardHelper$")
	cmd.Env = append(os.Environ(), "WEBUI_GUARD_TEST_ROLE=guard")
	return cmd, nil
}

func TestCoreGuardHelper(t *testing.T) {
	role := os.Getenv("WEBUI_GUARD_TEST_ROLE")
	if role == "" {
		return
	}
	if role == "guard" {
		if err := RunCoreGuard(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if role == "backend" {
		newCoreGuardCommand = guardTestCommand
		s := NewService(storage.NewPaths(os.Getenv("WEBUI_GUARD_TEST_BASE")), nil, Environment{})
		result := s.ExecBackground(os.Args[0], []string{"-test.run=^TestCoreGuardHelper$"}, "", ExecOptions{
			Guard: true, Env: map[string]string{"WEBUI_GUARD_TEST_ROLE": "core"},
		})
		_ = json.NewEncoder(os.Stdout).Encode(result)
		if !result.Flag {
			os.Exit(1)
		}
		if os.Getenv("WEBUI_GUARD_TEST_CRASH") == "1" {
			os.Exit(33)
		}
		select {}
	}
	if role != "core" {
		os.Exit(2)
	}
	if os.Args[len(os.Args)-1] == "check" {
		fmt.Println("INFO configuration checked")
		if os.Getenv("WEBUI_GUARD_TEST_SLOW_CHECK") == "1" {
			select {}
		}
		if os.Getenv("WEBUI_GUARD_TEST_BAD_CHECK") == "1" {
			os.Exit(7)
		}
		os.Exit(0)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	if os.Getenv("WEBUI_GUARD_TEST_IGNORE_STOP") == "1" {
		signal.Ignore(os.Interrupt)
	}
	fmt.Println("INFO[0000] sing-box started")
	if os.Getenv("WEBUI_GUARD_TEST_LONG_LINE") == "1" {
		fmt.Println("DEBUG " + strings.Repeat("x", 128<<10))
	}
	<-stop
	fmt.Fprint(os.Stderr, "INFO core shutdown tail")
	os.Exit(0)
}

func useGuardTestCommand(t *testing.T) {
	t.Helper()
	previous := newCoreGuardCommand
	newCoreGuardCommand = guardTestCommand
	t.Cleanup(func() { newCoreGuardCommand = previous })
}

func TestGuardCapturesCheckRuntimeLongLineAndShutdownTail(t *testing.T) {
	useGuardTestCommand(t)
	s := NewService(storage.NewPaths(t.TempDir()), nil, Environment{})
	var mu sync.Mutex
	var outputs []logging.CoreOutput
	ready := make(chan struct{}, 1)
	result := s.ExecBackground(os.Args[0], []string{"-test.run=^TestCoreGuardHelper$"}, "", ExecOptions{
		Guard: true, CheckArgs: []string{"-test.run=^TestCoreGuardHelper$", "--", "check"},
		Env: map[string]string{"WEBUI_GUARD_TEST_ROLE": "core", "WEBUI_GUARD_TEST_LONG_LINE": "1"},
		OnOutput: func(output logging.CoreOutput) {
			mu.Lock()
			outputs = append(outputs, output)
			mu.Unlock()
			if len(output.Message) > 64<<10 {
				ready <- struct{}{}
			}
		},
	})
	if !result.Flag {
		t.Fatal(result.Data)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 16*time.Second)
		defer cancel()
		_ = s.CloseCoreProcesses(ctx)
	})
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("no runtime output")
	}
	pid, _ := strconv.Atoi(result.Data)
	if result := s.KillProcess(pid, 2); !result.Flag {
		t.Fatal(result.Data)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(outputs) != 4 {
		t.Fatalf("received %d output records", len(outputs))
	}
	if outputs[0].Operation != "check" || outputs[1].Operation != "startup" || outputs[2].Operation != "runtime" || outputs[3].Operation != "shutdown" {
		t.Fatalf("unexpected phases: %s %s %s %s", outputs[0].Operation, outputs[1].Operation, outputs[2].Operation, outputs[3].Operation)
	}
	if outputs[3].Message != "INFO core shutdown tail" {
		t.Fatalf("tail = %q", outputs[3].Message)
	}
	if _, err := os.Stat(filepath.Join(s.BaseDir(), coreOwnerPath)); !os.IsNotExist(err) {
		t.Fatalf("ownership record remains: %v", err)
	}
}

func TestGuardStopsWhenOnlyBackendDies(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("crash_%t", crash), func(t *testing.T) {
			base := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestCoreGuardHelper$")
			cmd.Env = append(os.Environ(), "WEBUI_GUARD_TEST_ROLE=backend", "WEBUI_GUARD_TEST_BASE="+base)
			if crash {
				cmd.Env = append(cmd.Env, "WEBUI_GUARD_TEST_CRASH=1")
			}
			configureCoreGuard(cmd)
			pipe, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			var result Result
			if err := json.NewDecoder(bufio.NewReader(pipe)).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if !result.Flag {
				t.Fatal(result.Data)
			}
			owner, err := readCoreOwner(base)
			if err != nil {
				t.Fatal(err)
			}
			if !crash {
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			}
			_ = cmd.Wait()
			deadline := time.Now().Add(7 * time.Second)
			for {
				alive, err := identityAlive(owner.Core)
				if err == nil && !alive {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("core survived backend death: alive=%v err=%v", alive, err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := WaitCoreShutdown(ctx, base); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGuardRejectsFailedCheckWithoutStartingCore(t *testing.T) {
	useGuardTestCommand(t)
	s := NewService(storage.NewPaths(t.TempDir()), nil, Environment{})
	result := s.ExecBackground(os.Args[0], []string{"-test.run=^TestCoreGuardHelper$"}, "", ExecOptions{
		Guard: true, CheckArgs: []string{"-test.run=^TestCoreGuardHelper$", "--", "check"},
		Env: map[string]string{"WEBUI_GUARD_TEST_ROLE": "core", "WEBUI_GUARD_TEST_BAD_CHECK": "1"},
	})
	if result.Flag || !strings.Contains(result.Data, "invalid core config") {
		t.Fatalf("result=%+v", result)
	}
}

func TestGuardForcedStopWaitsForActualExit(t *testing.T) {
	useGuardTestCommand(t)
	s := NewService(storage.NewPaths(t.TempDir()), nil, Environment{})
	ready := make(chan struct{}, 1)
	result := s.ExecBackground(os.Args[0], []string{"-test.run=^TestCoreGuardHelper$"}, "", ExecOptions{
		Guard: true, Env: map[string]string{"WEBUI_GUARD_TEST_ROLE": "core", "WEBUI_GUARD_TEST_IGNORE_STOP": "1"},
		OnOutput: func(logging.CoreOutput) {
			select {
			case ready <- struct{}{}:
			default:
			}
		},
	})
	if !result.Flag {
		t.Fatal(result.Data)
	}
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("core not ready")
	}
	pid, _ := strconv.Atoi(result.Data)
	owner, err := readCoreOwner(s.BaseDir())
	if err != nil {
		t.Fatal(err)
	}
	if result := s.KillProcess(pid, 1); !result.Flag {
		t.Fatal(result.Data)
	}
	if alive, err := identityAlive(owner.Core); alive || err != nil {
		t.Fatalf("core still alive: %v %v", alive, err)
	}
}

func TestGuardCrashTriggersBackendCleanup(t *testing.T) {
	useGuardTestCommand(t)
	s := NewService(storage.NewPaths(t.TempDir()), nil, Environment{})
	exited := make(chan error, 1)
	result := s.ExecBackground(os.Args[0], []string{"-test.run=^TestCoreGuardHelper$"}, "", ExecOptions{
		Guard: true, Env: map[string]string{"WEBUI_GUARD_TEST_ROLE": "core"},
		OnExit: func(_ int, err error) { exited <- err },
	})
	if !result.Flag {
		t.Fatal(result.Data)
	}
	owner, err := readCoreOwner(s.BaseDir())
	if err != nil {
		t.Fatal(err)
	}
	guard, err := os.FindProcess(owner.Guard.PID)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	if err := guard.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err == nil {
			t.Fatal("unexpected guard death reported as success")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("guard cleanup did not finish")
	}
	if alive, err := identityAlive(owner.Core); alive || err != nil {
		t.Fatalf("core survived guard death: %v %v", alive, err)
	}
}

func TestGuardPipeDisconnectDuringCheckAndBlockedOutput(t *testing.T) {
	for _, mode := range []string{"check_control", "output", "blocked_output_control"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			cmd, _ := guardTestCommand()
			configureCoreGuard(cmd)
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = input.Close(); _ = output.Close(); _ = cmd.Process.Kill() })
			backend, err := IdentifyProcess(os.Getpid())
			if err != nil {
				t.Fatal(err)
			}
			request := coreGuardRequest{Version: coreGuardVersion, Type: "start", BaseDir: base, Executable: os.Args[0], Backend: backend,
				Args: []string{"-test.run=^TestCoreGuardHelper$"}, Env: append(os.Environ(), "WEBUI_GUARD_TEST_ROLE=core", "WEBUI_GUARD_TEST_LONG_LINE=1"), WorkingDir: base}
			if mode == "check_control" {
				request.CheckArgs = []string{"-test.run=^TestCoreGuardHelper$", "--", "check"}
				request.Env = append(request.Env, "WEBUI_GUARD_TEST_SLOW_CHECK=1")
			}
			if err := json.NewEncoder(input).Encode(request); err != nil {
				t.Fatal(err)
			}
			// Do not drain output: a long runtime record fills the private pipe.
			var owner coreOwner
			deadline := time.Now().Add(5 * time.Second)
			for {
				owner, err = readCoreOwner(base)
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if mode == "output" {
				_ = output.Close()
			} else {
				_ = input.Close()
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				t.Fatal("pipe loss did not stop guard")
			}
			if alive, err := identityAlive(owner.Core); alive || err != nil {
				t.Fatalf("pipe loss left core alive: %v %v", alive, err)
			}
		})
	}
}

func TestGuardStartCancellationClosesControlPipe(t *testing.T) {
	useGuardTestCommand(t)
	s := NewService(storage.NewPaths(t.TempDir()), nil, Environment{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checked := make(chan struct{}, 1)
	result := make(chan Result, 1)
	go func() {
		result <- s.ExecBackground(os.Args[0], []string{"-test.run=^TestCoreGuardHelper$"}, "", ExecOptions{
			Guard: true, Context: ctx, CheckArgs: []string{"-test.run=^TestCoreGuardHelper$", "--", "check"},
			Env:      map[string]string{"WEBUI_GUARD_TEST_ROLE": "core", "WEBUI_GUARD_TEST_SLOW_CHECK": "1"},
			OnOutput: func(logging.CoreOutput) { checked <- struct{}{} },
		})
	}()
	select {
	case <-checked:
	case <-time.After(5 * time.Second):
		t.Fatal("no check output")
	}
	owner, err := readCoreOwner(s.BaseDir())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case result := <-result:
		if result.Flag {
			t.Fatal("cancelled startup succeeded")
		}
	case <-time.After(7 * time.Second):
		t.Fatal("cancellation blocked")
	}
	if alive, err := identityAlive(owner.Core); alive || err != nil {
		t.Fatalf("cancelled check alive: %v %v", alive, err)
	}
}
