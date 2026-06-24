package runtime

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/event"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/rpcutil"
	"guiforcores/bridge/storage"
	"guiforcores/gen/app/v1"
	kernelv1 "guiforcores/gen/kernel/v1"

	connect "connectrpc.com/connect"
	"github.com/dop251/goja"
	"gopkg.in/yaml.v3"
)

const (
	userSettingsPath             = "data/user.yaml"
	subscriptionsFilePath        = "data/subscribes.yaml"
	runtimeRulesetsFilePath      = "data/rulesets.yaml"
	scheduledTasksPath           = "data/scheduledtasks.yaml"
	scheduledTaskLogsPath        = "data/scheduledtasklogs.yaml"
	defaultScheduledTaskLogLimit = 20
	rulesetHubPath               = "data/.cache/ruleset-list.json"
)

type KernelController interface {
	Status() (kernelv1.CoreStatus, string)
	Restart(ctx context.Context, profileID string) error
}

type AppConfigReader interface {
	Current() config.AppConfig
}

type appRuntimeService struct {
	platform *platform.Service
	paths    *storage.Paths
	config   AppConfigReader
	events   *event.Hub
	kernel   KernelController

	mu          sync.Mutex
	taskCancel  chan struct{}
	runningTask map[string]bool
	taskLogs    []scheduledTaskLog
}

type Service = appRuntimeService

var runtimePaths atomic.Pointer[storage.Paths]

func setRuntimePaths(paths *storage.Paths) {
	runtimePaths.Store(paths)
}

func GetPath(path string) string {
	return runtimePaths.Load().Resolve(path)
}

type invalidArgumentError struct {
	message string
}

func (e invalidArgumentError) Error() string {
	return e.message
}

func asConnectError(err error) error {
	if invalid, ok := err.(invalidArgumentError); ok {
		return rpcutil.AsConnectError(rpcutil.InvalidArgumentError{Message: invalid.message})
	}
	return rpcutil.AsConnectError(err)
}

func NewService(platformService *platform.Service, paths *storage.Paths, configStore AppConfigReader, events *event.Hub, kernelController KernelController) *Service {
	setRuntimePaths(paths)
	logs, _ := loadScheduledTaskLogs()
	tasks, _ := loadScheduledTasks()
	logs = trimScheduledTaskLogs(logs, tasks)
	return &Service{
		platform:    platformService,
		paths:       paths,
		config:      configStore,
		events:      events,
		kernel:      kernelController,
		runningTask: map[string]bool{},
		taskLogs:    logs,
	}
}

func newAppRuntimeService(_ any, kernelController KernelController) *Service {
	paths := runtimePaths.Load()
	configStore, _ := config.NewStore(paths)
	return NewService(nil, paths, configStore, nil, kernelController)
}

func (s *appRuntimeService) publish(eventName string, data ...any) {
	if s.events != nil {
		s.events.Publish(eventName, data...)
	}
}

func (s *appRuntimeService) StartScheduler() {
	if s.platform == nil {
		return
	}
	s.mu.Lock()
	if s.taskCancel != nil {
		close(s.taskCancel)
	}
	cancel := make(chan struct{})
	s.taskCancel = cancel
	s.mu.Unlock()

	go s.schedulerLoop(cancel)
}

func (s *appRuntimeService) StopScheduler() {
	s.mu.Lock()
	if s.taskCancel != nil {
		close(s.taskCancel)
		s.taskCancel = nil
	}
	s.mu.Unlock()
}

func (s *appRuntimeService) restartScheduler() {
	s.StartScheduler()
}

func (s *appRuntimeService) schedulerLoop(cancel <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastRun := map[string]int64{}

	for {
		select {
		case <-cancel:
			return
		case now := <-ticker.C:
			tasks, err := loadScheduledTasks()
			if err != nil {
				continue
			}
			for _, task := range tasks {
				if task.Disabled || task.ID == "" || task.Cron == "" {
					continue
				}
				if task.Type == scheduledTaskRunScript {
					continue
				}
				if cronMatches(task.Cron, now) {
					runKey := cronRunKey(task.Cron, now)
					if lastRun[task.ID] == runKey {
						continue
					}
					lastRun[task.ID] = runKey
					go s.runScheduledTask(context.Background(), task.ID)
				}
			}
		}
	}
}

func readRuntimeYAMLFile[T any](path string) (T, error) {
	var out T
	data, err := os.ReadFile(GetPath(path))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return out, nil
	}
	err = yaml.Unmarshal(data, &out)
	return out, err
}

func writeRuntimeYAMLFile(path string, value any) error {
	fullPath := GetPath(path)
	if err := os.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
		return err
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(fullPath, data, 0644)
}

func readUserSettingsMap() (map[string]any, error) {
	settings, err := readRuntimeYAMLFile[map[string]any](userSettingsPath)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, nil
}

func userSettingsJSON(settings map[string]any) (string, error) {
	data, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func saveUserSettingsJSON(settingsJSON string) (string, error) {
	var incoming map[string]any
	if strings.TrimSpace(settingsJSON) == "" {
		incoming = map[string]any{}
	} else if err := json.Unmarshal([]byte(settingsJSON), &incoming); err != nil {
		return "", invalidArgumentError{message: "invalid app settings json: " + err.Error()}
	}
	if incoming == nil {
		incoming = map[string]any{}
	}

	if err := writeRuntimeYAMLFile(userSettingsPath, incoming); err != nil {
		return "", err
	}
	return userSettingsJSON(incoming)
}

func jsonListFromYAML(path string) ([]string, error) {
	items, err := readRuntimeYAMLFile[[]map[string]any](path)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		out = append(out, string(data))
	}
	return out, nil
}

func saveJSONListAsYAML(path string, itemsJSON []string) ([]string, error) {
	items := make([]map[string]any, 0, len(itemsJSON))
	for _, raw := range itemsJSON {
		var item map[string]any
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		delete(item, "updating")
		items = append(items, item)
	}
	if err := writeRuntimeYAMLFile(path, items); err != nil {
		return nil, err
	}
	return jsonListFromYAML(path)
}

