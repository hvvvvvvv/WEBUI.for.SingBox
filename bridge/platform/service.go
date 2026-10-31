package platform

import (
	"os"
	"runtime"
	"sync"

	"guiforcores/bridge/event"
	"guiforcores/bridge/network"
	"guiforcores/bridge/storage"
)

type Environment struct {
	AppName      string `json:"appName"`
	AppVersion   string `json:"appVersion"`
	BasePath     string `json:"basePath"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Libc         string `json:"libc"`
	IsPrivileged bool   `json:"isPrivileged"`
}

type Service struct {
	paths            *storage.Paths
	events           *event.Hub
	environment      Environment
	managedProcessMu sync.Mutex
	managedProcesses map[int]*managedProcess
	guardMu          sync.Mutex
	guards           map[*guardProcess]struct{}
	guardsClosing    bool
	networkOptions   func() network.Options
}

type managedProcess struct {
	process *os.Process
	exited  <-chan struct{}
	guard   *guardProcess
}

func NewService(paths *storage.Paths, events *event.Hub, environment Environment) *Service {
	environment.BasePath = paths.BaseDir()
	if environment.OS == "" {
		environment.OS = runtime.GOOS
	}
	if environment.Arch == "" {
		environment.Arch = runtime.GOARCH
	}
	return &Service{
		paths:            paths,
		events:           events,
		environment:      environment,
		managedProcesses: make(map[int]*managedProcess),
	}
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

// Configure once during application construction; the provider reads current
// persisted settings for every request rather than caching a browser snapshot.
func (s *Service) SetNetworkOptions(provider func() network.Options) { s.networkOptions = provider }

func (s *Service) NetworkOptions() network.Options {
	if s.networkOptions != nil {
		return s.networkOptions()
	}
	return network.Options{}
}

func (s *Service) publish(eventName string, data ...any) {
	if s.events != nil {
		s.events.Publish(eventName, data...)
	}
}
