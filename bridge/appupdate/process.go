package appupdate

import (
	"os"
	"os/exec"
)

var startDirectUpdateProcess = runDirectUpdateProcess

func runDirectUpdateProcess(helperPath string, args []string, workingDir string) error {
	cmd := exec.Command(helperPath, args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	return cmd.Start()
}