func upsertJSONItem(path string, itemJSON string) (string, error) {
	var item map[string]any
	if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
		return "", err
	}
	id, _ := item["id"].(string)
	if id == "" {
		return "", fmt.Errorf("id is required")
	}
	delete(item, "updating")
	items, err := readRuntimeYAMLFile[[]map[string]any](path)
	if err != nil {
		return "", err
	}
	found := false
	for i := range items {
		if existingID, _ := items[i]["id"].(string); existingID == id {
			items[i] = item
			found = true
			break
		}
	}
	if !found {
		items = append(items, item)
	}
	if err := writeRuntimeYAMLFile(path, items); err != nil {
		return "", err
	}
	data, _ := json.Marshal(item)
	return string(data), nil
}

func deleteJSONItem(path string, id string) error {
	items, err := readRuntimeYAMLFile[[]map[string]any](path)
	if err != nil {
		return err
	}
	next := items[:0]
	found := false
	for _, item := range items {
		if existingID, _ := item["id"].(string); existingID == id {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		return nil
	}
	return writeRuntimeYAMLFile(path, next)
}

func scheduledTasksToJSON(items []scheduledTask) ([]string, error) {
	normalizeScheduledTasks(items)
	out := make([]string, 0, len(items))
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		out = append(out, string(data))
	}
	return out, nil
}

func saveScheduledTasksJSON(itemsJSON []string) ([]string, error) {
	items := make([]scheduledTask, 0, len(itemsJSON))
	for _, raw := range itemsJSON {
		var item scheduledTask
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := saveScheduledTasks(items); err != nil {
		return nil, err
	}
	return scheduledTasksToJSON(items)
}

func upsertScheduledTaskJSON(itemJSON string) (string, error) {
	var task scheduledTask
	if err := json.Unmarshal([]byte(itemJSON), &task); err != nil {
		return "", err
	}
	if task.ID == "" {
		return "", fmt.Errorf("id is required")
	}
	tasks, err := loadScheduledTasks()
	if err != nil {
		return "", err
	}
	found := false
	for i := range tasks {
		if tasks[i].ID == task.ID {
			tasks[i] = task
			found = true
			break
		}
	}
	if !found {
		tasks = append(tasks, task)
	}
	if err := saveScheduledTasks(tasks); err != nil {
		return "", err
	}
	task.LogLimit = normalizeScheduledTaskLogLimit(task.LogLimit)
	data, err := json.Marshal(task)
	return string(data), err
}

func loadSubscriptions() ([]subscription, error) {
	return readRuntimeYAMLFile[[]subscription](subscriptionsFilePath)
}

func saveSubscriptions(items []subscription) error {
	for i := range items {
		items[i].Updating = false
	}
	return writeRuntimeYAMLFile(subscriptionsFilePath, items)
}

func loadRulesets() ([]ruleset, error) {
	items, err := readRuntimeYAMLFile[[]ruleset](runtimeRulesetsFilePath)
	if err != nil {
		return nil, err
	}
	if normalizeRulesets(items, nil) {
		_ = saveRulesets(items)
	}
	return items, nil
}

func saveRulesets(items []ruleset) error {
	normalizeRulesets(items, nil)
	for i := range items {
		items[i].Updating = false
	}
	return writeRuntimeYAMLFile(runtimeRulesetsFilePath, items)
}

func rulesetsToJSON(items []ruleset) ([]string, error) {
	normalizeRulesets(items, nil)
	out := make([]string, 0, len(items))
	for _, item := range items {
		item.Updating = false
		data, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		out = append(out, string(data))
	}
	return out, nil
}

func saveRulesetsJSON(itemsJSON []string) ([]string, error) {
	existing, err := loadRulesets()
	if err != nil {
		return nil, err
	}
	existingByID := map[string]ruleset{}
	for _, item := range existing {
		existingByID[item.ID] = item
	}
	items := make([]ruleset, 0, len(itemsJSON))
	for _, raw := range itemsJSON {
		var item ruleset
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		if item.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		prev, ok := existingByID[item.ID]
		if ok {
			item.Path = prev.Path
			if item.Format != prev.Format {
				item.Path = managedRulesetPath(item.ID, item.Format)
				migrateRulesetFile(prev.Path, item.Path)
			}
		} else {
			item.Path = managedRulesetPath(item.ID, item.Format)
		}
		items = append(items, item)
	}
	if err := saveRulesets(items); err != nil {
		return nil, err
	}
	return rulesetsToJSON(items)
}

func upsertRulesetJSON(itemJSON string) (string, error) {
	var item ruleset
	if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
		return "", err
	}
	if item.ID == "" {
		return "", fmt.Errorf("id is required")
	}
	items, err := loadRulesets()
	if err != nil {
		return "", err
	}
	found := false
	for i := range items {
		if items[i].ID != item.ID {
			continue
		}
		prev := items[i]
		item.Path = prev.Path
		if item.Format != prev.Format || item.Path == "" {
			item.Path = managedRulesetPath(item.ID, item.Format)
			migrateRulesetFile(prev.Path, item.Path)
		}
		items[i] = item
		found = true
		break
	}
	if !found {
		item.Path = managedRulesetPath(item.ID, item.Format)
		items = append(items, item)
	}
	if err := saveRulesets(items); err != nil {
		return "", err
	}
	data, err := json.Marshal(item)
	return string(data), err
}

func deleteRuleset(id string) error {
	items, err := loadRulesets()
	if err != nil {
		return err
	}
	next := items[:0]
	for _, item := range items {
		if item.ID == id {
			if item.Path != "" {
				_ = os.Remove(GetPath(item.Path))
			}
			continue
		}
		next = append(next, item)
	}
	return saveRulesets(next)
}

func saveRuleSetContent(id string, content string) (string, error) {
	r, items, err := findRuleset(id)
	if err != nil {
		return "", err
	}
	if r.Format != "source" {
		return "", invalidArgumentError{message: "only source rulesets have editable content"}
	}
	if strings.TrimSpace(content) == "" {
		content = `{"version":1,"rules":[]}`
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return "", invalidArgumentError{message: "not a valid ruleset json: " + err.Error()}
	}
	if parsed["rules"] == nil {
		return "", invalidArgumentError{message: "not a valid ruleset json: missing rules"}
	}
	data, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(GetPath(r.Path)), os.ModePerm); err != nil {
		return "", err
	}
	if err := os.WriteFile(GetPath(r.Path), data, 0644); err != nil {
		return "", err
	}
	r.Count = countRules(parsed["rules"])
	r.UpdateTime = time.Now().UnixMilli()
	if err := saveRulesets(items); err != nil {
		return "", err
	}
	result, err := json.Marshal(r)
	return string(result), err
}

