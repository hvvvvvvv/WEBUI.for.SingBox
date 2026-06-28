package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

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

func TestUpsertRuleSetIgnoresClientPath(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	resp, err := service.UpsertRuleSet(context.Background(), connect.NewRequest(&appv1.UpsertRuleSetRequest{
		RulesetJson: `{"id":"ruleset/one","tag":"One","type":"Http","format":"source","path":"data/evil.json","url":"https://example.com/rules.json"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resp.Msg.GetRulesetJson(), "evil") {
		t.Fatalf("client path should be ignored, got %s", resp.Msg.GetRulesetJson())
	}
	if !strings.Contains(resp.Msg.GetRulesetJson(), `"path":"data/rulesets/ruleset_one.json"`) {
		t.Fatalf("expected managed source path, got %s", resp.Msg.GetRulesetJson())
	}
}

func TestSourceTypeValidationRejectsFile(t *testing.T) {
	tests := []struct {
		name string
		call func(*appRuntimeService) error
	}{
		{
			name: "upsert subscription",
			call: func(service *appRuntimeService) error {
				_, err := service.UpsertSubscription(context.Background(), connect.NewRequest(&appv1.UpsertSubscriptionRequest{
					SubscriptionJson: `{"id":"file-sub","name":"File","type":"File","url":"data/local/sub.json","path":"data/subscribes/file-sub.json"}`,
				}))
				return err
			},
		},
		{
			name: "save subscriptions",
			call: func(service *appRuntimeService) error {
				_, err := service.SaveSubscriptions(context.Background(), connect.NewRequest(&appv1.SaveSubscriptionsRequest{
					SubscriptionsJson: []string{`{"id":"file-sub","name":"File","type":"File","url":"data/local/sub.json","path":"data/subscribes/file-sub.json"}`},
				}))
				return err
			},
		},
		{
			name: "upsert ruleset",
			call: func(service *appRuntimeService) error {
				_, err := service.UpsertRuleSet(context.Background(), connect.NewRequest(&appv1.UpsertRuleSetRequest{
					RulesetJson: `{"id":"file-ruleset","tag":"File","type":"File","format":"source","url":"data/local/rules.json"}`,
				}))
				return err
			},
		},
		{
			name: "save rulesets",
			call: func(service *appRuntimeService) error {
				_, err := service.SaveRuleSets(context.Background(), connect.NewRequest(&appv1.SaveRuleSetsRequest{
					RulesetsJson: []string{`{"id":"file-ruleset","tag":"File","type":"File","format":"source","url":"data/local/rules.json"}`},
				}))
				return err
			},
		},
	}

	for _, test := range tests {
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
		`{"id":"http-sub","name":"HTTP","type":"Http","url":"https://example.com/sub.json","path":"data/subscribes/http-sub.json"}`,
		`{"id":"manual-sub","name":"Manual","type":"Manual","path":"data/subscribes/manual-sub.json"}`,
	} {
		if _, err := service.UpsertSubscription(context.Background(), connect.NewRequest(&appv1.UpsertSubscriptionRequest{
			SubscriptionJson: subscriptionJSON,
		})); err != nil {
			t.Fatalf("expected supported subscription type, got %v", err)
		}
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
	_, err := service.UpsertRuleSet(context.Background(), connect.NewRequest(&appv1.UpsertRuleSetRequest{
		RulesetJson: `{"id":"manual","tag":"Manual","type":"Manual","format":"source"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}

	saveResp, err := service.SaveRuleSetContent(context.Background(), connect.NewRequest(&appv1.SaveRuleSetContentRequest{
		Id:      "manual",
		Content: `{"version":1,"rules":["a",["b","c"]]}`,
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

	clearResp, err := service.ClearRuleSetContent(context.Background(), connect.NewRequest(&appv1.ClearRuleSetContentRequest{Id: "manual"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clearResp.Msg.GetRulesetJson(), `"count":0`) {
		t.Fatalf("expected count 0 after clear, got %s", clearResp.Msg.GetRulesetJson())
	}
}

func TestDeleteRuleSetRemovesManagedFile(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	_, err := service.UpsertRuleSet(context.Background(), connect.NewRequest(&appv1.UpsertRuleSetRequest{
		RulesetJson: `{"id":"manual","tag":"Manual","type":"Manual","format":"source"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SaveRuleSetContent(context.Background(), connect.NewRequest(&appv1.SaveRuleSetContentRequest{
		Id:      "manual",
		Content: `{"version":1,"rules":[]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GetPath("data/rulesets/manual.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteRuleSet(context.Background(), connect.NewRequest(&appv1.DeleteRuleSetRequest{Id: "manual"})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GetPath("data/rulesets/manual.json")); !os.IsNotExist(err) {
		t.Fatalf("expected managed file to be removed, got %v", err)
	}
}

func TestUpsertRuleSetFormatChangeUpdatesManagedPath(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)
	_, err := service.UpsertRuleSet(context.Background(), connect.NewRequest(&appv1.UpsertRuleSetRequest{
		RulesetJson: `{"id":"switch","tag":"Switch","type":"Http","format":"binary","url":"https://example.com/a.srs"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(GetPath("data/rulesets/switch.srs")), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GetPath("data/rulesets/switch.srs"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	resp, err := service.UpsertRuleSet(context.Background(), connect.NewRequest(&appv1.UpsertRuleSetRequest{
		RulesetJson: `{"id":"switch","tag":"Switch","type":"Http","format":"source","url":"https://example.com/a.json","path":"data/evil.json"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Msg.GetRulesetJson(), `"path":"data/rulesets/switch.json"`) {
		t.Fatalf("expected managed source path, got %s", resp.Msg.GetRulesetJson())
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

func TestGetAppSettingsReturnsEmptyObjectWhenMissing(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	resp, err := service.GetAppSettings(context.Background(), connect.NewRequest(&appv1.GetAppSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.GetSettingsJson() != "{}" {
		t.Fatalf("expected empty settings object, got %s", resp.Msg.GetSettingsJson())
	}
}

func TestSaveAppSettingsPersistsUserYAML(t *testing.T) {
	withTempBasePath(t)
	service := newAppRuntimeService(nil, nil)

	_, err := service.SaveAppSettings(context.Background(), connect.NewRequest(&appv1.SaveAppSettingsRequest{
		SettingsJson: `{"lang":"zh","kernel":{"profile":"profile-1"}}`,
	}))
	if err != nil {
		t.Fatal(err)
	}

	settings, err := readUserSettingsMap()
	if err != nil {
		t.Fatal(err)
	}
	if settings["lang"] != "zh" {
		t.Fatalf("expected lang to persist, got %#v", settings["lang"])
	}
	kernel, _ := settings["kernel"].(map[string]any)
	if kernel["profile"] != "profile-1" {
		t.Fatalf("expected kernel profile to persist, got %#v", kernel["profile"])
	}
}

func TestGetAppSettingsReturnsStoredFields(t *testing.T) {
	withTempBasePath(t)
	if err := writeRuntimeYAMLFile(userSettingsPath, map[string]any{
		"width":            900,
		"height":           700,
		"authSecret":       "secret",
		"windowStartState": 0,
		"unknownKey":       true,
		"lang":             "en",
		"kernel": map[string]any{
			"profile":    "profile-1",
			"unknownKey": "drop",
			"main": map[string]any{
				"env":     map[string]any{"A": "B"},
				"unknown": true,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := newAppRuntimeService(nil, nil)

	resp, err := service.GetAppSettings(context.Background(), connect.NewRequest(&appv1.GetAppSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"authSecret", "width", "height", "windowStartState", "unknownKey"} {
		if !strings.Contains(resp.Msg.GetSettingsJson(), field) {
			t.Fatalf("settings response should include stored field %q, got %s", field, resp.Msg.GetSettingsJson())
		}
	}
	if !strings.Contains(resp.Msg.GetSettingsJson(), "profile-1") {
		t.Fatalf("settings response should include current fields, got %s", resp.Msg.GetSettingsJson())
	}
}

func TestSaveAppSettingsPersistsIncomingFields(t *testing.T) {
	withTempBasePath(t)
	if err := writeRuntimeYAMLFile(userSettingsPath, map[string]any{
		"width":            900,
		"height":           700,
		"authSecret":       "secret",
		"windowStartState": 0,
		"unknownKey":       true,
		"lang":             "en",
	}); err != nil {
		t.Fatal(err)
	}
	service := newAppRuntimeService(nil, nil)

	resp, err := service.SaveAppSettings(context.Background(), connect.NewRequest(&appv1.SaveAppSettingsRequest{
		SettingsJson: `{"width":1,"height":2,"authSecret":"changed","windowStartState":1,"unknownKey":true,"lang":"zh","kernel":{"profile":"profile-1","unknownKey":"drop"}}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"authSecret", "width", "height", "windowStartState", "unknownKey"} {
		if !strings.Contains(resp.Msg.GetSettingsJson(), field) {
			t.Fatalf("settings response should include incoming field %q, got %s", field, resp.Msg.GetSettingsJson())
		}
	}

	settings, err := readUserSettingsMap()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := settings["width"]; !ok {
		t.Fatalf("expected incoming width to persist: %#v", settings)
	}
	if _, ok := settings["height"]; !ok {
		t.Fatalf("expected incoming height to persist: %#v", settings)
	}
	if settings["authSecret"] != "changed" || settings["unknownKey"] != true {
		t.Fatalf("expected incoming legacy fields to persist: %#v", settings)
	}
	if _, ok := settings["windowStartState"]; !ok {
		t.Fatalf("expected incoming windowStartState to persist: %#v", settings)
	}
	if settings["lang"] != "zh" {
		t.Fatalf("expected frontend field to update, got %#v", settings["lang"])
	}
	kernel, _ := settings["kernel"].(map[string]any)
	if kernel["profile"] != "profile-1" {
		t.Fatalf("expected kernel profile to persist, got %#v", kernel["profile"])
	}
	if kernel["unknownKey"] != "drop" {
		t.Fatalf("expected kernel unknown field to persist, got %#v", kernel)
	}
}

func TestSaveAppSettingsRejectsInvalidJSONWithoutOverwrite(t *testing.T) {
	withTempBasePath(t)
	if err := writeRuntimeYAMLFile(userSettingsPath, map[string]any{"lang": "en"}); err != nil {
		t.Fatal(err)
	}
	service := newAppRuntimeService(nil, nil)

	_, err := service.SaveAppSettings(context.Background(), connect.NewRequest(&appv1.SaveAppSettingsRequest{
		SettingsJson: `{"lang":`,
	}))
	if err == nil {
		t.Fatal("expected invalid json error")
	}
	settings, err := readUserSettingsMap()
	if err != nil {
		t.Fatal(err)
	}
	if settings["lang"] != "en" {
		t.Fatalf("invalid save should not overwrite existing settings: %#v", settings)
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

func TestUpsertScheduledTaskTrimsExistingLogs(t *testing.T) {
	withTempBasePath(t)
	if err := saveScheduledTasks([]scheduledTask{{ID: "task-1", Name: "Task", LogLimit: 5}}); err != nil {
		t.Fatal(err)
	}
	service := newAppRuntimeService(nil, nil)
	for i := 0; i < 5; i++ {
		service.recordTaskLog("task-1", "Task", int64(i), int64(i), []*appv1.TaskResult{taskResult(true, "r", "R", "ok")})
	}

	_, err := service.UpsertScheduledTask(context.Background(), connect.NewRequest(&appv1.UpsertScheduledTaskRequest{
		TaskJson: `{"id":"task-1","name":"Task","type":"update::all::subscription","cron":"0 * * * * *","logLimit":2,"disabled":true}`,
	}))
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

	_, err := service.DeleteScheduledTask(context.Background(), connect.NewRequest(&appv1.DeleteScheduledTaskRequest{Id: "task-1"}))
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
