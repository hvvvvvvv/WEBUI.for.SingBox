package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CoreOutput is timestamped at the process pipe, not when the backend receives it.
type CoreOutput struct {
	Time      time.Time `json:"time"`
	PID       int       `json:"pid"`
	Operation string    `json:"operation"`
	Message   string    `json:"message"`
}

type CoreWriter struct{ sink *dailyOutput }

func NewCoreWriter(directory string, days int) *CoreWriter {
	sink := newDailyOutput(io.Discard, LevelDebug, FileOptions{Directory: directory}, defaultDailyFileOps(), time.Now)
	sink.diagnostic = func(level slog.Level, message, operation, result string, err error) {
		attrs := []any{"component", "logging", "operation", operation, "directory", directory, "result", result}
		if err != nil {
			attrs = append(attrs, "error", err)
		}
		slog.Log(context.Background(), level, message, attrs...)
	}
	w := &CoreWriter{sink: sink}
	w.SetRetention(days)
	return w
}

func (w *CoreWriter) SetRetention(days int) {
	w.sink.mu.Lock()
	defer w.sink.mu.Unlock()
	if w.sink.closed || w.sink.retention == days {
		return
	}
	w.sink.retention = days
	w.sink.cleanupDate = ""
	if days <= 0 {
		w.sink.failure = ""
		w.sink.nextRetry = time.Time{}
		w.sink.closeCurrentFile()
		return
	}
	if err := w.sink.prepareDirectory(w.sink.now().In(time.Local).Format(dailyLogDateLayout)); err != nil {
		w.sink.reportFileFailure("initialize", err)
	}
}

var coreANSI = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))`)
var coreLevel = regexp.MustCompile(`(?i)(?:^|\s)(TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|PANIC)(?:\[|\s|:|$)`)

func (w *CoreWriter) Write(output CoreOutput, profileID string) {
	w.sink.mu.Lock()
	defer w.sink.mu.Unlock()
	if w.sink.closed || w.sink.retention <= 0 {
		return
	}
	message := coreANSI.ReplaceAllString(output.Message, "")
	level := "UNKNOWN"
	if match := coreLevel.FindStringSubmatch(message); match != nil {
		level = strings.ToUpper(match[1])
		if level == "WARNING" {
			level = "WARN"
		}
	}
	when := output.Time
	if when.IsZero() {
		when = w.sink.now()
	}
	line := fmt.Sprintf("%s %-5s component=core operation=%s msg=%s", when.In(time.Local).Format("2006-01-02T15:04:05.000 MST"), level, output.Operation, strconv.Quote(message))
	if profileID != "" {
		line += " profile_id=" + strconv.Quote(profileID)
	}
	if output.PID > 0 {
		line += fmt.Sprintf(" pid=%d", output.PID)
	}
	w.sink.writeFile([]byte(line + "\n"))
}

func (w *CoreWriter) Close() error { return w.sink.Close() }
