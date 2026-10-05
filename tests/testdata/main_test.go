package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kardianos/service"

	"guiforcores/bridge"
	"guiforcores/bridge/logging"
)

type fakeSystemService struct {
	status service.Status
	calls  []string
}

func (f *fakeSystemService) call(name string) error {
	f.calls = append(f.calls, name)
	return nil
}

func (f *fakeSystemService) Run() error       { return f.call("run") }
func (f *fakeSystemService) Start() error     { return f.call("start") }
func (f *fakeSystemService) Stop() error      { return f.call("stop") }
func (f *fakeSystemService) Restart() error   { return f.call("restart") }
func (f *fakeSystemService) Install() error   { return f.call("install") }
func (f *fakeSystemService) Uninstall() error { return f.call("uninstall") }
func (f *fakeSystemService) Status() (service.Status, error) {
	f.calls = append(f.calls, "status")
	return f.status, nil
}

type fakeApplication struct {
	run       func(context.Context) error
	closeCall int
}

func (a *fakeApplication) Run(ctx context.Context) error {
	return a.run(ctx)
}

func (a *fakeApplication) Close(context.Context) error {
	a.closeCall++
	return nil
}

func (a *fakeApplication) SetAuthSecret(string) error { return nil }

func TestServiceUninstallStopsRunningService(t *testing.T) {
	originalExecutable := currentExecutable
	originalFactory := newSystemService
	defer func() {
		currentExecutable = originalExecutable
		newSystemService = originalFactory
	}()
	currentExecutable = func() (string, error) { return "/opt/webui", nil }
	manager := &fakeSystemService{status: service.StatusRunning}
	newSystemService = func(service.Interface, *service.Config) (systemService, error) { return manager, nil }

	var stdout, stderr bytes.Buffer
	if code := run([]string{"service", "uninstall"}, &stdout, &stderr); code != 0 {
		t.Fatalf("uninstall failed: code=%d stderr=%q", code, stderr.String())
	}
	if !reflect.DeepEqual(manager.calls, []string{"status", "stop", "uninstall"}) {
		t.Fatalf("uninstall calls = %#v", manager.calls)
	}
}

func TestServiceProgramLifecycle(t *testing.T) {
	originalFactory := newApplication
	originalTimeout := serviceStopTimeout
	defer func() {
		newApplication = originalFactory
		serviceStopTimeout = originalTimeout
	}()

	app := &fakeApplication{run: func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}}
	var options bridge.Options
	newApplication = func(received bridge.Options) (application, error) {
		options = received
		return app, nil
	}
	program := &serviceProgram{serviceMode: true, exit: func(int) { t.Error("unexpected exit") }}
	if err := program.Start(nil); err != nil {
		t.Fatal(err)
	}
	if !options.ServiceMode {
		t.Fatal("service mode was not propagated")
	}
	if err := program.Stop(nil); err != nil {
		t.Fatal(err)
	}
	if err := program.Shutdown(nil); err != nil {
		t.Fatal(err)
	}
	if app.closeCall != 1 {
		t.Fatalf("Close called %d times", app.closeCall)
	}
}

func TestServiceProgramInitializationAndRuntimeFailure(t *testing.T) {
	originalFactory := newApplication
	defer func() { newApplication = originalFactory }()

	newApplication = func(bridge.Options) (application, error) { return nil, errors.New("init failed") }
	if err := (&serviceProgram{}).Start(nil); err == nil || !strings.Contains(err.Error(), "init failed") {
		t.Fatalf("Start error = %v", err)
	}

	var logOutput bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(logging.NewConsoleHandler(&logOutput, logging.LevelInfo)))
	defer slog.SetDefault(previousLogger)
	exited := make(chan int, 1)
	newApplication = func(bridge.Options) (application, error) {
		return &fakeApplication{run: func(context.Context) error { return errors.New("listen failed") }}, nil
	}
	program := &serviceProgram{exit: func(code int) { exited <- code }}
	if err := program.Start(nil); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-exited:
		if code != 1 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime failure did not trigger exit")
	}
	if !strings.Contains(logOutput.String(), "listen failed") {
		t.Fatalf("logged output = %q", logOutput.String())
	}
}

func TestServiceProgramStopTimeout(t *testing.T) {
	originalFactory := newApplication
	originalTimeout := serviceStopTimeout
	defer func() {
		newApplication = originalFactory
		serviceStopTimeout = originalTimeout
	}()

	release := make(chan struct{})
	app := &fakeApplication{run: func(context.Context) error {
		<-release
		return nil
	}}
	newApplication = func(bridge.Options) (application, error) { return app, nil }
	serviceStopTimeout = 10 * time.Millisecond
	program := &serviceProgram{exit: func(int) {}}
	if err := program.Start(nil); err != nil {
		t.Fatal(err)
	}
	err := program.Stop(nil)
	close(release)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("Stop error = %v", err)
	}
}
