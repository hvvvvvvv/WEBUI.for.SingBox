package platform

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"guiforcores/bridge/logging"
	"guiforcores/bridge/storage"
)

const CoreGuardCommand = "__core_guard"
const coreGuardVersion = 1

type coreGuardRequest struct {
	Version      int             `json:"version"`
	Type         string          `json:"type"`
	BaseDir      string          `json:"base_dir,omitempty"`
	Executable   string          `json:"executable,omitempty"`
	Args         []string        `json:"args,omitempty"`
	CheckArgs    []string        `json:"check_args,omitempty"`
	Env          []string        `json:"env,omitempty"`
	WorkingDir   string          `json:"working_dir,omitempty"`
	Backend      ProcessIdentity `json:"backend,omitempty"`
	GraceSeconds int             `json:"grace_seconds,omitempty"`
}

type coreGuardEvent struct {
	Version  int                 `json:"version"`
	Type     string              `json:"type"`
	Identity ProcessIdentity     `json:"identity,omitempty"`
	Output   *logging.CoreOutput `json:"output,omitempty"`
	Error    string              `json:"error,omitempty"`
	Expected bool                `json:"expected,omitempty"`
	ExitCode int                 `json:"exit_code,omitempty"`
}

type guardEmitter struct {
	events chan coreGuardEvent
	closed <-chan struct{}
	done   chan struct{}
}

func (e *guardEmitter) send(event coreGuardEvent) {
	event.Version = coreGuardVersion
	select {
	case e.events <- event:
	case <-e.closed:
	}
}

func newGuardEmitter(output io.Writer, closed <-chan struct{}, lost func()) *guardEmitter {
	e := &guardEmitter{events: make(chan coreGuardEvent, 64), closed: closed, done: make(chan struct{})}
	go func() {
		defer close(e.done)
		encoder := json.NewEncoder(output)
		for event := range e.events {
			if err := encoder.Encode(event); err != nil {
				lost()
				return
			}
		}
	}()
	return e
}

func (e *guardEmitter) finish() {
	close(e.events)
	select {
	case <-e.done:
	case <-e.closed:
	}
}

