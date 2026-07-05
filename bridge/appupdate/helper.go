package appupdate

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"guiforcores/bridge/platform"
)

type HelperOptions struct {
	ArchivePath string
	TargetPath  string
	ParentPID   int
	RestartArgs []string
	WorkingDir  string
}

func IsHelperMode(args []string) bool {
	for _, arg := range args {
		if arg == appUpdateHelperFlag {
			return true
		}
	}
	return false
}

func ParseHelperOptions(args []string) (HelperOptions, error) {
	var opts HelperOptions
	var restartArgsJSON string
	fs := flag.NewFlagSet("updater-helper", flag.ContinueOnError)
	fs.Bool(flagName(appUpdateHelperFlag), false, "")
	fs.StringVar(&opts.ArchivePath, flagName(appUpdateArchiveFlag), "", "")
	fs.StringVar(&opts.TargetPath, flagName(appUpdateTargetFlag), "", "")
	fs.IntVar(&opts.ParentPID, flagName(appUpdateParentFlag), 0, "")
	fs.StringVar(&restartArgsJSON, flagName(appUpdateArgsFlag), "[]", "")
	fs.StringVar(&opts.WorkingDir, flagName(appUpdateWorkingDirFlag), "", "")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.ArchivePath == "" || opts.TargetPath == "" || opts.ParentPID <= 0 {
		return opts, fmt.Errorf("invalid updater helper arguments")
	}
	if err := json.Unmarshal([]byte(restartArgsJSON), &opts.RestartArgs); err != nil {
		return opts, err
	}
	return opts, nil
}

func flagName(name string) string {
	return strings.TrimLeft(name, "-")
}

func RunHelper(opts HelperOptions) error {
	if err := waitForParentExit(opts.ParentPID, 30*time.Second); err != nil {
		return err
	}

	extractDir, err := os.MkdirTemp(filepath.Dir(opts.ArchivePath), "gui-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(extractDir)

	if err := extractZip(opts.ArchivePath, extractDir); err != nil {
		return err
	}
	if err := replaceApplication(extractDir, opts.TargetPath); err != nil {
		return err
	}
	_ = os.Remove(opts.ArchivePath)

	cmd := exec.Command(opts.TargetPath, opts.RestartArgs...)
	cmd.Env = os.Environ()
	if opts.WorkingDir != "" {
		cmd.Dir = opts.WorkingDir
	}
	return cmd.Start()
}

func waitForParentExit(pid int, timeout time.Duration) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		alive, err := platform.IsProcessAlive(proc)
		if err != nil {
			return err
		}
		if !alive {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for parent process %s to exit", strconv.Itoa(pid))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func replaceApplication(extractDir string, targetPath string) error {
	if runtime.GOOS == "darwin" {
		return replaceDarwinApp(extractDir, targetPath)
	}
	sourcePath := filepath.Join(extractDir, appTitle+executableSuffix())
	return replacePath(sourcePath, targetPath, 0o755)
}

func replaceDarwinApp(extractDir string, targetPath string) error {
	marker := string(os.PathSeparator) + "Contents" + string(os.PathSeparator) + "MacOS" + string(os.PathSeparator)
	index := strings.Index(targetPath, marker)
	if index < 0 {
		return fmt.Errorf("target executable is not inside a macOS .app bundle: %s", targetPath)
	}
	targetApp := targetPath[:index]
	sourceApp := filepath.Join(extractDir, appTitle+".app")
	return replacePath(sourceApp, targetApp, 0o755)
}

func replacePath(source string, target string, mode os.FileMode) error {
	if _, err := os.Stat(source); err != nil {
		return err
	}
	backup := target + ".bak"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(source, target); err != nil {
		if !fileExists(target) && fileExists(backup) {
			_ = os.Rename(backup, target)
		}
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(target, mode)
	}
	_ = os.RemoveAll(backup)
	return nil
}

func extractZip(archivePath string, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		path, err := safeJoin(targetDir, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(path, file.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		err = writeExtractedFile(path, src, file.Mode())
		closeErr := src.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func writeExtractedFile(path string, reader io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, reader)
	return err
}

func safeJoin(baseDir string, name string) (string, error) {
	target := filepath.Join(baseDir, name)
	cleanBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, cleanBase+string(os.PathSeparator)) {
		return "", errors.New("unsafe archive path: " + name)
	}
	return target, nil
}
