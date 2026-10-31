package main

import (
	"strings"

	"github.com/kardianos/service"
)

const (
	systemdServicePlatform = "linux-systemd"
	openRCServicePlatform  = "linux-openrc"
	procdServicePlatform   = "linux-procd"
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
	case procdServicePlatform:
		config.Option["SysvScript"] = makeProcdScript(config)
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

func makeProcdScript(config *service.Config) string {
	args := append([]string{config.Executable}, config.Arguments...)
	for index, arg := range args {
		// Quote each argument once for the shell, and prevent the service library's
		// template parser from interpreting literal braces in paths or arguments.
		args[index] = strings.ReplaceAll("'"+strings.ReplaceAll(arg, "'", "'\"'\"'")+"'", "{{", "{'{'")
	}
	return `#!/bin/sh /etc/rc.common
USE_PROCD=1
# Match kardianos/service's installation links; network starts at S20.
START=50
STOP=02

start_service() {
    procd_open_instance
    procd_set_param command ` + strings.Join(args, " ") + `
    procd_set_param respawn 3600 5 5
    # Allow the backend to drain streams and the core to remove its TUN routes.
    procd_set_param term_timeout 35
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}
`
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
