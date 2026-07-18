package appupdate

import (
	"archive/zip"
	"errors"
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
	ServiceMode bool
}

var (
	waitForUpdateParent       = waitForParentExit
	extractUpdateArchive      = extractZip
	replaceUpdatedApplication = replaceApplication
	controlUpdatedService     = runServiceControl
	startUpdatedApplication   = startApplication
	serviceControlDelay       = 300 * time.Millisecond
)

func RunHelper(opts HelperOptions) error {
	if opts.ServiceMode {
		time.Sleep(serviceControlDelay)
		if err := controlUpdatedService(opts.TargetPath, opts.WorkingDir, "stop"); err != nil {
			return fmt.Errorf("stop system service: %w", err)
		}
	}

	if err := waitForUpdateParent(opts.ParentPID, 30*time.Second); err != nil {
		return err
	}

	extractDir, err := os.MkdirTemp(filepath.Dir(opts.ArchivePath), "gui-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(extractDir)

	if err := extractUpdateArchive(opts.ArchivePath, extractDir); err != nil {
		return err
	}
	if err := replaceUpdatedApplication(extractDir, opts.TargetPath); err != nil {
		return err
	}
	_ = os.Remove(opts.ArchivePath)

	if opts.ServiceMode {
		if err := controlUpdatedService(opts.TargetPath, opts.WorkingDir, "start"); err != nil {
			return fmt.Errorf("start system service: %w", err)
		}
		return nil
	}
	return startUpdatedApplication(opts.TargetPath, opts.RestartArgs, opts.WorkingDir)
}

func runServiceControl(targetPath, workingDir, action string) error {
	cmd := exec.Command(targetPath, "service", action)
	cmd.Env = os.Environ()
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("%w: %s", err, message)
		}
		return err
	}
	return nil
}

func startApplication(targetPath string, args []string, workingDir string) error {
	cmd := exec.Command(targetPath, args...)
	cmd.Env = os.Environ()
	if workingDir != "" {
		cmd.Dir = workingDir
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
