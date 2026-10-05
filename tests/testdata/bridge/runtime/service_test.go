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

type staticAppConfig struct {
	value config.AppConfig
}

func (s staticAppConfig) Current() config.AppConfig {
	return s.value
}

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

func TestCreateManualSourceRuleSetDoesNotOverwriteExistingContent(t *testing.T) {
	withTempBasePath(t)
	service := newTestRuntimeService(nil)
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

func TestGetBinaryRuleSetContentDoesNotHoldRulesetLockWhileDecompiling(t *testing.T) {
	withTempBasePath(t)
	decompiler := &rulesetDecompilerStub{
		content: `{"version":2,"rules":[]}`,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newTestRuntimeService(decompiler)
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

func TestScheduledTaskLogsRespectTaskLimit(t *testing.T) {
	withTempBasePath(t)
	if err := saveScheduledTasks([]scheduledTask{{ID: "task-1", Name: "Task", LogLimit: 3}}); err != nil {
		t.Fatal(err)
	}
	service := newTestRuntimeService(nil)

	for i := 0; i < 5; i++ {
		service.recordTaskLog(context.Background(), "task-1", "Task", int64(i), int64(i), []*appv1.TaskResult{taskResult(true, "r", "R", "ok")})
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

func newTestRuntimeService(kernelController KernelController) *Service {
	paths := runtimePaths.Load()
	configStore, _ := config.NewStore(paths)
	return NewService(nil, paths, configStore, nil, kernelController)
}
