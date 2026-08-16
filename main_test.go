package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kardianos/service"

	"guiforcores/bridge"
	"guiforcores/bridge/appupdate"
)

type fakeSystemService struct {
	status    service.Status
	statusErr error
	errors    map[string]error
	calls     []string
	logger    service.Logger
}

func (f *fakeSystemService) call(name string) error {
	f.calls = append(f.calls, name)
	return f.errors[name]
}

func (f *fakeSystemService) Run() error       { return f.call("run") }
func (f *fakeSystemService) Start() error     { return f.call("start") }
func (f *fakeSystemService) Stop() error      { return f.call("stop") }
func (f *fakeSystemService) Restart() error   { return f.call("restart") }
func (f *fakeSystemService) Install() error   { return f.call("install") }
func (f *fakeSystemService) Uninstall() error { return f.call("uninstall") }
func (f *fakeSystemService) Status() (service.Status, error) {
	f.calls = append(f.calls, "status")
	return f.status, f.statusErr
}
func (f *fakeSystemService) Logger(chan<- error) (service.Logger, error) {
	if f.logger == nil {
		f.logger = &fakeServiceLogger{}
	}
	return f.logger, f.errors["logger"]
}

type fakeServiceLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *fakeServiceLogger) add(values ...any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, strings.TrimSpace(fmt.Sprint(values...)))
	return nil
}

func (l *fakeServiceLogger) Error(values ...any) error   { return l.add(values...) }
func (l *fakeServiceLogger) Warning(values ...any) error { return l.add(values...) }
func (l *fakeServiceLogger) Info(values ...any) error    { return l.add(values...) }
func (l *fakeServiceLogger) Errorf(format string, values ...any) error {
	return l.add(fmt.Sprintf(format, values...))
}
func (l *fakeServiceLogger) Warningf(format string, values ...any) error {
	return l.add(fmt.Sprintf(format, values...))
}
func (l *fakeServiceLogger) Infof(format string, values ...any) error {
	return l.add(fmt.Sprintf(format, values...))
}

type fakeApplication struct {
	run       func(context.Context) error
	close     func(context.Context) error
	reset     string
	closeCall int
}

func (a *fakeApplication) Run(ctx context.Context) error {
	if a.run != nil {
		return a.run(ctx)
	}
	return nil
}

func (a *fakeApplication) Close(ctx context.Context) error {
	a.closeCall++
	if a.close != nil {
		return a.close(ctx)
	}
	return nil
}

func (a *fakeApplication) SetAuthSecret(secret string) error {
	a.reset = secret
	return nil
}

