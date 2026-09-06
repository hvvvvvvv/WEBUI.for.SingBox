package logging

import (
	"bytes"
	"context"
	"errors"
	"log"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
)

func TestParseLevel(t *testing.T) {
	for _, value := range []Level{LevelDebug, LevelInfo, LevelWarn, LevelError} {
		parsed, err := ParseLevel(value.String())
		if err != nil || parsed != value {
			t.Fatalf("ParseLevel(%q) = %q, %v", value, parsed, err)
		}
	}
	if _, err := ParseLevel("trace"); err == nil {
		t.Fatal("expected invalid level error")
	}
}

func TestSetDefaultBridgesStandardLogger(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var output bytes.Buffer
	slog.SetDefault(New(&output, LevelInfo))
	log.Print("legacy message")
	if !strings.Contains(output.String(), `msg="legacy message"`) {
		t.Fatalf("legacy log output = %q", output.String())
	}
}

func TestCompleteClassifiesExpectedErrors(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var output bytes.Buffer
	slog.SetDefault(New(&output, LevelDebug))

	Complete(context.Background(), "profile", "create", "profile created", time.Now(), connect.NewError(connect.CodeInvalidArgument, errors.New("bad profile")))
	Complete(context.Background(), "scheduler", "run", "task completed", time.Now(), connect.NewError(connect.CodeCanceled, errors.New("stopped")))
	Complete(context.Background(), "network", "download", "download completed", time.Now(), context.DeadlineExceeded)
	Complete(context.Background(), "config", "save", "config saved", time.Now(), errors.New("disk full"))

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %q", output.String())
	}
	if !strings.Contains(lines[0], "WARN ") || !strings.Contains(lines[0], `result=failure`) || !strings.Contains(lines[0], `msg="profile create rejected"`) {
		t.Fatalf("invalid argument line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "INFO ") || !strings.Contains(lines[1], `result=cancelled`) {
		t.Fatalf("cancelled line = %q", lines[1])
	}
	if !strings.Contains(lines[2], "WARN ") || !strings.Contains(lines[2], `error="context deadline exceeded"`) {
		t.Fatalf("deadline line = %q", lines[2])
	}
	if !strings.Contains(lines[3], "ERROR") || !strings.Contains(lines[3], `source=logging_test.go:`) {
		t.Fatalf("internal error line = %q", lines[3])
	}
	for _, line := range lines[:3] {
		if strings.Contains(line, " source=") {
			t.Fatalf("non-error line contains source: %q", line)
		}
	}
}

func TestConsoleHandlerLevelFiltering(t *testing.T) {
	tests := []struct {
		level Level
		want  []string
	}{
		{LevelDebug, []string{"debug", "info", "warn", "error"}},
		{LevelInfo, []string{"info", "warn", "error"}},
		{LevelWarn, []string{"warn", "error"}},
		{LevelError, []string{"error"}},
	}
	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			var output bytes.Buffer
			logger := New(&output, tt.level)
			logger.Debug("debug")
			logger.Info("info")
			logger.Warn("warn")
			logger.Error("error")
			lines := strings.Split(strings.TrimSpace(output.String()), "\n")
			if len(lines) != len(tt.want) {
				t.Fatalf("got %d lines, want %d: %q", len(lines), len(tt.want), output.String())
			}
			for index, message := range tt.want {
				if !strings.Contains(lines[index], `msg="`+message+`"`) {
					t.Fatalf("line %d = %q, want message %q", index, lines[index], message)
				}
			}
		})
	}
}

func TestConsoleHandlerFormatting(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("CST", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocal })

	var output bytes.Buffer
	handler := NewConsoleHandler(&output, LevelDebug)
	logger := slog.New(handler.WithAttrs([]slog.Attr{slog.String("component", "kernel")})).WithGroup("request")
	record := slog.NewRecord(
		time.Date(2026, time.September, 3, 14, 25, 36, 218000000, time.FixedZone("UTC+2", 2*60*60)),
		slog.LevelInfo,
		"core\nstarted",
		0,
	)
	record.AddAttrs(
		slog.String("operation", "start"),
		slog.Duration("duration", 428*time.Millisecond),
		slog.Any("items", []string{"a", "b"}),
	)
	if err := logger.Handler().Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	want := "2026-09-03T20:25:36.218 CST INFO  component=kernel operation=log msg=\"core\\nstarted\" request_operation=\"start\" request_duration=428ms request_items=[\"a\",\"b\"]\n"
	if output.String() != want {
		t.Fatalf("output:\n%q\nwant:\n%q", output.String(), want)
	}
}

func TestConsoleHandlerErrorIncludesSourceAndErrorChain(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, LevelInfo)
	err := errors.New("disk full")
	logger.Error("save failed", "component", "config", "operation", "save", "error", errors.Join(errors.New("write config"), err))
	line := output.String()
	for _, fragment := range []string{
		`component=config`,
		`operation=save`,
		`msg="save failed"`,
		`error="write config\ndisk full"`,
		`source=logging_test.go:`,
	} {
		if !strings.Contains(line, fragment) {
			t.Fatalf("output %q does not contain %q", line, fragment)
		}
	}
}

func TestConsoleHandlerConcurrentWritesRemainWholeLines(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, LevelInfo)
	var workers sync.WaitGroup
	for index := range 50 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			logger.Info("message", "index", index)
		}()
	}
	workers.Wait()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 50 {
		t.Fatalf("got %d lines, want 50", len(lines))
	}
	for _, line := range lines {
		if strings.Count(line, `msg="message"`) != 1 {
			t.Fatalf("interleaved or malformed line: %q", line)
		}
	}
}
