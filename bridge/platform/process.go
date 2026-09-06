package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

func (a *App) Exec(path string, args []string, options ExecOptions) FlagResult {
	started := time.Now()

	exePath := a.ResolvePath(path)

	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		exePath = path
	}

	cmd := exec.Command(exePath, args...)
	SetCmdWindowHidden(cmd)

	cmd.Dir = options.WorkingDirectory
	cmd.Env = os.Environ()

	for key, value := range options.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	out, err := cmd.CombinedOutput()

	var output string
	if options.Convert {
		output = strings.TrimSpace(ConvertByte2String(out))
	} else {
		output = strings.TrimSpace(string(out))
	}

	if err != nil {
		slog.Debug("process execution failed", "component", "process", "operation", "execute", "executable", exePath, "args", args, "working_directory", options.WorkingDirectory, "duration", time.Since(started), "result", "failure", "error", err)
		if output == "" {
			output = err.Error()
		}
		return FlagResult{false, output}
	}
	slog.Info("process executed", "component", "process", "operation", "execute", "executable", exePath, "args", args, "working_directory", options.WorkingDirectory, "duration", time.Since(started), "result", "success")

	return FlagResult{true, output}
}

func (a *App) ExecBackground(path string, args []string, outEvent string, options ExecOptions) FlagResult {
	started := time.Now()
	exePath := a.ResolvePath(path)
	pidPath := ""

	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		exePath = path
	}

	if options.PIDFile != "" {
		pidPath = a.ResolvePath(options.PIDFile)
	}

	cmd := exec.Command(exePath, args...)
	SetCmdWindowHidden(cmd)

	cmd.Dir = options.WorkingDirectory
	cmd.Env = os.Environ()

	for key, value := range options.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Debug("process output pipe failed", "component", "process", "operation", "start", "executable", exePath, "args", args, "result", "failure", "error", err)
		return FlagResult{false, err.Error()}
	}

	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		slog.Debug("process start failed", "component", "process", "operation", "start", "executable", exePath, "args", args, "working_directory", options.WorkingDirectory, "result", "failure", "error", err)
		return FlagResult{false, err.Error()}
	}

	pid := strconv.Itoa(cmd.Process.Pid)
	pidNumber := cmd.Process.Pid

	if outEvent != "" {
		scanAndEmit := func(reader io.Reader) {
			scanner := bufio.NewScanner(reader)
			stopOutput := false
			for scanner.Scan() {
				var text string
				if options.Convert {
					text = ConvertByte2String(scanner.Bytes())
				} else {
					text = scanner.Text()
				}

				if !stopOutput {
					a.publish(outEvent, text)

					if options.StopOutputKeyword != "" && strings.Contains(text, options.StopOutputKeyword) {
						stopOutput = true
					}
				}
			}
		}

		go scanAndEmit(stdout)
	}

	if pidPath != "" {
		err := os.WriteFile(pidPath, []byte(pid), os.ModePerm)
		if err != nil {
			slog.Debug("process PID file write failed", "component", "process", "operation", "write_pid", "executable", exePath, "pid", pidNumber, "path", pidPath, "result", "failure", "error", err)
			exited := make(chan struct{})
			go func() {
				_ = cmd.Wait()
				close(exited)
			}()
			_ = SendExitSignal(cmd.Process)
			_ = waitForExitNotification(exited, 10*time.Second, cmd.Process.Kill)
			return FlagResult{false, err.Error()}
		}
	}

	exited := make(chan struct{})
	managed := &managedProcess{process: cmd.Process, exited: exited}
	a.trackProcess(pidNumber, managed)
	go func() {
		waitErr := cmd.Wait()
		if pidPath != "" {
			_ = os.Remove(pidPath)
		}
		close(exited)
		a.untrackProcess(pidNumber, managed)
		if options.OnExit != nil {
			options.OnExit(cmd.Process.Pid, waitErr)
		}
		if waitErr != nil {
			slog.Error("background process exited", "component", "process", "operation", "wait", "executable", exePath, "pid", pidNumber, "duration", time.Since(started), "result", "failure", "error", waitErr)
		} else {
			slog.Info("background process exited", "component", "process", "operation", "wait", "executable", exePath, "pid", pidNumber, "duration", time.Since(started), "result", "success")
		}
	}()
	slog.Info("background process started", "component", "process", "operation", "start", "executable", exePath, "args", args, "working_directory", options.WorkingDirectory, "pid", pidNumber, "event", outEvent, "result", "success")

	return FlagResult{true, pid}
}

