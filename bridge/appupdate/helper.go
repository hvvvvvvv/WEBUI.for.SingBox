package appupdate

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
)

type HelperOptions struct {
	ArchivePath      string
	PreparedUpdate   string
	TargetPath       string
	ParentPID        int
	ParentCreated    int64
	ParentExecutable string
	RestartArgs      []string
	WorkingDir       string
	ServiceMode      bool
}

var (
	acquireUpdateLock = func(base string) (io.Closer, error) {
		return storage.LockFile(filepath.Join(base, "data/backend.lock"))
	}
	waitForUpdateParent = waitForParentExit
	waitForUpdateCore   = func(base string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return platform.WaitCoreShutdown(ctx, base)
	}
	extractUpdateArchive      = extractZip
	replaceUpdatedApplication = replaceApplication
	controlUpdatedService     = runServiceControl
	startUpdatedApplication   = startApplication
	serviceControlDelay       = 300 * time.Millisecond
)

func RunHelper(opts HelperOptions) error {
	started := time.Now()
	slog.Info("update helper started", "component", "app_update", "operation", "replace", "target", opts.TargetPath, "service_mode", opts.ServiceMode)
	var extractDir string
	var err error
	if opts.PreparedUpdate != "" {
		extractDir, err = preparedUpdateDirectory(opts.ArchivePath, opts.TargetPath, opts.PreparedUpdate)
	} else {
		// Preserve support for directly invoked helpers; the application normally
		// prepares the update before launching us or requesting shutdown.
		extractDir, err = prepareApplicationUpdate(opts.ArchivePath)
	}
	if err != nil {
		return err
	}
	defer os.RemoveAll(extractDir)
	if err := validateUpdateCandidate(extractDir); err != nil {
		return err
	}
	if opts.ServiceMode {
		time.Sleep(serviceControlDelay)
		if err := controlUpdatedService(opts.TargetPath, opts.WorkingDir, "stop"); err != nil {
			return fmt.Errorf("stop system service: %w", err)
		}
	}

	if err := waitForUpdateParent(platform.ProcessIdentity{PID: opts.ParentPID, Created: opts.ParentCreated, Executable: opts.ParentExecutable}, 30*time.Second); err != nil {
		return err
	}
	updateLock, err := acquireUpdateLock(filepath.Dir(opts.TargetPath))
	if err != nil {
		return fmt.Errorf("lock application for update: %w", err)
	}
	defer updateLock.Close()
	if err := waitForUpdateCore(filepath.Dir(opts.TargetPath)); err != nil {
		return fmt.Errorf("wait for core shutdown: %w", err)
	}

	if err := replaceUpdatedApplication(extractDir, opts.TargetPath); err != nil {
		return err
	}
	_ = os.Remove(opts.ArchivePath)
	if err := updateLock.Close(); err != nil {
		return fmt.Errorf("release update lock: %w", err)
	}

	if opts.ServiceMode {
		if err := controlUpdatedService(opts.TargetPath, opts.WorkingDir, "start"); err != nil {
			return fmt.Errorf("start system service: %w", err)
		}
		slog.Info("update helper completed", "component", "app_update", "operation", "replace", "target", opts.TargetPath, "service_mode", true, "duration", time.Since(started), "result", "success")
		return nil
	}
	if err := startUpdatedApplication(opts.TargetPath, opts.RestartArgs, opts.WorkingDir); err != nil {
		return err
	}
	slog.Info("update helper completed", "component", "app_update", "operation", "replace", "target", opts.TargetPath, "service_mode", false, "duration", time.Since(started), "result", "success")
	return nil
}

const preparedUpdatePrefix = "gui-update-"

func prepareApplicationUpdate(archivePath string) (directory string, err error) {
	directory, err = os.MkdirTemp(filepath.Dir(archivePath), preparedUpdatePrefix+"*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(directory)
		}
	}()
	if err = extractUpdateArchive(archivePath, directory); err != nil {
		return directory, err
	}
	err = validateUpdateCandidate(directory)
	return directory, err
}

// A handoff contains a random directory name, never a caller-selected path.
// Resolve it only in the target application's cache and reject link redirects.
func preparedUpdateDirectory(archivePath, targetPath, name string) (string, error) {
	suffix, ok := strings.CutPrefix(name, preparedUpdatePrefix)
	if !ok || suffix == "" {
		return "", errors.New("invalid prepared update name")
	}
	for _, char := range suffix {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'z' {
				if char < 'A' || char > 'Z' {
					return "", errors.New("invalid prepared update name")
				}
			}
		}
	}
	expectedArchive, err := filepath.Abs(filepath.Join(filepath.Dir(targetPath), appUpdateCacheFilePath))
	if err != nil {
		return "", err
	}
	actualArchive, err := filepath.Abs(archivePath)
	if err != nil {
		return "", err
	}
	sameArchive := expectedArchive == actualArchive
	if runtime.GOOS == "windows" {
		sameArchive = strings.EqualFold(expectedArchive, actualArchive)
	}
	if !sameArchive {
		return "", errors.New("prepared update archive is outside the application cache")
	}
	directory := filepath.Join(filepath.Dir(expectedArchive), name)
	for _, path := range []string{filepath.Dir(expectedArchive), directory} {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("prepared update must use an ordinary cache directory")
		}
	}
	return directory, nil
}

func validateUpdateCandidate(extractDir string) error {
	path := filepath.Join(extractDir, appTitle+executableSuffix())
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("update executable is missing: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("update executable must be a non-empty regular file")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o755); err != nil {
			return fmt.Errorf("prepare update executable permissions: %w", err)
		}
	}
	return nil
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	return cmd.Start()
}

func waitForParentExit(identity platform.ProcessIdentity, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return platform.WaitProcessExit(ctx, identity)
}

func replaceApplication(extractDir string, targetPath string) error {
	// All current release archives, including macOS, contain a single server
	// binary. Replacing a legacy .app bundle would also discard its adjacent
	// YAML and logs; replacing only the executable preserves the data directory.
	sourcePath := filepath.Join(extractDir, appTitle+executableSuffix())
	return replacePath(sourcePath, targetPath, 0o755)
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
	if _, err := io.Copy(file, reader); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return file.Close()
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
