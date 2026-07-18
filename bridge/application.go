package bridge

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"guiforcores/bridge/appsystem"
	"guiforcores/bridge/appupdate"
	"guiforcores/bridge/auth"
	"guiforcores/bridge/config"
	"guiforcores/bridge/event"
	"guiforcores/bridge/kernel"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/profile"
	"guiforcores/bridge/ruleset"
	appruntime "guiforcores/bridge/runtime"
	"guiforcores/bridge/scheduler"
	"guiforcores/bridge/settings"
	"guiforcores/bridge/storage"
	"guiforcores/bridge/subscription"
	httptransport "guiforcores/bridge/transport/http"
)

type Options struct {
	Address     string
	Assets      embed.FS
	BaseDir     string
	AppName     string
	AppVersion  string
	ServiceMode bool
}

type Application struct {
	auth      *auth.Service
	events    *event.Hub
	kernel    *kernel.Service
	scheduler *scheduler.Service
	update    *appupdate.Service
	system    *appsystem.Service
	server    *httptransport.Server
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

	paths := storage.NewPaths(options.BaseDir)
	authService := auth.NewService(paths)
	events := event.NewHub(authService)
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
	profileService := profile.NewService(paths, events)
	kernelService := kernel.NewService(platformService, configService, appConfig, profileService, events)
	runtimeService := appruntime.NewService(platformService, paths, appConfig, events, kernelService)
	settingsService := settings.NewService(runtimeService)
	subscriptionService := subscription.NewService(runtimeService)
	ruleSetService := ruleset.NewService(runtimeService)
	schedulerService := scheduler.NewService(runtimeService)
	updateService := appupdate.NewService(platformService, appConfig, events, options.AppVersion, options.ServiceMode)
	systemService := appsystem.NewService(platformService)

	server, err := httptransport.NewServer(httptransport.Options{
		Address:       options.Address,
		Assets:        options.Assets,
		Platform:      platformService,
		Auth:          authService,
		Events:        events,
		Config:        configService,
		AppConfig:     config.NewAppService(appConfig),
		Profiles:      profileService,
		Kernel:        kernelService,
		Settings:      settingsService,
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
	}, nil
}

func (a *Application) Run(ctx context.Context) error {
	a.scheduler.Start()
	go a.kernel.AutoStart(ctx)
	return a.server.Run(ctx)
}

func (a *Application) SetAuthSecret(secret string) error {
	return a.auth.SetSecret(secret)
}

func (a *Application) Close(ctx context.Context) error {
	a.scheduler.Stop()
	err := a.server.Close(ctx)
	a.events.Close()
	return err
}
