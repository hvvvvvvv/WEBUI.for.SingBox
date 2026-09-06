package logging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRuntimeLoggerDisabledDoesNotTouchLogDirectory(t *testing.T) {
	for _, retention := range []int{0, -3} {
		t.Run(fmt.Sprintf("retention_%d", retention), func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "logs")
			var output bytes.Buffer
			logger, closer := NewRuntime(&output, LevelDebug, FileOptions{Directory: directory, RetentionDays: retention})
			logger.Info("message", "component", "test", "operation", "write")
			if err := closer.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("disabled file logger touched %q: %v", directory, err)
			}
			if !strings.Contains(output.String(), `msg="message"`) {
				t.Fatalf("stdout = %q", output.String())
			}
		})
	}
}

func TestRuntimeLoggerDisabledPreservesExistingLogs(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "2020-01-01.log")
	if err := os.WriteFile(path, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logger, closer := NewRuntime(&output, LevelInfo, FileOptions{Directory: directory, RetentionDays: 0})
	logger.Info("new message")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "existing\n" {
		t.Fatalf("disabled logger changed existing content: %q", content)
	}
}

func TestRuntimeLoggerAppendsLinesIdenticalToStdout(t *testing.T) {
	withLocalTimeZone(t, time.UTC)
	directory := t.TempDir()
	date := time.Now().In(time.Local).Format(dailyLogDateLayout)
	var expected strings.Builder

	for index := range 2 {
		var output bytes.Buffer
		logger, closer := NewRuntime(&output, LevelInfo, FileOptions{Directory: directory, RetentionDays: 7})
		logger.Info("message", "component", "test", "operation", "append", "index", index)
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
		expected.WriteString(output.String())
	}

	content, err := os.ReadFile(filepath.Join(directory, date+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != expected.String() {
		t.Fatalf("file content:\n%s\nstdout content:\n%s", content, expected.String())
	}
}

func TestRuntimeLoggerFilteringMatchesStdoutAndFile(t *testing.T) {
	withLocalTimeZone(t, time.UTC)
	tests := []struct {
		level Level
		lines int
	}{
		{LevelDebug, 4},
		{LevelInfo, 3},
		{LevelWarn, 2},
		{LevelError, 1},
	}
	for _, test := range tests {
		t.Run(test.level.String(), func(t *testing.T) {
			directory := t.TempDir()
			var output bytes.Buffer
			logger, closer := NewRuntime(&output, test.level, FileOptions{Directory: directory, RetentionDays: 1})
			logger.Debug("debug")
			logger.Info("info")
			logger.Warn("warn")
			logger.Error("error")
			if err := closer.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, time.Now().In(time.Local).Format(dailyLogDateLayout)+".log")
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != output.String() {
				t.Fatalf("file output differs from stdout:\nfile=%q\nstdout=%q", content, output.String())
			}
			if got := strings.Count(strings.TrimSpace(string(content)), "\n") + 1; got != test.lines {
				t.Fatalf("line count = %d, want %d", got, test.lines)
			}
		})
	}
}

func TestDailyOutputRotatesAndRetainsInclusiveCalendarDays(t *testing.T) {
	withLocalTimeZone(t, time.UTC)
	directory := t.TempDir()
	for _, name := range []string{
		"2026-09-03.log",
		"2026-09-04.log",
		"2026-09-06.log",
		"2026-09-09.log",
		"2026-9-03.log",
		"notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("existing\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	directoryEntry := filepath.Join(directory, "2026-09-02.log")
	if err := os.Mkdir(directoryEntry, 0o755); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(directory, "2026-09-01.log")
	symlinkCreated := os.Symlink(filepath.Join(directory, "2026-09-03.log"), symlink) == nil

	current := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.Local)
	var output bytes.Buffer
	sink := newDailyOutput(&output, LevelDebug, FileOptions{Directory: directory, RetentionDays: 3}, defaultDailyFileOps(), func() time.Time { return current })
	logger := slog.New(NewConsoleHandler(sink, LevelDebug))
	logRecord(t, logger, current, slog.LevelInfo, "first")

	if _, err := os.Stat(filepath.Join(directory, "2026-09-03.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired log still exists: %v", err)
	}
	for _, name := range []string{"2026-09-04.log", "2026-09-06.log", "2026-09-09.log", "2026-9-03.log", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("retained entry %q: %v", name, err)
		}
	}
	if info, err := os.Stat(directoryEntry); err != nil || !info.IsDir() {
		t.Fatalf("date-named directory was removed: %v", err)
	}
	if symlinkCreated {
		if info, err := os.Lstat(symlink); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("date-named symlink was removed: %v", err)
		}
	}

	current = time.Date(2026, time.September, 7, 0, 1, 0, 0, time.Local)
	logRecord(t, logger, current, slog.LevelInfo, "second")
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "2026-09-04.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("log outside new retention window still exists: %v", err)
	}
	for date, message := range map[string]string{"2026-09-06": "first", "2026-09-07": "second"} {
		content, err := os.ReadFile(filepath.Join(directory, date+".log"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), `msg="`+message+`"`) {
			t.Fatalf("%s.log = %q", date, content)
		}
	}
}

