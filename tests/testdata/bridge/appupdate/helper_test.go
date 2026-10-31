package appupdate

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"guiforcores/bridge/platform"
)

func replaceHelperRuntimeForTest(t *testing.T) func() {
	t.Helper()
	originalUpdateLock := acquireUpdateLock
	acquireUpdateLock = func(string) (io.Closer, error) { return io.NopCloser(strings.NewReader("")), nil }
	originalCoreWait := waitForUpdateCore
	waitForUpdateCore = func(string) error { return nil }
	originalWait := waitForUpdateParent
	originalExtract := extractUpdateArchive
	originalReplace := replaceUpdatedApplication
	originalControl := controlUpdatedService
	originalStart := startUpdatedApplication
	originalDelay := serviceControlDelay
	return func() {
		acquireUpdateLock = originalUpdateLock
		waitForUpdateCore = originalCoreWait
		waitForUpdateParent = originalWait
		extractUpdateArchive = originalExtract
		replaceUpdatedApplication = originalReplace
		controlUpdatedService = originalControl
		startUpdatedApplication = originalStart
		serviceControlDelay = originalDelay
	}
}

func TestHelperRefusesReplacementWhenCoreOrLockCannotBeVerified(t *testing.T) {
	for _, failAt := range []string{"lock", "core"} {
		t.Run(failAt, func(t *testing.T) {
			defer replaceHelperRuntimeForTest(t)()
			waitForUpdateParent = func(platform.ProcessIdentity, time.Duration) error { return nil }
			if failAt == "lock" {
				acquireUpdateLock = func(string) (io.Closer, error) { return nil, errors.New("conflict") }
			} else {
				waitForUpdateCore = func(string) error { return errors.New("identity cannot be verified") }
			}
			extractUpdateArchive = func(_ string, destination string) error {
				return os.WriteFile(filepath.Join(destination, appTitle+executableSuffix()), []byte("candidate"), 0755)
			}
			replaceUpdatedApplication = func(string, string) error { t.Error("replaced before cleanup confirmed"); return nil }
			startUpdatedApplication = func(string, []string, string) error { t.Error("started before cleanup confirmed"); return nil }
			base := t.TempDir()
			if err := RunHelper(HelperOptions{ArchivePath: filepath.Join(base, "update.zip"), TargetPath: filepath.Join(base, "webui")}); err == nil {
				t.Fatal("unsafe update succeeded")
			}
		})
	}
}

