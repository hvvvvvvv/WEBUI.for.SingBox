package logging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
)

// Level is the configured minimum application log level.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

func (l Level) String() string { return string(l) }

// ParseLevel validates a CLI log-level value.
func ParseLevel(value string) (Level, error) {
	level := Level(strings.ToLower(strings.TrimSpace(value)))
	switch level {
	case LevelDebug, LevelInfo, LevelWarn, LevelError:
		return level, nil
	default:
		return "", fmt.Errorf("invalid log level %q", value)
	}
}

func (l Level) SlogLevel() slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type synchronizedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

type ConsoleHandler struct {
	output *synchronizedWriter
	level  slog.Level
	attrs  []flatAttr
	groups []string
}

func NewConsoleHandler(output io.Writer, level Level) *ConsoleHandler {
	if output == nil {
		output = io.Discard
	}
	return &ConsoleHandler{
		output: &synchronizedWriter{w: output},
		level:  level.SlogLevel(),
	}
}

func New(output io.Writer, level Level) *slog.Logger {
	return slog.New(NewConsoleHandler(output, level))
}

func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *ConsoleHandler) Handle(_ context.Context, record slog.Record) error {
	attributes := append([]flatAttr(nil), h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		flattenAttr(&attributes, h.groups, attr)
		return true
	})

	component, attributes := takeAttribute(attributes, "component", slog.StringValue("app"))
	operation, attributes := takeAttribute(attributes, "operation", slog.StringValue("log"))

	timestamp := record.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	timestamp = timestamp.In(time.Local)

	var line strings.Builder
	line.WriteString(timestamp.Format("2006-01-02T15:04:05.000 MST"))
	line.WriteByte(' ')
	line.WriteString(fmt.Sprintf("%-5s", strings.ToUpper(record.Level.String())))
	writeTokenField(&line, "component", component)
	writeTokenField(&line, "operation", operation)
	writeField(&line, "msg", slog.StringValue(record.Message))
	for _, attr := range attributes {
		if attr.key == "result" {
			writeTokenField(&line, attr.key, attr.value)
		} else {
			writeField(&line, attr.key, attr.value)
		}
	}
	if record.Level >= slog.LevelError && record.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
		if frame.File != "" {
			writeTokenField(&line, "source", slog.StringValue(filepath.Base(frame.File)+":"+strconv.Itoa(frame.Line)))
		}
	}
	line.WriteByte('\n')

	h.output.mu.Lock()
	defer h.output.mu.Unlock()
	_, err := io.WriteString(h.output.w, line.String())
	return err
}

func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := h.clone()
	for _, attr := range attrs {
		flattenAttr(&clone.attrs, clone.groups, attr)
	}
	return clone
}

func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := h.clone()
	clone.groups = append(clone.groups, name)
	return clone
}

func (h *ConsoleHandler) clone() *ConsoleHandler {
	return &ConsoleHandler{
		output: h.output,
		level:  h.level,
		attrs:  append([]flatAttr(nil), h.attrs...),
		groups: append([]string(nil), h.groups...),
	}
}

type flatAttr struct {
	key   string
	value slog.Value
}

func flattenAttr(target *[]flatAttr, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		nestedGroups := groups
		if attr.Key != "" {
			nestedGroups = append(append([]string(nil), groups...), attr.Key)
		}
		for _, nested := range attr.Value.Group() {
			flattenAttr(target, nestedGroups, nested)
		}
		return
	}
	keyParts := append([]string(nil), groups...)
	if attr.Key != "" {
		keyParts = append(keyParts, attr.Key)
	}
	if len(keyParts) == 0 {
		return
	}
	*target = append(*target, flatAttr{key: strings.Join(keyParts, "_"), value: attr.Value})
}

func takeAttribute(attrs []flatAttr, key string, fallback slog.Value) (slog.Value, []flatAttr) {
	for index, attr := range attrs {
		if attr.key == key {
			remaining := append([]flatAttr(nil), attrs[:index]...)
			remaining = append(remaining, attrs[index+1:]...)
			return attr.value, remaining
		}
	}
	return fallback, attrs
}

