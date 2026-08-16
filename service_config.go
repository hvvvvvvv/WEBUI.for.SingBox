package main

import "github.com/kardianos/service"

const (
	systemdServicePlatform = "linux-systemd"
	openRCServicePlatform  = "linux-openrc"
	windowsServicePlatform = "windows-service"
	launchdServicePlatform = "darwin-launchd"
)

func configureServicePlatform(config *service.Config, platform string) {
	switch platform {
	case systemdServicePlatform:
		config.Dependencies = []string{
			"Wants=network-online.target",
			"After=network-online.target",
		}
	case openRCServicePlatform:
		config.Dependencies = []string{"need net"}
	case windowsServicePlatform:
		config.Option["DelayedAutoStart"] = true
	case launchdServicePlatform:
		// NetworkState is only available through a custom launchd template in
		// kardianos/service. RunAtLoad must be disabled so launchd waits for
		// network availability instead of starting the service immediately.
		config.Option["RunAtLoad"] = false
		config.Option["LaunchdConfig"] = networkAwareLaunchdConfig
	}
}

const networkAwareLaunchdConfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Disabled</key>
	<false/>
	{{if EnvVars}}<key>EnvironmentVariables</key>
	<dict>
{{range EnvVars}}{{.}}
{{end}}	</dict>
	{{end}}<key>KeepAlive</key>
	<dict>
		<key>NetworkState</key>
		<true/>
	</dict>
	<key>Label</key>
	<string>{{Name | html}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{Path | html}}</string>
{{range Arguments}}		<string>{{. | html}}</string>
{{end}}	</array>
	{{if ChRoot}}<key>RootDirectory</key>
	<string>{{ChRoot | html}}</string>
	{{end}}<key>RunAtLoad</key>
	<{{RunAtLoad}}/>
	<key>SessionCreate</key>
	<{{SessionCreate}}/>
	{{if StandardErrorPath}}<key>StandardErrorPath</key>
	<string>{{StandardErrorPath | html}}</string>
	{{end}}{{if StandardOutPath}}<key>StandardOutPath</key>
	<string>{{StandardOutPath | html}}</string>
	{{end}}{{if UserName}}<key>UserName</key>
	<string>{{UserName | html}}</string>
	{{end}}{{if WorkingDirectory}}<key>WorkingDirectory</key>
	<string>{{WorkingDirectory | html}}</string>
	{{end}}</dict>
</plist>
`
