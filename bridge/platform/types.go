package platform

import "net/http"

type RequestOptions struct {
	Proxy    string
	Insecure bool
	Redirect bool
	Timeout  int
	CancelID string `json:"CancelId"`
}

type ExecOptions struct {
	PIDFile           string `json:"PidFile"`
	StopOutputKeyword string
	WorkingDirectory  string
	Convert           bool
	Env               map[string]string
}

type IOOptions struct {
	Mode  string
	Range string
}

type Result struct {
	Flag bool   `json:"flag"`
	Data string `json:"data"`
}

// Compatibility aliases are kept inside the infrastructure package while
// legacy HTTP handlers are migrated to the new names.
type App = Service
type FlagResult = Result

type HTTPResult struct {
	Flag    bool        `json:"flag"`
	Status  int         `json:"status"`
	Headers http.Header `json:"headers"`
	Body    string      `json:"body"`
}

type WriteTracker struct {
	Total          int64
	Progress       int64
	LastEmitted    int64
	EmitThreshold  int64
	ProgressChange string
	publish        func(string, ...any)
}