func findRuleset(id string) (*ruleset, []ruleset, error) {
	items, err := loadRulesets()
	if err != nil {
		return nil, nil, err
	}
	for i := range items {
		if items[i].ID == id {
			return &items[i], items, nil
		}
	}
	return nil, items, fmt.Errorf("ruleset %q not found", id)
}

func normalizeRulesets(items []ruleset, existing map[string]ruleset) bool {
	changed := false
	for i := range items {
		items[i].Updating = false
		if items[i].Format == "" {
			items[i].Format = "binary"
			changed = true
		}
		if items[i].Path == "" {
			if prev, ok := existing[items[i].ID]; ok && prev.Path != "" && prev.Format == items[i].Format {
				items[i].Path = prev.Path
			} else {
				items[i].Path = managedRulesetPath(items[i].ID, items[i].Format)
			}
			changed = true
		}
	}
	return changed
}

func managedRulesetPath(id string, format string) string {
	ext := ".srs"
	if format == "source" {
		ext = ".json"
	}
	return "data/rulesets/" + safeRulesetFileName(id) + ext
}

func safeRulesetFileName(id string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", " ", "_")
	safe := replacer.Replace(id)
	safe = strings.Trim(safe, "._")
	if safe == "" {
		return "ruleset"
	}
	return safe
}

func migrateRulesetFile(from string, to string) {
	if from == "" || to == "" || from == to {
		return
	}
	fromPath := GetPath(from)
	toPath := GetPath(to)
	if _, err := os.Stat(fromPath); err != nil {
		return
	}
	if _, err := os.Stat(toPath); err == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(toPath), os.ModePerm); err != nil {
		return
	}
	_ = os.Rename(fromPath, toPath)
}

func loadScheduledTasks() ([]scheduledTask, error) {
	items, err := readRuntimeYAMLFile[[]scheduledTask](scheduledTasksPath)
	if err != nil {
		return nil, err
	}
	normalizeScheduledTasks(items)
	return items, nil
}

func saveScheduledTasks(items []scheduledTask) error {
	normalizeScheduledTasks(items)
	return writeRuntimeYAMLFile(scheduledTasksPath, items)
}

func normalizeScheduledTaskLogLimit(limit int) int {
	if limit < 1 {
		return defaultScheduledTaskLogLimit
	}
	return limit
}

func normalizeScheduledTasks(items []scheduledTask) {
	for i := range items {
		items[i].LogLimit = normalizeScheduledTaskLogLimit(items[i].LogLimit)
	}
}

func scheduledTaskLogLimitMap(tasks []scheduledTask) map[string]int {
	limits := make(map[string]int, len(tasks))
	for _, task := range tasks {
		if task.ID != "" {
			limits[task.ID] = normalizeScheduledTaskLogLimit(task.LogLimit)
		}
	}
	return limits
}

func loadScheduledTaskLogs() ([]scheduledTaskLog, error) {
	return readRuntimeYAMLFile[[]scheduledTaskLog](scheduledTaskLogsPath)
}

func saveScheduledTaskLogs(logs []scheduledTaskLog) error {
	if logs == nil {
		logs = []scheduledTaskLog{}
	}
	return writeRuntimeYAMLFile(scheduledTaskLogsPath, logs)
}

func taskResultsToRuntime(results []*appv1.TaskResult) []scheduledTaskResult {
	out := make([]scheduledTaskResult, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		out = append(out, scheduledTaskResult{
			Ok:     result.GetOk(),
			ID:     result.GetId(),
			Name:   result.GetName(),
			Result: result.GetResult(),
		})
	}
	return out
}

func taskResultsToProto(results []scheduledTaskResult) []*appv1.TaskResult {
	out := make([]*appv1.TaskResult, 0, len(results))
	for _, result := range results {
		out = append(out, &appv1.TaskResult{
			Ok:     result.Ok,
			Id:     result.ID,
			Name:   result.Name,
			Result: result.Result,
		})
	}
	return out
}

func scheduledTaskLogToProto(log scheduledTaskLog) *appv1.TaskLog {
	return &appv1.TaskLog{
		Id:        log.ID,
		Name:      log.Name,
		StartTime: log.StartTime,
		EndTime:   log.EndTime,
		Results:   taskResultsToProto(log.Results),
	}
}

func trimScheduledTaskLogs(logs []scheduledTaskLog, tasks []scheduledTask) []scheduledTaskLog {
	limits := scheduledTaskLogLimitMap(tasks)
	seen := map[string]int{}
	out := make([]scheduledTaskLog, 0, len(logs))
	for _, log := range logs {
		limit := normalizeScheduledTaskLogLimit(limits[log.ID])
		if seen[log.ID] >= limit {
			continue
		}
		seen[log.ID]++
		out = append(out, log)
	}
	return out
}

func removeScheduledTaskLogs(logs []scheduledTaskLog, id string) []scheduledTaskLog {
	out := logs[:0]
	for _, log := range logs {
		if log.ID != id {
			out = append(out, log)
		}
	}
	return out
}

func taskResult(ok bool, id string, name string, result string) *appv1.TaskResult {
	return &appv1.TaskResult{Ok: ok, Id: id, Name: name, Result: result}
}

func normalizeBase64(s string) string {
	normalized := strings.NewReplacer("\n", "", "\r", "", "\t", "", " ", "", "-", "+", "_", "/").Replace(s)
	if m := len(normalized) % 4; m != 0 {
		normalized += strings.Repeat("=", 4-m)
	}
	return normalized
}

func isValidBase64Payload(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(normalizeBase64(s))
	return err == nil
}

