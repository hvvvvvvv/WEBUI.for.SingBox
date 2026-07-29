package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/storage"
	"guiforcores/bridge/syncstate"
	appv1 "guiforcores/gen/app/v1"
	commonv1 "guiforcores/gen/common/v1"

	connect "connectrpc.com/connect"
)

func rulesetRevision(t *testing.T, service *appRuntimeService, id string) *commonv1.ExpectedRevision {
	t.Helper()
	response, err := service.ListRuleSets(context.Background(), connect.NewRequest(&appv1.ListRuleSetsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	return &commonv1.ExpectedRevision{
		InstanceId: response.Msg.GetState().GetInstanceId(),
		Revision:   response.Msg.GetState().GetItemRevisions()[id],
	}
}

func subscriptionRevision(t *testing.T, service *appRuntimeService, id string) *commonv1.ExpectedRevision {
	t.Helper()
	response, err := service.ListSubscriptions(context.Background(), connect.NewRequest(&appv1.ListSubscriptionsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	return &commonv1.ExpectedRevision{
		InstanceId: response.Msg.GetState().GetInstanceId(),
		Revision:   response.Msg.GetState().GetItemRevisions()[id],
	}
}

func scheduledTaskRevision(t *testing.T, service *appRuntimeService, id string) *commonv1.ExpectedRevision {
	t.Helper()
	response, err := service.ListScheduledTasks(context.Background(), connect.NewRequest(&appv1.ListScheduledTasksRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	return &commonv1.ExpectedRevision{
		InstanceId: response.Msg.GetState().GetInstanceId(),
		Revision:   response.Msg.GetState().GetItemRevisions()[id],
	}
}

func mutationRevision(state *commonv1.MutationState) *commonv1.ExpectedRevision {
	return &commonv1.ExpectedRevision{InstanceId: state.GetInstanceId(), Revision: state.GetItemRevision()}
}

func putRuleSetForTest(service *appRuntimeService, raw string) (string, error) {
	var item ruleset
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		return "", err
	}
	response, err := service.ListRuleSets(context.Background(), connect.NewRequest(&appv1.ListRuleSetsRequest{}))
	if err != nil {
		return "", err
	}
	if response.Msg.GetState().GetItemRevisions()[item.ID] == 0 {
		created, createErr := service.CreateRuleSet(context.Background(), connect.NewRequest(&appv1.CreateRuleSetRequest{RulesetJson: raw}))
		if createErr != nil {
			return "", createErr
		}
		return created.Msg.GetRulesetJson(), nil
	}
	updated, updateErr := service.UpdateRuleSetConfig(context.Background(), connect.NewRequest(&appv1.UpdateRuleSetConfigRequest{
		RulesetJson: raw,
		ExpectedRevision: &commonv1.ExpectedRevision{
			InstanceId: response.Msg.GetState().GetInstanceId(),
			Revision:   response.Msg.GetState().GetItemRevisions()[item.ID],
		},
	}))
	if updateErr != nil {
		return "", updateErr
	}
	return updated.Msg.GetRulesetJson(), nil
}

func putSubscriptionForTest(service *appRuntimeService, raw string) (string, error) {
	var item subscription
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		return "", err
	}
	response, err := service.ListSubscriptions(context.Background(), connect.NewRequest(&appv1.ListSubscriptionsRequest{}))
	if err != nil {
		return "", err
	}
	if response.Msg.GetState().GetItemRevisions()[item.ID] == 0 {
		created, createErr := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{SubscriptionJson: raw}))
		if createErr != nil {
			return "", createErr
		}
		return created.Msg.GetSubscriptionJson(), nil
	}
	updated, updateErr := service.UpdateSubscriptionConfig(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionConfigRequest{
		SubscriptionJson: raw,
		ExpectedRevision: &commonv1.ExpectedRevision{
			InstanceId: response.Msg.GetState().GetInstanceId(),
			Revision:   response.Msg.GetState().GetItemRevisions()[item.ID],
		},
	}))
	if updateErr != nil {
		return "", updateErr
	}
	return updated.Msg.GetSubscriptionJson(), nil
}

func putScheduledTaskForTest(service *appRuntimeService, raw string) (string, error) {
	var item scheduledTask
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		return "", err
	}
	response, err := service.ListScheduledTasks(context.Background(), connect.NewRequest(&appv1.ListScheduledTasksRequest{}))
	if err != nil {
		return "", err
	}
	if response.Msg.GetState().GetItemRevisions()[item.ID] == 0 {
		created, createErr := service.CreateScheduledTask(context.Background(), connect.NewRequest(&appv1.CreateScheduledTaskRequest{TaskJson: raw}))
		if createErr != nil {
			return "", createErr
		}
		return created.Msg.GetTaskJson(), nil
	}
	updated, updateErr := service.UpdateScheduledTask(context.Background(), connect.NewRequest(&appv1.UpdateScheduledTaskRequest{
		TaskJson: raw,
		ExpectedRevision: &commonv1.ExpectedRevision{
			InstanceId: response.Msg.GetState().GetInstanceId(),
			Revision:   response.Msg.GetState().GetItemRevisions()[item.ID],
		},
	}))
	if updateErr != nil {
		return "", updateErr
	}
	return updated.Msg.GetTaskJson(), nil
}

func writeRuleSetHubCache(hub rulesetHub) error {
	data, err := json.Marshal(hub)
	if err != nil {
		return err
	}
	path := GetPath(rulesetHubPath)
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func TestNextCronRunsCoalescesWildcardSecondsPerMinute(t *testing.T) {
	from := time.Date(2026, 6, 16, 10, 40, 58, 0, time.UTC)

	runs, err := nextCronRuns("* 41 * * * *", 3, from)
	if err != nil {
		t.Fatal(err)
	}

	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}

	first := time.UnixMilli(runs[0]).UTC()
	second := time.UnixMilli(runs[1]).UTC()

	if first.Minute() != 41 {
		t.Fatalf("expected first run at minute 41, got %s", first)
	}
	if second.Sub(first) < time.Hour-time.Minute {
		t.Fatalf("expected wildcard seconds to be coalesced per matching minute, got %s then %s", first, second)
	}
}

func TestSubscriptionRequestUserAgentPriority(t *testing.T) {
	tests := []struct {
		name              string
		requestHeaders    map[string]string
		defaultUserAgent  string
		expectedUserAgent string
	}{
		{
			name:              "subscription user agent takes priority",
			requestHeaders:    map[string]string{"User-Agent": "Subscription/1.0"},
			defaultUserAgent:  "Global/1.0",
			expectedUserAgent: "Subscription/1.0",
		},
		{
			name:              "global user agent is the fallback",
			requestHeaders:    map[string]string{},
			defaultUserAgent:  "Global/1.0",
			expectedUserAgent: "Global/1.0",
		},
		{
			name:              "subscription header is case insensitive",
			requestHeaders:    map[string]string{"user-agent": "Lowercase/1.0"},
			defaultUserAgent:  "Global/1.0",
			expectedUserAgent: "Lowercase/1.0",
		},
		{
			name:              "blank subscription user agent uses global fallback",
			requestHeaders:    map[string]string{"USER-AGENT": "  "},
			defaultUserAgent:  "Global/1.0",
			expectedUserAgent: "Global/1.0",
		},
		{
			name:              "http client default is the final fallback",
			requestHeaders:    map[string]string{"User-Agent": "  "},
			defaultUserAgent:  "  ",
			expectedUserAgent: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTempBasePath(t)
			previousRequest := subscriptionHTTPRequest
			actualUserAgent := ""
			subscriptionHTTPRequest = func(method string, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int) (*http.Response, string, error) {
				requestHeader := make(http.Header, len(headers))
				for key, value := range headers {
					requestHeader.Set(key, value)
				}
				actualUserAgent = requestHeader.Get("User-Agent")
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, `{"outbounds":[{"type":"direct","tag":"direct"}]}`, nil
			}
			defer func() {
				subscriptionHTTPRequest = previousRequest
			}()

			requestHeaders := make(map[string]string, len(tt.requestHeaders))
			for key, value := range tt.requestHeaders {
				requestHeaders[key] = value
			}
			if err := saveSubscriptions([]subscription{{
				ID:   "http-subscription",
				Name: "HTTP subscription",
				Type: "Http",
				URL:  "https://example.com/subscription",
				Header: subscriptionHeader{
					Request: requestHeaders,
				},
			}}); err != nil {
				t.Fatal(err)
			}

			service := &appRuntimeService{
				config: staticAppConfig{value: config.AppConfig{UserAgent: tt.defaultUserAgent}},
				state:  syncstate.NewCoordinator(),
			}
			if _, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{
				Id: "http-subscription",
			})); err != nil {
				t.Fatal(err)
			}

			if actualUserAgent != tt.expectedUserAgent {
				t.Fatalf("expected user agent %q, got %q", tt.expectedUserAgent, actualUserAgent)
			}
			items, err := loadSubscriptions()
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 {
				t.Fatalf("expected one subscription, got %d", len(items))
			}
			if !reflect.DeepEqual(items[0].Header.Request, tt.requestHeaders) {
				t.Fatalf("subscription request headers were modified: got %#v, want %#v", items[0].Header.Request, tt.requestHeaders)
			}
		})
	}
}

