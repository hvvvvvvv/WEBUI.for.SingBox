package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func proxiesForTest() []map[string]any {
	return []map[string]any{{"type": "direct", "tag": "original", "extra": map[string]any{"keep": true}}}
}

func TestSubscriptionPipelineOrderConfigurationIsolationAndSourceUpdate(t *testing.T) {
	service := serviceForTest(t)
	first := pluginForTest("first", `
function onSubscribe(proxies, subscription) {
  proxies[0].tag += configuration.suffix;
  subscription.name = "changed";
  configuration.suffix = "must not persist";
  proxies[0].extra.added = true;
}`)
	first.Configuration = map[string]any{"suffix": "-one"}
	saveForTest(t, service, first)
	saveForTest(t, service, pluginForTest("second", `const onSubscribe = (p) => p.map(x => ({...x, tag: x.tag + '-two'}));`))
	disabled := pluginForTest("disabled", `throw Error("disabled source evaluated")`)
	disabled.Disabled = true
	saveForTest(t, service, disabled)
	saveForTest(t, service, pluginForTest("manual-only", `throw Error("wrong trigger evaluated")`, OnManual))
	proxies, subscription := proxiesForTest(), map[string]any{"name": "subscription", "nested": map[string]any{"value": 1}}
	before, metadata, stored := jsonForTest(t, proxies), jsonForTest(t, subscription), jsonForTest(t, service.List())
	result, err := service.ApplySubscription(context.Background(), proxies, subscription)
	if err != nil || result[0]["tag"] != "original-one-two" {
		t.Fatalf("pipeline failed: %#v %v", result, err)
	}
	if jsonForTest(t, proxies) != before || jsonForTest(t, subscription) != metadata || jsonForTest(t, service.List()) != stored {
		t.Fatal("execution mutated caller or persistent data")
	}
	if _, err := service.Reorder([]string{"second", "first", "disabled", "manual-only"}, service.List().Revision); err != nil {
		t.Fatal(err)
	}
	result, err = service.ApplySubscription(context.Background(), proxies, subscription)
	if err != nil || result[0]["tag"] != "original-two-one" {
		t.Fatalf("reorder not applied: %#v %v", result, err)
	}
	first.Source = `function onSubscribe(p) { p[0].tag += '-updated'; return p }`
	saveForTest(t, service, first)
	result, err = service.ApplySubscription(context.Background(), proxies, subscription)
	if err != nil || result[0]["tag"] != "original-two-updated" {
		t.Fatalf("updated script did not replace compiled source: %#v %v", result, err)
	}
}

func TestManualTriggerLogsAndPureJSHostBoundary(t *testing.T) {
	service := serviceForTest(t)
	plugin := pluginForTest("manual", `
function onRun(proxies, subscription) {
  if ([typeof fetch, typeof require, typeof process, typeof Wails, typeof setTimeout,
       typeof Plugins.ReadFile, typeof Plugins.Exec, typeof Plugins.HttpGet].some(x => x !== 'undefined')) {
    throw Error('unexpected host access');
  }
  console.log('subscription', subscription.name);
  Plugins.Log('nodes', proxies.length);
  proxies[0].tag = 'manual';
}`, OnManual)
	saveForTest(t, service, plugin)
	input := proxiesForTest()
	result, logs, err := service.RunSubscription(context.Background(), "manual", input, map[string]any{"name": "sample"})
	if err != nil || result[0]["tag"] != "manual" || len(logs) != 2 || logs[0] != "subscription sample" || logs[1] != "nodes 1" {
		t.Fatalf("manual execution failed: %#v %#v %v", result, logs, err)
	}
	if input[0]["tag"] != "original" {
		t.Fatal("manual run mutated input")
	}
	if _, _, err := service.RunSubscription(context.Background(), "unknown", input, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing manual plugin accepted: %v", err)
	}
	plugin.Disabled = true
	saveForTest(t, service, plugin)
	if _, _, err := service.RunSubscription(context.Background(), "manual", input, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("disabled manual plugin accepted: %v", err)
	}
	plugin.Disabled = false
	plugin.Triggers = []string{OnSubscribe}
	saveForTest(t, service, plugin)
	if _, _, err := service.RunSubscription(context.Background(), "manual", input, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-manual plugin accepted: %v", err)
	}
}

