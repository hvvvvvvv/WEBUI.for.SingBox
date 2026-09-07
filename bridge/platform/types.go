package platform

import (
	"context"

	"guiforcores/bridge/logging"
)

type ExecOptions struct {
	Guard             bool                     `json:"-"`
	CheckArgs         []string                 `json:"-"`
	Context           context.Context          `json:"-"`
	OnOutput          func(logging.CoreOutput) `json:"-"`
	OnStarted         func(int)                `json:"-"`
	PIDFile           string                   `json:"PidFile"`
	StopOutputKeyword string
	WorkingDirectory  string
	Convert           bool
	Env               map[string]string
	OnExit            func(pid int, err error) `json:"-"`
}

type Result struct {
	Flag bool   `json:"flag"`
	Data string `json:"data"`
}

// Compatibility aliases are kept inside the infrastructure package while
// legacy HTTP handlers are migrated to the new names.
type App = Service
type FlagResult = Result