func TestServiceCommands(t *testing.T) {
	originalExecutable := currentExecutable
	originalFactory := newSystemService
	originalPlatform := currentServicePlatform
	defer func() {
		currentExecutable = originalExecutable
		newSystemService = originalFactory
		currentServicePlatform = originalPlatform
	}()

	currentExecutable = func() (string, error) { return filepath.Join("tmp", "webui"), nil }
	currentServicePlatform = func() string { return "linux-systemd" }
	manager := &fakeSystemService{errors: map[string]error{}}
	var config *service.Config
	newSystemService = func(_ service.Interface, received *service.Config) (systemService, error) {
		config = received
		return manager, nil
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"service", "install", "--addr", "127.0.0.1:8080"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || stdout.String() != "service install: ok\n" {
		t.Fatalf("install result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !reflect.DeepEqual(manager.calls, []string{"install"}) {
		t.Fatalf("install calls = %#v", manager.calls)
	}
	if config.Name != serviceName || config.DisplayName != serviceDisplayName || config.Description != serviceDescription {
		t.Fatalf("unexpected service identity: %#v", config)
	}
	if !filepath.IsAbs(config.Executable) || config.WorkingDirectory != filepath.Dir(config.Executable) {
		t.Fatalf("unexpected service paths: %#v", config)
	}
	if !reflect.DeepEqual(config.Arguments, []string{"--addr", "127.0.0.1:8080"}) {
		t.Fatalf("service arguments = %#v", config.Arguments)
	}
	if !reflect.DeepEqual(config.Dependencies, []string{
		"Wants=network-online.target",
		"After=network-online.target",
	}) {
		t.Fatalf("service dependencies = %#v", config.Dependencies)
	}
	for key, want := range map[string]any{
		"UserService": false,
		"RunAtLoad":   true,
		"KeepAlive":   true,
		"Restart":     "always",
		"StartType":   "automatic",
		"OnFailure":   "restart",
	} {
		if got := config.Option[key]; got != want {
			t.Errorf("option %s = %#v, want %#v", key, got, want)
		}
	}

	for _, action := range []string{"start", "stop", "restart"} {
		manager.calls = nil
		stdout.Reset()
		stderr.Reset()
		code = run([]string{"service", action}, &stdout, &stderr)
		if code != 0 || !reflect.DeepEqual(manager.calls, []string{action}) {
			t.Errorf("%s result: code=%d calls=%#v stderr=%q", action, code, manager.calls, stderr.String())
		}
	}
}

func TestServiceConfigUsesPlatformNetworkStartup(t *testing.T) {
	originalPlatform := currentServicePlatform
	t.Cleanup(func() { currentServicePlatform = originalPlatform })

	tests := []struct {
		name             string
		platform         string
		dependencies     []string
		delayedAutoStart bool
		runAtLoad        bool
		launchdConfig    bool
	}{
		{
			name:         "systemd",
			platform:     systemdServicePlatform,
			dependencies: []string{"Wants=network-online.target", "After=network-online.target"},
			runAtLoad:    true,
		},
		{
			name:         "OpenRC",
			platform:     openRCServicePlatform,
			dependencies: []string{"need net"},
			runAtLoad:    true,
		},
		{
			name:             "Windows",
			platform:         windowsServicePlatform,
			delayedAutoStart: true,
			runAtLoad:        true,
		},
		{
			name:          "launchd",
			platform:      launchdServicePlatform,
			runAtLoad:     false,
			launchdConfig: true,
		},
		{
			name:      "other",
			platform:  "unix-systemv",
			runAtLoad: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentServicePlatform = func() string { return test.platform }
			config := makeServiceConfig("/opt/webui", nil)

			if !reflect.DeepEqual(config.Dependencies, test.dependencies) {
				t.Errorf("dependencies = %#v, want %#v", config.Dependencies, test.dependencies)
			}
			if got, _ := config.Option["DelayedAutoStart"].(bool); got != test.delayedAutoStart {
				t.Errorf("DelayedAutoStart = %v, want %v", got, test.delayedAutoStart)
			}
			if got, _ := config.Option["RunAtLoad"].(bool); got != test.runAtLoad {
				t.Errorf("RunAtLoad = %v, want %v", got, test.runAtLoad)
			}
			launchdConfig, _ := config.Option["LaunchdConfig"].(string)
			if (launchdConfig != "") != test.launchdConfig {
				t.Errorf("LaunchdConfig present = %v, want %v", launchdConfig != "", test.launchdConfig)
			}
			if test.launchdConfig && !strings.Contains(launchdConfig, "<key>NetworkState</key>") {
				t.Error("LaunchdConfig does not wait for network state")
			}
		})
	}
}

func TestServiceUninstallStopsRunningService(t *testing.T) {
	originalExecutable := currentExecutable
	originalFactory := newSystemService
	defer func() {
		currentExecutable = originalExecutable
		newSystemService = originalFactory
	}()
	currentExecutable = func() (string, error) { return "/opt/webui", nil }
	manager := &fakeSystemService{status: service.StatusRunning, errors: map[string]error{}}
	newSystemService = func(service.Interface, *service.Config) (systemService, error) { return manager, nil }

	var stdout, stderr bytes.Buffer
	if code := run([]string{"service", "uninstall"}, &stdout, &stderr); code != 0 {
		t.Fatalf("uninstall failed: code=%d stderr=%q", code, stderr.String())
	}
	if !reflect.DeepEqual(manager.calls, []string{"status", "stop", "uninstall"}) {
		t.Fatalf("uninstall calls = %#v", manager.calls)
	}
}

func TestServiceStatusOutput(t *testing.T) {
	originalExecutable := currentExecutable
	originalFactory := newSystemService
	defer func() {
		currentExecutable = originalExecutable
		newSystemService = originalFactory
	}()
	currentExecutable = func() (string, error) { return "/opt/webui", nil }

	tests := []struct {
		name      string
		status    service.Status
		err       error
		wantCode  int
		wantOut   string
		wantError string
	}{
		{name: "running", status: service.StatusRunning, wantOut: "running\n"},
		{name: "stopped", status: service.StatusStopped, wantOut: "stopped\n"},
		{name: "not installed", err: service.ErrNotInstalled, wantOut: "not-installed\n"},
		{name: "unknown", status: service.StatusUnknown, wantCode: 1, wantError: "unknown"},
		{name: "backend error", err: errors.New("denied"), wantCode: 1, wantError: "denied"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := &fakeSystemService{status: test.status, statusErr: test.err, errors: map[string]error{}}
			newSystemService = func(service.Interface, *service.Config) (systemService, error) {
				return manager, nil
			}
			var stdout, stderr bytes.Buffer
			code := run([]string{"service", "status"}, &stdout, &stderr)
			if code != test.wantCode || stdout.String() != test.wantOut || !strings.Contains(stderr.String(), test.wantError) {
				t.Fatalf("status result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestServiceCommandSyntaxAndErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"service"}, &stdout, &stderr); code != 2 || !strings.Contains(stdout.String(), "Usage:") || !strings.Contains(stderr.String(), "install") {
		t.Fatalf("missing action: code=%d stderr=%q", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"service", "unknown"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "unknown") {
		t.Fatalf("unknown action: code=%d stderr=%q", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"service", "start", "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("extra argument code = %d", code)
	}
	for _, args := range [][]string{
		{"unknown"},
		{"--unknown-flag"},
		{"service", "start", "--addr", "127.0.0.1:8080"},
		{"service", "install", "extra"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%q) code = %d, want 2", args, code)
		}
	}
}

func TestDefaultServerAndResetAuthCommands(t *testing.T) {
	originalApplication := newApplication
	originalExecutable := currentExecutable
	originalFactory := newSystemService
	defer func() {
		newApplication = originalApplication
		currentExecutable = originalExecutable
		newSystemService = originalFactory
	}()

	currentExecutable = func() (string, error) { return "/opt/webui", nil }
	manager := &fakeSystemService{errors: map[string]error{}}
	var addresses []string
	newSystemService = func(program service.Interface, _ *service.Config) (systemService, error) {
		addresses = append(addresses, program.(*serviceProgram).options.Address)
		return manager, nil
	}

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "no arguments"},
		{name: "address", args: []string{"--addr", "127.0.0.1:8080"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager.calls = nil
			var stdout, stderr bytes.Buffer
			if code := run(test.args, &stdout, &stderr); code != 0 {
				t.Fatalf("run failed: code=%d stderr=%q", code, stderr.String())
			}
			if !reflect.DeepEqual(manager.calls, []string{"run"}) {
				t.Fatalf("manager calls = %#v", manager.calls)
			}
		})
	}
	if !reflect.DeepEqual(addresses, []string{defaultAddress, "127.0.0.1:8080"}) {
		t.Fatalf("server addresses = %#v", addresses)
	}

	app := &fakeApplication{}
	var options bridge.Options
	newApplication = func(received bridge.Options) (application, error) {
		options = received
		return app, nil
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--addr", "127.0.0.1:8081", "--reset-auth", "new-secret"}, &stdout, &stderr); code != 0 {
		t.Fatalf("reset failed: code=%d stderr=%q", code, stderr.String())
	}
	if options.Address != "127.0.0.1:8081" || app.reset != "new-secret" || stdout.String() != "Auth secret updated.\n" {
		t.Fatalf("options=%#v reset=%q stdout=%q", options, app.reset, stdout.String())
	}
}

func TestUpdaterCommandMapsHelperOptions(t *testing.T) {
	originalRunHelper := runUpdateHelper
	defer func() { runUpdateHelper = originalRunHelper }()

	var received appupdate.HelperOptions
	runUpdateHelper = func(options appupdate.HelperOptions) error {
		received = options
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"__updater",
		"--archive-path", "/tmp/update.zip",
		"--target-path", "/opt/webui",
		"--parent-pid", "42",
		"--restart-args", `["--addr","127.0.0.1:8080"]`,
		"--working-dir", "/opt",
		"--service-mode",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("updater failed: code=%d stderr=%q", code, stderr.String())
	}
	want := appupdate.HelperOptions{
		ArchivePath: "/tmp/update.zip",
		TargetPath:  "/opt/webui",
		ParentPID:   42,
		RestartArgs: []string{"--addr", "127.0.0.1:8080"},
		WorkingDir:  "/opt",
		ServiceMode: true,
	}
	if !reflect.DeepEqual(received, want) {
		t.Fatalf("helper options = %#v, want %#v", received, want)
	}
}

func TestCommandHelpHidesUpdater(t *testing.T) {
	help := renderCommandHelp(t, "--help")
	if !strings.Contains(help, "service") || !strings.Contains(help, "install") {
		t.Fatalf("public service commands missing from help: %q", help)
	}
	if !strings.Contains(help, "server") {
		t.Fatalf("server command missing from help: %q", help)
	}
	if strings.Contains(help, "__updater") {
		t.Fatalf("hidden updater command appeared in help: %q", help)
	}

	serverHelp := renderCommandHelp(t, "server", "--help")
	if !strings.Contains(serverHelp, "--addr") || !strings.Contains(serverHelp, "--reset-auth") {
		t.Fatalf("server flags missing from contextual help: %q", serverHelp)
	}
}

func renderCommandHelp(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	parser, err := newCommandParser(&commandLine{}, &stdout, &stderr, func(code int) {
		panic(code)
	})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != 0 {
				t.Fatalf("help exit = %#v", recovered)
			}
		}()
		_, _ = parser.Parse(args)
	}()
	return stdout.String()
}

func TestServiceControlErrorIsReturned(t *testing.T) {
	originalExecutable := currentExecutable
	originalFactory := newSystemService
	defer func() {
		currentExecutable = originalExecutable
		newSystemService = originalFactory
	}()
	currentExecutable = func() (string, error) { return "/opt/webui", nil }
	manager := &fakeSystemService{errors: map[string]error{"start": errors.New("permission denied")}}
	newSystemService = func(service.Interface, *service.Config) (systemService, error) { return manager, nil }

	var stdout, stderr bytes.Buffer
	code := run([]string{"service", "start"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "permission denied") || stdout.Len() != 0 {
		t.Fatalf("control error: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
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

	logger := &fakeServiceLogger{}
	exited := make(chan int, 1)
	newApplication = func(bridge.Options) (application, error) {
		return &fakeApplication{run: func(context.Context) error { return errors.New("listen failed") }}, nil
	}
	program := &serviceProgram{logger: logger, exit: func(code int) { exited <- code }}
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
	if len(logger.messages) == 0 || !strings.Contains(logger.messages[0], "listen failed") {
		t.Fatalf("logged messages = %#v", logger.messages)
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