func TestDailyOutputRetriesAndReportsRecoveryWithoutRecursion(t *testing.T) {
	withLocalTimeZone(t, time.UTC)
	root := t.TempDir()
	directory := filepath.Join(root, "logs")
	if err := os.WriteFile(directory, []byte("blocks directory creation"), 0o644); err != nil {
		t.Fatal(err)
	}
	current := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.Local)
	var output bytes.Buffer
	sink := newDailyOutput(&output, LevelDebug, FileOptions{Directory: directory, RetentionDays: 7}, defaultDailyFileOps(), func() time.Time { return current })
	logger := slog.New(NewConsoleHandler(sink, LevelDebug))
	logRecord(t, logger, current, slog.LevelInfo, "first")
	logRecord(t, logger, current.Add(time.Second), slog.LevelInfo, "second")
	if got := strings.Count(output.String(), `msg="file logging failed"`); got != 1 {
		t.Fatalf("failure diagnostic count = %d, output=%q", got, output.String())
	}

	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	current = current.Add(fileRetryInterval + time.Second)
	logRecord(t, logger, current, slog.LevelInfo, "recovered message")
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(output.String(), `msg="file logging recovered"`); got != 1 {
		t.Fatalf("recovery diagnostic count = %d, output=%q", got, output.String())
	}
	content, err := os.ReadFile(filepath.Join(directory, "2026-09-06.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `msg="recovered message"`) || strings.Contains(string(content), "file logging") {
		t.Fatalf("recovered file content = %q", content)
	}
}

func TestDailyOutputRetriesAfterWriteFailure(t *testing.T) {
	withLocalTimeZone(t, time.UTC)
	directory := t.TempDir()
	current := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.Local)
	operations := defaultDailyFileOps()
	openCount := 0
	operations.openFile = func(name string, flag int, mode os.FileMode) (dailyLogFile, error) {
		openCount++
		if openCount == 1 {
			return &writeErrorFile{writeErr: errors.New("disk full")}, nil
		}
		return os.OpenFile(name, flag, mode)
	}
	var output bytes.Buffer
	sink := newDailyOutput(&output, LevelDebug, FileOptions{Directory: directory, RetentionDays: 7}, operations, func() time.Time { return current })
	logger := slog.New(NewConsoleHandler(sink, LevelDebug))
	logRecord(t, logger, current, slog.LevelInfo, "failed write")
	if !strings.Contains(output.String(), `msg="file logging failed"`) || !strings.Contains(output.String(), `error="disk full"`) {
		t.Fatalf("write failure output = %q", output.String())
	}

	current = current.Add(fileRetryInterval + time.Second)
	logRecord(t, logger, current, slog.LevelInfo, "successful retry")
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(directory, "2026-09-06.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "failed write") || !strings.Contains(string(content), `msg="successful retry"`) {
		t.Fatalf("file after retry = %q", content)
	}
}