func writeHelperArchive(t *testing.T, path, name, content string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHelperPreparesUpdateBeforeStoppingService(t *testing.T) {
	for _, mode := range []string{"success", "corrupt_archive", "write_failure", "missing_binary", "empty_binary", "directory_binary"} {
		t.Run(mode, func(t *testing.T) {
			defer replaceHelperRuntimeForTest(t)()
			base := t.TempDir()
			archive := filepath.Join(base, "update.zip")
			target := filepath.Join(base, appTitle+executableSuffix())
			if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
				t.Fatal(err)
			}
			name, content := appTitle+executableSuffix(), "new binary"
			switch mode {
			case "missing_binary":
				name = "README.md"
			case "empty_binary":
				content = ""
			case "directory_binary":
				name, content = name+"/", ""
			}
			writeHelperArchive(t, archive, name, content)
			if mode == "corrupt_archive" {
				if err := os.WriteFile(archive, []byte("not a zip"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var steps []string
			extractUpdateArchive = func(source, destination string) error {
				steps = append(steps, "extract")
				if mode == "write_failure" {
					if err := os.WriteFile(filepath.Join(destination, "partial"), []byte("partial"), 0600); err != nil {
						return err
					}
					return syscall.ENOSPC
				}
				return extractZip(source, destination)
			}
			serviceControlDelay = 0
			controlUpdatedService = func(_, _, action string) error { steps = append(steps, action); return nil }
			waitForUpdateParent = func(platform.ProcessIdentity, time.Duration) error { steps = append(steps, "parent"); return nil }
			acquireUpdateLock = func(string) (io.Closer, error) {
				steps = append(steps, "lock")
				return io.NopCloser(strings.NewReader("")), nil
			}
			waitForUpdateCore = func(string) error { steps = append(steps, "core"); return nil }
			replaceUpdatedApplication = func(extractDir, target string) error {
				steps = append(steps, "replace")
				return replaceApplication(extractDir, target)
			}
			err := RunHelper(HelperOptions{ArchivePath: archive, TargetPath: target, ServiceMode: true})
			wantSteps, wantBinary := "extract", "old binary"
			if mode == "success" {
				wantSteps, wantBinary = "extract,stop,parent,lock,core,replace,start", "new binary"
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid update succeeded")
			}
			if mode == "write_failure" && !errors.Is(err, syscall.ENOSPC) {
				t.Fatalf("write error lost: %v", err)
			}
			if got := strings.Join(steps, ","); got != wantSteps {
				t.Fatalf("steps=%s want=%s", got, wantSteps)
			}
			binary, err := os.ReadFile(target)
			if err != nil || string(binary) != wantBinary {
				t.Fatalf("target=%q error=%v", binary, err)
			}
			entries, err := os.ReadDir(base)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "gui-update-") {
					t.Fatalf("extraction leftover: %s", entry.Name())
				}
			}
		})
	}
}

func TestHelperConsumesPreparedUpdateWithoutExtractingAgain(t *testing.T) {
	for _, serviceMode := range []bool{false, true} {
		t.Run(fmt.Sprint(serviceMode), func(t *testing.T) {
			defer replaceHelperRuntimeForTest(t)()
			base := t.TempDir()
			archive := filepath.Join(base, appUpdateCacheFilePath)
			if err := os.MkdirAll(filepath.Dir(archive), 0755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(base, appTitle+executableSuffix())
			if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
				t.Fatal(err)
			}
			writeHelperArchive(t, archive, appTitle+executableSuffix(), "new binary")
			prepared, err := prepareApplicationUpdate(archive)
			if err != nil {
				t.Fatal(err)
			}
			extractUpdateArchive = func(string, string) error { t.Fatal("helper repeated disk-heavy extraction after handoff"); return nil }
			waitForUpdateParent = func(platform.ProcessIdentity, time.Duration) error { return nil }
			serviceControlDelay = 0
			var stopped, restarted bool
			controlUpdatedService = func(_, _, action string) error {
				if action == "stop" {
					stopped = true
				}
				if action == "start" {
					restarted = true
				}
				return nil
			}
			startUpdatedApplication = func(string, []string, string) error { restarted = true; return nil }
			if err := RunHelper(HelperOptions{ArchivePath: archive, PreparedUpdate: filepath.Base(prepared), TargetPath: target, ServiceMode: serviceMode}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "new binary" {
				t.Fatalf("target=%q error=%v", data, err)
			}
			if stopped != serviceMode || !restarted {
				t.Fatalf("stop=%v restart=%v", stopped, restarted)
			}
			if _, err := os.Stat(prepared); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("prepared directory remains: %v", err)
			}
		})
	}
}

func TestPreparedUpdateHandoffRejectsPathsAndSymlinks(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, appTitle+executableSuffix())
	archive := filepath.Join(base, appUpdateCacheFilePath)
	cache := filepath.Dir(archive)
	if err := os.MkdirAll(cache, 0755); err != nil {
		t.Fatal(err)
	}
	name := preparedUpdatePrefix + "123abc"
	prepared := filepath.Join(cache, name)
	if err := os.Mkdir(prepared, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := preparedUpdateDirectory(archive, target, name); err != nil || got != prepared {
		t.Fatalf("valid handoff=%q error=%v", got, err)
	}
	for _, invalid := range []string{"", "..", prepared, name + "/child", name + `\child`, "gui-update-../outside", "gui-update-123:stream", "other-123"} {
		if _, err := preparedUpdateDirectory(archive, target, invalid); err == nil {
			t.Errorf("accepted handoff %q", invalid)
		}
	}
	if _, err := preparedUpdateDirectory(filepath.Join(t.TempDir(), "gui-update.zip"), target, name); err == nil {
		t.Error("accepted an archive outside the application cache")
	}
	t.Run("symlink", func(t *testing.T) {
		outside := t.TempDir()
		linkName := preparedUpdatePrefix + "link"
		if err := os.Symlink(outside, filepath.Join(cache, linkName)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := preparedUpdateDirectory(archive, target, linkName); err == nil {
			t.Fatal("accepted a symlink handoff")
		}
		if err := RunHelper(HelperOptions{ArchivePath: archive, TargetPath: target, PreparedUpdate: linkName}); err == nil {
			t.Fatal("helper accepted symlink")
		}
		if _, err := os.Stat(outside); err != nil {
			t.Fatalf("invalid handoff removed outside directory: %v", err)
		}
	})
}

func TestUpdateHelperCopyUsesExclusiveTemporaryFileAndCleansFailures(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, directory)
	}
	source := filepath.Join(directory, "source")
	if err := os.WriteFile(source, []byte("helper executable"), 0755); err != nil {
		t.Fatal(err)
	}
	predictable := filepath.Join(directory, fmt.Sprintf("%s-updater-%d%s", appTitle, os.Getpid(), executableSuffix()))
	if err := os.WriteFile(predictable, []byte("must not be overwritten"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := copyUpdateHelper(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(first) })
	second, err := copyUpdateHelper(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(second) })
	if first == second || first == predictable || second == predictable {
		t.Fatal("helper copy reused a predictable path")
	}
	data, err := os.ReadFile(first)
	if err != nil || string(data) != "helper executable" {
		t.Fatalf("helper=%q error=%v", data, err)
	}
	data, err = os.ReadFile(predictable)
	if err != nil || string(data) != "must not be overwritten" {
		t.Fatal("existing temporary file was overwritten")
	}
	before, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := copyUpdateHelper(directory); err == nil {
		t.Fatal("directory source unexpectedly copied")
	}
	after, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("failed helper copy leaked files: before=%d after=%d", len(before), len(after))
	}
}

func TestUpdateParentIdentityRejectsReuseAndMissingIdentity(t *testing.T) {
	identity, err := platform.IdentifyProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	identity.Created--
	if err := waitForParentExit(identity, time.Millisecond); err == nil {
		t.Fatal("PID reuse was accepted")
	}
	if err := waitForParentExit(platform.ProcessIdentity{PID: os.Getpid()}, time.Millisecond); err == nil {
		t.Fatal("incomplete identity was accepted")
	}
}

func TestReplaceApplicationPreservesYAMLAndLogsOnEveryPlatform(t *testing.T) {
	base, archive := t.TempDir(), t.TempDir()
	target := filepath.Join(base, appTitle+executableSuffix())
	source := filepath.Join(archive, appTitle+executableSuffix())
	if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new binary"), 0755); err != nil {
		t.Fatal(err)
	}
	preserved := map[string]string{
		"data/config.yaml":              "coreLogDays: 7\nautoStartKernel: false\n",
		"data/logs/core/2026-09-07.log": "INFO saved core output\n",
	}
	for name, content := range preserved {
		path := filepath.Join(base, name)
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := replaceApplication(archive, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "new binary" {
		t.Fatalf("replacement failed: %v", err)
	}
	for name, content := range preserved {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil || string(data) != content {
			t.Fatalf("lost %s: %v", name, err)
		}
	}
}
