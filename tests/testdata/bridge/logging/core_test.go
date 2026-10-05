package logging

import (
	"bytes"
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

func TestCoreWriterDynamicRetentionAndFormat(t *testing.T) {
	withLocalTimeZone(t, time.FixedZone("CST", 8*60*60))
	dir := filepath.Join(t.TempDir(), "core")
	w := NewCoreWriter(dir, 0)
	defer w.Close()
	when := time.Date(2026, 12, 31, 23, 59, 59, 123000000, time.Local)
	w.sink.now = func() time.Time { return when }
	w.Write(CoreOutput{Time: when, Operation: "startup", Message: "INFO ignored", PID: 10}, "p")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("disabled writer touched directory: %v", err)
	}
	w.SetRetention(2)
	for _, level := range []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL", "PANIC", "custom"} {
		w.Write(CoreOutput{Time: when, Operation: "runtime", Message: "\x1b[31m" + level + " test\x1b[0m\n\"quoted\"", PID: 10}, "p")
	}
	path := filepath.Join(dir, "2026-12-31.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 8 || bytes.Contains(data, []byte("\x1b")) || !bytes.Contains(data, []byte(`msg="TRACE test\n\"quoted\""`)) || !bytes.Contains(data, []byte("2026-12-31T23:59:59.123 CST TRACE component=core operation=runtime")) {
		t.Fatalf("unexpected formatting: %s", data)
	}
	if bytes.Contains(data, []byte("source=")) || !bytes.Contains(data, []byte("UNKNOWN")) {
		t.Fatalf("wrong core metadata: %s", data)
	}
	when = when.Add(2 * time.Second)
	w.Write(CoreOutput{Time: when, Operation: "runtime", Message: "INFO new year"}, "p")
	if _, err := os.Stat(filepath.Join(dir, "2027-01-01.log")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("two-day retention should preserve yesterday", err)
	}
	w.SetRetention(1)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("shortened retention did not clean old file", err)
	}
	w.SetRetention(0)
	when = when.AddDate(0, 0, 3)
	w.Write(CoreOutput{Time: when, Operation: "runtime", Message: "INFO disabled"}, "p")
	if _, err := os.Stat(filepath.Join(dir, "2027-01-01.log")); err != nil {
		t.Fatal("disabled writer deleted logs", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2027-01-04.log")); !os.IsNotExist(err) {
		t.Fatal("disabled writer created file", err)
	}
}

func TestCoreWriterConcurrentConfigurationAndWrites(t *testing.T) {
	dir := t.TempDir()
	w := NewCoreWriter(dir, 1)
	var wg sync.WaitGroup
	for n := range 8 {
		wg.Go(func() {
			for i := range 100 {
				w.Write(CoreOutput{Time: time.Now(), Operation: "runtime", Message: fmt.Sprintf("INFO worker=%d entry=%d", n, i)}, "p")
			}
		})
	}
	wg.Go(func() {
		for range 100 {
			w.SetRetention(2)
			w.SetRetention(1)
		}
	})
	wg.Wait()
	_ = w.Close()
	data, err := os.ReadFile(filepath.Join(dir, time.Now().In(time.Local).Format(dailyLogDateLayout)+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 800 {
		t.Fatalf("lines=%d", lines)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Count(line, "component=core") != 1 {
			t.Fatalf("interleaved line: %s", line)
		}
	}
}

func TestCoreWriterFailureRetryRecoveryUsesOnlyApplicationLogger(t *testing.T) {
	var app bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(NewConsoleHandler(&app, LevelDebug)))
	defer slog.SetDefault(previous)
	w := NewCoreWriter(t.TempDir(), 0)
	defer w.Close()
	when := time.Now()
	w.sink.now = func() time.Time { return when }
	opens := 0
	fail := true
	realOpen := w.sink.operations.openFile
	w.sink.operations.openFile = func(path string, flag int, mode os.FileMode) (dailyLogFile, error) {
		opens++
		if fail {
			return nil, errors.New("disk unavailable")
		}
		return realOpen(path, flag, mode)
	}
	w.SetRetention(1)
	entry := CoreOutput{Time: when, Operation: "runtime", Message: "DEBUG private core output"}
	w.Write(entry, "")
	w.Write(entry, "")
	if opens != 1 || strings.Count(app.String(), "file logging failed") != 1 {
		t.Fatalf("opens=%d diagnostics=%s", opens, app.String())
	}
	when = when.Add(time.Minute)
	fail = false
	entry.Time = when
	w.Write(entry, "")
	if opens != 2 || strings.Count(app.String(), "file logging recovered") != 1 {
		t.Fatalf("recovery: %d %s", opens, app.String())
	}
	if strings.Contains(app.String(), "private core output") {
		t.Fatal("core output leaked to app logger")
	}
	data, err := os.ReadFile(filepath.Join(w.sink.directory, when.In(time.Local).Format(dailyLogDateLayout)+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "private core output") != 1 || strings.Contains(string(data), "file logging") {
		t.Fatalf("unexpected file content: %s", data)
	}
}
