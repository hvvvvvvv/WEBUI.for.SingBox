//go:build linux

package appupdate

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	systemservice "github.com/kardianos/service"
)

const systemdUpdateDescription = "WebUI for sing-box updater"

var (
	currentUpdateServicePlatform = systemservice.Platform
	launchSystemdUpdateProcess   = runSystemdUpdateProcess
	nextSystemdUpdateUnitName    = func() string {
		return fmt.Sprintf("webui-for-singbox-update-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	executeSystemdRun = runSystemdRun
)

func startUpdateProcess(helperPath string, args []string, workingDir string, serviceMode bool) error {
	// systemd stops every process in a service cgroup by default, so an updater
	// launched as a direct child would be killed when it stops the parent unit.
	if serviceMode && currentUpdateServicePlatform() == "linux-systemd" {
		return launchSystemdUpdateProcess(helperPath, args, workingDir)
	}
	return startDirectUpdateProcess(helperPath, args, workingDir)
}

func runSystemdUpdateProcess(helperPath string, args []string, workingDir string) error {
	commandArgs := []string{
		"--quiet",
		"--unit", nextSystemdUpdateUnitName(),
		"--description", systemdUpdateDescription,
		"--service-type=exec",
		escapeSystemdUpdateArgument(helperPath),
	}
	for _, arg := range args {
		commandArgs = append(commandArgs, escapeSystemdUpdateArgument(arg))
	}

	output, err := executeSystemdRun(commandArgs, workingDir)
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message != "" {
		return fmt.Errorf("start updater with systemd-run: %w: %s", err, message)
	}
	return fmt.Errorf("start updater with systemd-run: %w", err)
}

func runSystemdRun(args []string, workingDir string) ([]byte, error) {
	cmd := exec.Command("systemd-run", args...)
	cmd.Env = os.Environ()
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	return cmd.CombinedOutput()
}

func escapeSystemdUpdateArgument(value string) string {
	return strings.ReplaceAll(value, "$", "$$")
}
