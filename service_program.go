package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"

	"guiforcores/bridge"
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
	Logger(chan<- error) (service.Logger, error)
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

func runApplication(addr string) error {
	executable, err := executablePath()
	if err != nil {
		return fmt.Errorf("failed to resolve executable: %w", err)
	}

	program := &serviceProgram{
		options:     bridge.Options{Address: addr, Assets: assets, AppVersion: version},
		serviceMode: !service.Interactive(),
		exit:        exitProcess,
	}
	manager, err := newSystemService(program, makeServiceConfig(executable, nil))
	if err != nil {
		return fmt.Errorf("failed to initialize system service: %w", err)
	}
	logger, err := manager.Logger(nil)
	if err != nil {
		return fmt.Errorf("failed to initialize service logger: %w", err)
	}
	program.logger = logger
	log.SetOutput(serviceLogWriter{logger: logger})

	if err := manager.Run(); err != nil {
		_ = logger.Errorf("Application service failed: %v", err)
		return fmt.Errorf("application service failed: %w", err)
	}
	return nil
}

type serviceProgram struct {
	options     bridge.Options
	serviceMode bool
	logger      service.Logger
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
		if runErr != nil {
			p.logErrorf("Application stopped unexpectedly: %v", runErr)
		} else {
			p.logErrorf("Application stopped unexpectedly")
		}
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

func (p *serviceProgram) logErrorf(format string, args ...any) {
	if p.logger != nil {
		_ = p.logger.Errorf(format, args...)
		return
	}
	log.Printf(format, args...)
}

type serviceLogWriter struct {
	logger service.Logger
}

func (w serviceLogWriter) Write(data []byte) (int, error) {
	message := strings.TrimSpace(string(data))
	if message != "" {
		if err := w.logger.Info(message); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}