func TestConfigHookAndFailedLaterPluginLeaveInputUntouched(t *testing.T) {
	service := serviceForTest(t)
	saveForTest(t, service, pluginForTest("config", `function onGenerate(c) { c.route.final = 'changed'; }`, OnGenerate))
	input := map[string]any{"route": map[string]any{"final": "direct"}, "outbounds": []any{map[string]any{"type": "direct"}}}
	before := jsonForTest(t, input)
	output, err := service.ApplyConfig(context.Background(), input)
	if err != nil || output["route"].(map[string]any)["final"] != "changed" || jsonForTest(t, input) != before {
		t.Fatalf("config hook failed: %#v %v", output, err)
	}
	saveForTest(t, service, pluginForTest("fail", `function onGenerate(c) { c.route.final = 'partial'; throw Error('later failure') }`, OnGenerate))
	stored := jsonForTest(t, service.List())
	output, err = service.ApplyConfig(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "later failure") || output != nil || jsonForTest(t, input) != before || jsonForTest(t, service.List()) != stored {
		t.Fatalf("failed pipeline leaked partial changes: %#v %v", output, err)
	}
}

func TestHookResultsValidatedAndSerializationInterruptible(t *testing.T) {
	for name, source := range map[string]string{
		"missing hook": `const value = 1`,
		"null":         `function onSubscribe(p) { return null }`,
		"wrong shape":  `function onSubscribe(p) { return {proxies:p} }`,
		"missing type": `function onSubscribe(p) { return [{tag:'test'}] }`,
		"missing tag":  `function onSubscribe(p) { return [{type:'direct'}] }`,
		"null element": `function onSubscribe(p) { return [null] }`,
		"cycle":        `function onSubscribe(p) { p[0].self = p; return p }`,
		"big output":   fmt.Sprintf(`function onSubscribe(p) { p[0].large = 'x'.repeat(%d); return p }`, MaxIOBytes),
		"proxy count":  fmt.Sprintf(`function onSubscribe(p) { return Array(%d).fill(p[0]) }`, MaxProxies+1),
		"pending":      `function onSubscribe(p) { return new Promise(() => {}) }`,
		"rejected":     `async function onSubscribe(p) { throw Error('failure') }`,
		"stack":        `function onSubscribe(p) { function recurse() { return recurse() } return recurse() }`,
	} {
		t.Run(name, func(t *testing.T) {
			service := serviceForTest(t)
			saveForTest(t, service, pluginForTest("invalid", source))
			input := proxiesForTest()
			before := jsonForTest(t, input)
			if output, err := service.ApplySubscription(context.Background(), input, nil); err == nil || output != nil {
				t.Fatalf("invalid output accepted: %#v %v", output, err)
			}
			if jsonForTest(t, input) != before {
				t.Fatal("failed script mutated input")
			}
		})
	}
	for name, source := range map[string]string{
		"top level":       `while(true) {}`,
		"hook":            `function onSubscribe(p) { while(true) {} }`,
		"getter":          `Object.defineProperty(globalThis, 'onSubscribe', {get() {while(true) {}}})`,
		"output toJSON":   `function onSubscribe(p) { p.toJSON = function() { while(true) {} }; return p }`,
		"error toString":  `function onSubscribe(p) { throw {toString() { while(true) {} }} }`,
		"logger toString": `function onSubscribe(p) { console.log({toString() { while(true) {} }}) }`,
	} {
		t.Run("interrupt "+name, func(t *testing.T) {
			service := serviceForTest(t)
			saveForTest(t, service, pluginForTest("loop", source))
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			started := time.Now()
			if _, err := service.ApplySubscription(ctx, proxiesForTest(), nil); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline, got %v", err)
			}
			if time.Since(started) > time.Second {
				t.Fatal("script did not stop promptly")
			}
		})
	}
}

func TestPureAsyncHooksAndNoPluginCompatibility(t *testing.T) {
	service := serviceForTest(t)
	var nilProxies []map[string]any
	if output, err := service.ApplySubscription(context.Background(), nilProxies, nil); err != nil || output != nil {
		t.Fatalf("no-op changed nil input: %#v %v", output, err)
	}
	saveForTest(t, service, pluginForTest("async", `async function onSubscribe(p) { await Promise.resolve(); p[0].tag='async'; return p }`))
	output, err := service.ApplySubscription(context.Background(), proxiesForTest(), nil)
	if err != nil || output[0]["tag"] != "async" {
		t.Fatalf("settled promise failed: %#v %v", output, err)
	}
}