func parseUserInfo(header string) map[string]int64 {
	out := map[string]int64{}
	for _, part := range strings.Split(header, ";") {
		pieces := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pieces) != 2 {
			continue
		}
		value, _ := strconv.ParseInt(pieces[1], 10, 64)
		out[pieces[0]] = value
	}
	return out
}

func readText(path string) (string, error) {
	data, err := os.ReadFile(GetPath(path))
	return string(data), err
}

func httpRequest(method string, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int) (*http.Response, string, error) {
	if method == "" {
		method = http.MethodGet
	}
	timeout := platform.GetTimeout(timeoutSeconds)
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: platform.GetProxy(""),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecure,
			},
		},
	}
	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, "", err
	}
	return resp, string(data), nil
}

func compileSmartRegexp(expr string) (*regexp.Regexp, error) {
	if expr == "" {
		return nil, nil
	}
	return regexp.Compile(expr)
}

func previousProxyID(proxies []proxyRef, tag string) string {
	for _, proxy := range proxies {
		if proxy.Tag == tag {
			return proxy.ID
		}
	}
	return "ID_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func proxyRefsFromOutbounds(outbounds []map[string]any, previous []proxyRef) []proxyRef {
	refs := make([]proxyRef, 0, len(outbounds))
	for _, outbound := range outbounds {
		tag, _ := outbound["tag"].(string)
		typ, _ := outbound["type"].(string)
		refs = append(refs, proxyRef{ID: previousProxyID(previous, tag), Tag: tag, Type: typ})
	}
	return refs
}

func runSubscribeScript(script string, proxies []map[string]any, sub subscription) ([]map[string]any, subscription, error) {
	if strings.TrimSpace(script) == "" {
		return proxies, sub, nil
	}
	vm := goja.New()
	if err := vm.Set("proxies", proxies); err != nil {
		return nil, sub, err
	}
	if err := vm.Set("subscription", sub); err != nil {
		return nil, sub, err
	}
	value, err := vm.RunString(script + "\n; onSubscribe(proxies, subscription)")
	if err != nil {
		return nil, sub, err
	}
	if promise, ok := value.Export().(*goja.Promise); ok {
		if promise.State() == goja.PromiseStateRejected {
			return nil, sub, fmt.Errorf("%v", promise.Result().Export())
		}
		value = promise.Result()
	}
	var result struct {
		Proxies      []map[string]any `json:"proxies"`
		Subscription subscription     `json:"subscription"`
	}
	data, err := json.Marshal(value.Export())
	if err != nil {
		return nil, sub, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, sub, err
	}
	if result.Proxies == nil {
		result.Proxies = proxies
	}
	if result.Subscription.ID == "" {
		result.Subscription = sub
	}
	return result.Proxies, result.Subscription, nil
}

func updateSubscriptionAt(items []subscription, idx int) (*appv1.TaskResult, bool) {
	sub := &items[idx]
	if sub.Disabled {
		return taskResult(false, sub.ID, sub.Name, sub.Name+" Disabled"), false
	}
	body := ""
	userInfo := map[string]int64{}
	var err error

	switch sub.Type {
	case "Manual":
		body, err = readText(sub.Path)
	case "File":
		body, err = readText(sub.URL)
	case "Http":
		resp, text, reqErr := httpRequest(sub.RequestMethod, sub.URL, sub.Header.Request, "", sub.InSecure, sub.RequestTimeout)
		err = reqErr
		body = text
		if resp != nil {
			for key, value := range sub.Header.Response {
				resp.Header.Set(key, value)
			}
			if info := resp.Header.Get("Subscription-Userinfo"); info != "" {
				userInfo = parseUserInfo(info)
			}
		}
	default:
		err = fmt.Errorf("unsupported subscription type %s", sub.Type)
	}
	if err != nil {
		return taskResult(false, sub.ID, sub.Name, fmt.Sprintf("Failed to update subscription [%s]. Reason: %v", sub.Name, err)), false
	}

	var proxies []map[string]any
	var parsed map[string]any
	if json.Unmarshal([]byte(body), &parsed) == nil {
		if arr, ok := parsed["outbounds"].([]any); ok {
			for _, item := range arr {
				if obj, ok := item.(map[string]any); ok {
					proxies = append(proxies, obj)
				}
			}
		} else if sub.Type == "Manual" {
			var manual []map[string]any
			if err := json.Unmarshal([]byte(body), &manual); err == nil {
				proxies = manual
			}
		}
	}
	if proxies == nil {
		var yamlObj map[string]any
		if yaml.Unmarshal([]byte(body), &yamlObj) == nil {
			if arr, ok := yamlObj["proxies"].([]any); ok {
				for _, item := range arr {
					if obj, ok := item.(map[string]any); ok {
						proxies = append(proxies, obj)
					}
				}
			}
		}
	}
	if proxies == nil && isValidBase64Payload(body) {
		return taskResult(false, sub.ID, sub.Name, "Subscription data must be converted to sing-box outbound format before import"), false
	}
	if proxies == nil {
		return taskResult(false, sub.ID, sub.Name, fmt.Sprintf("Failed to update subscription [%s]. Reason: Not a valid subscription data", sub.Name)), false
	}
	for _, proxy := range proxies {
		if _, hasName := proxy["name"]; hasName {
			if _, hasTag := proxy["tag"]; !hasTag {
				return taskResult(false, sub.ID, sub.Name, "Subscription data must be converted to sing-box outbound format before import"), false
			}
		}
	}

	if sub.Type != "Manual" {
		include, err := compileSmartRegexp(sub.Include)
		if err != nil {
			return taskResult(false, sub.ID, sub.Name, err.Error()), false
		}
		exclude, err := compileSmartRegexp(sub.Exclude)
		if err != nil {
			return taskResult(false, sub.ID, sub.Name, err.Error()), false
		}
		includeProtocol, err := compileSmartRegexp(sub.IncludeProtocol)
		if err != nil {
			return taskResult(false, sub.ID, sub.Name, err.Error()), false
		}
		excludeProtocol, err := compileSmartRegexp(sub.ExcludeProtocol)
		if err != nil {
			return taskResult(false, sub.ID, sub.Name, err.Error()), false
		}
		filtered := proxies[:0]
		for _, proxy := range proxies {
			tag, _ := proxy["tag"].(string)
			typ, _ := proxy["type"].(string)
			if include != nil && !include.MatchString(tag) {
				continue
			}
			if exclude != nil && exclude.MatchString(tag) {
				continue
			}
			if includeProtocol != nil && !includeProtocol.MatchString(typ) {
				continue
			}
			if excludeProtocol != nil && excludeProtocol.MatchString(typ) {
				continue
			}
			if sub.ProxyPrefix != "" && !strings.HasPrefix(tag, sub.ProxyPrefix) {
				proxy["tag"] = sub.ProxyPrefix + tag
			}
			filtered = append(filtered, proxy)
		}
		proxies = filtered
	}

	sub.Upload = userInfo["upload"]
	sub.Download = userInfo["download"]
	sub.Total = userInfo["total"]
	sub.Expire = userInfo["expire"] * 1000
	sub.UpdateTime = time.Now().UnixMilli()
	sub.Proxies = proxyRefsFromOutbounds(proxies, sub.Proxies)

	processedProxies, processedSub, err := runSubscribeScript(sub.Script, proxies, *sub)
	if err != nil {
		return taskResult(false, sub.ID, sub.Name, fmt.Sprintf("Failed to update subscription [%s]. Reason: %v", sub.Name, err)), false
	}
	*sub = processedSub
	sub.Proxies = proxyRefsFromOutbounds(processedProxies, sub.Proxies)

	if sub.Type == "Http" || (sub.Type == "File" && sub.URL != sub.Path) {
		data, err := json.MarshalIndent(processedProxies, "", "  ")
		if err != nil {
			return taskResult(false, sub.ID, sub.Name, err.Error()), false
		}
		if err := os.MkdirAll(filepath.Dir(GetPath(sub.Path)), os.ModePerm); err != nil {
			return taskResult(false, sub.ID, sub.Name, err.Error()), false
		}
		if err := os.WriteFile(GetPath(sub.Path), data, 0644); err != nil {
			return taskResult(false, sub.ID, sub.Name, err.Error()), false
		}
	}

	return taskResult(true, sub.ID, sub.Name, fmt.Sprintf("Subscription [%s] updated successfully.", sub.Name)), true
}

