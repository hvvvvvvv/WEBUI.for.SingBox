package kernel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/config"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/platform"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestShutdownRejectsStartsRestartsAndQueuedWork(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	service.setRunning(42, "profile", &profilev1.Profile{Id: "profile"})
	service.enqueueAutomaticRestart("profile", true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartCore(ctx, connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "profile"})); err == nil {
		t.Fatal("start allowed during shutdown")
	}
	if _, err := service.RestartCore(ctx, connect.NewRequest(&kernelv1.RestartCoreRequest{ProfileId: "profile"})); err == nil {
		t.Fatal("restart allowed during shutdown")
	}
	service.ProfilesChanged([]string{"profile"})
	service.restartQueueMu.Lock()
	pending := service.restartPending
	service.restartQueueMu.Unlock()
	if pending {
		t.Fatal("restart remained queued")
	}
}

type startingProcesses struct {
	fakeProcesses
	started chan struct{}
}

func (p startingProcesses) ExecBackground(_ string, _ []string, _ string, options platform.ExecOptions) platform.Result {
	close(p.started)
	<-options.Context.Done()
	return platform.Result{Data: options.Context.Err().Error()}
}

func TestShutdownCancelsStartInProgress(t *testing.T) {
	processes := startingProcesses{started: make(chan struct{})}
	service := NewService(processes, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	result := make(chan error, 1)
	go func() {
		_, err := service.StartCore(context.Background(), connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: "profile"}))
		result <- err
	}()
	select {
	case <-processes.started:
	case <-time.After(time.Second):
		t.Fatal("start never reached process runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("cancelled start succeeded")
	}
	if status, _ := service.Status(); status != kernelv1.CoreStatus_CORE_STATUS_STOPPED {
		t.Fatal(status)
	}
}

func TestUnconfirmedExitKeepsPIDAndBlocksReplacement(t *testing.T) {
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	service.setRunning(42, "profile", nil)
	service.handleCoreProcessExit(42, errors.Join(platform.ErrCoreExitUnconfirmed, errors.New("permission denied")))
	if service.corePID != 42 {
		t.Fatal("lost ownership of unconfirmed process")
	}
	if err := service.setStarting("profile"); err == nil {
		t.Fatal("replacement allowed before process exited")
	}
}

func TestRetentionChangeAppliesWithoutCoreRestart(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "logs")
	writer := logging.NewCoreWriter(directory, 0)
	defer writer.Close()
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	service.SetCoreLogWriter(writer)
	service.setRunning(42, "profile", nil)
	previous := config.AppConfig{Profile: "profile", Branch: "main"}
	current := previous
	current.CoreLogDays = 7
	service.AppConfigChanged(previous, current)
	writer.Write(logging.CoreOutput{Time: time.Now(), Message: "INFO saved", Operation: "runtime"}, "profile")
	path := filepath.Join(directory, time.Now().Format("2006-01-02")+".log")
	before, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(before), "saved") {
		t.Fatalf("log not saved: %v", err)
	}
	service.AppConfigChanged(current, previous)
	writer.Write(logging.CoreOutput{Message: "INFO disabled", Operation: "runtime"}, "profile")
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("disabled logging still wrote")
	}
	if service.corePID != 42 || service.restartRequired || service.restarting {
		t.Fatal("retention change affected core lifecycle")
	}
}

func TestShutdownDuringStartHandshakeDoesNotReportCrash(t *testing.T) {
	events := &recordingEvents{}
	service := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, events)
	if err := service.setStarting("profile"); err != nil {
		t.Fatal(err)
	}
	service.BeginShutdown()
	// A queued started event can arrive just after shutdown cancelled the check.
	service.updateCoreState(func() { service.corePID = 42 })
	service.handleCoreProcessExit(42, nil)
	if len(events.named("kernelCrashed")) != 0 {
		t.Fatal("planned shutdown reported as a crash")
	}
	if status, _ := service.Status(); status != kernelv1.CoreStatus_CORE_STATUS_STOPPED {
		t.Fatal(status)
	}
}
