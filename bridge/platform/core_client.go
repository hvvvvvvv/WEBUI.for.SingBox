package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"guiforcores/bridge/storage"
)

var newCoreGuardCommand = func() (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return exec.Command(executable, CoreGuardCommand), nil
}

type guardProcess struct {
	cmd       *exec.Cmd
	control   io.WriteCloser
	controlMu sync.Mutex
	stopOnce  sync.Once
	done      chan struct{}
	finished  chan struct{}
	mu        sync.Mutex
	identity  ProcessIdentity
	err       error
}

func (g *guardProcess) command(request coreGuardRequest) error {
	g.controlMu.Lock()
	defer g.controlMu.Unlock()
	request.Version = coreGuardVersion
	return json.NewEncoder(g.control).Encode(request)
}

func (g *guardProcess) requestStop(seconds int) {
	g.stopOnce.Do(func() {
		if err := g.command(coreGuardRequest{Type: "stop", GraceSeconds: seconds}); err != nil {
			_ = g.control.Close()
		}
	})
}

func (g *guardProcess) stop(seconds int) error {
	seconds = max(0, min(seconds, 10))
	g.requestStop(seconds)
	timer := time.NewTimer(time.Duration(seconds+5) * time.Second)
	defer timer.Stop()
	select {
	case <-g.done:
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.err
	case <-timer.C:
		return errors.New("core guard did not confirm process exit")
	}
}

func (s *Service) execCoreGuard(path string, args []string, outEvent string, options ExecOptions) Result {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{false, err.Error()}
	}
	backend, err := IdentifyProcess(os.Getpid())
	if err != nil {
		return Result{false, err.Error()}
	}
	cmd, err := newCoreGuardCommand()
	if err != nil {
		return Result{false, err.Error()}
	}
	configureCoreGuard(cmd)
	control, err := cmd.StdinPipe()
	if err != nil {
		return Result{false, err.Error()}
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = control.Close()
		return Result{false, err.Error()}
	}
	// Guard failures travel through the protocol; stderr cannot leak core content
	// to the application console or fill an undrained pipe.
	cmd.Stderr = io.Discard
	g := &guardProcess{cmd: cmd, control: control, done: make(chan struct{}), finished: make(chan struct{})}
	s.guardMu.Lock()
	if s.guardsClosing {
		s.guardMu.Unlock()
		_ = control.Close()
		_ = output.Close()
		return Result{false, "application is shutting down"}
	}
	if s.guards == nil {
		s.guards = make(map[*guardProcess]struct{})
	}
	s.guards[g] = struct{}{}
	err = cmd.Start()
	if err != nil {
		delete(s.guards, g)
	}
	s.guardMu.Unlock()
	if err != nil {
		_ = control.Close()
		_ = output.Close()
		return Result{false, err.Error()}
	}
	base, _ := filepath.Abs(s.BaseDir())
	executable, _ := filepath.Abs(s.ResolvePath(path))
	env := os.Environ()
	for k, v := range options.Env {
		env = append(env, k+"="+v)
	}
	working := options.WorkingDirectory
	if working == "" {
		working = base
	}
	ready := make(chan Result, 1)
	go s.readCoreGuard(g, output, ready, outEvent, options)
	if err := g.command(coreGuardRequest{Type: "start", BaseDir: base, Executable: executable, Args: args, CheckArgs: options.CheckArgs, Env: env, WorkingDir: working, Backend: backend}); err != nil {
		_ = control.Close()
		return Result{false, err.Error()}
	}
	select {
	case result := <-ready:
		return result
	case <-ctx.Done():
		_ = control.Close() // cancellation during check/start forces the unfinished core down
		select {
		case <-g.done:
		case <-time.After(5 * time.Second):
		}
		return Result{false, ctx.Err().Error()}
	}
}

