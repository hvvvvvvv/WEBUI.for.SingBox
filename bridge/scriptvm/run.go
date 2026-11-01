// Package scriptvm bounds the legacy subscription/configuration script hooks.
// It provides no host APIs. Time/size limits are not a separate memory sandbox;
// scripts still run inside the application process and must be trusted.
package scriptvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/dop251/goja"
)

const (
	Timeout        = 2 * time.Second
	MaxSourceBytes = 256 << 10
	MaxIOBytes     = 32 << 20
	maxErrorBytes  = 4096
)

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// Run evaluates source then expression with JSON-owned globals. The native
// string conversion functions are captured before user code can replace them.
// Pure async hooks may settle immediately; pending host work is unsupported.
func Run(parent context.Context, source string, globals map[string]any, expression string) ([]byte, error) {
	if len(source) > MaxSourceBytes {
		return nil, fmt.Errorf("script source exceeds %d bytes", MaxSourceBytes)
	}
	ctx, cancel := context.WithTimeout(parent, Timeout)
	defer cancel()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	vm := goja.New()
	vm.SetMaxCallStackSize(512)
	stop := context.AfterFunc(ctx, func() { vm.Interrupt(ctx.Err()) })
	defer stop()
	jsonObject := vm.Get("JSON").ToObject(vm)
	parse, _ := goja.AssertFunction(jsonObject.Get("parse"))
	stringify, _ := goja.AssertFunction(jsonObject.Get("stringify"))
	toString, _ := goja.AssertFunction(vm.Get("String"))
	boundedError := func(message string) error {
		if len(message) > maxErrorBytes {
			message = message[:maxErrorBytes] + "..."
		}
		return errors.New(message)
	}
	describeError := func(value goja.Value, fallback string) error {
		if cause := contextError(ctx); cause != nil {
			return cause
		}
		text, err := toString(goja.Undefined(), value)
		if cause := contextError(ctx); cause != nil {
			return cause
		}
		if err == nil {
			return boundedError(text.String())
		}
		return boundedError(fallback)
	}
	failure := func(err error) error {
		if cause := contextError(ctx); cause != nil {
			return cause
		}
		var exception *goja.Exception
		if errors.As(err, &exception) {
			return describeError(exception.Value(), "JavaScript execution failed")
		}
		return boundedError(err.Error())
	}
	inputBytes := 0
	for name, input := range globals {
		payload, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("script input: %w", err)
		}
		inputBytes += len(payload)
		if inputBytes > MaxIOBytes {
			return nil, fmt.Errorf("script input exceeds %d bytes", MaxIOBytes)
		}
		value, err := parse(goja.Undefined(), vm.ToValue(string(payload)))
		if err != nil {
			return nil, failure(err)
		}
		if err = vm.Set(name, value); err != nil {
			return nil, failure(err)
		}
	}
	value, err := vm.RunString(source + "\n; " + expression)
	if err != nil {
		return nil, failure(err)
	}
	if value != nil && value.ExportType() == reflect.TypeOf((*goja.Promise)(nil)) {
		promise := value.Export().(*goja.Promise)
		switch promise.State() {
		case goja.PromiseStateFulfilled:
			value = promise.Result()
		case goja.PromiseStateRejected:
			return nil, describeError(promise.Result(), "script returned a rejected promise")
		default:
			return nil, errors.New("script returned a pending promise; asynchronous host operations are unavailable")
		}
	}
	serialized, err := stringify(goja.Undefined(), value)
	if err != nil {
		return nil, failure(err)
	}
	if goja.IsUndefined(serialized) {
		return nil, errors.New("script result is not JSON data")
	}
	output := serialized.String()
	if len(output) > MaxIOBytes {
		return nil, fmt.Errorf("script output exceeds %d bytes", MaxIOBytes)
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return []byte(output), nil
}
