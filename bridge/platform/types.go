package platform

type ExecOptions struct {
	PIDFile           string `json:"PidFile"`
	StopOutputKeyword string
	WorkingDirectory  string
	Convert           bool
	Env               map[string]string
}

type Result struct {
	Flag bool   `json:"flag"`
	Data string `json:"data"`
}

// Compatibility aliases are kept inside the infrastructure package while
// legacy HTTP handlers are migrated to the new names.
type App = Service
type FlagResult = Result