func TestDailyOutputRetentionAcrossMonthAndYearBoundaries(t *testing.T) {
	withLocalTimeZone(t, time.UTC)
	tests := []struct {
		name     string
		current  time.Time
		removed  string
		retained string
	}{
		{
			name:     "year",
			current:  time.Date(2027, time.January, 1, 12, 0, 0, 0, time.Local),
			removed:  "2026-12-30.log",
			retained: "2026-12-31.log",
		},
		{
			name:     "leap month",
			current:  time.Date(2028, time.March, 1, 12, 0, 0, 0, time.Local),
			removed:  "2028-02-28.log",
			retained: "2028-02-29.log",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			for _, name := range []string{test.removed, test.retained} {
				if err := os.WriteFile(filepath.Join(directory, name), []byte("log"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			sink := newDailyOutput(&output, LevelInfo, FileOptions{Directory: directory, RetentionDays: 2}, defaultDailyFileOps(), func() time.Time { return test.current })
			if err := sink.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(directory, test.removed)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expired file %q still exists: %v", test.removed, err)
			}
			if _, err := os.Stat(filepath.Join(directory, test.retained)); err != nil {
				t.Fatalf("retained file %q: %v", test.retained, err)
			}
		})
	}
}

func TestDailyOutputReportsCleanupAndCloseFailuresToStdoutOnly(t *testing.T) {
	withLocalTimeZone(t, time.UTC)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "2026-09-01.log"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	closeFailure := &closeErrorFile{closeErr: errors.New("close denied")}
	operations := defaultDailyFileOps()
	operations.remove = func(string) error { return errors.New("remove denied") }
	operations.openFile = func(string, int, os.FileMode) (dailyLogFile, error) { return closeFailure, nil }
	current := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.Local)
	var output bytes.Buffer
	sink := newDailyOutput(&output, LevelDebug, FileOptions{Directory: directory, RetentionDays: 1}, operations, func() time.Time { return current })
	logger := slog.New(NewConsoleHandler(sink, LevelDebug))
	logRecord(t, logger, current, slog.LevelInfo, "message")
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{`msg="expired log cleanup failed"`, `msg="log file close failed"`} {
		if !strings.Contains(output.String(), message) {
			t.Fatalf("stdout %q does not contain %q", output.String(), message)
		}
		if strings.Contains(closeFailure.String(), message) {
			t.Fatalf("diagnostic %q was written to log file", message)
		}
	}
}

func TestDailyOutputConcurrentLinesMatchStdout(t *testing.T) {
	withLocalTimeZone(t, time.UTC)
	directory := t.TempDir()
	var output bytes.Buffer
	logger, closer := NewRuntime(&output, LevelInfo, FileOptions{Directory: directory, RetentionDays: 1})
	var workers sync.WaitGroup
	for index := range 100 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			logger.Info("message", "component", "test", "operation", "concurrent", "index", index)
		}()
	}
	workers.Wait()
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(directory, time.Now().In(time.Local).Format(dailyLogDateLayout)+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != output.String() {
		t.Fatal("concurrent file output differs from stdout")
	}
	if lines := strings.Count(strings.TrimSpace(string(content)), "\n") + 1; lines != 100 {
		t.Fatalf("line count = %d, want 100", lines)
	}
}

type closeErrorFile struct {
	bytes.Buffer
	closeErr error
}

func (f *closeErrorFile) Close() error { return f.closeErr }

type writeErrorFile struct {
	writeErr error
}

func (f *writeErrorFile) Write([]byte) (int, error) { return 0, f.writeErr }
func (f *writeErrorFile) Close() error              { return nil }

func logRecord(t *testing.T, logger *slog.Logger, timestamp time.Time, level slog.Level, message string) {
	t.Helper()
	record := slog.NewRecord(timestamp, level, message, 0)
	record.AddAttrs(slog.String("component", "test"), slog.String("operation", "write"), slog.String("result", "success"))
	if err := logger.Handler().Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
}

func withLocalTimeZone(t *testing.T, location *time.Location) {
	t.Helper()
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
}