func TestCreateRuleSetIgnoresClientPath(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	item, err := putRuleSetForTest(service, `{"id":"ruleset/one","tag":"One","type":"Http","format":"source","path":"data/evil.json","url":"https://example.com/rules.json"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(item, "evil") {
		t.Fatalf("client path should be ignored, got %s", item)
	}
	if !strings.Contains(item, `"path":"data/rulesets/ruleset_one.json"`) {
		t.Fatalf("expected managed source path, got %s", item)
	}
}

func TestCreateManualSourceRuleSetCreatesDefaultContent(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	item, err := putRuleSetForTest(service, `{"id":"manual","tag":"Manual","type":"Manual","format":"source"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(item, `"count":0`) {
		t.Fatalf("expected default count 0, got %s", item)
	}

	data, err := os.ReadFile(GetPath("data/rulesets/manual.json"))
	if err != nil {
		t.Fatal(err)
	}
	var content struct {
		Version int   `json:"version"`
		Rules   []any `json:"rules"`
	}
	if err := json.Unmarshal(data, &content); err != nil {
		t.Fatal(err)
	}
	if content.Version != 2 || len(content.Rules) != 0 {
		t.Fatalf("expected empty version 2 ruleset, got %s", string(data))
	}
}

func TestCreateHttpSourceRuleSetDoesNotCreateDefaultContent(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	_, err := putRuleSetForTest(service, `{"id":"http","tag":"HTTP","type":"Http","format":"source","url":"https://example.com/rules.json"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GetPath("data/rulesets/http.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no default content for HTTP ruleset, got %v", err)
	}
}

func TestCreateManualSourceRuleSetDoesNotOverwriteExistingContent(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	path := GetPath("data/rulesets/manual.json")
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	existingContent := []byte(`{"version":2,"rules":["keep"]}`)
	if err := os.WriteFile(path, existingContent, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := putRuleSetForTest(service, `{"id":"manual","tag":"Manual","type":"Manual","format":"source"}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(existingContent) {
		t.Fatalf("expected existing content to remain, got %s", string(data))
	}
}

func TestSourceTypeValidationRejectsFile(t *testing.T) {
	tests := []struct {
		name string
		call func(*appRuntimeService) error
	}{
		{
			name: "create subscription",
			call: func(service *appRuntimeService) error {
				_, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
					SubscriptionJson: `{"id":"file-sub","name":"File","type":"File","url":"data/local/sub.json"}`,
				}))
				return err
			},
		},
		{
			name: "create another subscription",
			call: func(service *appRuntimeService) error {
				_, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
					SubscriptionJson: `{"id":"file-sub-2","name":"File","type":"File","url":"data/local/sub.json"}`,
				}))
				return err
			},
		},
		{
			name: "create ruleset",
			call: func(service *appRuntimeService) error {
				_, err := service.CreateRuleSet(context.Background(), connect.NewRequest(&appv1.CreateRuleSetRequest{
					RulesetJson: `{"id":"file-ruleset","tag":"File","type":"File","format":"source","url":"data/local/rules.json"}`,
				}))
				return err
			},
		},
		{
			name: "create another ruleset",
			call: func(service *appRuntimeService) error {
				_, err := service.CreateRuleSet(context.Background(), connect.NewRequest(&appv1.CreateRuleSetRequest{
					RulesetJson: `{"id":"file-ruleset-2","tag":"File","type":"File","format":"source","url":"data/local/rules.json"}`,
				}))
				return err
			},
		},
	}

	for index := range tests {
		test := &tests[index]
		t.Run(test.name, func(t *testing.T) {
			withTempBasePath(t)
			err := test.call(newAppRuntimeService(nil, nil))
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestSubscriptionSourceTypeValidationAllowsHttpAndManual(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	for _, subscriptionJSON := range []string{
		`{"id":"http-sub","name":"HTTP","type":"Http","url":"https://example.com/sub.json"}`,
		`{"id":"manual-sub","name":"Manual","type":"Manual"}`,
	} {
		if _, err := putSubscriptionForTest(service, subscriptionJSON); err != nil {
			t.Fatalf("expected supported subscription type, got %v", err)
		}
	}
}

func TestSubscriptionNodeConversionFieldDefaultsAndRoundTrips(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	put := func(raw string) subscription {
		t.Helper()
		itemJSON, err := putSubscriptionForTest(service, raw)
		if err != nil {
			t.Fatal(err)
		}
		var item subscription
		if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
			t.Fatal(err)
		}
		return item
	}

	missing := put(`{"id":"missing","name":"Missing","type":"Http","url":"https://example.com/sub"}`)
	if missing.EnableNodeConversion {
		t.Fatal("missing enableNodeConversion field must default to false")
	}

	enabled := put(`{"id":"enabled","name":"Enabled","type":"Http","url":"https://example.com/sub","enableNodeConversion":true}`)
	if !enabled.EnableNodeConversion {
		t.Fatal("explicit enableNodeConversion=true was not preserved")
	}

	disabled := put(`{"id":"disabled","name":"Disabled","type":"Http","url":"https://example.com/sub","enableNodeConversion":false}`)
	if disabled.EnableNodeConversion {
		t.Fatal("explicit enableNodeConversion=false was not preserved")
	}

	items, err := loadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]bool, len(items))
	for _, item := range items {
		values[item.ID] = item.EnableNodeConversion
	}
	if values["missing"] || !values["enabled"] || values["disabled"] {
		t.Fatalf("persisted node conversion values = %#v", values)
	}

	legacyYAML := "- id: legacy\n  name: Legacy\n  type: Http\n  url: https://example.com/sub\n"
	if err := os.WriteFile(GetPath(subscriptionsFilePath), []byte(legacyYAML), 0644); err != nil {
		t.Fatal(err)
	}
	legacy, err := loadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 1 || legacy[0].EnableNodeConversion {
		t.Fatalf("legacy subscription without field = %#v, want conversion disabled", legacy)
	}
}

