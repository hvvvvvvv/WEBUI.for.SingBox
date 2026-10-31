package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const (
	HookTimeout     = 2 * time.Second
	PipelineTimeout = 5 * time.Second
	MaxCallStack    = 512
	MaxLogLines     = 100
	MaxLogBytes     = 32 << 10
	MaxLogLineBytes = 4 << 10
)

type execution struct {
	plugin  Plugin
	program *goja.Program
}

func (s *Service) ApplySubscription(ctx context.Context, proxies []map[string]any, subscription map[string]any) ([]map[string]any, error) {
	result, _, err := s.run(ctx, OnSubscribe, "", proxies, subscription)
	if err != nil {
		return nil, err
	}
	return result.([]map[string]any), nil
}

func (s *Service) RunSubscription(ctx context.Context, id string, proxies []map[string]any, subscription map[string]any) ([]map[string]any, []string, error) {
	result, logs, err := s.run(ctx, OnManual, id, proxies, subscription)
	if err != nil {
		return nil, logs, err
	}
	return result.([]map[string]any), logs, nil
}

func (s *Service) ApplyConfig(ctx context.Context, config map[string]any) (map[string]any, error) {
	result, _, err := s.run(ctx, OnGenerate, "", config, nil)
	if err != nil {
		return nil, err
	}
	return result.(map[string]any), nil
}