func countRules(value any) int {
	switch v := value.(type) {
	case []any:
		total := 0
		for _, item := range v {
			total += countRules(item)
		}
		return total
	case map[string]any:
		total := 0
		for _, item := range v {
			total += countRules(item)
		}
		return total
	case string:
		return 1
	default:
		return 0
	}
}

func updateRulesetAt(items []ruleset, idx int) (*appv1.TaskResult, bool) {
	r := &items[idx]
	if r.Disabled {
		return taskResult(false, r.ID, r.Tag, r.Tag+" Disabled"), false
	}
	var err error
	if r.Format == "source" {
		body := ""
		exists := true
		switch r.Type {
		case "File":
			body, err = readText(r.URL)
		case "Http":
			_, body, err = httpRequest(http.MethodGet, r.URL, nil, "", false, 15)
		case "Manual":
			body, err = readText(r.Path)
			if err != nil {
				body = `{"version":1,"rules":[]}`
				exists = false
				err = nil
			}
		default:
			err = fmt.Errorf("unsupported ruleset type %s", r.Type)
		}
		if err != nil {
			return taskResult(false, r.ID, r.Tag, fmt.Sprintf("Failed to update rule-set [%s]. Reason: %v", r.Tag, err)), false
		}
		var rules map[string]any
		if err := json.Unmarshal([]byte(body), &rules); err != nil || rules["rules"] == nil {
			return taskResult(false, r.ID, r.Tag, fmt.Sprintf("Failed to update rule-set [%s]. Reason: Not a valid ruleset data", r.Tag)), false
		}
		r.Count = countRules(rules["rules"])
		if ((r.Type == "Http" || r.Type == "File") && r.URL != r.Path) || (r.Type == "Manual" && !exists) {
			data, _ := json.MarshalIndent(rules, "", "  ")
			if err := os.MkdirAll(filepath.Dir(GetPath(r.Path)), os.ModePerm); err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
			if err := os.WriteFile(GetPath(r.Path), data, 0644); err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
		}
	}
	if r.Format == "binary" {
		if r.Type == "File" && r.URL != r.Path {
			input, err := os.ReadFile(GetPath(r.URL))
			if err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
			if err := os.MkdirAll(filepath.Dir(GetPath(r.Path)), os.ModePerm); err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
			if err := os.WriteFile(GetPath(r.Path), input, 0644); err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
		} else if r.Type == "Http" {
			resp, err := http.Get(r.URL)
			if err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
			if err := os.MkdirAll(filepath.Dir(GetPath(r.Path)), os.ModePerm); err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
			if err := os.WriteFile(GetPath(r.Path), data, 0644); err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
		}
	}
	r.UpdateTime = time.Now().UnixMilli()
	return taskResult(true, r.ID, r.Tag, fmt.Sprintf("Ruleset [%s] updated successfully.", r.Tag)), true
}

func (s *appRuntimeService) markRuntimeChanged(kind string, id string) {
	s.publish("runtimeChange", kind, id)
	if !s.shouldAutoRestartKernel(id) {
		return
	}
	if s.kernel == nil {
		return
	}
	status, profileID := s.kernel.Status()
	running := status == kernelv1.CoreStatus_CORE_STATUS_RUNNING
	if running && profileID != "" {
		go func() {
			_ = s.kernel.Restart(context.Background(), profileID)
		}()
	}
}

func (s *appRuntimeService) shouldAutoRestartKernel(_ string) bool {
	return s.config.Current().AutoRestartKernel
}

func mustRead(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

func (s *appRuntimeService) updateSubscription(id string) ([]*appv1.TaskResult, error) {
	items, err := loadSubscriptions()
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ID == id {
			result, changed := updateSubscriptionAt(items, i)
			if changed {
				if err := saveSubscriptions(items); err != nil {
					return nil, err
				}
				s.markRuntimeChanged("subscription", id)
			}
			return []*appv1.TaskResult{result}, nil
		}
	}
	return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("subscription %q not found", id))
}

