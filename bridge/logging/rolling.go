package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	dailyLogDateLayout = "2006-01-02"
	fileRetryInterval  = time.Minute
	allFourDigitDates  = 366 * 10000
)

// FileOptions configures optional daily application log files.
type FileOptions struct {
	Directory     string
	RetentionDays int
}

// NewRuntime creates the process logger. Its output always includes stdout;
// when RetentionDays is positive, identical lines are appended to daily files.
func NewRuntime(output io.Writer, level Level, options FileOptions) (*slog.Logger, io.Closer) {
	sink := newDailyOutput(output, level, options, defaultDailyFileOps(), time.Now)
	return slog.New(NewConsoleHandler(sink, level)), sink
}

type dailyLogFile interface {
	io.Writer
	io.Closer
}

type dailyFileOps struct {
	mkdirAll func(string, os.FileMode) error
	readDir  func(string) ([]os.DirEntry, error)
	openFile func(string, int, os.FileMode) (dailyLogFile, error)
	remove   func(string) error
}

func defaultDailyFileOps() dailyFileOps {
	return dailyFileOps{
		mkdirAll: os.MkdirAll,
		readDir:  os.ReadDir,
		openFile: func(name string, flag int, perm os.FileMode) (dailyLogFile, error) {
			return os.OpenFile(name, flag, perm)
		},
		remove: os.Remove,
	}
}

type dailyOutput struct {
	mu sync.Mutex

	stdout      io.Writer
	diagnostics *ConsoleHandler
	directory   string
	retention   int
	operations  dailyFileOps
	now         func() time.Time
	retryAfter  time.Duration
	file        dailyLogFile
	fileDate    string
	cleanupDate string
	failure     string
	nextRetry   time.Time
	closed      bool
}

func newDailyOutput(output io.Writer, level Level, options FileOptions, operations dailyFileOps, now func() time.Time) *dailyOutput {
	if output == nil {
		output = io.Discard
	}
	if now == nil {
		now = time.Now
	}
	directory := options.Directory
	if directory != "" {
		directory = filepath.Clean(directory)
	}
	sink := &dailyOutput{
		stdout:      output,
		diagnostics: NewConsoleHandler(output, level),
		directory:   directory,
		retention:   options.RetentionDays,
		operations:  operations,
		now:         now,
		retryAfter:  fileRetryInterval,
	}
	if sink.retention > 0 {
		date := sink.now().In(time.Local).Format(dailyLogDateLayout)
		if err := sink.prepareDirectory(date); err != nil {
			sink.reportFileFailure("initialize", err)
		}
	}
	return sink
}

func (w *dailyOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	written, stdoutErr := w.stdout.Write(data)
	if stdoutErr == nil && written != len(data) {
		stdoutErr = io.ErrShortWrite
	}
	if w.retention > 0 && !w.closed {
		w.writeFile(data)
	}
	return written, stdoutErr
}

func (w *dailyOutput) writeFile(data []byte) {
	now := w.now()
	if w.failure != "" && now.Before(w.nextRetry) {
		return
	}

	date := dateFromLogLine(data, now)
	if w.file == nil || w.fileDate != date {
		w.closeCurrentFile()
		if err := w.prepareDirectory(date); err != nil {
			w.reportFileFailure("open", err)
			return
		}
		path := filepath.Join(w.directory, date+".log")
		file, err := w.operations.openFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			w.reportFileFailure("open", err)
			return
		}
		w.file = file
		w.fileDate = date
	}

	written, err := w.file.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		if closeErr := w.file.Close(); closeErr != nil {
			w.reportDiagnostic(slog.LevelWarn, "log file close failed", "close", "failure", closeErr)
		}
		w.file = nil
		w.reportFileFailure("write", err)
		return
	}
	if w.failure != "" {
		w.failure = ""
		w.nextRetry = time.Time{}
		w.reportDiagnostic(slog.LevelInfo, "file logging recovered", "recover", "success", nil)
	}
}

func (w *dailyOutput) prepareDirectory(date string) error {
	if w.directory == "" {
		return errors.New("log directory is empty")
	}
	if err := w.operations.mkdirAll(w.directory, 0o755); err != nil {
		return err
	}
	if w.cleanupDate == date {
		return nil
	}
	w.cleanupDate = date
	if err := w.removeExpired(date); err != nil {
		w.reportDiagnostic(slog.LevelWarn, "expired log cleanup failed", "cleanup", "failure", err)
	}
	return nil
}

func (w *dailyOutput) removeExpired(currentDate string) error {
	current, err := time.ParseInLocation(dailyLogDateLayout, currentDate, time.Local)
	if err != nil {
		return err
	}
	entries, err := w.operations.readDir(w.directory)
	if err != nil {
		return err
	}
	if w.retention > allFourDigitDates {
		return nil
	}
	cutoff := current.AddDate(0, 0, -(w.retention - 1))
	var cleanupErr error
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if len(name) != len("2006-01-02.log") || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateText := strings.TrimSuffix(name, ".log")
		date, parseErr := time.ParseInLocation(dailyLogDateLayout, dateText, time.Local)
		if parseErr != nil || date.Format(dailyLogDateLayout) != dateText || !date.Before(cutoff) {
			continue
		}
		if removeErr := w.operations.remove(filepath.Join(w.directory, name)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, removeErr)
		}
	}
	return cleanupErr
}

func (w *dailyOutput) reportFileFailure(operation string, err error) {
	failure := err.Error()
	if failure != w.failure {
		w.reportDiagnostic(slog.LevelError, "file logging failed", operation, "failure", err)
	}
	w.failure = failure
	w.nextRetry = w.now().Add(w.retryAfter)
}

func (w *dailyOutput) reportDiagnostic(level slog.Level, message, operation, result string, err error) {
	if !w.diagnostics.Enabled(context.Background(), level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])
	record := slog.NewRecord(w.now(), level, message, pcs[0])
	record.AddAttrs(
		slog.String("component", "logging"),
		slog.String("operation", operation),
		slog.String("directory", w.directory),
		slog.String("result", result),
	)
	if err != nil {
		record.AddAttrs(slog.Any("error", err))
	}
	_ = w.diagnostics.Handle(context.Background(), record)
}

func (w *dailyOutput) closeCurrentFile() {
	if w.file == nil {
		return
	}
	if err := w.file.Close(); err != nil {
		w.reportDiagnostic(slog.LevelWarn, "log file close failed", "close", "failure", err)
	}
	w.file = nil
	w.fileDate = ""
}

func (w *dailyOutput) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	w.closeCurrentFile()
	return nil
}

func dateFromLogLine(data []byte, fallback time.Time) string {
	if len(data) >= len(dailyLogDateLayout) {
		candidate := string(data[:len(dailyLogDateLayout)])
		if parsed, err := time.ParseInLocation(dailyLogDateLayout, candidate, time.Local); err == nil && parsed.Format(dailyLogDateLayout) == candidate {
			return candidate
		}
	}
	return fallback.In(time.Local).Format(dailyLogDateLayout)
}
