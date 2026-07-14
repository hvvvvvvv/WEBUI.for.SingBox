package runtime

type proxyRef struct {
	ID   string `json:"id" yaml:"id"`
	Tag  string `json:"tag" yaml:"tag"`
	Type string `json:"type" yaml:"type"`
}

type subscriptionHeader struct {
	Request  map[string]string `json:"request" yaml:"request"`
	Response map[string]string `json:"response" yaml:"response"`
}

type subscription struct {
	ID                   string             `json:"id" yaml:"id"`
	Name                 string             `json:"name" yaml:"name"`
	Upload               int64              `json:"upload" yaml:"upload"`
	Download             int64              `json:"download" yaml:"download"`
	Total                int64              `json:"total" yaml:"total"`
	Expire               int64              `json:"expire" yaml:"expire"`
	UpdateTime           int64              `json:"updateTime" yaml:"updateTime"`
	Type                 string             `json:"type" yaml:"type"`
	URL                  string             `json:"url" yaml:"url"`
	Website              string             `json:"website" yaml:"website"`
	Include              string             `json:"include" yaml:"include"`
	Exclude              string             `json:"exclude" yaml:"exclude"`
	IncludeProtocol      string             `json:"includeProtocol" yaml:"includeProtocol"`
	ExcludeProtocol      string             `json:"excludeProtocol" yaml:"excludeProtocol"`
	ProxyPrefix          string             `json:"proxyPrefix" yaml:"proxyPrefix"`
	Disabled             bool               `json:"disabled" yaml:"disabled"`
	InSecure             bool               `json:"inSecure" yaml:"inSecure"`
	EnableNodeConversion bool               `json:"enableNodeConversion" yaml:"enableNodeConversion"`
	RequestMethod        string             `json:"requestMethod" yaml:"requestMethod"`
	RequestTimeout       int                `json:"requestTimeout" yaml:"requestTimeout"`
	Header               subscriptionHeader `json:"header" yaml:"header"`
	Proxies              []proxyRef         `json:"proxies" yaml:"proxies"`
	Script               string             `json:"script" yaml:"script"`
	Updating             bool               `json:"updating,omitempty" yaml:"-"`
}

type ruleset struct {
	ID         string `json:"id" yaml:"id"`
	Tag        string `json:"tag" yaml:"tag"`
	UpdateTime int64  `json:"updateTime" yaml:"updateTime"`
	Disabled   bool   `json:"disabled" yaml:"disabled"`
	Type       string `json:"type" yaml:"type"`
	Format     string `json:"format" yaml:"format"`
	Path       string `json:"path" yaml:"path"`
	URL        string `json:"url" yaml:"url"`
	Count      int    `json:"count" yaml:"count"`
	Updating   bool   `json:"updating,omitempty" yaml:"-"`
}

const (
	scheduledTaskUpdateSubscription    = "update::subscription"
	scheduledTaskUpdateRuleset         = "update::ruleset"
	scheduledTaskUpdateAllSubscription = "update::all::subscription"
	scheduledTaskUpdateAllRuleset      = "update::all::ruleset"
	scheduledTaskRunScript             = "run::script"
)

type scheduledTask struct {
	ID            string   `json:"id" yaml:"id"`
	Name          string   `json:"name" yaml:"name"`
	Type          string   `json:"type" yaml:"type"`
	Subscriptions []string `json:"subscriptions" yaml:"subscriptions"`
	Rulesets      []string `json:"rulesets" yaml:"rulesets"`
	Script        string   `json:"script" yaml:"script"`
	Cron          string   `json:"cron" yaml:"cron"`
	Notification  bool     `json:"notification" yaml:"notification"`
	Disabled      bool     `json:"disabled" yaml:"disabled"`
	LastTime      int64    `json:"lastTime" yaml:"lastTime"`
	LogLimit      int      `json:"logLimit" yaml:"logLimit"`
}

type scheduledTaskResult struct {
	Ok     bool   `json:"ok" yaml:"ok"`
	ID     string `json:"id" yaml:"id"`
	Name   string `json:"name" yaml:"name"`
	Result string `json:"result" yaml:"result"`
}

type scheduledTaskLog struct {
	ID        string                `json:"id" yaml:"id"`
	Name      string                `json:"name" yaml:"name"`
	StartTime int64                 `json:"startTime" yaml:"startTime"`
	EndTime   int64                 `json:"endTime" yaml:"endTime"`
	Results   []scheduledTaskResult `json:"results" yaml:"results"`
}
