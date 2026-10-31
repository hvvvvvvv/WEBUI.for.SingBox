package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"guiforcores/bridge/plugincatalog"
	pluginservice "guiforcores/bridge/plugins"
	"guiforcores/bridge/syncstate"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

type subscriptionPluginsStub struct {
	apply func(context.Context, []map[string]any, map[string]any) ([]map[string]any, error)
	run   func(context.Context, string, []map[string]any, map[string]any) ([]map[string]any, []string, error)
}

func (s subscriptionPluginsStub) ApplySubscription(ctx context.Context, proxies []map[string]any, sub map[string]any) ([]map[string]any, error) {
	if s.apply == nil {
		return proxies, nil
	}
	return s.apply(ctx, proxies, sub)
}

func (s subscriptionPluginsStub) RunSubscription(ctx context.Context, id string, proxies []map[string]any, sub map[string]any) ([]map[string]any, []string, error) {
	return s.run(ctx, id, proxies, sub)
}

type pluginKernelRecorder struct{ calls int }

func (k *pluginKernelRecorder) ReferencedResourcesChanged(syncstate.Domain, []string) { k.calls++ }

func writePluginFixture(t *testing.T, items []subscription, id, content string) {
	t.Helper()
	if err := saveSubscriptions(items); err != nil {
		t.Fatal(err)
	}
	path := GetPath(subscriptionContentPath(id))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func readPluginFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(GetPath(path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stubSubscriptionResponse(t *testing.T, body string) {
	t.Helper()
	previous := subscriptionHTTPRequest
	subscriptionHTTPRequest = func(string, string, map[string]string, string, bool, int) (*http.Response, string, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body, nil
	}
	t.Cleanup(func() { subscriptionHTTPRequest = previous })
}

func TestAutomaticSubscriptionPluginsRunAfterExistingScript(t *testing.T) {
	withTempBasePath(t)
	stubSubscriptionResponse(t, `[{"type":"direct","tag":"downloaded"}]`)
	items := []subscription{{ID: "selected", Name: "Selected", Type: "Http", URL: "https://example.invalid", Script: `function onSubscribe(proxies, subscription) { proxies[0].tag += "-script"; return {proxies, subscription}; }`}}
	writePluginFixture(t, items, "selected", `[{"type":"direct","tag":"old"}]`)
	service := newTestRuntimeService(nil)
	calls := 0
	service.SetPlugins(subscriptionPluginsStub{apply: func(ctx context.Context, proxies []map[string]any, sub map[string]any) ([]map[string]any, error) {
		calls++
		if proxies[0]["tag"] != "downloaded-script" || sub["id"] != "selected" || sub["name"] != "Selected" {
			t.Fatalf("wrong hook order or metadata: %v %v", proxies, sub)
		}
		sub["name"] = "must not persist"
		proxies[0]["tag"] = "downloaded-script-plugin"
		return proxies, nil
	}})
	response, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "selected"}))
	if err != nil || !response.Msg.Results[0].GetOk() || calls != 1 {
		t.Fatalf("automatic plugin failed: response=%v error=%v calls=%d", response, err, calls)
	}
	cache := readPluginFixture(t, subscriptionContentPath("selected"))
	after, err := loadSubscriptions()
	if err != nil || !strings.Contains(string(cache), "downloaded-script-plugin") || after[0].Proxies[0].Tag != "downloaded-script-plugin" || after[0].Name != "Selected" {
		t.Fatalf("hook output not committed consistently: %s %+v %v", cache, after, err)
	}
}