func (s *appRuntimeService) updateAllSubscriptions() ([]*appv1.TaskResult, error) {
	items, err := loadSubscriptions()
	if err != nil {
		return nil, err
	}
	results := make([]*appv1.TaskResult, 0, len(items))
	changed := false
	for i := range items {
		if items[i].Disabled {
			continue
		}
		result, didChange := updateSubscriptionAt(items, i)
		results = append(results, result)
		changed = changed || didChange
	}
	if changed {
		if err := saveSubscriptions(items); err != nil {
			return nil, err
		}
		s.markRuntimeChanged("subscriptions", "")
	}
	return results, nil
}

func (s *appRuntimeService) updateRuleset(id string) ([]*appv1.TaskResult, error) {
	items, err := loadRulesets()
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ID == id {
			result, changed := updateRulesetAt(items, i)
			if changed {
				if err := saveRulesets(items); err != nil {
					return nil, err
				}
				s.markRuntimeChanged("ruleset", id)
			}
			return []*appv1.TaskResult{result}, nil
		}
	}
	return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ruleset %q not found", id))
}

func (s *appRuntimeService) updateAllRulesets() ([]*appv1.TaskResult, error) {
	items, err := loadRulesets()
	if err != nil {
		return nil, err
	}
	results := make([]*appv1.TaskResult, 0, len(items))
	changed := false
	for i := range items {
		if items[i].Disabled {
			continue
		}
		result, didChange := updateRulesetAt(items, i)
		results = append(results, result)
		changed = changed || didChange
	}
	if changed {
		if err := saveRulesets(items); err != nil {
			return nil, err
		}
		s.markRuntimeChanged("rulesets", "")
	}
	return results, nil
}

func (s *appRuntimeService) runScheduledTask(ctx context.Context, id string) (scheduledTaskLog, error) {
	s.mu.Lock()
	if s.runningTask[id] {
		s.mu.Unlock()
		now := time.Now().UnixMilli()
		result := []*appv1.TaskResult{taskResult(false, id, "", "Skipped: task is already running")}
		log := s.recordTaskLog(id, "", now, now, result)
		return log, nil
	}
	s.runningTask[id] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.runningTask, id)
		s.mu.Unlock()
	}()

	tasks, err := loadScheduledTasks()
	if err != nil {
		return scheduledTaskLog{}, err
	}
	idx := -1
	for i := range tasks {
		if tasks[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return scheduledTaskLog{}, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %q not found", id))
	}
	task := tasks[idx]
	start := time.Now().UnixMilli()
	var results []*appv1.TaskResult
	switch task.Type {
	case scheduledTaskUpdateSubscription:
		for _, subID := range task.Subscriptions {
			r, err := s.updateSubscription(subID)
			if err != nil {
				results = append(results, taskResult(false, subID, "", err.Error()))
				continue
			}
			results = append(results, r...)
		}
	case scheduledTaskUpdateRuleset:
		for _, rulesetID := range task.Rulesets {
			r, err := s.updateRuleset(rulesetID)
			if err != nil {
				results = append(results, taskResult(false, rulesetID, "", err.Error()))
				continue
			}
			results = append(results, r...)
		}
	case scheduledTaskUpdateAllSubscription:
		results, err = s.updateAllSubscriptions()
	case scheduledTaskUpdateAllRuleset:
		results, err = s.updateAllRulesets()
	case scheduledTaskRunScript:
		results = []*appv1.TaskResult{taskResult(false, task.ID, task.Name, "run::script is not supported by the backend scheduler")}
	default:
		results = []*appv1.TaskResult{taskResult(false, task.ID, task.Name, "unsupported scheduled task type: "+task.Type)}
	}
	if err != nil {
		results = append(results, taskResult(false, task.ID, task.Name, err.Error()))
	}
	end := time.Now().UnixMilli()
	tasks[idx].LastTime = end
	_ = saveScheduledTasks(tasks)
	log := s.recordTaskLog(task.ID, task.Name, start, end, results)
	s.publish("scheduledTaskFinished", task.ID)
	_ = ctx
	return log, nil
}

func (s *appRuntimeService) recordTaskLog(id string, name string, start int64, end int64, results []*appv1.TaskResult) scheduledTaskLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	log := scheduledTaskLog{
		ID:        id,
		Name:      name,
		StartTime: start,
		EndTime:   end,
		Results:   taskResultsToRuntime(results),
	}
	s.taskLogs = append([]scheduledTaskLog{log}, s.taskLogs...)

	tasks, err := loadScheduledTasks()
	if err == nil {
		s.taskLogs = trimScheduledTaskLogs(s.taskLogs, tasks)
	}
	_ = saveScheduledTaskLogs(s.taskLogs)
	return log
}

func (s *appRuntimeService) GetAppSettings(ctx context.Context, req *connect.Request[appv1.GetAppSettingsRequest]) (*connect.Response[appv1.GetAppSettingsResponse], error) {
	settings, err := readUserSettingsMap()
	if err != nil {
		return nil, asConnectError(err)
	}
	settingsJSON, err := userSettingsJSON(settings)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.GetAppSettingsResponse{SettingsJson: settingsJSON}), nil
}

func (s *appRuntimeService) SaveAppSettings(ctx context.Context, req *connect.Request[appv1.SaveAppSettingsRequest]) (*connect.Response[appv1.SaveAppSettingsResponse], error) {
	settingsJSON, err := saveUserSettingsJSON(req.Msg.GetSettingsJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.SaveAppSettingsResponse{SettingsJson: settingsJSON}), nil
}

func (s *appRuntimeService) ListSubscriptions(ctx context.Context, req *connect.Request[appv1.ListSubscriptionsRequest]) (*connect.Response[appv1.ListSubscriptionsResponse], error) {
	items, err := jsonListFromYAML(subscriptionsFilePath)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.ListSubscriptionsResponse{SubscriptionsJson: items}), nil
}

