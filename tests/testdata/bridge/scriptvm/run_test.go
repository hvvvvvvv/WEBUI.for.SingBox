package scriptvm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunInterruptsAllUserCodeBoundaries(t *testing.T) {
	for name, source := range map[string]string{
		"top-level":         `while (true) {}`,
		"hook":              `function hook(){while(true){}}`,
		"getter":            `function hook(){return {get value(){while(true){}}}}`,
		"toJSON":            `function hook(){return {toJSON(){while(true){}}}}`,
		"error-toString":    `function hook(){throw {toString(){while(true){}}}}`,
		"rejected-toString": `async function hook(){throw {toString(){while(true){}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			_, err := Run(ctx, source, nil, "hook()")
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected bounded cancellation, got %v", err)
			}
		})
	}
}

func TestRunDoesNotExposeCallerMapsOrModifiedJSONFunctions(t *testing.T) {
	input := map[string]any{"tag": "before"}
	result, err := Run(context.Background(), `async function hook(){input.tag='after'; JSON.stringify=()=>'{"tag":"fake"}'; return input}`, map[string]any{"input": input}, "hook()")
	if err != nil || string(result) != `{"tag":"after"}` || input["tag"] != "before" {
		t.Fatalf("result=%s input=%v err=%v", result, input, err)
	}
}

func TestRunRejectsUnsupportedOrExcessiveResults(t *testing.T) {
	for _, source := range []string{
		`function hook(){return new Promise(()=>{})}`,
		`function hook(){return Promise.reject('failed')}`,
		`function hook(){return hook()}`,
		`function hook(){const cyclic={};cyclic.self=cyclic;return cyclic}`,
	} {
		if _, err := Run(context.Background(), source, nil, "hook()"); err == nil {
			t.Fatalf("invalid result was accepted: %s", source)
		}
	}
	if _, err := Run(context.Background(), strings.Repeat(" ", MaxSourceBytes+1), nil, "null"); err == nil {
		t.Fatal("oversized source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, "", nil, "null"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context ignored: %v", err)
	}
}
