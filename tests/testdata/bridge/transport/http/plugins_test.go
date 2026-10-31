package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/plugincatalog"
	"guiforcores/bridge/plugins"
	appruntime "guiforcores/bridge/runtime"
	"guiforcores/bridge/storage"
)

type pluginRunnerFunc func(context.Context, string, string) (appruntime.PluginSubscriptionResult, error)

func (f pluginRunnerFunc) RunPluginOnSubscription(ctx context.Context, id, sub string) (appruntime.PluginSubscriptionResult, error) {
	return f(ctx, id, sub)
}

func pluginHTTPService(t *testing.T) *plugins.Service {
	t.Helper()
	service, err := plugins.NewService(storage.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func pluginHTTPHandler(t *testing.T, service *plugins.Service, catalog *plugincatalog.Service, runner SubscriptionPluginRunner) http.Handler {
	t.Helper()
	authService := auth.NewService(storage.NewPaths(t.TempDir()))
	if err := authService.SetSecret("plugin-test-secret"); err != nil {
		t.Fatal(err)
	}
	authService.AddSession("plugin-session")
	server := &Server{options: Options{Auth: authService, Plugins: service, PluginRunner: runner}, pluginCatalog: catalog}
	mux := http.NewServeMux()
	server.registerPluginRoutes(mux)
	return server.buildRootHandler(http.NotFoundHandler(), mux)
}

func pluginHTTPRequest(t *testing.T, handler http.Handler, method, path, body string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authenticated {
		req.Header.Set("Authorization", "Bearer plugin-session")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func pluginBody(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func httpPlugin(id string) plugins.Plugin {
	return plugins.Plugin{ID: id, Name: id, Version: "1", Triggers: []string{plugins.OnManual}, Source: `function onRun(p) { return p }`}
}

func TestPluginHTTPAuthenticationAndMethods(t *testing.T) {
	service := pluginHTTPService(t)
	handler := pluginHTTPHandler(t, service, nil, nil)
	for _, path := range []string{"/api/plugins", "/api/plugins/catalog", "/api/plugins/save", "/api/plugins/delete", "/api/plugins/reorder", "/api/plugins/install", "/api/plugins/update", "/api/plugins/run"} {
		method, wrong := http.MethodPost, http.MethodGet
		if path == "/api/plugins" || path == "/api/plugins/catalog" {
			method, wrong = http.MethodGet, http.MethodPost
		}
		if response := pluginHTTPRequest(t, handler, method, path, "{}", false); response.Code != http.StatusUnauthorized {
			t.Fatalf("%s is not authenticated: %d %s", path, response.Code, response.Body.String())
		}
		response := pluginHTTPRequest(t, handler, wrong, path, "{}", true)
		if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != method || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s wrong method: %d headers=%v", path, response.Code, response.Header())
		}
	}
	for _, path := range []string{"/api/plugins", "/api/plugins/catalog"} {
		response := pluginHTTPRequest(t, handler, http.MethodGet, path, "", true)
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !json.Valid(response.Body.Bytes()) {
			t.Fatalf("read endpoint failed: %s %d %s", path, response.Code, response.Body.String())
		}
	}
	if service.List().Revision != 0 {
		t.Fatal("unauthenticated or wrong-method calls changed plugins")
	}
}

func TestPluginHTTPStrictMutationPayloads(t *testing.T) {
	service := pluginHTTPService(t)
	handler := pluginHTTPHandler(t, service, nil, nil)
	valid := pluginBody(t, map[string]any{"plugin": httpPlugin("test"), "expected_revision": 0})
	cases := []struct{ operation, body string }{
		{"save", "null"}, {"save", "[]"}, {"save", "{}"},
		{"save", valid + " {}"},
		{"save", strings.Replace(valid, `"expected_revision":0`, `"expected_revision":0,"expected_revision":0`, 1)},
		{"save", strings.Replace(valid, `"name":"test"`, `"name":"test","name":"other"`, 1)},
		{"save", strings.Replace(valid, `"source":`, `"unknown":1,"source":`, 1)},
		{"save", strings.Replace(valid, `"expected_revision":0`, `"expected_revision":null`, 1)},
		{"save", strings.Replace(valid, `"expected_revision":0`, `"expected_revision":-1`, 1)},
		{"save", strings.Replace(valid, `"expected_revision":0`, `"expected_revision":0.5`, 1)},
		{"save", strings.Replace(valid, `"expected_revision":0`, `"expected_revision":"0"`, 1)},
		{"save", strings.Replace(valid, `"expected_revision":0`, `"expected_revision":0,"id":"ignored"`, 1)},
		{"save", `{"plugin":null,"expected_revision":0}`},
		{"reorder", `{"ids":null,"expected_revision":0}`},
		{"run", `{"id":"test","subscription_id":""}`},
		{"run", `{"id":"test","subscription_id":"sub","expected_revision":0}`},
		{"delete", `{"id":" ","expected_revision":0}`},
		{"delete", `{"id":"test"}`},
	}
	for _, tc := range cases {
		response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/"+tc.operation, tc.body, true)
		if response.Code != http.StatusBadRequest {
			t.Errorf("accepted %s body %s: %d %s", tc.operation, tc.body, response.Code, response.Body.String())
		}
	}
	large := `{"id":"` + strings.Repeat("a", 2<<20) + `","expected_revision":0}`
	if response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/delete", large, true); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unbounded request body: %d", response.Code)
	}
	if service.List().Revision != 0 || len(service.List().Plugins) != 0 {
		t.Fatal("invalid requests changed plugins")
	}
	response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/save", valid, true)
	if response.Code != http.StatusOK || service.List().Revision != 1 {
		t.Fatalf("valid save failed: %d %s", response.Code, response.Body.String())
	}
	response = pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/save", valid, true)
	if response.Code != http.StatusConflict || service.List().Revision != 1 {
		t.Fatalf("stale revision accepted: %d %s", response.Code, response.Body.String())
	}
}

func TestPluginHTTPUpdatePreservesUserSettingsAndInstallDoesNotExecute(t *testing.T) {
	service := pluginHTTPService(t)
	previous := httpPlugin("catalog-plugin")
	previous.Disabled = true
	previous.Configuration = map[string]any{"suffix": "user-setting"}
	if _, err := service.Save(previous, 0); err != nil {
		t.Fatal(err)
	}
	candidate := previous
	candidate.Version, candidate.Name = "2", "Updated name"
	candidate.Disabled = false
	candidate.Triggers = []string{plugins.OnManual, plugins.OnSubscribe}
	candidate.Configuration = map[string]any{"suffix": "catalog-default"}
	candidate.Source = `function onRun(p){return p}; function onSubscribe(p){return p}`
	newPlugin := httpPlugin("new-plugin")
	newPlugin.Source = `throw new Error("install must compile without evaluating source"); function onRun(p){return p}`
	cache := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(cache, []byte(pluginBody(t, []plugins.Plugin{candidate, newPlugin})), 0600); err != nil {
		t.Fatal(err)
	}
	handler := pluginHTTPHandler(t, service, plugincatalog.NewService(cache), nil)
	request := func(op, id string, revision uint64) *httptest.ResponseRecorder {
		return pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/"+op, pluginBody(t, map[string]any{"id": id, "expected_revision": revision}), true)
	}
	if response := request("update", previous.ID, 0); response.Code != http.StatusConflict {
		t.Fatalf("stale update accepted: %d %s", response.Code, response.Body.String())
	}
	if response := request("update", "unknown", 1); response.Code != http.StatusNotFound {
		t.Fatalf("updating uninstalled plugin accepted: %d %s", response.Code, response.Body.String())
	}
	if response := request("update", previous.ID, 1); response.Code != http.StatusOK {
		t.Fatalf("update failed: %d %s", response.Code, response.Body.String())
	}
	updated := service.List().Plugins[0]
	if updated.Version != "2" || updated.Source != candidate.Source || updated.Name != candidate.Name || !updated.Disabled || !reflect.DeepEqual(updated.Triggers, previous.Triggers) || !reflect.DeepEqual(updated.Configuration, previous.Configuration) {
		t.Fatalf("update lost source or user settings: %+v", updated)
	}
	if response := request("install", previous.ID, 2); response.Code != http.StatusConflict {
		t.Fatalf("install overwrote existing plugin: %d", response.Code)
	}
	if response := request("install", newPlugin.ID, 2); response.Code != http.StatusOK {
		t.Fatalf("install executed source or failed: %d %s", response.Code, response.Body.String())
	}
	response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/reorder", `{"ids":["new-plugin","catalog-plugin"],"expected_revision":3}`, true)
	if response.Code != http.StatusOK || service.List().Plugins[0].ID != "new-plugin" {
		t.Fatalf("reorder failed: %d %s", response.Code, response.Body.String())
	}
	if response := request("delete", newPlugin.ID, 4); response.Code != http.StatusOK || len(service.List().Plugins) != 1 {
		t.Fatalf("delete failed: %d %s", response.Code, response.Body.String())
	}
}

func TestPluginHTTPManualDispatchAndContextErrors(t *testing.T) {
	service := pluginHTTPService(t)
	calls := 0
	var failure error
	runner := pluginRunnerFunc(func(ctx context.Context, pluginID, subscriptionID string) (appruntime.PluginSubscriptionResult, error) {
		calls++
		if pluginID != "chosen-plugin" || subscriptionID != "chosen-subscription" {
			t.Fatalf("manual dispatch selected wrong input: %s %s", pluginID, subscriptionID)
		}
		return appruntime.PluginSubscriptionResult{Logs: []string{"ran"}, Before: 2, After: 1, SubscriptionID: subscriptionID}, failure
	})
	handler := pluginHTTPHandler(t, service, nil, runner)
	body := `{"id":"chosen-plugin","subscription_id":"chosen-subscription"}`
	response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/run", body, true)
	var result appruntime.PluginSubscriptionResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || calls != 1 || result.Before != 2 || result.After != 1 || len(result.Logs) != 1 {
		t.Fatalf("manual result lost: %d %s %v", response.Code, response.Body.String(), err)
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		failure = err
		if response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/run", body, true); response.Code != http.StatusRequestTimeout {
			t.Fatalf("context error mapping: %d %s", response.Code, response.Body.String())
		}
	}
}