func TestSaveSubscriptionContent(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	_, err := putSubscriptionForTest(service, `{"id":"manual","name":"Manual","type":"Manual"}`)
	if err != nil {
		t.Fatal(err)
	}

	saveResp, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id:               "manual",
		Content:          `[{"tag":"direct","type":"direct"}]`,
		ProxyIds:         []string{"ID_direct"},
		ExpectedRevision: subscriptionRevision(t, service, "manual"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saveResp.Msg.GetSubscriptionJson(), `"path"`) {
		t.Fatalf("subscription content response should not include path, got %s", saveResp.Msg.GetSubscriptionJson())
	}
	if !strings.Contains(saveResp.Msg.GetSubscriptionJson(), `"tag":"direct"`) {
		t.Fatalf("expected proxy metadata update, got %s", saveResp.Msg.GetSubscriptionJson())
	}
	if _, err := os.Stat(GetPath("data/subscribes/manual.json")); err != nil {
		t.Fatal(err)
	}

	contentResp, err := service.GetSubscriptionContent(context.Background(), connect.NewRequest(&appv1.GetSubscriptionContentRequest{Id: "manual"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contentResp.Msg.GetContent(), `"tag": "direct"`) {
		t.Fatalf("expected saved content, got %s", contentResp.Msg.GetContent())
	}
}

func TestSubscriptionResponsesNormalizeEmptyProxies(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	created, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
		SubscriptionJson: `{"id":"created","name":"Created","type":"Manual"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(created.Msg.GetSubscriptionJson(), `"proxies":[]`) {
		t.Fatalf("created subscription did not normalize proxies: %s", created.Msg.GetSubscriptionJson())
	}

	legacyYAML := "- id: missing\n  name: Missing\n  type: Manual\n- id: null\n  name: Null\n  type: Manual\n  proxies: null\n"
	if err := os.WriteFile(GetPath(subscriptionsFilePath), []byte(legacyYAML), 0644); err != nil {
		t.Fatal(err)
	}
	listed, err := service.ListSubscriptions(context.Background(), connect.NewRequest(&appv1.ListSubscriptionsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Msg.GetSubscriptionsJson()) != 2 {
		t.Fatalf("listed subscriptions = %#v", listed.Msg.GetSubscriptionsJson())
	}
	for _, raw := range listed.Msg.GetSubscriptionsJson() {
		if !strings.Contains(raw, `"proxies":[]`) {
			t.Fatalf("listed subscription did not normalize proxies: %s", raw)
		}
	}
}

func TestSaveSubscriptionContentPreservesExplicitProxyIdentity(t *testing.T) {
	withTempBasePath(t)
	events := &recordingRuntimeEvents{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, nil)

	created, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
		SubscriptionJson: `{"id":"manual","name":"Manual","type":"Manual"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id:               "manual",
		Content:          `[{"tag":"first","type":"direct"},{"tag":"second","type":"block"}]`,
		ProxyIds:         []string{"ID_first", "ID_second"},
		ExpectedRevision: mutationRevision(created.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}

	events.events = nil
	renamed, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id:               "manual",
		Content:          `[{"tag":"second","type":"block"},{"tag":"renamed","type":"direct","__id_in_gui":"ID_first"}]`,
		ProxyIds:         []string{"ID_second", "ID_first"},
		ExpectedRevision: mutationRevision(first.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}

	var item subscription
	if err := json.Unmarshal([]byte(renamed.Msg.GetSubscriptionJson()), &item); err != nil {
		t.Fatal(err)
	}
	want := []proxyRef{
		{ID: "ID_second", Tag: "second", Type: "block"},
		{ID: "ID_first", Tag: "renamed", Type: "direct"},
	}
	if !reflect.DeepEqual(item.Proxies, want) {
		t.Fatalf("proxy identities after rename and reorder = %#v, want %#v", item.Proxies, want)
	}
	content, err := os.ReadFile(GetPath(subscriptionContentPath("manual")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "__id_in_gui") {
		t.Fatalf("persisted subscription content leaked GUI identity: %s", content)
	}
	if len(events.events) != 1 || events.events[0].name != "resourceChanged" {
		t.Fatalf("rename and reorder events = %#v", events.events)
	}
}

func TestSaveSubscriptionContentResolvesMissingProxyIDs(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	created, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
		SubscriptionJson: `{"id":"manual","name":"Manual","type":"Manual"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id:               "manual",
		Content:          `[{"tag":"existing","type":"direct"}]`,
		ProxyIds:         []string{"ID_existing"},
		ExpectedRevision: mutationRevision(created.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id:               "manual",
		Content:          `[{"tag":"existing","type":"block"},{"tag":"new","type":"direct"}]`,
		ProxyIds:         []string{"", ""},
		ExpectedRevision: mutationRevision(first.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}

	var item subscription
	if err := json.Unmarshal([]byte(second.Msg.GetSubscriptionJson()), &item); err != nil {
		t.Fatal(err)
	}
	if len(item.Proxies) != 2 || item.Proxies[0].ID != "ID_existing" {
		t.Fatalf("same-tag fallback did not preserve identity: %#v", item.Proxies)
	}
	if item.Proxies[1].ID == "" || item.Proxies[1].ID == item.Proxies[0].ID {
		t.Fatalf("new proxy did not receive a unique identity: %#v", item.Proxies)
	}
}

func TestSaveSubscriptionContentRejectsInvalidProxyIDsAtomically(t *testing.T) {
	withTempBasePath(t)
	events := &recordingRuntimeEvents{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, nil)

	created, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
		SubscriptionJson: `{"id":"manual","name":"Manual","type":"Manual"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id:               "manual",
		Content:          `[{"tag":"existing","type":"direct"}]`,
		ProxyIds:         []string{"ID_existing"},
		ExpectedRevision: mutationRevision(created.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	contentBefore, err := os.ReadFile(GetPath(subscriptionContentPath("manual")))
	if err != nil {
		t.Fatal(err)
	}
	subscriptionsBefore, err := os.ReadFile(GetPath(subscriptionsFilePath))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		content  string
		proxyIDs []string
	}{
		{name: "length mismatch", content: `[{"tag":"changed","type":"direct"}]`},
		{
			name:     "duplicate ids",
			content:  `[{"tag":"first","type":"direct"},{"tag":"second","type":"block"}]`,
			proxyIDs: []string{"ID_duplicate", "ID_duplicate"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events.events = nil
			_, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
				Id:               "manual",
				Content:          test.content,
				ProxyIds:         test.proxyIDs,
				ExpectedRevision: mutationRevision(saved.Msg.GetState()),
			}))
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("error code = %v, want invalid argument", connect.CodeOf(err))
			}
			contentAfter, readErr := os.ReadFile(GetPath(subscriptionContentPath("manual")))
			if readErr != nil {
				t.Fatal(readErr)
			}
			subscriptionsAfter, readErr := os.ReadFile(GetPath(subscriptionsFilePath))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(contentAfter, contentBefore) || !bytes.Equal(subscriptionsAfter, subscriptionsBefore) {
				t.Fatal("invalid proxy identities partially changed subscription data")
			}
			listed, listErr := service.ListSubscriptions(context.Background(), connect.NewRequest(&appv1.ListSubscriptionsRequest{}))
			if listErr != nil {
				t.Fatal(listErr)
			}
			if listed.Msg.GetState().GetItemRevisions()["manual"] != saved.Msg.GetState().GetItemRevision() {
				t.Fatalf("invalid save advanced item revision: %#v", listed.Msg.GetState())
			}
			if len(events.events) != 0 {
				t.Fatalf("invalid save published events: %#v", events.events)
			}
		})
	}
}

func TestSaveSubscriptionContentTreatsIdentityChangesAsMutations(t *testing.T) {
	withTempBasePath(t)
	events := &recordingRuntimeEvents{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, nil)

	created, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
		SubscriptionJson: `{"id":"manual","name":"Manual","type":"Manual"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id:               "manual",
		Content:          `[{"tag":"same","type":"direct"}]`,
		ProxyIds:         []string{"ID_old"},
		ExpectedRevision: mutationRevision(created.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	contentBefore, err := os.ReadFile(GetPath(subscriptionContentPath("manual")))
	if err != nil {
		t.Fatal(err)
	}

	events.events = nil
	changed, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id:               "manual",
		Content:          `[{"tag":"same","type":"direct"}]`,
		ProxyIds:         []string{"ID_new"},
		ExpectedRevision: mutationRevision(first.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if changed.Msg.GetState().GetItemRevision() <= first.Msg.GetState().GetItemRevision() {
		t.Fatalf("identity-only change did not advance item revision: %#v", changed.Msg.GetState())
	}
	contentAfter, err := os.ReadFile(GetPath(subscriptionContentPath("manual")))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contentAfter, contentBefore) {
		t.Fatal("identity-only change rewrote subscription content")
	}
	if len(events.events) != 1 || events.events[0].name != "resourceChanged" {
		t.Fatalf("identity-only change events = %#v", events.events)
	}

	events.events = nil
	noOp, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id:               "manual",
		Content:          `[{"tag":"same","type":"direct"}]`,
		ProxyIds:         []string{"ID_new"},
		ExpectedRevision: mutationRevision(changed.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if noOp.Msg.GetState().GetStateRevision() != changed.Msg.GetState().GetStateRevision() {
		t.Fatalf("exact no-op advanced state revision: %#v", noOp.Msg.GetState())
	}
	if len(events.events) != 0 {
		t.Fatalf("exact no-op published events: %#v", events.events)
	}
}

func TestListRuleSetsBackfillsMissingPath(t *testing.T) {
	withTempBasePath(t)
	if err := writeRuntimeYAMLFile(runtimeRulesetsFilePath, []map[string]any{{
		"id":     "legacy",
		"tag":    "Legacy",
		"type":   "Manual",
		"format": "source",
	}}); err != nil {
		t.Fatal(err)
	}

	service := newAppRuntimeService(nil, nil)
	resp, err := service.ListRuleSets(context.Background(), connect.NewRequest(&appv1.ListRuleSetsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Msg.GetRulesetsJson()) != 1 || !strings.Contains(resp.Msg.GetRulesetsJson()[0], `"path":"data/rulesets/legacy.json"`) {
		t.Fatalf("expected managed path in response, got %#v", resp.Msg.GetRulesetsJson())
	}
	items, err := loadRulesets()
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Path != "data/rulesets/legacy.json" {
		t.Fatalf("expected persisted managed path, got %q", items[0].Path)
	}
}

func TestSaveRuleSetContentAndClear(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	_, err := putRuleSetForTest(service, `{"id":"manual","tag":"Manual","type":"Manual","format":"source"}`)
	if err != nil {
		t.Fatal(err)
	}

	saveResp, err := service.SaveRuleSetContent(context.Background(), connect.NewRequest(&appv1.SaveRuleSetContentRequest{
		Id:               "manual",
		Content:          `{"version":1,"rules":["a",["b","c"]]}`,
		ExpectedRevision: rulesetRevision(t, service, "manual"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saveResp.Msg.GetRulesetJson(), `"count":3`) {
		t.Fatalf("expected count update, got %s", saveResp.Msg.GetRulesetJson())
	}

	contentResp, err := service.GetRuleSetContent(context.Background(), connect.NewRequest(&appv1.GetRuleSetContentRequest{Id: "manual"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contentResp.Msg.GetContent(), `"rules"`) {
		t.Fatalf("expected saved content, got %s", contentResp.Msg.GetContent())
	}

	clearResp, err := service.ClearRuleSetContent(context.Background(), connect.NewRequest(&appv1.ClearRuleSetContentRequest{
		Id:               "manual",
		ExpectedRevision: mutationRevision(saveResp.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clearResp.Msg.GetRulesetJson(), `"count":0`) {
		t.Fatalf("expected count 0 after clear, got %s", clearResp.Msg.GetRulesetJson())
	}
	contentResp, err = service.GetRuleSetContent(context.Background(), connect.NewRequest(&appv1.GetRuleSetContentRequest{Id: "manual"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contentResp.Msg.GetContent(), `"version": 2`) {
		t.Fatalf("expected version 2 after clear, got %s", contentResp.Msg.GetContent())
	}
}

type rulesetDecompilerStub struct {
	path    string
	content string
	err     error
	started chan struct{}
	release chan struct{}
}

func (*rulesetDecompilerStub) ReferencedResourcesChanged(syncstate.Domain, []string) {}

func (d *rulesetDecompilerStub) DecompileRuleSet(sourcePath string) (string, error) {
	d.path = sourcePath
	if d.started != nil {
		close(d.started)
	}
	if d.release != nil {
		<-d.release
	}
	return d.content, d.err
}

func TestGetBinaryRuleSetContentUsesCoreDecompiler(t *testing.T) {
	withTempBasePath(t)
	decompiler := &rulesetDecompilerStub{content: `{"version":2,"rules":[{"domain":["example.com"]}]}`}
	service := newAppRuntimeService(nil, decompiler)
	_, err := putRuleSetForTest(
		service,
		`{"id":"binary","tag":"Binary","type":"Http","format":"binary","url":"https://example.com/ruleset.srs"}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.GetRuleSetContent(
		context.Background(),
		connect.NewRequest(&appv1.GetRuleSetContentRequest{Id: "binary"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetContent() != decompiler.content {
		t.Fatalf("content = %q, want %q", response.Msg.GetContent(), decompiler.content)
	}
	if decompiler.path != GetPath("data/rulesets/binary.srs") {
		t.Fatalf("decompile path = %q, want managed ruleset path", decompiler.path)
	}
	if response.Msg.GetRevision().GetRevision() == 0 {
		t.Fatal("expected binary content revision")
	}
}

func TestGetBinaryRuleSetContentRequiresCoreDecompiler(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	_, err := putRuleSetForTest(
		service,
		`{"id":"binary","tag":"Binary","type":"Http","format":"binary","url":"https://example.com/ruleset.srs"}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.GetRuleSetContent(
		context.Background(),
		connect.NewRequest(&appv1.GetRuleSetContentRequest{Id: "binary"}),
	)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("error = %v, want failed precondition", err)
	}
}

func TestGetBinaryRuleSetContentDoesNotHoldRulesetLockWhileDecompiling(t *testing.T) {
	withTempBasePath(t)
	decompiler := &rulesetDecompilerStub{
		content: `{"version":2,"rules":[]}`,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newAppRuntimeService(nil, decompiler)
	_, err := putRuleSetForTest(
		service,
		`{"id":"binary","tag":"Binary","type":"Http","format":"binary","url":"https://example.com/ruleset.srs"}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	getDone := make(chan error, 1)
	go func() {
		_, getErr := service.GetRuleSetContent(
			context.Background(),
			connect.NewRequest(&appv1.GetRuleSetContentRequest{Id: "binary"}),
		)
		getDone <- getErr
	}()
	<-decompiler.started

	listDone := make(chan error, 1)
	go func() {
		_, listErr := service.ListRuleSets(
			context.Background(),
			connect.NewRequest(&appv1.ListRuleSetsRequest{}),
		)
		listDone <- listErr
	}()
	select {
	case listErr := <-listDone:
		if listErr != nil {
			t.Fatal(listErr)
		}
	case <-time.After(time.Second):
		t.Fatal("ListRuleSets blocked while binary rule-set was being decompiled")
	}

	close(decompiler.release)
	if getErr := <-getDone; getErr != nil {
		t.Fatal(getErr)
	}
}

func TestSaveBinaryRuleSetContentRemainsRejected(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	_, err := putRuleSetForTest(
		service,
		`{"id":"binary","tag":"Binary","type":"Http","format":"binary","url":"https://example.com/ruleset.srs"}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.SaveRuleSetContent(
		context.Background(),
		connect.NewRequest(&appv1.SaveRuleSetContentRequest{
			Id:               "binary",
			Content:          `{"version":2,"rules":[]}`,
			ExpectedRevision: rulesetRevision(t, service, "binary"),
		}),
	)
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("error = %v, want invalid argument", err)
	}
}

func TestPreviewRuleSetHub(t *testing.T) {
	withTempBasePath(t)
	previousRequest := ruleSetHubHTTPRequest
	ruleSetHubHTTPRequest = func(method string, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int) (*http.Response, string, error) {
		if method != http.MethodGet {
			t.Fatalf("expected GET, got %s", method)
		}
		if rawURL != "https://hub.example/geosite/cn.json" {
			t.Fatalf("unexpected preview url %s", rawURL)
		}
		return &http.Response{StatusCode: http.StatusOK}, `{"version":2,"rules":[{"domain":["example.com"]}]}`, nil
	}
	t.Cleanup(func() {
		ruleSetHubHTTPRequest = previousRequest
	})

	if err := writeRuleSetHubCache(rulesetHub{
		Geosite: "https://hub.example/geosite/",
		Geoip:   "https://hub.example/geoip/",
		List: []rulesetHubItem{{
			Name: "cn",
			Type: "geosite",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	service := newAppRuntimeService(nil, nil)
	resp, err := service.PreviewRuleSetHub(context.Background(), connect.NewRequest(&appv1.PreviewRuleSetHubRequest{
		Index:  0,
		Format: "source",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Msg.GetContent(), `"example.com"`) {
		t.Fatalf("expected preview content, got %s", resp.Msg.GetContent())
	}
}

func TestPreviewRuleSetHubValidation(t *testing.T) {
	tests := []struct {
		name    string
		hub     rulesetHub
		request appv1.PreviewRuleSetHubRequest
	}{
		{
			name: "index out of range",
			hub: rulesetHub{
				Geosite: "https://example.com/geosite/",
				List:    []rulesetHubItem{{Name: "cn", Type: "geosite"}},
			},
			request: appv1.PreviewRuleSetHubRequest{Index: 1, Format: "source"},
		},
		{
			name: "invalid format",
			hub: rulesetHub{
				Geosite: "https://example.com/geosite/",
				List:    []rulesetHubItem{{Name: "cn", Type: "geosite"}},
			},
			request: appv1.PreviewRuleSetHubRequest{Index: 0, Format: "other"},
		},
		{
			name: "invalid type",
			hub: rulesetHub{
				Geosite: "https://example.com/geosite/",
				List:    []rulesetHubItem{{Name: "cn", Type: "other"}},
			},
			request: appv1.PreviewRuleSetHubRequest{Index: 0, Format: "source"},
		},
	}

	for index := range tests {
		test := &tests[index]
		t.Run(test.name, func(t *testing.T) {
			withTempBasePath(t)
			if err := writeRuleSetHubCache(test.hub); err != nil {
				t.Fatal(err)
			}
			service := newAppRuntimeService(nil, nil)
			_, err := service.PreviewRuleSetHub(context.Background(), connect.NewRequest(&test.request))
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestPreviewRuleSetHubMissingCache(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	_, err := service.PreviewRuleSetHub(context.Background(), connect.NewRequest(&appv1.PreviewRuleSetHubRequest{
		Index:  0,
		Format: "source",
	}))
	if err == nil {
		t.Fatal("expected missing cache error")
	}
}

func TestDeleteRuleSetRemovesManagedFile(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	_, err := putRuleSetForTest(service, `{"id":"manual","tag":"Manual","type":"Manual","format":"source"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SaveRuleSetContent(context.Background(), connect.NewRequest(&appv1.SaveRuleSetContentRequest{
		Id:               "manual",
		Content:          `{"version":1,"rules":[]}`,
		ExpectedRevision: rulesetRevision(t, service, "manual"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GetPath("data/rulesets/manual.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteRuleSet(context.Background(), connect.NewRequest(&appv1.DeleteRuleSetRequest{
		Id:               "manual",
		ExpectedRevision: rulesetRevision(t, service, "manual"),
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GetPath("data/rulesets/manual.json")); !os.IsNotExist(err) {
		t.Fatalf("expected managed file to be removed, got %v", err)
	}
}

func TestUpdateRuleSetFormatChangesManagedPath(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	_, err := putRuleSetForTest(service, `{"id":"switch","tag":"Switch","type":"Http","format":"binary","url":"https://example.com/a.srs"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(GetPath("data/rulesets/switch.srs")), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GetPath("data/rulesets/switch.srs"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	item, err := putRuleSetForTest(service, `{"id":"switch","tag":"Switch","type":"Http","format":"source","url":"https://example.com/a.json","path":"data/evil.json"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(item, `"path":"data/rulesets/switch.json"`) {
		t.Fatalf("expected managed source path, got %s", item)
	}
	if _, err := os.Stat(GetPath("data/rulesets/switch.json")); err != nil {
		t.Fatalf("expected migrated ruleset file: %v", err)
	}
}

func TestNextCronRunsKeepsSteppedSeconds(t *testing.T) {
	from := time.Date(2026, 6, 16, 10, 40, 58, 0, time.UTC)

	runs, err := nextCronRuns("*/10 41 * * * *", 3, from)
	if err != nil {
		t.Fatal(err)
	}

	first := time.UnixMilli(runs[0]).UTC()
	second := time.UnixMilli(runs[1]).UTC()
	if first.Second() != 0 || second.Second() != 10 {
		t.Fatalf("expected stepped seconds to remain second-level, got %s then %s", first, second)
	}
}

func withTempBasePath(t *testing.T) {
	t.Helper()
	previous := runtimePaths.Load()
	runtimePaths.Store(storage.NewPaths(t.TempDir()))
	t.Cleanup(func() {
		runtimePaths.Store(previous)
	})
}

type recordingRuntimeEvents struct {
	events []struct {
		name string
		data []any
	}
}

func (e *recordingRuntimeEvents) Publish(name string, data ...any) {
	e.events = append(e.events, struct {
		name string
		data []any
	}{name: name, data: data})
}

func TestScheduledTaskTypeValidation(t *testing.T) {
	validTypes := []string{
		scheduledTaskUpdateSubscription,
		scheduledTaskUpdateRuleset,
		scheduledTaskUpdateAllSubscription,
		scheduledTaskUpdateAllRuleset,
	}
	for _, taskType := range validTypes {
		t.Run("accepts "+taskType, func(t *testing.T) {
			raw, err := json.Marshal(scheduledTask{ID: "task", Type: taskType})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeScheduledTask(string(raw)); err != nil {
				t.Fatalf("expected supported type, got %v", err)
			}
		})
	}

	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	invalidTypes := []string{"", "run::script", "unknown::task"}
	for _, taskType := range invalidTypes {
		t.Run("rejects "+taskType, func(t *testing.T) {
			raw, err := json.Marshal(scheduledTask{ID: "task", Type: taskType})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.CreateScheduledTask(context.Background(), connect.NewRequest(&appv1.CreateScheduledTaskRequest{TaskJson: string(raw)}))
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("create unsupported task type code = %v, want %v", connect.CodeOf(err), connect.CodeInvalidArgument)
			}
			_, err = service.UpdateScheduledTask(context.Background(), connect.NewRequest(&appv1.UpdateScheduledTaskRequest{TaskJson: string(raw)}))
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("update unsupported task type code = %v, want %v", connect.CodeOf(err), connect.CodeInvalidArgument)
			}
		})
	}

	response, err := service.CreateScheduledTask(context.Background(), connect.NewRequest(&appv1.CreateScheduledTaskRequest{
		TaskJson: `{"id":"supported","type":"update::all::subscription","script":"ignored"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.Msg.GetTaskJson(), `"script"`) {
		t.Fatalf("scheduled task response retained removed script field: %s", response.Msg.GetTaskJson())
	}
}

func TestUnsupportedScheduledTaskIsNotSchedulableAndFailsManualRun(t *testing.T) {
	withTempBasePath(t)
	if err := saveScheduledTasks([]scheduledTask{{
		ID:   "unsupported",
		Name: "Unsupported",
		Type: "run::script",
		Cron: "* * * * * *",
	}}); err != nil {
		t.Fatal(err)
	}

	tasks, err := loadScheduledTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one persisted task, got %d", len(tasks))
	}
	if shouldScheduleTask(tasks[0]) {
		t.Fatal("unsupported persisted task must not be scheduled")
	}

	service := newAppRuntimeService(nil, nil)
	response, err := service.RunScheduledTask(context.Background(), connect.NewRequest(&appv1.RunScheduledTaskRequest{Id: "unsupported"}))
	if err != nil {
		t.Fatal(err)
	}
	results := response.Msg.GetResults()
	if len(results) != 1 || results[0].GetOk() || results[0].GetResult() != "unsupported scheduled task type: run::script" {
		t.Fatalf("unexpected unsupported task result: %#v", results)
	}
}

func TestTaskResultMetadataRoundTripsScheduledTaskStorage(t *testing.T) {
	want := &appv1.TaskResult{
		Ok:            false,
		Id:            "subscription",
		Name:          "Subscription",
		Result:        "failed",
		SuccessCount:  2,
		FilteredCount: 3,
		SkippedCount:  4,
		FailureReason: "reason",
	}
	got := taskResultsToProto(taskResultsToRuntime([]*appv1.TaskResult{want}))[0]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("task result metadata did not round trip: got %#v, want %#v", got, want)
	}
}

func TestScheduledTaskLogsDefaultLimitPersists(t *testing.T) {
	withTempBasePath(t)
	if err := writeRuntimeYAMLFile(scheduledTasksPath, []map[string]any{{"id": "task-1", "name": "Task"}}); err != nil {
		t.Fatal(err)
	}
	service := newAppRuntimeService(nil, nil)

	for i := 0; i < 25; i++ {
		service.recordTaskLog("task-1", "Task", int64(i), int64(i), []*appv1.TaskResult{taskResult(true, "r", "R", "ok")})
	}

	logs, err := loadScheduledTaskLogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != defaultScheduledTaskLogLimit {
		t.Fatalf("expected default limit %d, got %d", defaultScheduledTaskLogLimit, len(logs))
	}
	if logs[0].StartTime != 24 {
		t.Fatalf("expected newest log first, got %d", logs[0].StartTime)
	}
}

func TestScheduledTaskLogsRespectTaskLimit(t *testing.T) {
	withTempBasePath(t)
	if err := saveScheduledTasks([]scheduledTask{{ID: "task-1", Name: "Task", LogLimit: 3}}); err != nil {
		t.Fatal(err)
	}
	service := newAppRuntimeService(nil, nil)

	for i := 0; i < 5; i++ {
		service.recordTaskLog("task-1", "Task", int64(i), int64(i), []*appv1.TaskResult{taskResult(true, "r", "R", "ok")})
	}

	logs, err := loadScheduledTaskLogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}
	if logs[0].StartTime != 4 || logs[2].StartTime != 2 {
		t.Fatalf("expected latest three logs, got %#v", logs)
	}
}

func TestUpdateScheduledTaskTrimsExistingLogs(t *testing.T) {
	withTempBasePath(t)
	if err := saveScheduledTasks([]scheduledTask{{ID: "task-1", Name: "Task", LogLimit: 5}}); err != nil {
		t.Fatal(err)
	}
	service := newAppRuntimeService(nil, nil)
	for i := 0; i < 5; i++ {
		service.recordTaskLog("task-1", "Task", int64(i), int64(i), []*appv1.TaskResult{taskResult(true, "r", "R", "ok")})
	}

	_, err := putScheduledTaskForTest(service, `{"id":"task-1","name":"Task","type":"update::all::subscription","cron":"0 * * * * *","logLimit":2,"disabled":true}`)
	if err != nil {
		t.Fatal(err)
	}

	logs, err := loadScheduledTaskLogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected trimmed logs to 2, got %d", len(logs))
	}
}

func TestDeleteAndClearScheduledTaskLogsPersist(t *testing.T) {
	withTempBasePath(t)
	if err := saveScheduledTasks([]scheduledTask{{ID: "task-1", Name: "Task", LogLimit: 3}}); err != nil {
		t.Fatal(err)
	}
	service := newAppRuntimeService(nil, nil)
	service.recordTaskLog("task-1", "Task", 1, 1, []*appv1.TaskResult{taskResult(true, "r", "R", "ok")})

	_, err := service.DeleteScheduledTask(context.Background(), connect.NewRequest(&appv1.DeleteScheduledTaskRequest{
		Id:               "task-1",
		ExpectedRevision: scheduledTaskRevision(t, service, "task-1"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	logs, err := loadScheduledTaskLogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected logs removed after delete, got %d", len(logs))
	}

	service.recordTaskLog("orphan", "Orphan", 1, 1, []*appv1.TaskResult{taskResult(true, "r", "R", "ok")})
	_, err = service.ClearScheduledTaskLogs(context.Background(), connect.NewRequest(&appv1.ClearScheduledTaskLogsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	logs, err = loadScheduledTaskLogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected logs cleared, got %d", len(logs))
	}
}

func TestRunScheduledTaskResponseEndTimeUpdatesLastTime(t *testing.T) {
	withTempBasePath(t)
	if err := saveScheduledTasks([]scheduledTask{{
		ID:       "task-1",
		Name:     "Task",
		Type:     scheduledTaskUpdateAllSubscription,
		Cron:     "0 * * * * *",
		LogLimit: 3,
	}}); err != nil {
		t.Fatal(err)
	}
	service := newAppRuntimeService(nil, nil)

	resp, err := service.RunScheduledTask(context.Background(), connect.NewRequest(&appv1.RunScheduledTaskRequest{Id: "task-1"}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetStartTime() == 0 || resp.Msg.GetEndTime() == 0 {
		t.Fatalf("expected response start/end time, got %d/%d", resp.Msg.GetStartTime(), resp.Msg.GetEndTime())
	}
	if resp.Msg.GetEndTime() < resp.Msg.GetStartTime() {
		t.Fatalf("expected end time >= start time, got %d/%d", resp.Msg.GetStartTime(), resp.Msg.GetEndTime())
	}

	tasks, err := loadScheduledTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(tasks))
	}
	if tasks[0].LastTime != resp.Msg.GetEndTime() {
		t.Fatalf("expected lastTime to equal response endTime, got %d != %d", tasks[0].LastTime, resp.Msg.GetEndTime())
	}

	logs, err := loadScheduledTaskLogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one persisted log, got %d", len(logs))
	}
	if logs[0].EndTime != resp.Msg.GetEndTime() {
		t.Fatalf("expected persisted log endTime to equal response endTime, got %d != %d", logs[0].EndTime, resp.Msg.GetEndTime())
	}
}

func TestScheduledTaskFinishedPublishesNotificationIntent(t *testing.T) {
	withTempBasePath(t)
	if err := saveScheduledTasks([]scheduledTask{{
		ID:           "task-1",
		Name:         "Task",
		Type:         scheduledTaskUpdateAllSubscription,
		Cron:         "0 * * * * *",
		LogLimit:     3,
		Notification: true,
	}}); err != nil {
		t.Fatal(err)
	}
	events := &recordingRuntimeEvents{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, nil)

	if _, err := service.RunScheduledTask(context.Background(), connect.NewRequest(&appv1.RunScheduledTaskRequest{Id: "task-1"})); err != nil {
		t.Fatal(err)
	}
	finished := func() []struct {
		name string
		data []any
	} {
		var result []struct {
			name string
			data []any
		}
		for _, item := range events.events {
			if item.name == "scheduledTaskFinished" {
				result = append(result, item)
			}
		}
		return result
	}
	if got := finished(); len(got) != 1 || len(got[0].data) != 2 || got[0].data[0] != "task-1" || got[0].data[1] != false {
		t.Fatalf("manual completion event = %#v", got)
	}

	if _, err := service.runScheduledTask(context.Background(), "task-1", true); err != nil {
		t.Fatal(err)
	}
	if got := finished(); len(got) != 2 || len(got[1].data) != 2 || got[1].data[0] != "task-1" || got[1].data[1] != true {
		t.Fatalf("unexpected background completion event: %#v", got)
	}

	service.mu.Lock()
	service.runningTask["task-1"] = true
	service.mu.Unlock()
	if _, err := service.runScheduledTask(context.Background(), "task-1", true); err != nil {
		t.Fatal(err)
	}
	if got := finished(); len(got) != 3 || got[2].data[1] != true {
		t.Fatalf("skipped background run did not publish completion: %#v", got)
	}
}

func TestScheduledTaskMutationsPublishResourceEvents(t *testing.T) {
	withTempBasePath(t)
	events := &recordingRuntimeEvents{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, nil)
	task := scheduledTask{
		ID:       "task-1",
		Name:     "Task",
		Type:     scheduledTaskUpdateAllSubscription,
		Cron:     "0 * * * * *",
		Disabled: true,
	}
	taskJSON, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := putScheduledTaskForTest(service, string(taskJSON)); err != nil {
		t.Fatal(err)
	}
	task.Name = "Updated"
	taskJSON, _ = json.Marshal(task)
	if _, err := putScheduledTaskForTest(service, string(taskJSON)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteScheduledTask(context.Background(), connect.NewRequest(&appv1.DeleteScheduledTaskRequest{
		Id:               task.ID,
		ExpectedRevision: scheduledTaskRevision(t, service, task.ID),
	})); err != nil {
		t.Fatal(err)
	}

	wantOperations := []string{"upsert", "upsert", "delete"}
	configurationEvents := make([]struct {
		name string
		data []any
	}, 0, len(wantOperations))
	for _, event := range events.events {
		if event.name == "resourceChanged" {
			configurationEvents = append(configurationEvents, event)
		}
	}
	if len(configurationEvents) != len(wantOperations) {
		t.Fatalf("configuration events = %#v", events.events)
	}
	for i, wantOperation := range wantOperations {
		event := configurationEvents[i]
		if event.name != "resourceChanged" || len(event.data) != 1 {
			t.Fatalf("event %d = %#v", i, event)
		}
		payload, ok := event.data[0].(map[string]any)
		if !ok || payload["domain"] != "scheduledTasks" || payload["operation"] != wantOperation {
			t.Fatalf("event %d payload = %#v, want operation %q", i, event.data, wantOperation)
		}
	}

	if _, err := putScheduledTaskForTest(service, "{invalid"); err == nil {
		t.Fatal("expected invalid task error")
	}
	if len(events.events) != len(wantOperations) {
		t.Fatalf("failed mutation published an event: %#v", events.events)
	}
}

func TestConcurrentRuleSetCreatesPreserveBothItems(t *testing.T) {
	withTempBasePath(t)
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	requests := []ruleset{
		{ID: "ruleset-1", Tag: "First", Type: "Http", Format: "binary", URL: "https://example.com/1.srs"},
		{ID: "ruleset-2", Tag: "Second", Type: "Http", Format: "binary", URL: "https://example.com/2.srs"},
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(requests))
	for _, item := range requests {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, err := json.Marshal(item)
			if err == nil {
				_, err = putRuleSetForTest(service, string(raw))
			}
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}

	items, err := loadRulesets()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("rulesets = %#v", items)
	}
}

func TestRuleSetUpdateSerializesWithConfigurationEdit(t *testing.T) {
	withTempBasePath(t)
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"version":1,"rules":[{"domain":["example.com"]}]}`))
	}))
	defer server.Close()

	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	item := ruleset{ID: "ruleset-1", Tag: "Original", Type: "Http", Format: "source", URL: server.URL}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := putRuleSetForTest(service, string(raw)); err != nil {
		t.Fatal(err)
	}

	updateDone := make(chan error, 1)
	go func() {
		_, err := service.UpdateRuleSet(context.Background(), connect.NewRequest(&appv1.UpdateRuleSetRequest{Id: item.ID}))
		updateDone <- err
	}()
	<-started

	item.Tag = "Edited"
	item.Count = 1
	raw, _ = json.Marshal(item)
	editDone := make(chan error, 1)
	go func() {
		_, err := putRuleSetForTest(service, string(raw))
		editDone <- err
	}()
	select {
	case err := <-editDone:
		t.Fatalf("configuration edit was not serialized with update: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(release)
	if err := <-updateDone; err != nil {
		t.Fatal(err)
	}
	if err := <-editDone; err != nil {
		t.Fatal(err)
	}
	items, err := loadRulesets()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Tag != item.Tag || items[0].Count != 1 {
		t.Fatalf("ruleset configuration = %#v", items)
	}
	content, err := os.ReadFile(GetPath(items[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(content, &decoded); err != nil || decoded["rules"] == nil {
		t.Fatalf("ruleset content is invalid: %v\n%s", err, content)
	}
}

func TestConcurrentScheduledTaskCreatesPreserveBothItems(t *testing.T) {
	withTempBasePath(t)
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	requests := []scheduledTask{
		{ID: "task-1", Name: "First", Type: scheduledTaskUpdateAllSubscription, Cron: "0 * * * * *", Disabled: true},
		{ID: "task-2", Name: "Second", Type: scheduledTaskUpdateAllSubscription, Cron: "0 * * * * *", Disabled: true},
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(requests))
	for _, task := range requests {
		task := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, err := json.Marshal(task)
			if err == nil {
				_, err = putScheduledTaskForTest(service, string(raw))
			}
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}

	items, err := loadScheduledTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("scheduled tasks = %#v", items)
	}
}

func TestScheduledTaskCompletionMergesLastTimeIntoLatestConfiguration(t *testing.T) {
	withTempBasePath(t)
	if err := saveSubscriptions([]subscription{{
		ID: "subscription-1", Name: "Subscription", Type: "Http", URL: "https://example.com/subscription",
	}}); err != nil {
		t.Fatal(err)
	}
	task := scheduledTask{
		ID:            "task-1",
		Name:          "Original",
		Type:          scheduledTaskUpdateSubscription,
		Cron:          "0 * * * * *",
		Subscriptions: []string{"subscription-1"},
		LogLimit:      3,
	}
	if err := saveScheduledTasks([]scheduledTask{task}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	previousRequest := subscriptionHTTPRequest
	subscriptionHTTPRequest = func(string, string, map[string]string, string, bool, int) (*http.Response, string, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, `{"outbounds":[{"type":"direct","tag":"direct"}]}`, nil
	}
	t.Cleanup(func() { subscriptionHTTPRequest = previousRequest })

	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	runErrors := make(chan error, 1)
	go func() {
		_, err := service.RunScheduledTask(context.Background(), connect.NewRequest(&appv1.RunScheduledTaskRequest{Id: task.ID}))
		runErrors <- err
	}()
	<-started

	task.Name = "Edited while running"
	task.Disabled = true
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := putScheduledTaskForTest(service, string(raw)); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-runErrors; err != nil {
		t.Fatal(err)
	}

	items, err := loadScheduledTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != task.Name || !items[0].Disabled || items[0].LastTime == 0 {
		t.Fatalf("task completion overwrote latest configuration: %#v", items)
	}
}

func TestScheduledTaskCompletionDoesNotRestoreDeletedTask(t *testing.T) {
	withTempBasePath(t)
	if err := saveSubscriptions([]subscription{{
		ID: "subscription-1", Name: "Subscription", Type: "Http", URL: "https://example.com/subscription",
	}}); err != nil {
		t.Fatal(err)
	}
	task := scheduledTask{
		ID:            "task-1",
		Name:          "Task",
		Type:          scheduledTaskUpdateSubscription,
		Cron:          "0 * * * * *",
		Subscriptions: []string{"subscription-1"},
	}
	if err := saveScheduledTasks([]scheduledTask{task}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	previousRequest := subscriptionHTTPRequest
	subscriptionHTTPRequest = func(string, string, map[string]string, string, bool, int) (*http.Response, string, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, `{"outbounds":[{"type":"direct","tag":"direct"}]}`, nil
	}
	t.Cleanup(func() { subscriptionHTTPRequest = previousRequest })

	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	runErrors := make(chan error, 1)
	go func() {
		_, err := service.RunScheduledTask(context.Background(), connect.NewRequest(&appv1.RunScheduledTaskRequest{Id: task.ID}))
		runErrors <- err
	}()
	<-started
	if _, err := service.DeleteScheduledTask(context.Background(), connect.NewRequest(&appv1.DeleteScheduledTaskRequest{
		Id:               task.ID,
		ExpectedRevision: scheduledTaskRevision(t, service, task.ID),
	})); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-runErrors; err != nil {
		t.Fatal(err)
	}

	items, err := loadScheduledTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("completed task was restored after deletion: %#v", items)
	}
}

func TestSubscriptionVersionConflictsAndManagedFields(t *testing.T) {
	withTempBasePath(t)
	if err := saveSubscriptions([]subscription{{
		ID: "first", Name: "First", Type: "Http", URL: "https://example.com/first",
		Upload: 1, Download: 2, Total: 3, Expire: 4, UpdateTime: 5,
		Proxies: []proxyRef{{ID: "proxy", Tag: "Proxy", Type: "direct"}},
	}, {
		ID: "second", Name: "Second", Type: "Manual",
	}}); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	snapshot, err := service.ListSubscriptions(context.Background(), connect.NewRequest(&appv1.ListSubscriptionsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	firstRevision := subscriptionRevision(t, service, "first")
	secondRevision := subscriptionRevision(t, service, "second")

	updated, err := service.UpdateSubscriptionConfig(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionConfigRequest{
		SubscriptionJson: `{"id":"first","name":"First edited","type":"Http","url":"https://example.com/edited"}`,
		ExpectedRevision: firstRevision,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var normalized subscription
	if err := json.Unmarshal([]byte(updated.Msg.GetSubscriptionJson()), &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.Upload != 1 || normalized.Download != 2 || normalized.Total != 3 || normalized.Expire != 4 || normalized.UpdateTime != 5 || len(normalized.Proxies) != 1 {
		t.Fatalf("server-managed subscription fields were overwritten: %#v", normalized)
	}

	_, err = service.UpdateSubscriptionConfig(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionConfigRequest{
		SubscriptionJson: `{"id":"first","name":"stale overwrite","type":"Http","url":"https://example.com/stale"}`,
		ExpectedRevision: firstRevision,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale subscription update code = %v", connect.CodeOf(err))
	}
	if _, err := service.UpdateSubscriptionConfig(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionConfigRequest{
		SubscriptionJson: `{"id":"second","name":"Second edited","type":"Manual"}`,
		ExpectedRevision: secondRevision,
	})); err != nil {
		t.Fatalf("editing another subscription should succeed: %v", err)
	}
	if updated.Msg.GetState().GetStateRevision() <= snapshot.Msg.GetState().GetStateRevision() {
		t.Fatal("subscription configuration did not advance state")
	}

	restarted := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	_, err = restarted.UpdateSubscriptionConfig(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionConfigRequest{
		SubscriptionJson: `{"id":"first","name":"after restart","type":"Http","url":"https://example.com/edited"}`,
		ExpectedRevision: mutationRevision(updated.Msg.GetState()),
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("old subscription instance revision code = %v", connect.CodeOf(err))
	}
}

func TestSubscriptionRuntimeUpdateDoesNotConflictUnlessScriptChangesConfig(t *testing.T) {
	withTempBasePath(t)
	previousRequest := subscriptionHTTPRequest
	subscriptionHTTPRequest = func(string, string, map[string]string, string, bool, int) (*http.Response, string, error) {
		header := make(http.Header)
		header.Set("Subscription-Userinfo", "upload=10; download=20; total=100; expire=2000000000")
		return &http.Response{StatusCode: http.StatusOK, Header: header}, `{"outbounds":[{"type":"direct","tag":"direct"}]}`, nil
	}
	t.Cleanup(func() { subscriptionHTTPRequest = previousRequest })

	events := &recordingRuntimeEvents{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, nil)
	created, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
		SubscriptionJson: `{"id":"runtime","name":"Runtime","type":"Http","url":"https://example.com/subscription"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	baseRevision := mutationRevision(created.Msg.GetState())
	events.events = nil
	downloaded, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "runtime"}))
	if err != nil {
		t.Fatal(err)
	}
	if downloaded.Msg.GetState().GetItemRevision() != baseRevision.GetRevision() {
		t.Fatalf("runtime update changed entity revision: %#v", downloaded.Msg.GetState())
	}
	if len(events.events) != 1 || events.events[0].name != "resourceChanged" || events.events[0].data[0].(map[string]any)["operation"] != "runtime" {
		t.Fatalf("runtime subscription event = %#v", events.events)
	}

	configured, err := service.UpdateSubscriptionConfig(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionConfigRequest{
		SubscriptionJson: `{"id":"runtime","name":"Edited after download","type":"Http","url":"https://example.com/subscription","script":"function onSubscribe(proxies, subscription) { subscription.Name = 'Changed by script'; return { proxies, subscription }; }"}`,
		ExpectedRevision: baseRevision,
	}))
	if err != nil {
		t.Fatalf("runtime fields caused a configuration conflict: %v", err)
	}
	var normalized subscription
	if err := json.Unmarshal([]byte(configured.Msg.GetSubscriptionJson()), &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.Upload != 10 || normalized.Download != 20 || normalized.Total != 100 || normalized.Expire == 0 || len(normalized.Proxies) != 1 {
		t.Fatalf("configuration edit did not preserve downloaded fields: %#v", normalized)
	}

	events.events = nil
	scripted, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "runtime"}))
	if err != nil {
		t.Fatal(err)
	}
	if scripted.Msg.GetState().GetItemRevision() <= configured.Msg.GetState().GetItemRevision() {
		t.Fatalf("script configuration change did not advance entity revision: %#v", scripted.Msg.GetState())
	}
	if len(events.events) != 1 || events.events[0].data[0].(map[string]any)["operation"] != "upsert" {
		t.Fatalf("scripted subscription event = %#v", events.events)
	}
}

func TestSubscriptionContentAndOrderVersionsAreIndependent(t *testing.T) {
	withTempBasePath(t)
	events := &recordingRuntimeEvents{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, nil)
	for _, id := range []string{"first", "second"} {
		if _, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
			SubscriptionJson: `{"id":"` + id + `","name":"` + id + `","type":"Manual"}`,
		})); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := service.ListSubscriptions(context.Background(), connect.NewRequest(&appv1.ListSubscriptionsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	orderRevision := &commonv1.ExpectedRevision{
		InstanceId: snapshot.Msg.GetState().GetInstanceId(), Revision: snapshot.Msg.GetState().GetOrderRevision(),
	}
	content, err := service.GetSubscriptionContent(context.Background(), connect.NewRequest(&appv1.GetSubscriptionContentRequest{Id: "first"}))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id: "first", Content: `[{"type":"direct","tag":"direct"}]`, ProxyIds: []string{"ID_direct"}, ExpectedRevision: content.Msg.GetRevision(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := service.ReorderSubscriptions(context.Background(), connect.NewRequest(&appv1.ReorderSubscriptionsRequest{
		Ids: []string{"second", "first"}, ExpectedOrderRevision: orderRevision,
	}))
	if err != nil {
		t.Fatalf("content edit should not conflict with subscription reorder: %v", err)
	}
	_, err = service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id: "first", Content: `[{"type":"block","tag":"stale"}]`, ProxyIds: []string{"ID_direct"}, ExpectedRevision: content.Msg.GetRevision(),
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale subscription content code = %v", connect.CodeOf(err))
	}

	events.events = nil
	noOp, err := service.SaveSubscriptionContent(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionContentRequest{
		Id: "first", Content: `[{"type":"direct","tag":"direct"}]`, ProxyIds: []string{"ID_direct"}, ExpectedRevision: mutationRevision(saved.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if noOp.Msg.GetState().GetStateRevision() != reordered.Msg.GetState().GetStateRevision() || len(events.events) != 0 {
		t.Fatalf("no-op subscription content advanced state or published event: state=%#v events=%#v", noOp.Msg.GetState(), events.events)
	}

	latest, err := service.ListSubscriptions(context.Background(), connect.NewRequest(&appv1.ListSubscriptionsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	staleOrder := &commonv1.ExpectedRevision{
		InstanceId: latest.Msg.GetState().GetInstanceId(), Revision: latest.Msg.GetState().GetOrderRevision(),
	}
	if _, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
		SubscriptionJson: `{"id":"third","name":"third","type":"Manual"}`,
	})); err != nil {
		t.Fatal(err)
	}
	_, err = service.ReorderSubscriptions(context.Background(), connect.NewRequest(&appv1.ReorderSubscriptionsRequest{
		Ids: []string{"first", "second"}, ExpectedOrderRevision: staleOrder,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("structural subscription/order conflict code = %v", connect.CodeOf(err))
	}
}

func TestRuleSetVersionConflictsAndRuntimeFields(t *testing.T) {
	withTempBasePath(t)
	if err := saveRulesets([]ruleset{{
		ID: "first", Tag: "First", Type: "Http", Format: "source", URL: "https://example.com/first.json",
		Path: "data/rulesets/first.json", Count: 7, UpdateTime: 123,
	}, {
		ID: "second", Tag: "Second", Type: "Http", Format: "binary", URL: "https://example.com/second.srs",
		Path: "data/rulesets/second.srs",
	}}); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	snapshot, err := service.ListRuleSets(context.Background(), connect.NewRequest(&appv1.ListRuleSetsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	firstRevision := rulesetRevision(t, service, "first")
	secondRevision := rulesetRevision(t, service, "second")

	requested := ruleset{
		ID: "first", Tag: "First edited", Type: "Http", Format: "source", URL: "https://example.com/edited.json",
		Path: "data/client-controlled.json", Count: 0, UpdateTime: 0,
	}
	raw, _ := json.Marshal(requested)
	updated, err := service.UpdateRuleSetConfig(context.Background(), connect.NewRequest(&appv1.UpdateRuleSetConfigRequest{
		RulesetJson:      string(raw),
		ExpectedRevision: firstRevision,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var normalized ruleset
	if err := json.Unmarshal([]byte(updated.Msg.GetRulesetJson()), &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.Path != "data/rulesets/first.json" || normalized.Count != 7 || normalized.UpdateTime != 123 {
		t.Fatalf("server-managed fields were overwritten: %#v", normalized)
	}

	requested.Tag = "stale overwrite"
	raw, _ = json.Marshal(requested)
	_, err = service.UpdateRuleSetConfig(context.Background(), connect.NewRequest(&appv1.UpdateRuleSetConfigRequest{
		RulesetJson:      string(raw),
		ExpectedRevision: firstRevision,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale ruleset update code = %v", connect.CodeOf(err))
	}

	second := ruleset{ID: "second", Tag: "Second edited", Type: "Http", Format: "binary", URL: "https://example.com/second.srs"}
	raw, _ = json.Marshal(second)
	if _, err := service.UpdateRuleSetConfig(context.Background(), connect.NewRequest(&appv1.UpdateRuleSetConfigRequest{
		RulesetJson:      string(raw),
		ExpectedRevision: secondRevision,
	})); err != nil {
		t.Fatalf("editing another ruleset should succeed: %v", err)
	}

	if updated.Msg.GetState().GetStateRevision() <= snapshot.Msg.GetState().GetStateRevision() {
		t.Fatalf("state revision did not advance: before=%d after=%d", snapshot.Msg.GetState().GetStateRevision(), updated.Msg.GetState().GetStateRevision())
	}
}

func TestRuleSetRuntimeUpdateDoesNotConflictWithConfigurationEdit(t *testing.T) {
	withTempBasePath(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":2,"rules":[{"domain":["example.com"]}]}`))
	}))
	defer server.Close()

	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	created, err := service.CreateRuleSet(context.Background(), connect.NewRequest(&appv1.CreateRuleSetRequest{
		RulesetJson: `{"id":"runtime","tag":"Runtime","type":"Http","format":"source","url":"` + server.URL + `"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	baseRevision := mutationRevision(created.Msg.GetState())
	before := created.Msg.GetState().GetStateRevision()
	downloaded, err := service.UpdateRuleSet(context.Background(), connect.NewRequest(&appv1.UpdateRuleSetRequest{Id: "runtime"}))
	if err != nil {
		t.Fatal(err)
	}
	if downloaded.Msg.GetState().GetStateRevision() <= before {
		t.Fatal("runtime update did not advance visible state")
	}
	if downloaded.Msg.GetState().GetItemRevision() != baseRevision.GetRevision() {
		t.Fatalf("runtime update changed entity revision: %#v", downloaded.Msg.GetState())
	}

	requested := ruleset{ID: "runtime", Tag: "Edited after download", Type: "Http", Format: "source", URL: server.URL}
	raw, _ := json.Marshal(requested)
	if _, err := service.UpdateRuleSetConfig(context.Background(), connect.NewRequest(&appv1.UpdateRuleSetConfigRequest{
		RulesetJson:      string(raw),
		ExpectedRevision: baseRevision,
	})); err != nil {
		t.Fatalf("runtime fields caused an edit conflict: %v", err)
	}
}

func TestRuleSetOrderRevisionConflictsWithStructuralChanges(t *testing.T) {
	withTempBasePath(t)
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	for _, id := range []string{"first", "second"} {
		if _, err := service.CreateRuleSet(context.Background(), connect.NewRequest(&appv1.CreateRuleSetRequest{
			RulesetJson: `{"id":"` + id + `","tag":"` + id + `","type":"Http","format":"binary","url":"https://example.com/` + id + `.srs"}`,
		})); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := service.ListRuleSets(context.Background(), connect.NewRequest(&appv1.ListRuleSetsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	orderRevision := &commonv1.ExpectedRevision{
		InstanceId: snapshot.Msg.GetState().GetInstanceId(), Revision: snapshot.Msg.GetState().GetOrderRevision(),
	}

	first := ruleset{ID: "first", Tag: "edited", Type: "Http", Format: "binary", URL: "https://example.com/first.srs"}
	raw, _ := json.Marshal(first)
	if _, err := service.UpdateRuleSetConfig(context.Background(), connect.NewRequest(&appv1.UpdateRuleSetConfigRequest{
		RulesetJson: string(raw), ExpectedRevision: rulesetRevision(t, service, "first"),
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReorderRuleSets(context.Background(), connect.NewRequest(&appv1.ReorderRuleSetsRequest{
		Ids: []string{"second", "first"}, ExpectedOrderRevision: orderRevision,
	})); err != nil {
		t.Fatalf("content edit should not conflict with reorder: %v", err)
	}
	_, err = service.ReorderRuleSets(context.Background(), connect.NewRequest(&appv1.ReorderRuleSetsRequest{
		Ids: []string{"first", "second"}, ExpectedOrderRevision: orderRevision,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale ruleset order code = %v", connect.CodeOf(err))
	}

	latest, err := service.ListRuleSets(context.Background(), connect.NewRequest(&appv1.ListRuleSetsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	beforeCreate := &commonv1.ExpectedRevision{
		InstanceId: latest.Msg.GetState().GetInstanceId(), Revision: latest.Msg.GetState().GetOrderRevision(),
	}
	if _, err := service.CreateRuleSet(context.Background(), connect.NewRequest(&appv1.CreateRuleSetRequest{
		RulesetJson: `{"id":"third","tag":"third","type":"Http","format":"binary","url":"https://example.com/third.srs"}`,
	})); err != nil {
		t.Fatal(err)
	}
	_, err = service.ReorderRuleSets(context.Background(), connect.NewRequest(&appv1.ReorderRuleSetsRequest{
		Ids: []string{"first", "second"}, ExpectedOrderRevision: beforeCreate,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("create/ruleset order conflict code = %v", connect.CodeOf(err))
	}
}

func TestScheduledTaskVersionsPreserveRuntimeState(t *testing.T) {
	withTempBasePath(t)
	if err := saveScheduledTasks([]scheduledTask{{
		ID: "first", Name: "First", Type: scheduledTaskUpdateAllSubscription, Cron: "0 * * * * *", Disabled: true, LastTime: 123,
	}, {
		ID: "second", Name: "Second", Type: scheduledTaskUpdateAllSubscription, Cron: "0 * * * * *", Disabled: true,
	}}); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	firstRevision := scheduledTaskRevision(t, service, "first")
	secondRevision := scheduledTaskRevision(t, service, "second")
	requested := scheduledTask{
		ID: "first", Name: "First edited", Type: scheduledTaskUpdateAllSubscription, Cron: "0 * * * * *", Disabled: true, LastTime: 0,
	}
	raw, _ := json.Marshal(requested)
	updated, err := service.UpdateScheduledTask(context.Background(), connect.NewRequest(&appv1.UpdateScheduledTaskRequest{
		TaskJson: string(raw), ExpectedRevision: firstRevision,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var normalized scheduledTask
	if err := json.Unmarshal([]byte(updated.Msg.GetTaskJson()), &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.LastTime != 123 {
		t.Fatalf("lastTime was overwritten: %#v", normalized)
	}

	requested.Name = "stale overwrite"
	raw, _ = json.Marshal(requested)
	_, err = service.UpdateScheduledTask(context.Background(), connect.NewRequest(&appv1.UpdateScheduledTaskRequest{
		TaskJson: string(raw), ExpectedRevision: firstRevision,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale scheduled task update code = %v", connect.CodeOf(err))
	}

	second := scheduledTask{ID: "second", Name: "Second edited", Type: scheduledTaskUpdateAllSubscription, Cron: "0 * * * * *", Disabled: true}
	raw, _ = json.Marshal(second)
	if _, err := service.UpdateScheduledTask(context.Background(), connect.NewRequest(&appv1.UpdateScheduledTaskRequest{
		TaskJson: string(raw), ExpectedRevision: secondRevision,
	})); err != nil {
		t.Fatalf("editing another scheduled task should succeed: %v", err)
	}

	if _, err := service.RunScheduledTask(context.Background(), connect.NewRequest(&appv1.RunScheduledTaskRequest{Id: "first"})); err != nil {
		t.Fatal(err)
	}
	requested.Name = "Edited after runtime update"
	raw, _ = json.Marshal(requested)
	if _, err := service.UpdateScheduledTask(context.Background(), connect.NewRequest(&appv1.UpdateScheduledTaskRequest{
		TaskJson: string(raw), ExpectedRevision: mutationRevision(updated.Msg.GetState()),
	})); err != nil {
		t.Fatalf("lastTime update caused an edit conflict: %v", err)
	}
}

func TestNarrowNoOpUpdatesDoNotAdvanceVersionsOrPublishEvents(t *testing.T) {
	withTempBasePath(t)
	events := &recordingRuntimeEvents{}
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, events, nil)

	subscriptionResponse, err := service.CreateSubscription(context.Background(), connect.NewRequest(&appv1.CreateSubscriptionRequest{
		SubscriptionJson: `{"id":"subscription","name":"Subscription","type":"Manual"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	events.events = nil
	subscriptionNoOp, err := service.UpdateSubscriptionConfig(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionConfigRequest{
		SubscriptionJson: subscriptionResponse.Msg.GetSubscriptionJson(),
		ExpectedRevision: mutationRevision(subscriptionResponse.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if subscriptionNoOp.Msg.GetState().GetStateRevision() != subscriptionResponse.Msg.GetState().GetStateRevision() || len(events.events) != 0 {
		t.Fatalf("subscription no-op advanced state or published events: state=%#v events=%#v", subscriptionNoOp.Msg.GetState(), events.events)
	}

	rulesetResponse, err := service.CreateRuleSet(context.Background(), connect.NewRequest(&appv1.CreateRuleSetRequest{
		RulesetJson: `{"id":"ruleset","tag":"Ruleset","type":"Http","format":"binary","url":"https://example.com/ruleset.srs"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	events.events = nil
	rulesetNoOp, err := service.UpdateRuleSetConfig(context.Background(), connect.NewRequest(&appv1.UpdateRuleSetConfigRequest{
		RulesetJson:      rulesetResponse.Msg.GetRulesetJson(),
		ExpectedRevision: mutationRevision(rulesetResponse.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if rulesetNoOp.Msg.GetState().GetStateRevision() != rulesetResponse.Msg.GetState().GetStateRevision() || len(events.events) != 0 {
		t.Fatalf("ruleset no-op advanced state or published events: state=%#v events=%#v", rulesetNoOp.Msg.GetState(), events.events)
	}

	taskResponse, err := service.CreateScheduledTask(context.Background(), connect.NewRequest(&appv1.CreateScheduledTaskRequest{
		TaskJson: `{"id":"task","name":"Task","type":"update::all::subscription","cron":"0 * * * * *","disabled":true,"logLimit":20}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	events.events = nil
	taskNoOp, err := service.UpdateScheduledTask(context.Background(), connect.NewRequest(&appv1.UpdateScheduledTaskRequest{
		TaskJson:         taskResponse.Msg.GetTaskJson(),
		ExpectedRevision: mutationRevision(taskResponse.Msg.GetState()),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if taskNoOp.Msg.GetState().GetStateRevision() != taskResponse.Msg.GetState().GetStateRevision() || len(events.events) != 0 {
		t.Fatalf("scheduled task no-op advanced state or published events: state=%#v events=%#v", taskNoOp.Msg.GetState(), events.events)
	}
}

func TestScheduledTaskOrderVersionIsIndependentFromEntityVersions(t *testing.T) {
	withTempBasePath(t)
	service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
	for _, id := range []string{"first", "second"} {
		if _, err := service.CreateScheduledTask(context.Background(), connect.NewRequest(&appv1.CreateScheduledTaskRequest{
			TaskJson: `{"id":"` + id + `","name":"` + id + `","type":"update::all::subscription","cron":"0 * * * * *","disabled":true}`,
		})); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := service.ListScheduledTasks(context.Background(), connect.NewRequest(&appv1.ListScheduledTasksRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	orderRevision := &commonv1.ExpectedRevision{
		InstanceId: snapshot.Msg.GetState().GetInstanceId(), Revision: snapshot.Msg.GetState().GetOrderRevision(),
	}
	first := scheduledTask{ID: "first", Name: "edited", Type: scheduledTaskUpdateAllSubscription, Cron: "0 * * * * *", Disabled: true}
	raw, _ := json.Marshal(first)
	if _, err := service.UpdateScheduledTask(context.Background(), connect.NewRequest(&appv1.UpdateScheduledTaskRequest{
		TaskJson: string(raw), ExpectedRevision: scheduledTaskRevision(t, service, "first"),
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReorderScheduledTasks(context.Background(), connect.NewRequest(&appv1.ReorderScheduledTasksRequest{
		Ids: []string{"second", "first"}, ExpectedOrderRevision: orderRevision,
	})); err != nil {
		t.Fatalf("entity edit should not conflict with task reorder: %v", err)
	}
	_, err = service.ReorderScheduledTasks(context.Background(), connect.NewRequest(&appv1.ReorderScheduledTasksRequest{
		Ids: []string{"first", "second"}, ExpectedOrderRevision: orderRevision,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale scheduled task order code = %v", connect.CodeOf(err))
	}

	latest, err := service.ListScheduledTasks(context.Background(), connect.NewRequest(&appv1.ListScheduledTasksRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	beforeDelete := &commonv1.ExpectedRevision{
		InstanceId: latest.Msg.GetState().GetInstanceId(), Revision: latest.Msg.GetState().GetOrderRevision(),
	}
	if _, err := service.DeleteScheduledTask(context.Background(), connect.NewRequest(&appv1.DeleteScheduledTaskRequest{
		Id: "second", ExpectedRevision: scheduledTaskRevision(t, service, "second"),
	})); err != nil {
		t.Fatal(err)
	}
	_, err = service.ReorderScheduledTasks(context.Background(), connect.NewRequest(&appv1.ReorderScheduledTasksRequest{
		Ids: []string{"first", "second"}, ExpectedOrderRevision: beforeDelete,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("delete/task order conflict code = %v", connect.CodeOf(err))
	}
}