func writeField(line *strings.Builder, key string, value slog.Value) {
	line.WriteByte(' ')
	line.WriteString(key)
	line.WriteByte('=')
	line.WriteString(formatValue(value.Resolve()))
}

func writeTokenField(line *strings.Builder, key string, value slog.Value) {
	value = value.Resolve()
	if value.Kind() != slog.KindString || !isSafeToken(value.String()) {
		writeField(line, key, value)
		return
	}
	line.WriteByte(' ')
	line.WriteString(key)
	line.WriteByte('=')
	line.WriteString(value.String())
}

func isSafeToken(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case strings.ContainsRune("._:/+-", char):
		default:
			return false
		}
	}
	return true
}

func formatValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		return strconv.Quote(value.String())
	case slog.KindBool:
		return strconv.FormatBool(value.Bool())
	case slog.KindInt64:
		return strconv.FormatInt(value.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(value.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(value.Float64(), 'g', -1, 64)
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return strconv.Quote(value.Time().Format(time.RFC3339Nano))
	case slog.KindAny:
		item := value.Any()
		if err, ok := item.(error); ok {
			return strconv.Quote(err.Error())
		}
		encoded, marshalErr := json.Marshal(item)
		if marshalErr == nil {
			return string(encoded)
		}
		return strconv.Quote(fmt.Sprint(item))
	default:
		return strconv.Quote(value.String())
	}
}

type loggerContextKey struct{}

func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok && logger != nil {
			return logger
		}
	}
	return slog.Default()
}

// Complete records the final result of a business operation with consistent
// duration and result fields. The caller program counter is preserved so error
// records point to the operation boundary rather than this helper.
func Complete(ctx context.Context, component, operation, message string, started time.Time, err error, attrs ...any) {
	logger := FromContext(ctx).With(attrs...)
	level := slog.LevelInfo
	result := "success"
	if err != nil {
		message = component + " " + strings.ReplaceAll(operation, "_", " ") + " failed"
		result = "failure"
		switch {
		case errors.Is(err, context.Canceled), connect.CodeOf(err) == connect.CodeCanceled:
			level = slog.LevelInfo
			result = "cancelled"
			message = component + " " + strings.ReplaceAll(operation, "_", " ") + " cancelled"
		case errors.Is(err, context.DeadlineExceeded):
			level = slog.LevelWarn
			message = component + " " + strings.ReplaceAll(operation, "_", " ") + " rejected"
		case isExpectedConnectCode(connect.CodeOf(err)):
			level = slog.LevelWarn
			message = component + " " + strings.ReplaceAll(operation, "_", " ") + " rejected"
		default:
			level = slog.LevelError
		}
	}
	if !logger.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])
	record := slog.NewRecord(time.Now(), level, message, pcs[0])
	record.AddAttrs(
		slog.String("component", component),
		slog.String("operation", operation),
		slog.Duration("duration", time.Since(started)),
		slog.String("result", result),
	)
	if err != nil {
		record.AddAttrs(slog.Any("error", err))
	}
	_ = logger.Handler().Handle(ctx, record)
}

func isExpectedConnectCode(code connect.Code) bool {
	switch code {
	case connect.CodeInvalidArgument, connect.CodeNotFound, connect.CodeAlreadyExists,
		connect.CodePermissionDenied, connect.CodeUnauthenticated, connect.CodeResourceExhausted,
		connect.CodeFailedPrecondition, connect.CodeAborted, connect.CodeOutOfRange,
		connect.CodeDeadlineExceeded:
		return true
	default:
		return false
	}
}

// Partial records a completed operation whose individual results include one
// or more recoverable failures.
func Partial(ctx context.Context, component, operation, message string, started time.Time, attrs ...any) {
	logger := FromContext(ctx).With(attrs...)
	logger.WarnContext(ctx, message,
		"component", component,
		"operation", operation,
		"duration", time.Since(started),
		"result", "failure",
	)
}
