//go:build !linux

package appupdate

func startUpdateProcess(helperPath string, args []string, workingDir string, _ bool) error {
	return startDirectUpdateProcess(helperPath, args, workingDir)
}