func (s *Service) readCoreGuard(g *guardProcess, output io.ReadCloser, ready chan<- Result, outEvent string, options ExecOptions) {
	defer func() {
		s.guardMu.Lock()
		delete(s.guards, g)
		s.guardMu.Unlock()
		close(g.finished)
	}()
	decoder := json.NewDecoder(output)
	var started, reportedExit, expected, stopFrontend bool
	var corePID int
	var resultErr error
	var managed *managedProcess
	for {
		var event coreGuardEvent
		if err := decoder.Decode(&event); err != nil {
			if !errors.Is(err, io.EOF) {
				resultErr = fmt.Errorf("read core guard: %w", err)
			}
			break
		}
		if event.Version != coreGuardVersion {
			resultErr = errors.New("unsupported core guard protocol")
			_ = g.control.Close()
			break
		}
		switch event.Type {
		case "spawn":
			g.mu.Lock()
			g.identity = event.Identity
			g.mu.Unlock()
		case "started":
			if started {
				resultErr = errors.New("duplicate core guard start event")
				_ = g.control.Close()
				continue
			}
			started = true
			corePID = event.Identity.PID
			managed = &managedProcess{exited: g.done, guard: g}
			s.trackProcess(corePID, managed)
			if options.OnStarted != nil {
				options.OnStarted(corePID)
			}
			ready <- Result{true, strconv.Itoa(corePID)}
		case "output":
			if event.Output == nil {
				continue
			}
			if options.OnOutput != nil {
				options.OnOutput(*event.Output)
			}
			if outEvent != "" && !stopFrontend {
				s.publish(outEvent, event.Output.Message)
				if options.StopOutputKeyword != "" && strings.Contains(event.Output.Message, options.StopOutputKeyword) {
					stopFrontend = true
				}
			}
		case "error":
			resultErr = errors.New(event.Error)
		case "exit":
			reportedExit, expected = true, event.Expected
			if event.Error != "" {
				resultErr = errors.New(event.Error)
			}
		}
	}
	_ = output.Close()
	_ = g.control.Close()
	waitErr := g.cmd.Wait()
	if !reportedExit && started {
		if resultErr == nil {
			resultErr = errors.New("core guard exited unexpectedly")
		}
		g.mu.Lock()
		identity := g.identity
		g.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := terminateIdentity(ctx, identity, false); err != nil {
			resultErr = errors.Join(resultErr, ErrCoreExitUnconfirmed, err)
		}
		cancel()
	} else if waitErr != nil && resultErr == nil {
		resultErr = fmt.Errorf("core guard failed: %w", waitErr)
	}
	// A failed guard can disappear between spawn and started. Its recorded child
	// still needs cleanup even though the RPC never received a core PID.
	if !started && !reportedExit {
		g.mu.Lock()
		identity := g.identity
		g.mu.Unlock()
		if identity.PID == 0 {
			if owner, err := readCoreOwner(s.BaseDir()); err == nil && owner.Guard.PID == g.cmd.Process.Pid {
				identity = owner.Core
			}
		}
		if identity.PID > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := terminateIdentity(ctx, identity, false); err != nil {
				resultErr = errors.Join(resultErr, ErrCoreExitUnconfirmed, err)
			}
			cancel()
		}
	}
	// A previous callback must never race a new guard's ownership write.
	if lock, err := storage.LockFile(filepath.Join(s.BaseDir(), CoreLockPath)); err == nil {
		if owner, err := readCoreOwner(s.BaseDir()); err == nil && owner.Guard.PID == g.cmd.Process.Pid {
			if alive, err := identityAlive(owner.Core); err == nil && !alive {
				resultErr = errors.Join(resultErr, removeCoreOwner(s.BaseDir(), owner.Session))
			} else {
				resultErr = errors.Join(resultErr, ErrCoreExitUnconfirmed, err)
			}
		}
		_ = lock.Close()
	}
	g.mu.Lock()
	g.err = resultErr
	g.mu.Unlock()
	close(g.done) // never wait for OnExit: it may need a lock held by the stopper
	if !started {
		if resultErr == nil {
			resultErr = errors.New("core start cancelled")
		}
		ready <- Result{false, resultErr.Error()}
		return
	}
	if options.OnExit != nil {
		options.OnExit(corePID, resultErr)
	}
	if managed != nil {
		s.untrackProcess(corePID, managed)
	}
	if resultErr != nil && !expected {
		slog.Error("core process exited", "component", "process", "operation", "wait", "pid", corePID, "result", "failure", "error", resultErr)
	} else {
		slog.Info("core process exited", "component", "process", "operation", "wait", "pid", corePID, "result", "success")
	}
}

func (s *Service) CloseCoreProcesses(ctx context.Context) error {
	s.guardMu.Lock()
	s.guardsClosing = true
	guards := make([]*guardProcess, 0, len(s.guards))
	for guard := range s.guards {
		guards = append(guards, guard)
	}
	s.guardMu.Unlock()
	for _, g := range guards {
		g.requestStop(10)
	}
	var result error
	for _, g := range guards {
		select {
		case <-g.finished:
			g.mu.Lock()
			result = errors.Join(result, g.err)
			g.mu.Unlock()
		case <-ctx.Done():
			_ = g.control.Close()
			result = errors.Join(result, ctx.Err())
		}
	}
	return result
}