func TestAutomaticSubscriptionPluginFailureKeepsPriorCacheAndMetadata(t *testing.T) {
	withTempBasePath(t)
	stubSubscriptionResponse(t, `[{"type":"direct","tag":"downloaded"}]`)
	writePluginFixture(t, []subscription{{ID: "selected", Name: "Selected", Type: "Http", URL: "https://example.invalid", UpdateTime: 42, Proxies: []proxyRef{{ID: "ID_old", Tag: "old", Type: "direct"}}}}, "selected", `[{"type":"direct","tag":"old"}]`)
	beforeCache := readPluginFixture(t, subscriptionContentPath("selected"))
	beforeMetadata := readPluginFixture(t, subscriptionsFilePath)
	events := &recordingRuntimeEvents{}
	kernel := &pluginKernelRecorder{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, kernel)
	service.SetPlugins(subscriptionPluginsStub{apply: func(ctx context.Context, proxies []map[string]any, sub map[string]any) ([]map[string]any, error) {
		proxies[0]["tag"] = "changed"
		sub["name"] = "changed"
		return nil, errors.New("plugin failed")
	}})
	response, err := service.UpdateAllSubscriptions(context.Background(), connect.NewRequest(&appv1.UpdateAllSubscriptionsRequest{}))
	if err != nil || len(response.Msg.Results) != 1 || response.Msg.Results[0].GetOk() || !strings.Contains(response.Msg.Results[0].GetFailureReason(), "plugin failed") {
		t.Fatalf("hook error was not reported: %v %v", response, err)
	}
	if string(beforeCache) != string(readPluginFixture(t, subscriptionContentPath("selected"))) || string(beforeMetadata) != string(readPluginFixture(t, subscriptionsFilePath)) || len(events.events) != 0 || kernel.calls != 0 {
		t.Fatal("failed hook modified persistent state or notified listeners")
	}
}

func TestAutomaticManualSubscriptionPluginCommitsInPlaceChanges(t *testing.T) {
	withTempBasePath(t)
	writePluginFixture(t, []subscription{{ID: "selected", Type: "Manual"}}, "selected", `[{"type":"direct","tag":"old"}]`)
	service := newTestRuntimeService(nil)
	service.SetPlugins(subscriptionPluginsStub{apply: func(ctx context.Context, proxies []map[string]any, sub map[string]any) ([]map[string]any, error) {
		proxies[0]["tag"] = "changed-in-place"
		return proxies, nil
	}})
	response, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "selected"}))
	if err != nil || !response.Msg.Results[0].GetOk() {
		t.Fatalf("manual source update failed: %v %v", response, err)
	}
	after, _ := loadSubscriptions()
	if after[0].Proxies[0].Tag != "changed-in-place" || !strings.Contains(string(readPluginFixture(t, subscriptionContentPath("selected"))), "changed-in-place") {
		t.Fatal("manual source metadata and cache disagree after plugin transformation")
	}
}

func TestManualPluginCanReturnAnEmptyNodeList(t *testing.T) {
	withTempBasePath(t)
	writePluginFixture(t, []subscription{{ID: "selected", Type: "Manual"}}, "selected", `[{"type":"direct","tag":"old"}]`)
	service := newTestRuntimeService(nil)
	service.SetPlugins(subscriptionPluginsStub{run: func(context.Context, string, []map[string]any, map[string]any) ([]map[string]any, []string, error) {
		return []map[string]any{}, nil, nil
	}})
	result, err := service.RunPluginOnSubscription(context.Background(), "manual-plugin", "selected")
	if err != nil || result.Before != 1 || result.After != 0 || result.Logs == nil {
		t.Fatalf("empty output rejected: %+v %v", result, err)
	}
	if string(readPluginFixture(t, subscriptionContentPath("selected"))) != "[]" {
		t.Fatal("empty output was not stored as a node array")
	}
	items, _ := loadSubscriptions()
	if len(items[0].Proxies) != 0 {
		t.Fatal("empty output kept stale node references")
	}
}