func TestLogLimitsAndInputLimits(t *testing.T) {
	for name, statement := range map[string]string{
		"line":  fmt.Sprintf(`console.log('x'.repeat(%d));`, MaxLogLineBytes+1),
		"count": fmt.Sprintf(`for(let i=0;i<%d;i++) console.log('entry');`, MaxLogLines+1),
		"bytes": `for(let i=0;i<20;i++) console.log('x'.repeat(3000));`,
	} {
		t.Run(name, func(t *testing.T) {
			service := serviceForTest(t)
			saveForTest(t, service, pluginForTest("log", "function onRun(p) { "+statement+" return p; }", OnManual))
			_, logs, err := service.RunSubscription(context.Background(), "log", proxiesForTest(), nil)
			if err == nil || !strings.Contains(err.Error(), "log") {
				t.Fatalf("log limit not enforced: %v", err)
			}
			if len(logs) > MaxLogLines || len(strings.Join(logs, "")) > MaxLogBytes {
				t.Fatal("returned logs exceeded limits")
			}
		})
	}
	service := serviceForTest(t)
	saveForTest(t, service, pluginForTest("pass", "function onSubscribe(p) { return p }"))
	input := proxiesForTest()
	input[0]["oversized"] = strings.Repeat("x", MaxIOBytes)
	if _, err := service.ApplySubscription(context.Background(), input, nil); err == nil || !strings.Contains(err.Error(), "input exceeds") {
		t.Fatalf("input limit not enforced: %v", err)
	}
}

func TestPipelineDeadlineIncludesWaitingAndMultipleHooks(t *testing.T) {
	service := serviceForTest(t)
	saveForTest(t, service, pluginForTest("one", "function onSubscribe(p) { const until=Date.now()+80; while(Date.now()<until) {} return p }"))
	saveForTest(t, service, pluginForTest("two", "function onSubscribe(p) { const until=Date.now()+80; while(Date.now()<until) {} return p }"))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	if _, err := service.ApplySubscription(ctx, proxiesForTest(), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pipeline did not share the deadline: %v", err)
	}
	service.runSlot <- struct{}{}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := service.ApplySubscription(ctx, proxiesForTest(), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("execution queue ignored context cancellation: %v", err)
	}
	<-service.runSlot
	if _, err := service.ApplySubscription(context.Background(), proxiesForTest(), nil); err != nil {
		t.Fatalf("canceled run prevented retry: %v", err)
	}
}

func TestRunningPipelineUsesOneInstallationSnapshot(t *testing.T) {
	service := serviceForTest(t)
	saveForTest(t, service, pluginForTest("wait", `function onSubscribe(p) { const until=Date.now()+200; while(Date.now()<until) {} return p }`))
	saveForTest(t, service, pluginForTest("version", `function onSubscribe(p) { p[0].tag += '-old'; return p }`))
	type response struct {
		proxies []map[string]any
		err     error
	}
	finished := make(chan response, 1)
	go func() {
		result, err := service.ApplySubscription(context.Background(), proxiesForTest(), nil)
		finished <- response{proxies: result, err: err}
	}()
	deadline := time.Now().Add(time.Second)
	for len(service.runSlot) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("pipeline did not acquire execution slot")
		}
		time.Sleep(time.Millisecond)
	}
	saveForTest(t, service, pluginForTest("version", `function onSubscribe(p) { p[0].tag += '-new'; return p }`))
	for index := 0; index < 10; index++ {
		copy := service.List()
		copy.Plugins[0].Source = "corrupted caller copy"
		copy.Plugins[0].Triggers[0] = OnManual
	}
	current := <-finished
	if current.err != nil || current.proxies[0]["tag"] != "original-old" {
		t.Fatalf("running pipeline observed mid-run installation edits: %#v %v", current.proxies, current.err)
	}
	next, err := service.ApplySubscription(context.Background(), proxiesForTest(), nil)
	if err != nil || next[0]["tag"] != "original-new" {
		t.Fatalf("next pipeline did not pick up committed source: %#v %v", next, err)
	}
}
