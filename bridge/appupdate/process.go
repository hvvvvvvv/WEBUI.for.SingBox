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
	if err := cmd.Start(); err != nil {
		return err
	}
	// A failed helper can exit while the original application keeps running.
	// Reap it without waiting here for the update to finish.
	go func() { _ = cmd.Wait() }()
	return nil
}
