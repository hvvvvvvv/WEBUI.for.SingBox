package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kardianos/service"

	"guiforcores/bridge"
	"guiforcores/bridge/logging"
)

type application interface {
	Run(context.Context) error
	Close(context.Context) error
	SetAuthSecret(string) error
}

type systemService interface {
	Run() error
	Start() error
	Stop() error
	Restart() error
	Install() error
	Uninstall() error
	Status() (service.Status, error)
}

var (
	newApplication = func(options bridge.Options) (application, error) {
		return bridge.New(options)
	}
	newSystemService = func(program service.Interface, config *service.Config) (systemService, error) {
		return service.New(program, config)
	}
	currentExecutable  = os.Executable
	exitProcess        = os.Exit
	serviceStopTimeout = 5 * time.Second
)

func runApplication(addr string, stdout io.Writer, level logging.Level, logDays int) error {
	executable, err := executablePath()
	if err != nil {
		return fmt.Errorf("failed to resolve executable: %w", err)
	}

	program := &serviceProgram{
		options:     bridge.Options{Address: addr, Assets: assets, AppVersion: version, LogLevel: level, LogDays: logDays},
		serviceMode: !service.Interactive(),
		exit:        exitProcess,
	}
	previousLogger := slog.Default()
	logger, closer := logging.NewRuntime(stdout, level, logging.FileOptions{
		Directory:     filepath.Join(filepath.Dir(executable), "data", "logs", "app"),
		RetentionDays: logDays,
	})
	slog.SetDefault(logger)
	defer func() {
		_ = closer.Close()
		slog.SetDefault(previousLogger)
	}()
	manager, err := newSystemService(program, makeServiceConfig(executable, nil))
	if err != nil {
		return fmt.Errorf("failed to initialize system service: %w", err)
	}
	if err := manager.Run(); err != nil {
		slog.Error("application service failed", "component", "app", "operation", "run", "result", "failure", "error", err)
		return fmt.Errorf("application service failed: %w", err)
	}
	return nil
}

type serviceProgram struct {
	options     bridge.Options
	serviceMode bool
	exit        func(int)

	mu        sync.Mutex
	app       application
	cancel    context.CancelFunc
	done      chan struct{}
	stopping  bool
	stopOnce  sync.Once
	stopError error
}

func (p *serviceProgram) Start(service.Service) error {
	options := p.options
	options.ServiceMode = p.serviceMode
	app, err := newApplication(options)
	if err != nil {
		return fmt.Errorf("initialize application: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	p.mu.Lock()
	p.app = app
	p.cancel = cancel
	p.done = done
	p.mu.Unlock()

	go func() {
		runErr := app.Run(ctx)
		close(done)

		p.mu.Lock()
		stopping := p.stopping
		p.mu.Unlock()
		if stopping {
			return
		}
		args := []any{"component", "app", "operation", "run", "result", "failure"}
		if runErr != nil {
			args = append(args, "error", runErr)
		}
		slog.Error("application stopped unexpectedly", args...)
		if p.exit != nil {
			p.exit(1)
		}
	}()
	return nil
}

func (p *serviceProgram) Stop(service.Service) error {
	return p.stop()
}

func (p *serviceProgram) Shutdown(service.Service) error {
	return p.stop()
}

func (p *serviceProgram) stop() error {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.stopping = true
		app := p.app
		cancel := p.cancel
		done := p.done
		p.mu.Unlock()

		if app == nil {
			return
		}
		if cancel != nil {
			cancel()
		}
		closeContext, closeCancel := context.WithTimeout(context.Background(), serviceStopTimeout)
		defer closeCancel()
		p.stopError = app.Close(closeContext)
		if done != nil {
			select {
			case <-done:
			case <-closeContext.Done():
				p.stopError = errors.Join(p.stopError, fmt.Errorf("wait for application shutdown: %w", closeContext.Err()))
			}
		}
	})
	return p.stopError
}