// RunCoreGuard owns a core for exactly as long as its backend control pipe lives.
// stdout is a private event channel; it must never contain application log lines.
func RunCoreGuard(input io.ReadCloser, output io.WriteCloser) error {
	prepareCoreGuardIO()
	decoder := json.NewDecoder(input)
	var request coreGuardRequest
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("read guard initialization: %w", err)
	}
	if request.Version != coreGuardVersion || request.Type != "start" || !filepath.IsAbs(request.BaseDir) || !filepath.IsAbs(request.Executable) || request.Backend.PID != os.Getppid() {
		return errors.New("invalid core guard initialization")
	}
	if alive, err := identityAlive(request.Backend); err != nil || !alive {
		return errors.New("core guard backend is no longer alive")
	}
	parentLost := make(chan struct{})
	var lostOnce sync.Once
	// Do not close a blocking stdout descriptor here: Close itself may wait for
	// a stalled Write. A separate bounded forwarding goroutine cannot prevent
	// control EOF from reaching the process owner and terminating the core.
	lost := func() { lostOnce.Do(func() { close(parentLost) }) }
	emitter := newGuardEmitter(output, parentLost, lost)
	defer emitter.finish()
	stop := make(chan int, 1)
	go func() {
		for {
			var command coreGuardRequest
			if err := decoder.Decode(&command); err != nil {
				lost()
				return
			}
			if command.Version != coreGuardVersion || command.Type != "stop" {
				lost()
				return
			}
			select {
			case stop <- max(0, min(command.GraceSeconds, 10)):
			default:
			}
		}
	}()
	lock, err := storage.LockFile(filepath.Join(request.BaseDir, CoreLockPath))
	if err != nil {
		emitter.send(coreGuardEvent{Type: "error", Error: err.Error()})
		return err
	}
	defer lock.Close()
	if _, err := os.Stat(filepath.Join(request.BaseDir, coreOwnerPath)); !errors.Is(err, os.ErrNotExist) {
		conflict := errors.New("previous core ownership record remains; refusing replacement core")
		emitter.send(coreGuardEvent{Type: "error", Error: conflict.Error()})
		return conflict
	}
	guard, err := IdentifyProcess(os.Getpid())
	if err != nil {
		return err
	}
	owner := coreOwner{Session: rand.Text(), Backend: request.Backend, Guard: guard, ConfigPath: filepath.Join(request.BaseDir, "data/sing-box/config.json")}
	// Keep the guard identity after the core exits. The backend removes the record
	// only after Wait confirms that this executable is no longer in use.
	for _, stage := range []string{"check", "startup"} {
		args := request.Args
		if stage == "check" {
			args = request.CheckArgs
			if len(args) == 0 {
				continue
			}
		}
		select {
		case <-parentLost:
			return nil
		case <-stop:
			return nil
		default:
		}
		cmd := exec.Command(request.Executable, args...)
		SetCmdWindowHidden(cmd)
		cmd.Dir, cmd.Env = request.WorkingDir, request.Env
		var phase atomic.Value
		phase.Store(stage)
		var pid atomic.Int64
		pidReady := make(chan struct{})
		writer := &coreLineWriter{emit: func(line string) {
			receivedAt := time.Now()
			<-pidReady
			operation := phase.Load().(string)
			emitter.send(coreGuardEvent{Type: "output", Output: &logging.CoreOutput{Time: receivedAt, PID: int(pid.Load()), Operation: operation, Message: line}})
			if operation == "startup" && strings.Contains(line, "sing-box started") {
				phase.CompareAndSwap("startup", "runtime")
			}
		}}
		cmd.Stdout, cmd.Stderr = writer, writer
		if err := cmd.Start(); err != nil {
			emitter.send(coreGuardEvent{Type: "error", Error: fmt.Sprintf("start core %s: %v", stage, err)})
			return err
		}
		pid.Store(int64(cmd.Process.Pid))
		close(pidReady)
		identity, identityErr := IdentifyProcess(cmd.Process.Pid)
		if identityErr == nil {
			owner.Core = identity
			identityErr = writeCoreOwner(request.BaseDir, owner)
		}
		if identityErr != nil {
			_ = cmd.Process.Kill()
			waitErr := cmd.Wait()
			writer.flush()
			// A successful check can exit before its identity is queried. It is
			// already reaped and needs no ownership record in that case.
			if stage == "check" && waitErr == nil && owner.Core.PID == 0 {
				continue
			}
			emitter.send(coreGuardEvent{Type: "error", Error: fmt.Sprintf("record core identity: %v", identityErr)})
			return identityErr
		}
		emitter.send(coreGuardEvent{Type: "spawn", Identity: identity})
		if stage == "startup" {
			emitter.send(coreGuardEvent{Type: "started", Identity: identity})
		}
		exited := make(chan error, 1)
		go func() { err := cmd.Wait(); writer.flush(); exited <- err }()
		var timer *time.Timer
		var timeout <-chan time.Time
		var stopping, killed, warned bool
		lostChannel := parentLost
		for {
			select {
			case <-lostChannel:
				lostChannel = nil
				stopping, killed = true, true
				phase.Store("shutdown")
				_ = cmd.Process.Kill()
				if timer != nil {
					timer.Stop()
				}
				timer = time.NewTimer(5 * time.Second)
				timeout = timer.C
			case seconds := <-stop:
				if stopping {
					continue
				}
				stopping = true
				phase.Store("shutdown")
				if seconds == 0 {
					_ = cmd.Process.Kill()
					killed = true
					seconds = 5
				} else {
					_ = SendExitSignal(cmd.Process)
				}
				timer = time.NewTimer(time.Duration(seconds) * time.Second)
				timeout = timer.C
			case <-timeout:
				_ = cmd.Process.Kill()
				if killed && !warned {
					warned = true
					emitter.send(coreGuardEvent{Type: "error", Error: "core has not exited after forced termination"})
				}
				killed = true
				timer.Reset(5 * time.Second)
			case waitErr := <-exited:
				if timer != nil {
					timer.Stop()
				}
				if stage == "check" && waitErr == nil && !stopping {
					goto nextStage
				}
				message := ""
				if waitErr != nil && !stopping {
					message = waitErr.Error()
				}
				if stage == "check" && !stopping {
					message = "invalid core config: " + message
				}
				emitter.send(coreGuardEvent{Type: "exit", Identity: identity, Error: message, Expected: stopping, ExitCode: cmd.ProcessState.ExitCode()})
				return nil
			}
		}
	nextStage:
	}
	return nil
}

// Using Cmd's shared Writer lets Wait drain both streams before finalizing the
// trailing line. There is no Scanner's 64 KiB token limit.
type coreLineWriter struct {
	buffer bytes.Buffer
	emit   func(string)
}

func (w *coreLineWriter) Write(data []byte) (int, error) {
	length := len(data)
	for len(data) > 0 {
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			_, _ = w.buffer.Write(data)
			break
		}
		_, _ = w.buffer.Write(data[:index])
		w.emit(strings.TrimSuffix(w.buffer.String(), "\r"))
		w.buffer.Reset()
		data = data[index+1:]
	}
	return length, nil
}

func (w *coreLineWriter) flush() {
	if w.buffer.Len() > 0 {
		w.emit(w.buffer.String())
		w.buffer.Reset()
	}
}