func TestManualPluginUsesOnlySelectedCacheWithoutRestart(t *testing.T) {
	withTempBasePath(t)
	items := []subscription{
		{ID: "selected", Name: "Selected", Type: "Http", URL: "https://example.invalid", Script: `throw new Error("must not rerun subscription script")`, Proxies: []proxyRef{{ID: "ID_old", Tag: "old", Type: "direct"}}},
		{ID: "other", Name: "Other", Type: "Manual", UpdateTime: 55},
	}
	writePluginFixture(t, items, "selected", `[{"type":"direct","tag":"old"}]`)
	before, _ := loadSubscriptions()
	previousRequest := subscriptionHTTPRequest
	subscriptionHTTPRequest = func(string, string, map[string]string, string, bool, int) (*http.Response, string, error) {
		t.Fatal("manual plugin downloaded a subscription")
		return nil, "", nil
	}
	t.Cleanup(func() { subscriptionHTTPRequest = previousRequest })
	events := &recordingRuntimeEvents{}
	kernel := &pluginKernelRecorder{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, kernel)
	service.SetPlugins(subscriptionPluginsStub{run: func(ctx context.Context, id string, proxies []map[string]any, sub map[string]any) ([]map[string]any, []string, error) {
		if id != "manual-plugin" || len(proxies) != 1 || proxies[0]["tag"] != "old" || sub["id"] != "selected" {
			t.Fatalf("wrong manual inputs: %s %v %v", id, proxies, sub)
		}
		sub["name"] = "must not persist"
		proxies[0]["__id_in_gui"] = "discard"
		return append(proxies, map[string]any{"type": "direct", "tag": "added"}), []string{"done"}, nil
	}})
	result, err := service.RunPluginOnSubscription(context.Background(), "manual-plugin", "selected")
	if err != nil || result.Before != 1 || result.After != 2 || result.SubscriptionID != "selected" || !reflect.DeepEqual(result.Logs, []string{"done"}) {
		t.Fatalf("unexpected result: %+v %v", result, err)
	}
	after, _ := loadSubscriptions()
	if !reflect.DeepEqual(before[1], after[1]) || after[0].Name != "Selected" || len(after[0].Proxies) != 2 || after[0].Proxies[0].ID != "ID_old" {
		t.Fatalf("wrong metadata mutation: %+v", after)
	}
	if strings.Contains(string(readPluginFixture(t, subscriptionContentPath("selected"))), "__id_in_gui") {
		t.Fatal("GUI identity leaked into persisted cache")
	}
	if kernel.calls != 0 || len(events.events) != 1 || events.events[0].name != "resourceChanged" {
		t.Fatalf("manual plugin restarted core or omitted event: kernel=%d events=%v", kernel.calls, events.events)
	}
	event := events.events[0].data[0].(map[string]any)
	if !reflect.DeepEqual(event["ids"], []string{"selected"}) || event["domain"] != "subscriptions" {
		t.Fatalf("wrong change event: %v", event)
	}
	payload, _ := json.Marshal(result)
	if !strings.Contains(string(payload), `"subscription_id":"selected"`) || !strings.Contains(string(payload), `"logs":["done"]`) {
		t.Fatalf("wrong result contract: %s", payload)
	}
}

func TestManualPluginFailureAndCancellationDoNotPersist(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "plugin error", true: "request canceled"}[canceled], func(t *testing.T) {
			withTempBasePath(t)
			writePluginFixture(t, []subscription{{ID: "selected", Name: "Selected", Type: "Manual", UpdateTime: 7}}, "selected", `[{"type":"direct","tag":"old"}]`)
			cache := readPluginFixture(t, subscriptionContentPath("selected"))
			metadata := readPluginFixture(t, subscriptionsFilePath)
			events := &recordingRuntimeEvents{}
			service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service.SetPlugins(subscriptionPluginsStub{run: func(context.Context, string, []map[string]any, map[string]any) ([]map[string]any, []string, error) {
				if canceled {
					cancel()
					return []map[string]any{{"type": "direct", "tag": "new"}}, nil, nil
				}
				return nil, []string{"failed"}, errors.New("disabled plugin")
			}})
			result, err := service.RunPluginOnSubscription(ctx, "manual-plugin", "selected")
			if err == nil || (canceled && !errors.Is(err, context.Canceled)) || (!canceled && len(result.Logs) != 1) {
				t.Fatalf("failure lost: %+v %v", result, err)
			}
			if string(cache) != string(readPluginFixture(t, subscriptionContentPath("selected"))) || string(metadata) != string(readPluginFixture(t, subscriptionsFilePath)) || len(events.events) != 0 {
				t.Fatal("failed manual plugin modified persistent state")
			}
		})
	}
}

