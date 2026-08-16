package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/alecthomas/kong"
	"github.com/kardianos/service"

	"guiforcores/bridge"
	"guiforcores/bridge/appupdate"
)

const (
	defaultAddress     = "0.0.0.0:9090"
	serviceName        = "webui.for.singbox"
	serviceDisplayName = "WebUI for sing-box"
	serviceDescription = "Web UI service for managing sing-box."
)

type commandLine struct {
	Server  serverCommand  `cmd:"" default:"withargs" help:"Run the HTTP server."`
	Service serviceCommand `cmd:"" help:"Manage the system service."`
	Updater updaterCommand `cmd:"" name:"__updater" hidden:""`
}

type serverCommand struct {
	Addr      string `name:"addr" default:"${defaultAddress}" help:"HTTP server listen address."`
	ResetAuth string `name:"reset-auth" help:"Reset auth secret (provide new secret, or 'clear' to remove)."`
}

type serviceCommand struct {
	Install   serviceInstallCommand   `cmd:"" help:"Install the system service."`
	Uninstall serviceUninstallCommand `cmd:"" help:"Uninstall the system service."`
	Start     serviceStartCommand     `cmd:"" help:"Start the system service."`
	Stop      serviceStopCommand      `cmd:"" help:"Stop the system service."`
	Restart   serviceRestartCommand   `cmd:"" help:"Restart the system service."`
	Status    serviceStatusCommand    `cmd:"" help:"Show the system service status."`
}

type serviceInstallCommand struct {
	Addr string `name:"addr" default:"${defaultAddress}" help:"HTTP server listen address."`
}

type serviceUninstallCommand struct{}
type serviceStartCommand struct{}
type serviceStopCommand struct{}
type serviceRestartCommand struct{}
type serviceStatusCommand struct{}

type updaterCommand struct {
	ArchivePath string `name:"archive-path" required:""`
	TargetPath  string `name:"target-path" required:""`
	ParentPID   int    `name:"parent-pid" required:""`
	RestartArgs string `name:"restart-args" default:"[]"`
	WorkingDir  string `name:"working-dir"`
	ServiceMode bool   `name:"service-mode"`
}

type commandRuntime struct {
	stdout io.Writer
}

var (
	runUpdateHelper        = appupdate.RunHelper
	currentServicePlatform = service.Platform
)

