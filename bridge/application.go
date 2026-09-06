package bridge

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	Address     string
	Assets      embed.FS
	BaseDir     string
	AppName     string
	AppVersion  string
	ServiceMode bool
	LogLevel    logging.Level
	LogDays     int
}

type Application struct {
	auth      *auth.Service
	events    *event.Hub
	kernel    *kernel.Service
	scheduler *scheduler.Service
	update    *appupdate.Service
	system    *appsystem.Service
	server    *httptransport.Server
	options   Options
}

func New(options Options) (*Application, error) {
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
	authService := auth.NewService(paths)
	events := event.NewHub(authService)
	resourceState := syncstate.NewCoordinator()
	privileged, _ := platform.IsPrivileged()
	platformService := platform.NewService(paths, events, platform.Environment{
		FromTaskScheduler: slices.Contains(os.Args, "tasksch"),
		AppName:           options.AppName,
		AppVersion:        options.AppVersion,
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		Libc:              platform.DetectLibc(),
		IsPrivileged:      privileged,
	})

	appConfig, err := config.NewStore(paths)
	if err != nil {
		return nil, fmt.Errorf("load app config: %w", err)
	}
	configService := config.NewService(paths, options.AppName)
	profileService := profile.NewService(paths, events, resourceState)
	kernelService := kernel.NewService(platformService, configService, appConfig, profileService, events)
	profileService.SetChangeHandler(kernelService)
	appConfigService := config.NewAppService(appConfig)
	appConfigService.SetChangeHandler(kernelService)
	runtimeService := appruntime.NewService(platformService, paths, appConfig, events, kernelService, resourceState)
	subscriptionService := subscription.NewService(runtimeService)
	ruleSetService := ruleset.NewService(runtimeService)
	schedulerService := scheduler.NewService(runtimeService)
	updateService := appupdate.NewService(platformService, appConfig, events, options.AppVersion, options.ServiceMode, options.LogLevel, options.LogDays)
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
		auth:      authService,
		events:    events,
		kernel:    kernelService,
		scheduler: schedulerService,
		update:    updateService,
		system:    systemService,
		server:    server,
		options:   options,
	}, nil
}

func (a *Application) Run(ctx context.Context) error {
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
	err := a.server.Run(ctx)
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
	started := time.Now()
	a.scheduler.Stop()
	err := a.server.Close(ctx)
	a.events.Close()
	if err != nil {
		slog.Error("application shutdown failed", "component", "app", "operation", "shutdown", "duration", time.Since(started), "result", "failure", "error", err)
		return err
	}
	slog.Info("application stopped", "component", "app", "operation", "shutdown", "duration", time.Since(started), "result", "success")
	return err
}