func TestManualPluginRequiresExistingCachedSubscription(t *testing.T) {
	withTempBasePath(t)
	if err := saveSubscriptions([]subscription{{ID: "selected", Type: "Http"}}); err != nil {
		t.Fatal(err)
	}
	service := newTestRuntimeService(nil)
	service.SetPlugins(subscriptionPluginsStub{run: func(context.Context, string, []map[string]any, map[string]any) ([]map[string]any, []string, error) {
		t.Fatal("plugin ran without valid cached subscription")
		return nil, nil, nil
	}})
	for _, id := range []string{"missing", "selected"} {
		_, err := service.RunPluginOnSubscription(context.Background(), "manual-plugin", id)
		if err == nil {
			t.Fatalf("missing cache/subscription %q accepted", id)
		}
	}
	writePluginFixture(t, []subscription{{ID: "selected", Type: "Manual"}}, "selected", `{"outbounds":[]}`)
	if _, err := service.RunPluginOnSubscription(context.Background(), "manual-plugin", "selected"); err == nil {
		t.Fatal("invalid cache accepted")
	}
}

func TestManualPluginWaitsForRefreshAndUsesCommittedNodes(t *testing.T) {
	withTempBasePath(t)
	writePluginFixture(t, []subscription{{ID: "selected", Name: "Selected", Type: "Http"}}, "selected", `[{"type":"direct","tag":"old"}]`)
	previousRequest := subscriptionHTTPRequest
	entered, release := make(chan struct{}), make(chan struct{})
	subscriptionHTTPRequest = func(string, string, map[string]string, string, bool, int) (*http.Response, string, error) {
		close(entered)
		<-release
		return &http.Response{StatusCode: 200, Header: make(http.Header)}, `[{"type":"direct","tag":"refreshed"}]`, nil
	}
	t.Cleanup(func() { subscriptionHTTPRequest = previousRequest })
	service := newTestRuntimeService(nil)
	manualInput := make(chan string, 1)
	service.SetPlugins(subscriptionPluginsStub{run: func(ctx context.Context, id string, proxies []map[string]any, sub map[string]any) ([]map[string]any, []string, error) {
		manualInput <- proxies[0]["tag"].(string)
		proxies[0]["tag"] = "manual-final"
		return proxies, nil, nil
	}})
	refreshDone := make(chan error, 1)
	go func() {
		_, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "selected"}))
		refreshDone <- err
	}()
	<-entered
	manualDone := make(chan error, 1)
	go func() {
		_, err := service.RunPluginOnSubscription(context.Background(), "manual-plugin", "selected")
		manualDone <- err
	}()
	select {
	case <-manualInput:
		close(release)
		t.Fatal("manual plugin did not share refresh lock")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	if err := <-manualDone; err != nil {
		t.Fatal(err)
	}
	if got := <-manualInput; got != "refreshed" {
		t.Fatalf("manual plugin read stale nodes: %s", got)
	}
	if !strings.Contains(string(readPluginFixture(t, subscriptionContentPath("selected"))), "manual-final") {
		t.Fatal("concurrent refresh overwrote manual plugin result")
	}
}