func run(args []string, stdout, stderr io.Writer) int {
	cli := &commandLine{}
	parser, err := newCommandParser(cli, stdout, stderr, exitProcess)
	if err != nil {
		fmt.Fprintf(stderr, "Failed to initialize command line: %v\n", err)
		return 1
	}
	ctx, err := parser.Parse(args)
	if err != nil {
		var parseError *kong.ParseError
		if errors.As(err, &parseError) && parseError.Context != nil {
			_ = parseError.Context.PrintUsage(true)
			fmt.Fprintln(stdout)
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := ctx.Run(&commandRuntime{stdout: stdout}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func newCommandParser(cli *commandLine, stdout, stderr io.Writer, exit func(int)) (*kong.Kong, error) {
	return kong.New(
		cli,
		kong.Name(serviceName),
		kong.Description(serviceDescription),
		kong.Vars{"defaultAddress": defaultAddress},
		kong.Writers(stdout, stderr),
		kong.Exit(exit),
	)
}

func (c *serverCommand) Run(runtime *commandRuntime) error {
	if c.ResetAuth != "" {
		return resetAuth(c.Addr, c.ResetAuth, runtime.stdout)
	}
	return runApplication(c.Addr)
}

func resetAuth(addr, secret string, stdout io.Writer) error {
	app, err := newApplication(bridge.Options{Address: addr, Assets: assets, AppVersion: version})
	if err != nil {
		return fmt.Errorf("failed to initialize application: %w", err)
	}

	if secret == "clear" {
		if err := app.SetAuthSecret(""); err != nil {
			return fmt.Errorf("failed to clear auth secret: %w", err)
		}
		fmt.Fprintln(stdout, "Auth secret cleared.")
		return nil
	}
	if err := app.SetAuthSecret(secret); err != nil {
		return fmt.Errorf("failed to update auth secret: %w", err)
	}
	fmt.Fprintln(stdout, "Auth secret updated.")
	return nil
}

func (c *serviceInstallCommand) Run(runtime *commandRuntime) error {
	manager, err := systemServiceManager([]string{"--addr", c.Addr})
	if err != nil {
		return serviceCommandError("install", err)
	}
	if err := manager.Install(); err != nil {
		return serviceCommandError("install", err)
	}
	printServiceSuccess(runtime.stdout, "install")
	return nil
}

func (*serviceUninstallCommand) Run(runtime *commandRuntime) error {
	manager, err := systemServiceManager(nil)
	if err != nil {
		return serviceCommandError("uninstall", err)
	}
	status, statusErr := manager.Status()
	if statusErr != nil && !errors.Is(statusErr, service.ErrNotInstalled) {
		return serviceCommandError("uninstall", statusErr)
	}
	if status == service.StatusRunning {
		if err := manager.Stop(); err != nil {
			return serviceCommandError("uninstall", fmt.Errorf("stop service: %w", err))
		}
	}
	if err := manager.Uninstall(); err != nil {
		return serviceCommandError("uninstall", err)
	}
	printServiceSuccess(runtime.stdout, "uninstall")
	return nil
}

func (*serviceStartCommand) Run(runtime *commandRuntime) error {
	manager, err := systemServiceManager(nil)
	if err != nil {
		return serviceCommandError("start", err)
	}
	if err := manager.Start(); err != nil {
		return serviceCommandError("start", err)
	}
	printServiceSuccess(runtime.stdout, "start")
	return nil
}

func (*serviceStopCommand) Run(runtime *commandRuntime) error {
	manager, err := systemServiceManager(nil)
	if err != nil {
		return serviceCommandError("stop", err)
	}
	if err := manager.Stop(); err != nil {
		return serviceCommandError("stop", err)
	}
	printServiceSuccess(runtime.stdout, "stop")
	return nil
}

func (*serviceRestartCommand) Run(runtime *commandRuntime) error {
	manager, err := systemServiceManager(nil)
	if err != nil {
		return serviceCommandError("restart", err)
	}
	if err := manager.Restart(); err != nil {
		return serviceCommandError("restart", err)
	}
	printServiceSuccess(runtime.stdout, "restart")
	return nil
}

func (*serviceStatusCommand) Run(runtime *commandRuntime) error {
	manager, err := systemServiceManager(nil)
	if err != nil {
		return serviceCommandError("status", err)
	}
	status, err := manager.Status()
	if errors.Is(err, service.ErrNotInstalled) {
		fmt.Fprintln(runtime.stdout, "not-installed")
		return nil
	}
	if err != nil {
		return serviceCommandError("status", err)
	}
	switch status {
	case service.StatusRunning:
		fmt.Fprintln(runtime.stdout, "running")
	case service.StatusStopped:
		fmt.Fprintln(runtime.stdout, "stopped")
	default:
		return serviceCommandError("status", errors.New("status is unknown"))
	}
	return nil
}

func (c *updaterCommand) Run(*commandRuntime) error {
	if c.ParentPID <= 0 {
		return errors.New("updater helper failed: parent-pid must be greater than zero")
	}
	var restartArgs []string
	if err := json.Unmarshal([]byte(c.RestartArgs), &restartArgs); err != nil {
		return fmt.Errorf("updater helper failed: decode restart arguments: %w", err)
	}
	opts := appupdate.HelperOptions{
		ArchivePath: c.ArchivePath,
		TargetPath:  c.TargetPath,
		ParentPID:   c.ParentPID,
		RestartArgs: restartArgs,
		WorkingDir:  c.WorkingDir,
		ServiceMode: c.ServiceMode,
	}
	if err := runUpdateHelper(opts); err != nil {
		return fmt.Errorf("updater helper failed: %w", err)
	}
	return nil
}

func systemServiceManager(arguments []string) (systemService, error) {
	executable, err := executablePath()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	return newSystemService(&serviceProgram{}, makeServiceConfig(executable, arguments))
}

func executablePath() (string, error) {
	executable, err := currentExecutable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(executable)
}

func makeServiceConfig(executable string, arguments []string) *service.Config {
	executable = filepath.Clean(executable)
	config := &service.Config{
		Name:             serviceName,
		DisplayName:      serviceDisplayName,
		Description:      serviceDescription,
		Arguments:        arguments,
		Executable:       executable,
		WorkingDirectory: filepath.Dir(executable),
		Option: service.KeyValue{
			"UserService": false,
			"RunAtLoad":   true,
			"KeepAlive":   true,
			"Restart":     "always",
			"StartType":   "automatic",
			"OnFailure":   "restart",
		},
	}
	if currentServicePlatform() == "linux-systemd" {
		config.Dependencies = []string{
			"Wants=network-online.target",
			"After=network-online.target",
		}
	}
	return config
}

func serviceCommandError(action string, err error) error {
	return fmt.Errorf("service %s failed: %w", action, err)
}

func printServiceSuccess(stdout io.Writer, action string) {
	fmt.Fprintf(stdout, "service %s: ok\n", action)
}