func (s *appRuntimeService) SaveSubscriptions(ctx context.Context, req *connect.Request[appv1.SaveSubscriptionsRequest]) (*connect.Response[appv1.SaveSubscriptionsResponse], error) {
	items, err := saveJSONListAsYAML(subscriptionsFilePath, req.Msg.GetSubscriptionsJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	s.markRuntimeChanged("subscriptions", "")
	return connect.NewResponse(&appv1.SaveSubscriptionsResponse{SubscriptionsJson: items}), nil
}

func (s *appRuntimeService) UpsertSubscription(ctx context.Context, req *connect.Request[appv1.UpsertSubscriptionRequest]) (*connect.Response[appv1.UpsertSubscriptionResponse], error) {
	item, err := upsertJSONItem(subscriptionsFilePath, req.Msg.GetSubscriptionJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	var decoded map[string]any
	_ = json.Unmarshal([]byte(item), &decoded)
	id, _ := decoded["id"].(string)
	s.markRuntimeChanged("subscription", id)
	return connect.NewResponse(&appv1.UpsertSubscriptionResponse{SubscriptionJson: item}), nil
}

func (s *appRuntimeService) DeleteSubscription(ctx context.Context, req *connect.Request[appv1.DeleteSubscriptionRequest]) (*connect.Response[appv1.DeleteSubscriptionResponse], error) {
	if err := deleteJSONItem(subscriptionsFilePath, req.Msg.GetId()); err != nil {
		return nil, asConnectError(err)
	}
	s.markRuntimeChanged("subscription", req.Msg.GetId())
	return connect.NewResponse(&appv1.DeleteSubscriptionResponse{}), nil
}

func (s *appRuntimeService) UpdateSubscription(ctx context.Context, req *connect.Request[appv1.UpdateSubscriptionRequest]) (*connect.Response[appv1.UpdateSubscriptionResponse], error) {
	results, err := s.updateSubscription(req.Msg.GetId())
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.UpdateSubscriptionResponse{Results: results}), nil
}

func (s *appRuntimeService) UpdateAllSubscriptions(ctx context.Context, req *connect.Request[appv1.UpdateAllSubscriptionsRequest]) (*connect.Response[appv1.UpdateAllSubscriptionsResponse], error) {
	results, err := s.updateAllSubscriptions()
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.UpdateAllSubscriptionsResponse{Results: results}), nil
}

func (s *appRuntimeService) ListRuleSets(ctx context.Context, req *connect.Request[appv1.ListRuleSetsRequest]) (*connect.Response[appv1.ListRuleSetsResponse], error) {
	rulesets, err := loadRulesets()
	if err != nil {
		return nil, asConnectError(err)
	}
	items, err := rulesetsToJSON(rulesets)
	if err != nil {
		return nil, asConnectError(err)
	}
	hub := string(mustRead(GetPath(rulesetHubPath)))
	return connect.NewResponse(&appv1.ListRuleSetsResponse{RulesetsJson: items, HubJson: hub}), nil
}

func (s *appRuntimeService) SaveRuleSets(ctx context.Context, req *connect.Request[appv1.SaveRuleSetsRequest]) (*connect.Response[appv1.SaveRuleSetsResponse], error) {
	items, err := saveRulesetsJSON(req.Msg.GetRulesetsJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	s.markRuntimeChanged("rulesets", "")
	return connect.NewResponse(&appv1.SaveRuleSetsResponse{RulesetsJson: items}), nil
}

func (s *appRuntimeService) UpsertRuleSet(ctx context.Context, req *connect.Request[appv1.UpsertRuleSetRequest]) (*connect.Response[appv1.UpsertRuleSetResponse], error) {
	item, err := upsertRulesetJSON(req.Msg.GetRulesetJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	var decoded map[string]any
	_ = json.Unmarshal([]byte(item), &decoded)
	id, _ := decoded["id"].(string)
	s.markRuntimeChanged("ruleset", id)
	return connect.NewResponse(&appv1.UpsertRuleSetResponse{RulesetJson: item}), nil
}

func (s *appRuntimeService) DeleteRuleSet(ctx context.Context, req *connect.Request[appv1.DeleteRuleSetRequest]) (*connect.Response[appv1.DeleteRuleSetResponse], error) {
	if err := deleteRuleset(req.Msg.GetId()); err != nil {
		return nil, asConnectError(err)
	}
	s.markRuntimeChanged("ruleset", req.Msg.GetId())
	return connect.NewResponse(&appv1.DeleteRuleSetResponse{}), nil
}

func (s *appRuntimeService) UpdateRuleSet(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetRequest]) (*connect.Response[appv1.UpdateRuleSetResponse], error) {
	results, err := s.updateRuleset(req.Msg.GetId())
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.UpdateRuleSetResponse{Results: results}), nil
}

func (s *appRuntimeService) UpdateAllRuleSets(ctx context.Context, req *connect.Request[appv1.UpdateAllRuleSetsRequest]) (*connect.Response[appv1.UpdateAllRuleSetsResponse], error) {
	results, err := s.updateAllRulesets()
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.UpdateAllRuleSetsResponse{Results: results}), nil
}

func (s *appRuntimeService) UpdateRuleSetHub(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetHubRequest]) (*connect.Response[appv1.UpdateRuleSetHubResponse], error) {
	_, body, err := httpRequest(http.MethodGet, "https://github.com/GUI-for-Cores/Ruleset-Hub/releases/download/latest/sing-full.json", nil, "", false, 60)
	if err != nil {
		return nil, asConnectError(err)
	}
	var check map[string]any
	if err := json.Unmarshal([]byte(body), &check); err != nil {
		return nil, asConnectError(err)
	}
	if err := os.MkdirAll(filepath.Dir(GetPath(rulesetHubPath)), os.ModePerm); err != nil {
		return nil, asConnectError(err)
	}
	if err := os.WriteFile(GetPath(rulesetHubPath), []byte(body), 0644); err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.UpdateRuleSetHubResponse{HubJson: body}), nil
}

func (s *appRuntimeService) GetRuleSetContent(ctx context.Context, req *connect.Request[appv1.GetRuleSetContentRequest]) (*connect.Response[appv1.GetRuleSetContentResponse], error) {
	r, _, err := findRuleset(req.Msg.GetId())
	if err != nil {
		return nil, asConnectError(err)
	}
	if r.Format != "source" {
		return nil, asConnectError(invalidArgumentError{message: "only source rulesets have editable content"})
	}
	content, err := readText(r.Path)
	if os.IsNotExist(err) {
		content = ""
		err = nil
	}
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.GetRuleSetContentResponse{Content: content}), nil
}