func TestPluginMetadataCommitFailureRestoresPreviousCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	previous := []byte(`[{"type":"direct","tag":"old"}]`)
	if err := os.WriteFile(path, previous, 0600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("metadata persistence failed")
	err := commitPluginSubscription(context.Background(), path, previous, []byte("[]"), func() error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("metadata error lost: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(previous) {
		t.Fatalf("failed metadata commit changed cached nodes: %s %v", after, err)
	}
}

func TestSubscriptionHooksWithPluginService(t *testing.T) {
	withTempBasePath(t)
	stubSubscriptionResponse(t, `[{"type":"direct","tag":"node"}]`)
	writePluginFixture(t, []subscription{{ID: "selected", Name: "Selected", Type: "Http"}}, "selected", `[{"type":"direct","tag":"old"}]`)
	plugins, err := pluginservice.NewService(runtimePaths.Load())
	if err != nil {
		t.Fatal(err)
	}
	plugin := pluginservice.Plugin{ID: "real", Name: "Real plugin", Version: "1", Triggers: []string{pluginservice.OnSubscribe, pluginservice.OnManual}, Source: `
function onSubscribe(proxies, subscription) { proxies[0].tag += '-auto'; return proxies }
function onRun(proxies, subscription) { Plugins.Log(subscription.name); proxies[0].tag += '-manual'; return proxies }
`}
	if _, err := plugins.Save(plugin, 0); err != nil {
		t.Fatal(err)
	}
	service := newTestRuntimeService(nil)
	service.SetPlugins(plugins)
	response, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "selected"}))
	if err != nil || !response.Msg.Results[0].GetOk() {
		t.Fatalf("real onSubscribe failed: %v %v", response, err)
	}
	first, _ := loadSubscriptions()
	selectedProxyID := first[0].Proxies[0].ID
	response, err = service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "selected"}))
	if err != nil || !response.Msg.Results[0].GetOk() {
		t.Fatalf("second real onSubscribe failed: %v %v", response, err)
	}
	second, _ := loadSubscriptions()
	if second[0].Proxies[0].ID != selectedProxyID || second[0].Proxies[0].Tag != "node-auto" {
		t.Fatalf("repeated rename broke selected node identity: first=%v second=%v", first[0].Proxies, second[0].Proxies)
	}
	result, err := service.RunPluginOnSubscription(context.Background(), plugin.ID, "selected")
	if err != nil || !reflect.DeepEqual(result.Logs, []string{"Selected"}) || !strings.Contains(string(readPluginFixture(t, subscriptionContentPath("selected"))), "node-auto-manual") {
		t.Fatalf("real onRun failed: %+v %v", result, err)
	}
	afterManual, _ := loadSubscriptions()
	if afterManual[0].Proxies[0].ID != selectedProxyID {
		t.Fatalf("manual rename broke selected node identity: %v", afterManual[0].Proxies)
	}
	if strings.Contains(string(readPluginFixture(t, subscriptionContentPath("selected"))), "__id_in_gui") {
		t.Fatal("real plugin persisted GUI identity into core cache")
	}
	previous := readPluginFixture(t, subscriptionContentPath("selected"))
	plugin.Disabled = true
	if _, err := plugins.Save(plugin, plugins.List().Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunPluginOnSubscription(context.Background(), plugin.ID, "selected"); !errors.Is(err, pluginservice.ErrInvalid) {
		t.Fatalf("disabled real plugin ran: %v", err)
	}
	if string(previous) != string(readPluginFixture(t, subscriptionContentPath("selected"))) {
		t.Fatal("disabled plugin altered cache")
	}
}

// Actual plugin sources live only in the independent repository. Main's normal
// tests above use tiny synthetic hooks; CI supplies this explicit external path
// to additionally exercise the maintained naming/deduplication implementations.
func externalSubscriptionPlugins(t *testing.T) []pluginservice.Plugin {
	t.Helper()
	directory := os.Getenv("WEBUI_PLUGIN_REPO")
	if directory == "" {
		t.Skip("set WEBUI_PLUGIN_REPO to an independent plugin repository checkout")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("open WEBUI_PLUGIN_REPO: %v", err)
	}
	defer root.Close()
	available, err := plugincatalog.Read(func(name string) ([]byte, error) {
		info, err := root.Lstat(name)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > pluginservice.MaxSourceBytes {
			return nil, fmt.Errorf("external fixture %q must be a regular file of at most %d bytes", name, pluginservice.MaxSourceBytes)
		}
		file, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return io.ReadAll(io.LimitReader(file, pluginservice.MaxSourceBytes+1))
	})
	if err != nil {
		t.Fatalf("invalid WEBUI_PLUGIN_REPO catalog: %v", err)
	}
	return available
}

