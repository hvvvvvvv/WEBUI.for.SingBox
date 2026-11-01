package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"
)

// An isolated test process proves the historical infinite loop cannot retain
// the runtime subscription lock forever, without hanging the whole test run.
func TestSubscriptionScriptExecutionIsBounded(t *testing.T) {
	if os.Getenv("WEBUI_TEST_SUBSCRIPTION_LOOP") == "1" {
		_, _, err := runSubscribeScript(`function onSubscribe() { while (true) {} }`, nil, subscription{})
		if err == nil {
			t.Fatal("infinite subscription script succeeded")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSubscriptionScriptExecutionIsBounded$")
	command.Env = append(os.Environ(), "WEBUI_TEST_SUBSCRIPTION_LOOP=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("subscription script never returned; it would block every subscription operation")
	}
	if err != nil {
		t.Fatalf("script subprocess failed: %v\n%s", err, output)
	}
}

func TestSubscriptionScriptKeepsLegacyFieldsAndSettledAsync(t *testing.T) {
	input := []map[string]any{{"type": "direct", "tag": "before"}}
	sub := subscription{ID: "selected", Name: "Before", Header: subscriptionHeader{Request: map[string]string{"X-Test": "before"}}}
	proxies, updated, err := runSubscribeScript(`async function onSubscribe(proxies, subscription) {
		proxies[0].tag = 'after'; subscription.Name = 'After';
		subscription.Header.Request['X-Test'] = 'after';
		return { proxies, subscription };
	}`, input, sub)
	if err != nil || len(proxies) != 1 || proxies[0]["tag"] != "after" || updated.Name != "After" || updated.Header.Request["X-Test"] != "after" {
		t.Fatalf("legacy script contract changed: proxies=%v subscription=%+v err=%v", proxies, updated, err)
	}
	if input[0]["tag"] != "before" || sub.Header.Request["X-Test"] != "before" {
		t.Fatal("script mutated caller-owned input")
	}
}

func TestSubscriptionScriptRejectsPendingPromise(t *testing.T) {
	_, _, err := runSubscribeScript(`function onSubscribe() { return new Promise(() => {}); }`, nil, subscription{})
	if err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("pending async work was accepted: %v", err)
	}
}

func TestSubscriptionScriptPreservesNilFallbackAndExplicitEmptyResult(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"undefined", `proxies[0].tag = 'after'`, 1},
		{"null result", `proxies[0].tag = 'after'; return null`, 1},
		{"null proxies", `proxies[0].tag = 'after'; return {proxies:null, subscription}`, 1},
		{"missing proxies", `proxies[0].tag = 'after'; return {subscription}`, 1},
		{"explicit empty", `return {proxies:[], subscription}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []map[string]any{{"type": "direct", "tag": "before"}}
			output, sub, err := runSubscribeScript(`function onSubscribe(proxies, subscription) {`+tc.body+`}`, input, subscription{ID: "selected"})
			if err != nil || output == nil || len(output) != tc.count || sub.ID != "selected" {
				t.Fatalf("fallback/empty contract changed: %v %+v %v", output, sub, err)
			}
			if tc.count > 0 && output[0]["tag"] != "after" {
				t.Fatalf("fallback lost mutation: %v", output)
			}
			if input[0]["tag"] != "before" {
				t.Fatal("fallback changed caller-owned nodes")
			}
		})
	}
	proxies, _, err := runSubscribeScript("", nil, subscription{})
	if err != nil || proxies != nil {
		t.Fatalf("empty script changed nil input: %v %v", proxies, err)
	}
}

func TestSubscriptionScriptRejectsMalformedNodes(t *testing.T) {
	for _, value := range []string{`null`, `{"tag":"missing-type"}`, `{"type":"direct","tag":" "}`, `{"type":"direct","tag":123}`} {
		_, _, err := runSubscribeScript(`function onSubscribe() { return {proxies:[`+value+`]}; }`, nil, subscription{ID: "selected"})
		if err == nil {
			t.Errorf("malformed node would replace the subscription cache: %s", value)
		}
	}
}

func TestManualSubscriptionScriptKeepsCacheAndReferencesTogether(t *testing.T) {
	withTempBasePath(t)
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	item := subscription{ID: "manual-script", Type: "Manual", Proxies: []proxyRef{{ID: "before-id", Type: "direct", Tag: "before"}},
		Script: `function onSubscribe(proxies, subscription) { proxies[0].tag = 'after'; return {proxies, subscription}; }`}
	if err := saveSubscriptions([]subscription{item}); err != nil {
		t.Fatal(err)
	}
	path := GetPath(subscriptionContentPath(item.ID))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := storage.AtomicWriteFile(path, []byte(`[{"type":"direct","tag":"before"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	response, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: item.ID}))
	if err != nil || !response.Msg.GetResults()[0].GetOk() {
		t.Fatalf("manual script refresh failed: %v %v", response, err)
	}
	saved, err := loadSubscriptions()
	if err != nil || len(saved[0].Proxies) != 1 || saved[0].Proxies[0].Tag != "after" {
		t.Fatalf("script did not update references: %+v %v", saved, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `"after"`) || strings.Contains(string(data), `"before"`) {
		t.Fatalf("manual cache disagrees with persisted references: %s %v", data, err)
	}
}
