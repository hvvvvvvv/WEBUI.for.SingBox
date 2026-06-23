package bridge

import (
	"net/http"
)

var AppInstance *App

// App struct
type App struct {
}

type EnvResult struct {
	PreventExit  bool   `json:"-"`
	FromTaskSch  bool   `json:"-"`
	AppName      string `json:"appName"`
	AppVersion   string `json:"appVersion"`
	BasePath     string `json:"basePath"`
	OS           string `json:"os"`
	ARCH         string `json:"arch"`
	Libc         string `json:"libc"`
	IsPrivileged bool   `json:"isPrivileged"`
}

type RequestOptions struct {
	Proxy    string
	Insecure bool
	Redirect bool
	Timeout  int
	CancelId string
}

type ExecOptions struct {
	PidFile           string
	StopOutputKeyword string
	WorkingDirectory  string
	Convert           bool
	Env               map[string]string
}

type Range struct {
	Start *int64
	End   *int64
}

type IOOptions struct {
	Mode  string // Binary / Text
	Range string // "start-end" / "start-" / "-end"
}

type FlagResult struct {
	Flag bool   `json:"flag"`
	Data string `json:"data"`
}

type HTTPResult struct {
	Flag    bool        `json:"flag"`
	Status  int         `json:"status"`
	Headers http.Header `json:"headers"`
	Body    string      `json:"body"`
}

type AppConfig struct {
	AutoStartKernel   bool                 `yaml:"autoStartKernel"`
	AutoRestartKernel bool                 `yaml:"autoRestartKernel"`
	UserAgent         string               `yaml:"userAgent"`
	GitHubApiToken    string               `yaml:"githubApiToken"`
	RollingRelease    bool                 `yaml:"rollingRelease"`
	Branch            string               `yaml:"branch"`
	Profile           string               `yaml:"profile"`
	Main              AppCoreRuntimeConfig `yaml:"main"`
	Alpha             AppCoreRuntimeConfig `yaml:"alpha"`
}

type AppCoreRuntimeConfig struct {
	Env  map[string]string `yaml:"env"`
	Args []string          `yaml:"args"`
}

type WriteTracker struct {
	Total          int64
	Progress       int64
	LastEmitted    int64
	EmitThreshold  int64
	ProgressChange string
}
