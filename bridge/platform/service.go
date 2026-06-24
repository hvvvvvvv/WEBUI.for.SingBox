package platform

import (
	"runtime"

	"guiforcores/bridge/event"
	"guiforcores/bridge/storage"
)

type Environment struct {
	FromTaskScheduler bool   `json:"-"`
	AppName           string `json:"appName"`
	AppVersion        string `json:"appVersion"`
	BasePath          string `json:"basePath"`
	OS                string `json:"os"`
	Arch              string `json:"arch"`
	Libc              string `json:"libc"`
	IsPrivileged      bool   `json:"isPrivileged"`
}

type Service struct {
	paths       *storage.Paths
	events      *event.Hub
	environment Environment
}

func NewService(paths *storage.Paths, events *event.Hub, environment Environment) *Service {
	environment.BasePath = paths.BaseDir()
	if environment.OS == "" {
		environment.OS = runtime.GOOS
	}
	if environment.Arch == "" {
		environment.Arch = runtime.GOARCH
	}
	return &Service{paths: paths, events: events, environment: environment}
}

func (s *Service) Paths() *storage.Paths {
	return s.paths
}

func (s *Service) Events() *event.Hub {
	return s.events
}

func (s *Service) Environment() Environment {
	return s.environment
}

func (s *Service) ResolvePath(path string) string {
	return s.paths.Resolve(path)
}

func (s *Service) BaseDir() string {
	return s.paths.BaseDir()
}

func (s *Service) publish(eventName string, data ...any) {
	if s.events != nil {
		s.events.Publish(eventName, data...)
	}
}