func (s *Service) executions(trigger, id string) ([]execution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []execution
	for _, plugin := range s.state.Plugins {
		if trigger == OnManual && plugin.ID != id {
			continue
		}
		selected := false
		for _, candidate := range plugin.Triggers {
			selected = selected || candidate == trigger
		}
		if trigger == OnManual && (plugin.Disabled || !selected) {
			return nil, fmt.Errorf("%w: plugin %q must be enabled and have the manual trigger", ErrInvalid, id)
		}
		if plugin.Disabled || !selected {
			continue
		}
		copy := cloneSnapshot(Snapshot{Plugins: []Plugin{plugin}}).Plugins[0]
		result = append(result, execution{plugin: copy, program: s.programs[plugin.ID]})
	}
	if trigger == OnManual && len(result) == 0 {
		return nil, ErrNotFound
	}
	for i := range result {
		for j := 0; j < i; j++ {
			if err := checkConflict(result[i].plugin, result[j].plugin); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func (s *Service) run(ctx context.Context, trigger, id string, input any, subscription map[string]any) (any, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, PipelineTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	selected, err := s.executions(trigger, id)
	if err != nil {
		return nil, nil, err
	}
	if len(selected) == 0 {
		return input, []string{}, nil
	}
	if trigger == OnManual && selected[0].plugin.ManualTarget == ManualDiagnostic {
		return nil, nil, fmt.Errorf("%w: diagnostic plugins must run without a subscription", ErrInvalid)
	}
	select {
	case s.runSlot <- struct{}{}:
		defer func() { <-s.runSlot }()
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	if trigger != OnGenerate && input.([]map[string]any) == nil {
		input = []map[string]any{}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, nil, fmt.Errorf("plugin input: %w", err)
	}
	metadata, err := json.Marshal(subscription)
	if err != nil {
		return nil, nil, fmt.Errorf("subscription input: %w", err)
	}
	if len(payload)+len(metadata) > MaxIOBytes {
		return nil, nil, fmt.Errorf("plugin input exceeds %d bytes", MaxIOBytes)
	}
	if _, err := decodeResult(payload, trigger); err != nil {
		return nil, nil, fmt.Errorf("plugin input: %w", err)
	}
	logs := &logBuffer{lines: []string{}}
	for _, current := range selected {
		if err := ctx.Err(); err != nil {
			return nil, logs.lines, err
		}
		payload, err = runHook(ctx, current, trigger, payload, metadata, logs)
		if err != nil {
			return nil, logs.lines, fmt.Errorf("plugin %q: %w", current.plugin.ID, err)
		}
		if _, err := decodeResult(payload, trigger); err != nil {
			return nil, logs.lines, fmt.Errorf("plugin %q output: %w", current.plugin.ID, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, logs.lines, err
	}
	result, err := decodeResult(payload, trigger)
	return result, logs.lines, err
}

// Every invocation gets a fresh VM and JSON-owned objects. No Go maps, service
// instances, filesystem, shell or desktop bridge are exposed to JS. Network
// access is installed separately only for an explicit diagnostic invocation.
func runHook(parent context.Context, current execution, trigger string, payload, metadata []byte, logs *logBuffer) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, HookTimeout)
	defer cancel()
	return executeHook(ctx, current, trigger, payload, metadata, logs, nil)
}

func executeHook(ctx context.Context, current execution, trigger string, payload, metadata []byte, logs *logBuffer, diagnostic *diagnosticRun) ([]byte, error) {
	vm := goja.New()
	vm.SetMaxCallStackSize(MaxCallStack)
	stop := context.AfterFunc(ctx, func() { vm.Interrupt(ctx.Err()) })
	defer stop()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jsonObject := vm.Get("JSON").ToObject(vm)
	parse, _ := goja.AssertFunction(jsonObject.Get("parse"))
	stringify, _ := goja.AssertFunction(jsonObject.Get("stringify"))
	toString, _ := goja.AssertFunction(vm.Get("String"))
	failure := func(err error) error { return executionError(ctx, toString, err) }
	argument, err := parse(goja.Undefined(), vm.ToValue(string(payload)))
	if err != nil {
		return nil, err
	}
	subscription, err := parse(goja.Undefined(), vm.ToValue(string(metadata)))
	if err != nil {
		return nil, err
	}
	configuration := map[string]any{}
	for _, setting := range current.plugin.ConfigurationSchema {
		configuration[setting.Key] = setting.Value
	}
	for key, value := range current.plugin.Configuration {
		configuration[key] = value
	}
	configurationJSON, _ := json.Marshal(configuration)
	settings, err := parse(goja.Undefined(), vm.ToValue(string(configurationJSON)))
	if err != nil {
		return nil, err
	}
	if err := vm.Set("configuration", settings); err != nil {
		return nil, err
	}
	logger := func(call goja.FunctionCall) goja.Value {
		logs.append(vm, call.Arguments)
		return goja.Undefined()
	}
	console := vm.NewObject()
	for _, name := range []string{"log", "info", "warn", "error", "debug"} {
		if err := console.Set(name, logger); err != nil {
			return nil, err
		}
	}
	api := vm.NewObject()
	if err := api.Set("Log", logger); err != nil {
		return nil, err
	}
	if diagnostic != nil {
		if err := diagnostic.install(vm, api); err != nil {
			return nil, err
		}
	}
	if err := vm.Set("console", console); err != nil {
		return nil, err
	}
	if err := vm.Set("Plugins", api); err != nil {
		return nil, err
	}
	if _, err := vm.RunProgram(current.program); err != nil {
		return nil, failure(err)
	}
	hook := "onSubscribe"
	if trigger == OnGenerate {
		hook = "onGenerate"
	} else if trigger == OnManual {
		hook = "onRun"
	}
	// Accessor-backed globals may themselves execute user code. Resolve inside
	// RunString so exceptions and interrupts become errors rather than Go panics.
	value, err := vm.RunString(hook)
	if err != nil {
		return nil, failure(err)
	}
	function, ok := goja.AssertFunction(value)
	if !ok {
		return nil, fmt.Errorf("declared trigger requires function %s", hook)
	}
	args := []goja.Value{argument}
	if trigger != OnGenerate {
		args = append(args, subscription)
	}
	if diagnostic != nil {
		args = nil
	}
	result, err := function(goja.Undefined(), args...)
	if err != nil {
		return nil, failure(err)
	}
	// Pure async functions can settle their promises without an event loop.
	// Pending external work has no supported host API and cannot be awaited.
	if result != nil && result.ExportType() == reflect.TypeOf((*goja.Promise)(nil)) {
		promise := result.Export().(*goja.Promise)
		switch promise.State() {
		case goja.PromiseStateFulfilled:
			result = promise.Result()
		case goja.PromiseStateRejected:
			return nil, fmt.Errorf("%s returned a rejected promise", hook)
		default:
			return nil, fmt.Errorf("%s returned a pending promise; asynchronous host operations are unavailable", hook)
		}
	}
	if result == nil || goja.IsUndefined(result) {
		result = argument
	}
	if goja.IsNull(result) && diagnostic == nil {
		return nil, fmt.Errorf("%s must return transformed data or mutate its argument", hook)
	}
	serialized, err := stringify(goja.Undefined(), result)
	if err != nil {
		return nil, failure(err)
	}
	if goja.IsUndefined(serialized) {
		return nil, fmt.Errorf("%s result is not JSON data", hook)
	}
	output := serialized.String()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := MaxIOBytes
	if diagnostic != nil {
		limit = MaxDiagnosticReportBytes
	}
	if len(output) > limit {
		return nil, fmt.Errorf("output exceeds %d bytes", limit)
	}
	return []byte(output), nil
}

func executionError(ctx context.Context, toString goja.Callable, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	message := "JavaScript execution failed"
	if exception, ok := err.(*goja.Exception); ok {
		// Exception.Error() may invoke a user-defined toString. Keep that work
		// within the interruptible VM, and return only inert bounded text to Go.
		value, conversionErr := toString(goja.Undefined(), exception.Value())
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if conversionErr == nil {
			message = value.String()
		}
	} else {
		message = err.Error()
	}
	if len(message) > MaxLogLineBytes {
		message = message[:MaxLogLineBytes] + "..."
	}
	return fmt.Errorf("%s", message)
}

type logBuffer struct {
	lines []string
	bytes int
}

func (logs *logBuffer) append(vm *goja.Runtime, arguments []goja.Value) {
	if len(logs.lines) >= MaxLogLines || len(arguments) > 100 {
		panic(vm.NewTypeError("plugin log limit exceeded"))
	}
	var line strings.Builder
	for index, argument := range arguments {
		text := argument.String()
		if line.Len()+len(text)+1 > MaxLogLineBytes {
			panic(vm.NewTypeError("plugin log line limit exceeded"))
		}
		if index > 0 {
			line.WriteByte(' ')
		}
		line.WriteString(text)
	}
	if logs.bytes+line.Len() > MaxLogBytes {
		panic(vm.NewTypeError("plugin log byte limit exceeded"))
	}
	logs.lines = append(logs.lines, line.String())
	logs.bytes += line.Len()
}

func decodeResult(payload []byte, trigger string) (any, error) {
	if trigger != OnGenerate {
		var proxies []map[string]any
		if err := decodeJSON(payload, &proxies, false); err != nil || proxies == nil {
			return nil, fmt.Errorf("expected an array of proxy objects")
		}
		if len(proxies) > MaxProxies {
			return nil, fmt.Errorf("proxy count exceeds %d", MaxProxies)
		}
		for index, proxy := range proxies {
			if err := validateProxy(proxy, true); err != nil {
				return nil, fmt.Errorf("proxies[%d]: %w", index, err)
			}
		}
		return proxies, nil
	}
	var config map[string]any
	if err := decodeJSON(payload, &config, false); err != nil || config == nil {
		return nil, fmt.Errorf("expected a configuration object")
	}
	for _, field := range []string{"inbounds", "outbounds", "endpoints"} {
		value, exists := config[field]
		if !exists {
			continue
		}
		objects, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an array", field)
		}
		for index, value := range objects {
			object, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s[%d] must be an object", field, index)
			}
			if err := validateProxy(object, false); err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", field, index, err)
			}
		}
	}
	return config, nil
}

func validateProxy(proxy map[string]any, requireTag bool) error {
	kind, ok := proxy["type"].(string)
	if !ok || strings.TrimSpace(kind) == "" || len(kind) > 128 {
		return fmt.Errorf("type must be a nonempty string of at most 128 bytes")
	}
	tag, exists := proxy["tag"]
	if !exists && !requireTag {
		return nil
	}
	text, ok := tag.(string)
	if !ok || (requireTag && strings.TrimSpace(text) == "") || len(text) > 1024 {
		return fmt.Errorf("tag must be a string of at most 1024 bytes%s", map[bool]string{true: " and cannot be empty", false: ""}[requireTag])
	}
	return nil
}