func TestExternalSubscriptionPluginsKeepNodeIDsAcrossRefreshes(t *testing.T) {
	available := externalSubscriptionPlugins(t)
	const body = `[{"type":"shadowsocks","tag":"HK","server":"127.0.0.1","server_port":1,"method":"aes-128-gcm","password":"p"},{"type":"shadowsocks","tag":"HK","server":"127.0.0.1","server_port":2,"method":"aes-128-gcm","password":"p"},{"type":"shadowsocks","tag":"HK duplicate","server":"127.0.0.1","server_port":1,"method":"aes-128-gcm","password":"p"}]`
	groups := make(map[string][]pluginservice.Plugin)
	var dedupe pluginservice.Plugin
	for _, plugin := range available {
		if plugin.ID == "plugin-duplicate-proxies-removal" {
			dedupe = plugin
		}
	}
	if dedupe.ID == "" {
		t.Fatal("external catalog is missing the deduplication plugin")
	}
	for _, plugin := range available {
		for _, trigger := range plugin.Triggers {
			if trigger == pluginservice.OnSubscribe {
				groups[plugin.ID] = []pluginservice.Plugin{plugin}
				if plugin.ID != dedupe.ID {
					groups[plugin.ID+"+dedupe"] = []pluginservice.Plugin{plugin, dedupe}
					groups["dedupe+"+plugin.ID] = []pluginservice.Plugin{dedupe, plugin}
				}
				break
			}
		}
	}
	for name, group := range groups {
		t.Run(name, func(t *testing.T) {
			withTempBasePath(t)
			stubSubscriptionResponse(t, body)
			writePluginFixture(t, []subscription{{ID: "selected", Name: "Selected", Type: "Http"}}, "selected", body)
			engine, err := pluginservice.NewService(runtimePaths.Load())
			if err != nil {
				t.Fatal(err)
			}
			for _, plugin := range group {
				if _, err := engine.Save(plugin, engine.List().Revision); err != nil {
					t.Fatal(err)
				}
			}
			service := newTestRuntimeService(nil)
			service.SetPlugins(engine)
			refresh := func() []proxyRef {
				t.Helper()
				response, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "selected"}))
				if err != nil || !response.Msg.Results[0].GetOk() {
					t.Fatalf("external plugin refresh failed: %v %v", response, err)
				}
				items, _ := loadSubscriptions()
				return items[0].Proxies
			}
			first, second := refresh(), refresh()
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("external transformation replaced selected node identities: first=%v second=%v", first, second)
			}
			if (strings.Contains(name, "dedupe") || name == "plugin-duplicate-proxies-removal") && len(second) != 2 {
				t.Fatalf("GUI identity prevented duplicate removal: %v", second)
			}
			if strings.Contains(string(readPluginFixture(t, subscriptionContentPath("selected"))), "__id_in_gui") {
				t.Fatal("external plugin persisted internal node identity")
			}
		})
	}
}

func TestExternalFirstManualRenamePreservesSelectedNodeID(t *testing.T) {
	available := externalSubscriptionPlugins(t)
	withTempBasePath(t)
	writePluginFixture(t, []subscription{{ID: "selected", Name: "Selected", Type: "Manual", Proxies: []proxyRef{{ID: "ID_selected", Tag: "HK node", Type: "direct"}}}}, "selected", `[{"type":"direct","tag":"HK node"}]`)
	engine, err := pluginservice.NewService(runtimePaths.Load())
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range available {
		if plugin.ID == "plugin-beautiful-node-name" {
			if _, err := engine.Save(plugin, 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(engine.List().Plugins) != 1 {
		t.Fatal("external catalog is missing the manual naming plugin")
	}
	service := newTestRuntimeService(nil)
	service.SetPlugins(engine)
	if _, err := service.RunPluginOnSubscription(context.Background(), "plugin-beautiful-node-name", "selected"); err != nil {
		t.Fatal(err)
	}
	items, _ := loadSubscriptions()
	if len(items[0].Proxies) != 1 || items[0].Proxies[0].Tag == "HK node" || items[0].Proxies[0].ID != "ID_selected" {
		t.Fatalf("first external manual rename invalidated selection: %v", items[0].Proxies)
	}
}