func (s *appRuntimeService) SaveRuleSetContent(ctx context.Context, req *connect.Request[appv1.SaveRuleSetContentRequest]) (*connect.Response[appv1.SaveRuleSetContentResponse], error) {
	item, err := saveRuleSetContent(req.Msg.GetId(), req.Msg.GetContent())
	if err != nil {
		return nil, asConnectError(err)
	}
	s.markRuntimeChanged("ruleset", req.Msg.GetId())
	return connect.NewResponse(&appv1.SaveRuleSetContentResponse{RulesetJson: item}), nil
}

func (s *appRuntimeService) ClearRuleSetContent(ctx context.Context, req *connect.Request[appv1.ClearRuleSetContentRequest]) (*connect.Response[appv1.ClearRuleSetContentResponse], error) {
	item, err := saveRuleSetContent(req.Msg.GetId(), `{"version":1,"rules":[]}`)
	if err != nil {
		return nil, asConnectError(err)
	}
	s.markRuntimeChanged("ruleset", req.Msg.GetId())
	return connect.NewResponse(&appv1.ClearRuleSetContentResponse{RulesetJson: item}), nil
}

func (s *appRuntimeService) ListScheduledTasks(ctx context.Context, req *connect.Request[appv1.ListScheduledTasksRequest]) (*connect.Response[appv1.ListScheduledTasksResponse], error) {
	tasks, err := loadScheduledTasks()
	if err != nil {
		return nil, asConnectError(err)
	}
	items, err := scheduledTasksToJSON(tasks)
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.ListScheduledTasksResponse{TasksJson: items}), nil
}

func (s *appRuntimeService) SaveScheduledTasks(ctx context.Context, req *connect.Request[appv1.SaveScheduledTasksRequest]) (*connect.Response[appv1.SaveScheduledTasksResponse], error) {
	items, err := saveScheduledTasksJSON(req.Msg.GetTasksJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	tasks, _ := loadScheduledTasks()
	s.mu.Lock()
	s.taskLogs = trimScheduledTaskLogs(s.taskLogs, tasks)
	_ = saveScheduledTaskLogs(s.taskLogs)
	s.mu.Unlock()
	s.restartScheduler()
	return connect.NewResponse(&appv1.SaveScheduledTasksResponse{TasksJson: items}), nil
}

func (s *appRuntimeService) UpsertScheduledTask(ctx context.Context, req *connect.Request[appv1.UpsertScheduledTaskRequest]) (*connect.Response[appv1.UpsertScheduledTaskResponse], error) {
	var task scheduledTask
	if err := json.Unmarshal([]byte(req.Msg.GetTaskJson()), &task); err != nil {
		return nil, asConnectError(err)
	}
	if !task.Disabled && task.Type == scheduledTaskRunScript {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("run::script is not supported by the backend scheduler"))
	}
	item, err := upsertScheduledTaskJSON(req.Msg.GetTaskJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	tasks, _ := loadScheduledTasks()
	s.mu.Lock()
	s.taskLogs = trimScheduledTaskLogs(s.taskLogs, tasks)
	_ = saveScheduledTaskLogs(s.taskLogs)
	s.mu.Unlock()
	s.restartScheduler()
	return connect.NewResponse(&appv1.UpsertScheduledTaskResponse{TaskJson: item}), nil
}

func (s *appRuntimeService) DeleteScheduledTask(ctx context.Context, req *connect.Request[appv1.DeleteScheduledTaskRequest]) (*connect.Response[appv1.DeleteScheduledTaskResponse], error) {
	if err := deleteJSONItem(scheduledTasksPath, req.Msg.GetId()); err != nil {
		return nil, asConnectError(err)
	}
	s.mu.Lock()
	s.taskLogs = removeScheduledTaskLogs(s.taskLogs, req.Msg.GetId())
	_ = saveScheduledTaskLogs(s.taskLogs)
	s.mu.Unlock()
	s.restartScheduler()
	return connect.NewResponse(&appv1.DeleteScheduledTaskResponse{}), nil
}

func (s *appRuntimeService) RunScheduledTask(ctx context.Context, req *connect.Request[appv1.RunScheduledTaskRequest]) (*connect.Response[appv1.RunScheduledTaskResponse], error) {
	log, err := s.runScheduledTask(ctx, req.Msg.GetId())
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.RunScheduledTaskResponse{
		Results:   taskResultsToProto(log.Results),
		StartTime: log.StartTime,
		EndTime:   log.EndTime,
	}), nil
}

func (s *appRuntimeService) ListScheduledTaskLogs(ctx context.Context, req *connect.Request[appv1.ListScheduledTaskLogsRequest]) (*connect.Response[appv1.ListScheduledTaskLogsResponse], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	logs := make([]*appv1.TaskLog, 0, len(s.taskLogs))
	for _, log := range s.taskLogs {
		if req.Msg.GetId() == "" || log.ID == req.Msg.GetId() {
			logs = append(logs, scheduledTaskLogToProto(log))
		}
	}
	return connect.NewResponse(&appv1.ListScheduledTaskLogsResponse{Logs: logs}), nil
}

func (s *appRuntimeService) ClearScheduledTaskLogs(ctx context.Context, req *connect.Request[appv1.ClearScheduledTaskLogsRequest]) (*connect.Response[appv1.ClearScheduledTaskLogsResponse], error) {
	s.mu.Lock()
	s.taskLogs = nil
	_ = saveScheduledTaskLogs(s.taskLogs)
	s.mu.Unlock()
	return connect.NewResponse(&appv1.ClearScheduledTaskLogsResponse{}), nil
}

func (s *appRuntimeService) NextScheduledTaskRuns(ctx context.Context, req *connect.Request[appv1.NextScheduledTaskRunsRequest]) (*connect.Response[appv1.NextScheduledTaskRunsResponse], error) {
	count := int(req.Msg.GetCount())
	if count <= 0 {
		count = 10
	}
	times, err := nextCronRuns(req.Msg.GetCron(), count, time.Now())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&appv1.NextScheduledTaskRunsResponse{Times: times}), nil
}
