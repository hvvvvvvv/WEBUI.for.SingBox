package bridge

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"guiforcores/bridge/appsystem"
	"guiforcores/bridge/appupdate"
	"guiforcores/bridge/auth"
	"guiforcores/bridge/config"
	"guiforcores/bridge/event"
	"guiforcores/bridge/kernel"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/profile"
	"guiforcores/bridge/ruleset"
	appruntime "guiforcores/bridge/runtime"
	"guiforcores/bridge/scheduler"
	"guiforcores/bridge/storage"
	"guiforcores/bridge/subscription"
	"guiforcores/bridge/syncstate"
	httptransport "guiforcores/bridge/transport/http"
)

type Options struct {
	AcquireInstanceLock bool
	RequestShutdown     func()
	Address             string
	Assets              embed.FS
	BaseDir             string
	AppName             string
	AppVersion          string
	ServiceMode         bool
	LogLevel            logging.Level
	LogDays             int
}

type Application struct {
	instanceLock *storage.FileLock
	closeOnce    sync.Once
	closeErr     error
	auth         *auth.Service
	events       *event.Hub
	kernel       *kernel.Service
	scheduler    *scheduler.Service
	server       *httptransport.Server
	options      Options
}

func New(options Options) (_ *Application, resultErr error) {
	if options.Address == "" {
		options.Address = "0.0.0.0:9090"
	}

	executable, err := os.Executable()
	if err != nil && options.BaseDir == "" {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	if options.BaseDir == "" {
		options.BaseDir = filepath.Dir(executable)
	}
	if options.AppName == "" {
		options.AppName = filepath.Base(executable)
	}
	if options.AppVersion == "" {
		options.AppVersion = "unknown"
	}
	if _, err := logging.ParseLevel(options.LogLevel.String()); err != nil {
		options.LogLevel = logging.LevelInfo
	}

	paths := storage.NewPaths(options.BaseDir)
	var instanceLock *storage.FileLock
	if options.AcquireInstanceLock {
		instanceLock, err = storage.LockFile(paths.Resolve("data/backend.lock"))
		if err != nil {
			return nil, fmt.Errorf("another backend is using this data directory: %w", err)
		}
		defer func() {
			if resultErr != nil {
				_ = instanceLock.Close()
			}
		}()
	}
	authService := auth.NewService(paths)
	events := event.NewHub(authService)
	resourceState := syncstate.NewCoordinator()
	privileged, _ := platform.IsPrivileged()
	platformService := platform.NewService(paths, events, platform.Environment{
		AppName:      options.AppName,
		AppVersion:   options.AppVersion,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Libc:         platform.DetectLibc(),
		IsPrivileged: privileged,
	})

	appConfig, err := config.NewStore(paths)
	if err != nil {
		return nil, fmt.Errorf("load app config: %w", err)
	}
	configService := config.NewService(paths, options.AppName)
	profileService := profile.NewService(paths, events, resourceState)
	kernelService := kernel.NewService(platformService, configService, appConfig, profileService, events)
	coreLogs := logging.NewCoreWriter(paths.Resolve("data/logs/core"), int(appConfig.Current().CoreLogDays))
	defer func() {
		if resultErr != nil {
			_ = coreLogs.Close()
		}
	}()
	kernelService.SetCoreLogWriter(coreLogs)
	profileService.SetChangeHandler(kernelService)
	appConfigService := config.NewAppService(appConfig)
	appConfigService.SetChangeHandler(kernelService)
	runtimeService := appruntime.NewService(platformService, paths, appConfig, events, kernelService, resourceState)
	subscriptionService := subscription.NewService(runtimeService)
	ruleSetService := ruleset.NewService(runtimeService)
	schedulerService := scheduler.NewService(runtimeService)
	updateService := appupdate.NewService(platformService, appConfig, events, options.AppVersion, options.ServiceMode, options.LogLevel, options.LogDays)
	updateService.SetShutdownHandler(options.RequestShutdown)
	systemService := appsystem.NewService(platformService)

	server, err := httptransport.NewServer(httptransport.Options{
		Address:       options.Address,
		Assets:        options.Assets,
		Platform:      platformService,
		Auth:          authService,
		Events:        events,
		Config:        configService,
		AppConfig:     appConfigService,
		Profiles:      profileService,
		Kernel:        kernelService,
		Subscriptions: subscriptionService,
		RuleSets:      ruleSetService,
		Scheduler:     schedulerService,
		Update:        updateService,
		System:        systemService,
		RollingRelease: func() bool {
			return appConfig.Current().RollingRelease
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create HTTP server: %w", err)
	}

	return &Application{
		instanceLock: instanceLock,
		auth:         authService,
		events:       events,
		kernel:       kernelService,
		scheduler:    schedulerService,
		server:       server,
		options:      options,
	}, nil
}

func (a *Application) Run(ctx context.Context) error {
	prepareContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	err := platform.PrepareCore(prepareContext, a.options.BaseDir)
	cancel()
	if err != nil {
		return fmt.Errorf("prepare core lifecycle: %w", err)
	}
	slog.Info("application starting",
		"component", "app",
		"operation", "start",
		"version", a.options.AppVersion,
		"address", a.options.Address,
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
		"base_directory", a.options.BaseDir,
		"log_level", a.options.LogLevel.String(),
		"log_days", a.options.LogDays,
	)
	a.scheduler.Start()
	go a.kernel.AutoStart(ctx)
	err = a.server.Run(ctx)
	if err != nil {
		return err
	}
	slog.Info("application run completed", "component", "app", "operation", "run", "result", "success")
	return nil
}

func (a *Application) SetAuthSecret(secret string) error {
	return a.auth.SetSecret(secret)
}

func (a *Application) Close(ctx context.Context) error {
	a.closeOnce.Do(func() { a.closeErr = a.close(ctx) })
	return a.closeErr
}

func (a *Application) close(ctx context.Context) error {
	started := time.Now()
	a.kernel.BeginShutdown()
	a.scheduler.Stop()
	err := a.kernel.Close(ctx)
	err = errors.Join(err, a.server.Close(ctx))
	a.events.Close()
	err = errors.Join(err, a.instanceLock.Close())
	if err != nil {
		slog.Error("application shutdown failed", "component", "app", "operation", "shutdown", "duration", time.Since(started), "result", "failure", "error", err)
		return err
	}
	slog.Info("application stopped", "component", "app", "operation", "shutdown", "duration", time.Since(started), "result", "success")
	return err
}
