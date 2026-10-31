package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"guiforcores/bridge/plugins"
	appruntime "guiforcores/bridge/runtime"
)

func TestPluginHTTPDiagnosticRunsWithoutSubscriptionAndCannotReachCacheRunner(t *testing.T) {
	s := pluginHTTPService(t)
	p := httpPlugin("diagnostic")
	p.ManualTarget = plugins.ManualDiagnostic
	p.Source = `function onRun(){if(arguments.length!==0)throw Error('subscription leaked');console.log('checked');return {status:'ok', nested:[1,true]}}`
	if _, err := s.Save(p, 0); err != nil {
		t.Fatal(err)
	}
	subscriptionCalls := 0
	runner := pluginRunnerFunc(func(context.Context, string, string) (appruntime.PluginSubscriptionResult, error) {
		subscriptionCalls++
		return appruntime.PluginSubscriptionResult{}, nil
	})
	handler := pluginHTTPHandler(t, s, nil, runner)
	before := s.List()
	response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/run", `{"id":"diagnostic"}`, true)
	var result struct {
		Report map[string]any `json:"report"`
		Logs   []string       `json:"logs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Report["status"] != "ok" || len(result.Logs) != 1 || result.Logs[0] != "checked" {
		t.Fatalf("diagnostic result failed: %d %s %v", response.Code, response.Body.String(), err)
	}
	if subscriptionCalls != 0 || !reflect.DeepEqual(before, s.List()) {
		t.Fatal("diagnostic reached subscription runner or changed storage")
	}
	for _, body := range []string{`{"id":"diagnostic","subscription_id":"sub"}`, `{"id":"diagnostic","subscription_id":null}`, `{"id":"diagnostic","headers":{}}`} {
		if response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/run", body, true); response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe diagnostic body accepted: %s %d %s", body, response.Code, response.Body.String())
		}
	}
	if response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/run", `{"id":"absent"}`, true); response.Code != http.StatusNotFound {
		t.Fatalf("unknown diagnostic status=%d", response.Code)
	}
	p.Disabled = true
	if _, err := s.Save(p, s.List().Revision); err != nil {
		t.Fatal(err)
	}
	if response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/run", `{"id":"diagnostic"}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("disabled diagnostic ran: %d %s", response.Code, response.Body.String())
	}
	ordinary := httpPlugin("ordinary")
	if _, err := s.Save(ordinary, s.List().Revision); err != nil {
		t.Fatal(err)
	}
	if response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/run", `{"id":"ordinary"}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("subscription plugin ran without target: %d %s", response.Code, response.Body.String())
	}
	if subscriptionCalls != 0 {
		t.Fatal("rejected diagnostic calls reached subscription runner")
	}
}