func (a *App) trackProcess(pid int, process *managedProcess) {
	a.managedProcessMu.Lock()
	a.managedProcesses[pid] = process
	a.managedProcessMu.Unlock()
}

func (a *App) untrackProcess(pid int, process *managedProcess) {
	a.managedProcessMu.Lock()
	if a.managedProcesses[pid] == process {
		delete(a.managedProcesses, pid)
	}
	a.managedProcessMu.Unlock()
}

func (a *App) trackedProcess(pid int) *managedProcess {
	a.managedProcessMu.Lock()
	defer a.managedProcessMu.Unlock()
	return a.managedProcesses[pid]
}

func (a *App) ProcessInfo(pid int32) FlagResult {
	slog.Debug("process information requested", "component", "process", "operation", "inspect", "pid", pid)
	proc, err := process.NewProcess(pid)
	if err != nil {
		return FlagResult{false, err.Error()}
	}

	name, err := proc.Name()
	if err != nil {
		return FlagResult{false, err.Error()}
	}

	return FlagResult{true, name}
}

func (a *App) ProcessMemory(pid int32) FlagResult {
	slog.Debug("process memory requested", "component", "process", "operation", "memory", "pid", pid)
	proc, err := process.NewProcess(pid)
	if err != nil {
		return FlagResult{false, err.Error()}
	}

	memInfo, err := proc.MemoryInfo()
	if err != nil {
		return FlagResult{false, err.Error()}
	}

	return FlagResult{true, strconv.FormatUint(memInfo.RSS, 10)}
}

func (a *App) KillProcess(pid int, timeout int) FlagResult {
	started := time.Now()
	managed := a.trackedProcess(pid)
	var target *os.Process
	if managed != nil {
		target = managed.process
	} else {
		process, err := os.FindProcess(pid)
		if err != nil {
			return FlagResult{false, err.Error()}
		}
		target = process
	}

	if err := SendExitSignal(target); err != nil {
		slog.Warn("process exit signal failed", "component", "process", "operation", "signal", "pid", pid, "result", "failure", "error", err)
	}

	var err error
	if managed != nil {
		err = waitForTrackedProcessExitWithTimeout(target, managed.exited, timeout)
	} else {
		err = waitForProcessExitWithTimeout(target, timeout)
	}
	if err != nil {
		slog.Debug("process termination failed", "component", "process", "operation", "terminate", "pid", pid, "timeout", time.Duration(timeout)*time.Second, "duration", time.Since(started), "result", "failure", "error", err)
		return FlagResult{false, err.Error()}
	}
	slog.Info("process terminated", "component", "process", "operation", "terminate", "pid", pid, "timeout", time.Duration(timeout)*time.Second, "duration", time.Since(started), "result", "success")

	return FlagResult{true, "Success"}
}

func waitForTrackedProcessExitWithTimeout(process *os.Process, exited <-chan struct{}, timeoutSeconds int) error {
	if err := waitForExitNotification(exited, time.Duration(timeoutSeconds)*time.Second, process.Kill); err != nil {
		return fmt.Errorf("timed out after %d seconds waiting for process %d, and failed to kill it: %w", timeoutSeconds, process.Pid, err)
	}
	return nil
}

func waitForExitNotification(exited <-chan struct{}, timeout time.Duration, forceKill func() error) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-exited:
		return nil
	case <-timer.C:
		if err := forceKill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		return nil
	}
}

func waitForProcessExitWithTimeout(process *os.Process, timeoutSeconds int) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	interval := 10 * time.Millisecond
	maxInterval := 1000 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			if killErr := process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				return fmt.Errorf("timed out after %d seconds waiting for process %d, and failed to kill it: %w", timeoutSeconds, process.Pid, killErr)
			}
			return nil

		default:
			alive, err := IsProcessAlive(process)
			if err != nil {
				return fmt.Errorf("failed to check status of process %d: %w", process.Pid, err)
			}
			if !alive {
				return nil
			}

			time.Sleep(interval)
			interval = min(time.Duration(interval*2), maxInterval)
		}
	}
}
