package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/rpcutil"
	"guiforcores/bridge/storage"
	"guiforcores/bridge/syncstate"
	"guiforcores/gen/app/v1"
	commonv1 "guiforcores/gen/common/v1"

	connect "connectrpc.com/connect"
	"github.com/dop251/goja"
	"gopkg.in/yaml.v3"
)

const (
	subscriptionsFilePath        = "data/subscribes.yaml"
	runtimeRulesetsFilePath      = "data/rulesets.yaml"
	scheduledTasksPath           = "data/scheduledtasks.yaml"
	scheduledTaskLogsPath        = "data/scheduledtasklogs.yaml"
	defaultScheduledTaskLogLimit = 20
	maxSubscriptionBodyBytes     = 32 << 20
	rulesetHubPath               = "data/.cache/ruleset-list.json"
	defaultSourceRuleSetContent  = `{"version":2,"rules":[]}`
)

type KernelController interface {
	ReferencedResourcesChanged(domain syncstate.Domain, ids []string)
}

type AppConfigReader interface {
	Current() config.AppConfig
}

type EventPublisher interface {
	Publish(eventName string, data ...any)
}

type rulesetHub struct {
	Geosite string           `json:"geosite"`
	Geoip   string           `json:"geoip"`
	List    []rulesetHubItem `json:"list"`
}

type rulesetHubItem struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type appRuntimeService struct {
	platform *platform.Service
	paths    *storage.Paths
	config   AppConfigReader
	events   EventPublisher
	kernel   KernelController
	state    *syncstate.Coordinator

	mu               sync.Mutex
	subscriptionsMu  sync.Mutex
	rulesetsMu       sync.Mutex
	scheduledTasksMu sync.Mutex
	taskCancel       chan struct{}
	runningTask      map[string]bool
	taskLogs         []scheduledTaskLog
}

type Service = appRuntimeService

var runtimePaths atomic.Pointer[storage.Paths]

var ruleSetHubHTTPRequest = httpRequest
var (
	errHTTPResponseTooLarge = errors.New("HTTP response body exceeds the allowed size")
	subscriptionHTTPRequest = subscriptionHTTPCall
)

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

func NewService(platformService *platform.Service, paths *storage.Paths, configStore AppConfigReader, events EventPublisher, kernelController KernelController, coordinators ...*syncstate.Coordinator) *Service {
	setRuntimePaths(paths)
	state := syncstate.NewCoordinator()
	if len(coordinators) > 0 && coordinators[0] != nil {
		state = coordinators[0]
	}
	logs, _ := loadScheduledTaskLogs()
	tasks, _ := loadScheduledTasks()
	logs = trimScheduledTaskLogs(logs, tasks)
	return &Service{
		platform:    platformService,
		paths:       paths,
		config:      configStore,
		events:      events,
		kernel:      kernelController,
		state:       state,
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

func (s *appRuntimeService) publishResourceChanged(domain syncstate.Domain, operation syncstate.Operation, ids []string, state interface {
	GetInstanceId() string
	GetStateRevision() uint64
}) {
	if ids == nil {
		ids = []string{}
	}
	s.publish("resourceChanged", map[string]any{
		"domain":        string(domain),
		"operation":     string(operation),
		"ids":           ids,
		"instanceId":    state.GetInstanceId(),
		"stateRevision": state.GetStateRevision(),
	})
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
			s.scheduledTasksMu.Lock()
			tasks, err := loadScheduledTasks()
			s.scheduledTasksMu.Unlock()
			if err != nil {
				continue
			}
			for _, task := range tasks {
				if !shouldScheduleTask(task) {
					continue
				}
				if cronMatches(task.Cron, now) {
					runKey := cronRunKey(task.Cron, now)
					if lastRun[task.ID] == runKey {
						continue
					}
					lastRun[task.ID] = runKey
					go s.runScheduledTask(context.Background(), task.ID, true)
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
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return storage.AtomicWriteFile(fullPath, data, 0644)
}

func validateSourceType(value any, resource string) error {
	sourceType, _ := value.(string)
	if sourceType == "Http" || sourceType == "Manual" {
		return nil
	}
	return invalidArgumentError{message: fmt.Sprintf("unsupported %s type %q", resource, sourceType)}
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

func loadSubscriptions() ([]subscription, error) {
	items, err := readRuntimeYAMLFile[[]subscription](subscriptionsFilePath)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []subscription{}
	}
	return items, nil
}

func saveSubscriptions(items []subscription) error {
	for i := range items {
		items[i].Updating = false
	}
	return writeRuntimeYAMLFile(subscriptionsFilePath, items)
}

func subscriptionsToJSON(items []subscription) ([]string, error) {
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

func decodeSubscription(itemJSON string) (subscription, error) {
	var item subscription
	if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
		return item, err
	}
	if item.ID == "" {
		return item, invalidArgumentError{message: "id is required"}
	}
	if err := validateSourceType(item.Type, "subscription"); err != nil {
		return item, err
	}
	item.Updating = false
	return item, nil
}

func deleteSubscription(id string) error {
	items, err := loadSubscriptions()
	if err != nil {
		return err
	}
	next := items[:0]
	found := false
	for _, item := range items {
		if item.ID == id {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		return nil
	}
	if err := saveSubscriptions(next); err != nil {
		return err
	}
	_ = os.Remove(GetPath(subscriptionContentPath(id)))
	return nil
}

func saveSubscriptionContent(id string, content string) (string, bool, error) {
	sub, items, err := findSubscription(id)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(content) == "" {
		content = "[]"
	}
	var proxies []map[string]any
	if err := json.Unmarshal([]byte(content), &proxies); err != nil {
		return "", false, invalidArgumentError{message: "not a valid subscription json: " + err.Error()}
	}
	data, err := json.MarshalIndent(proxies, "", "  ")
	if err != nil {
		return "", false, err
	}
	path := subscriptionContentPath(id)
	previous, readErr := os.ReadFile(GetPath(path))
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", false, readErr
	}
	if bytes.Equal(previous, data) {
		result, marshalErr := json.Marshal(sub)
		return string(result), false, marshalErr
	}
	if err := os.MkdirAll(filepath.Dir(GetPath(path)), os.ModePerm); err != nil {
		return "", false, err
	}
	if err := storage.AtomicWriteFile(GetPath(path), data, 0644); err != nil {
		return "", false, err
	}
	sub.Proxies = proxyRefsFromOutbounds(proxies, sub.Proxies)
	sub.UpdateTime = time.Now().UnixMilli()
	if err := saveSubscriptions(items); err != nil {
		return "", false, err
	}
	result, err := json.Marshal(sub)
	return string(result), true, err
}

func findSubscription(id string) (*subscription, []subscription, error) {
	items, err := loadSubscriptions()
	if err != nil {
		return nil, nil, err
	}
	for i := range items {
		if items[i].ID == id {
			return &items[i], items, nil
		}
	}
	return nil, items, fmt.Errorf("subscription %q not found", id)
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

func normalizeRuleSetContent(content string) ([]byte, int, error) {
	if strings.TrimSpace(content) == "" {
		content = defaultSourceRuleSetContent
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, 0, invalidArgumentError{message: "not a valid ruleset json: " + err.Error()}
	}
	if parsed["rules"] == nil {
		return nil, 0, invalidArgumentError{message: "not a valid ruleset json: missing rules"}
	}
	data, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return nil, 0, err
	}
	return data, countRules(parsed["rules"]), nil
}

func saveRuleSetContent(id string, content string) (string, bool, error) {
	r, items, err := findRuleset(id)
	if err != nil {
		return "", false, err
	}
	if r.Format != "source" {
		return "", false, invalidArgumentError{message: "only source rulesets have editable content"}
	}
	data, count, err := normalizeRuleSetContent(content)
	if err != nil {
		return "", false, err
	}
	current, readErr := readText(r.Path)
	if readErr == nil {
		if currentData, _, normalizeErr := normalizeRuleSetContent(current); normalizeErr == nil && bytes.Equal(currentData, data) {
			result, marshalErr := json.Marshal(r)
			return string(result), false, marshalErr
		}
	} else if !os.IsNotExist(readErr) {
		return "", false, readErr
	}
	if err := storage.AtomicWriteFile(GetPath(r.Path), data, 0644); err != nil {
		return "", false, err
	}
	r.Count = count
	r.UpdateTime = time.Now().UnixMilli()
	if err := saveRulesets(items); err != nil {
		return "", false, err
	}
	result, err := json.Marshal(r)
	return string(result), true, err
}

func ensureDefaultManualRuleSetContent(item ruleset) (bool, error) {
	if item.Type != "Manual" || item.Format != "source" || item.Path == "" {
		return false, nil
	}

	path := GetPath(item.Path)
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(defaultSourceRuleSetContent), &parsed); err != nil {
		return false, err
	}
	data, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return false, err
	}
	if err := storage.AtomicWriteFile(path, data, 0644); err != nil {
		return false, err
	}
	return true, nil
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

func subscriptionContentPath(id string) string {
	return "data/subscribes/" + safeFileName(id, "subscription") + ".json"
}

func safeRulesetFileName(id string) string {
	return safeFileName(id, "ruleset")
}

func safeFileName(id string, fallback string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", " ", "_")
	safe := replacer.Replace(id)
	safe = strings.Trim(safe, "._")
	if safe == "" {
		return fallback
	}
	return safe
}

func migrateRulesetFile(from string, to string) {
	migrateManagedFile(from, to)
}

func migrateManagedFile(from string, to string) {
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

func isSupportedScheduledTaskType(taskType string) bool {
	switch taskType {
	case scheduledTaskUpdateSubscription,
		scheduledTaskUpdateRuleset,
		scheduledTaskUpdateAllSubscription,
		scheduledTaskUpdateAllRuleset:
		return true
	default:
		return false
	}
}

func shouldScheduleTask(task scheduledTask) bool {
	return !task.Disabled && task.ID != "" && task.Cron != "" && isSupportedScheduledTaskType(task.Type)
}

func rulesetIDs(items []ruleset) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func subscriptionIDs(items []subscription) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func scheduledTaskIDs(items []scheduledTask) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func stringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateRuntimeOrderIDs(resource string, current []string, requested []string) error {
	if len(current) != len(requested) {
		return invalidArgumentError{message: fmt.Sprintf("order must contain every %s id exactly once", resource)}
	}
	seen := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		if _, exists := seen[id]; exists {
			return invalidArgumentError{message: fmt.Sprintf("duplicate %s id %q in order", resource, id)}
		}
		seen[id] = struct{}{}
	}
	for _, id := range current {
		if _, exists := seen[id]; !exists {
			return invalidArgumentError{message: fmt.Sprintf("order must contain every %s id exactly once", resource)}
		}
	}
	return nil
}

func rulesetConfigEqual(left ruleset, right ruleset) bool {
	left.UpdateTime, right.UpdateTime = 0, 0
	left.Path, right.Path = "", ""
	left.Count, right.Count = 0, 0
	left.Updating, right.Updating = false, false
	return left == right
}

func subscriptionConfigEqual(left subscription, right subscription) bool {
	left.Upload, right.Upload = 0, 0
	left.Download, right.Download = 0, 0
	left.Total, right.Total = 0, 0
	left.Expire, right.Expire = 0, 0
	left.UpdateTime, right.UpdateTime = 0, 0
	left.Proxies, right.Proxies = nil, nil
	left.Updating, right.Updating = false, false
	if len(left.Header.Request) == 0 {
		left.Header.Request = nil
	}
	if len(right.Header.Request) == 0 {
		right.Header.Request = nil
	}
	if len(left.Header.Response) == 0 {
		left.Header.Response = nil
	}
	if len(right.Header.Response) == 0 {
		right.Header.Response = nil
	}
	return reflect.DeepEqual(left, right)
}

func cloneSubscription(item subscription) subscription {
	item.Header.Request = cloneStringMap(item.Header.Request)
	item.Header.Response = cloneStringMap(item.Header.Response)
	item.Proxies = append([]proxyRef(nil), item.Proxies...)
	return item
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func scheduledTaskConfigEqual(left scheduledTask, right scheduledTask) bool {
	left.LastTime, right.LastTime = 0, 0
	if len(left.Subscriptions) == 0 {
		left.Subscriptions = nil
	}
	if len(right.Subscriptions) == 0 {
		right.Subscriptions = nil
	}
	if len(left.Rulesets) == 0 {
		left.Rulesets = nil
	}
	if len(right.Rulesets) == 0 {
		right.Rulesets = nil
	}
	return reflect.DeepEqual(left, right)
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
			Ok:            result.GetOk(),
			ID:            result.GetId(),
			Name:          result.GetName(),
			Result:        result.GetResult(),
			SuccessCount:  result.GetSuccessCount(),
			FilteredCount: result.GetFilteredCount(),
			SkippedCount:  result.GetSkippedCount(),
			FailureReason: result.GetFailureReason(),
		})
	}
	return out
}

func taskResultsToProto(results []scheduledTaskResult) []*appv1.TaskResult {
	out := make([]*appv1.TaskResult, 0, len(results))
	for _, result := range results {
		out = append(out, &appv1.TaskResult{
			Ok:            result.Ok,
			Id:            result.ID,
			Name:          result.Name,
			Result:        result.Result,
			SuccessCount:  result.SuccessCount,
			FilteredCount: result.FilteredCount,
			SkippedCount:  result.SkippedCount,
			FailureReason: result.FailureReason,
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

func subscriptionFailureResult(sub *subscription, reason error) *appv1.TaskResult {
	failureReason := reason.Error()
	return &appv1.TaskResult{
		Ok:            false,
		Id:            sub.ID,
		Name:          sub.Name,
		Result:        fmt.Sprintf("Failed to update subscription [%s]. Reason: %s", sub.Name, failureReason),
		FailureReason: failureReason,
	}
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
	return httpRequestWithLimit(method, rawURL, headers, body, insecure, timeoutSeconds, 0)
}

func subscriptionHTTPCall(method string, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int) (*http.Response, string, error) {
	return httpRequestWithLimit(method, rawURL, headers, body, insecure, timeoutSeconds, maxSubscriptionBodyBytes)
}

func httpRequestWithLimit(method string, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int, maxBodyBytes int64) (*http.Response, string, error) {
	if method == "" {
		method = http.MethodGet
	}
	timeout := platform.GetTimeout(timeoutSeconds)
	transport := &http.Transport{
		Proxy: platform.GetProxy(""),
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecure,
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
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
	data, err := readHTTPBody(resp.Body, maxBodyBytes)
	return resp, data, err
}

func readHTTPBody(reader io.Reader, maxBodyBytes int64) (string, error) {
	if maxBodyBytes > 0 {
		reader = io.LimitReader(reader, maxBodyBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if maxBodyBytes > 0 && int64(len(data)) > maxBodyBytes {
		return "", errHTTPResponseTooLarge
	}
	return string(data), nil
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

func subscriptionRequestHeaders(headers map[string]string, defaultUserAgent string) map[string]string {
	result := make(map[string]string, len(headers)+1)
	userAgent := ""
	for key, value := range headers {
		if strings.EqualFold(key, "User-Agent") {
			if userAgent == "" && strings.TrimSpace(value) != "" {
				userAgent = value
			}
			continue
		}
		result[key] = value
	}
	if userAgent == "" && strings.TrimSpace(defaultUserAgent) != "" {
		userAgent = defaultUserAgent
	}
	if userAgent != "" {
		result["User-Agent"] = userAgent
	}
	return result
}

func updateSubscriptionAt(items []subscription, idx int, defaultUserAgent string) (*appv1.TaskResult, bool) {
	sub := &items[idx]
	if sub.Disabled {
		return subscriptionFailureResult(sub, errors.New("subscription is disabled")), false
	}
	body := ""
	userInfo := map[string]int64{}
	var err error

	switch sub.Type {
	case "Manual":
		body, err = readText(subscriptionContentPath(sub.ID))
	case "Http":
		headers := subscriptionRequestHeaders(sub.Header.Request, defaultUserAgent)
		resp, text, reqErr := subscriptionHTTPRequest(sub.RequestMethod, sub.URL, headers, "", sub.InSecure, sub.RequestTimeout)
		switch {
		case errors.Is(reqErr, errHTTPResponseTooLarge):
			err = errHTTPResponseTooLarge
		case reqErr != nil:
			err = errors.New("subscription request failed")
		case resp == nil:
			err = errors.New("subscription request returned no response")
		case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
			err = fmt.Errorf("subscription request returned HTTP status %d", resp.StatusCode)
		}
		body = text
		if err == nil && resp != nil {
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
		return subscriptionFailureResult(sub, err), false
	}

	parseResult, err := parseSubscriptionBody(body, sub.Type, sub.EnableNodeConversion)
	if err != nil {
		return subscriptionFailureResult(sub, err), false
	}
	proxies := parseResult.proxies
	filteredCount := 0

	if sub.Type != "Manual" {
		include, err := compileSmartRegexp(sub.Include)
		if err != nil {
			return subscriptionFailureResult(sub, err), false
		}
		exclude, err := compileSmartRegexp(sub.Exclude)
		if err != nil {
			return subscriptionFailureResult(sub, err), false
		}
		includeProtocol, err := compileSmartRegexp(sub.IncludeProtocol)
		if err != nil {
			return subscriptionFailureResult(sub, err), false
		}
		excludeProtocol, err := compileSmartRegexp(sub.ExcludeProtocol)
		if err != nil {
			return subscriptionFailureResult(sub, err), false
		}
		filtered := proxies[:0]
		for _, proxy := range proxies {
			tag, _ := proxy["tag"].(string)
			typ, _ := proxy["type"].(string)
			if include != nil && !include.MatchString(tag) {
				filteredCount++
				continue
			}
			if exclude != nil && exclude.MatchString(tag) {
				filteredCount++
				continue
			}
			if includeProtocol != nil && !includeProtocol.MatchString(typ) {
				filteredCount++
				continue
			}
			if excludeProtocol != nil && excludeProtocol.MatchString(typ) {
				filteredCount++
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
		return subscriptionFailureResult(sub, err), false
	}
	processedSub.ID = sub.ID
	processedSub.Updating = false
	*sub = processedSub
	sub.Proxies = proxyRefsFromOutbounds(processedProxies, sub.Proxies)

	if sub.Type == "Http" {
		data, err := json.MarshalIndent(processedProxies, "", "  ")
		if err != nil {
			return subscriptionFailureResult(sub, err), false
		}
		contentPath := subscriptionContentPath(sub.ID)
		if err := os.MkdirAll(filepath.Dir(GetPath(contentPath)), os.ModePerm); err != nil {
			return subscriptionFailureResult(sub, err), false
		}
		if err := storage.AtomicWriteFile(GetPath(contentPath), data, 0644); err != nil {
			return subscriptionFailureResult(sub, err), false
		}
	}

	message := fmt.Sprintf(
		"Subscription [%s] updated successfully. Imported %d proxies; filtered %d proxies; skipped %d invalid or unsupported proxies.",
		sub.Name,
		len(processedProxies),
		filteredCount,
		parseResult.skipped,
	)
	return &appv1.TaskResult{
		Ok:            true,
		Id:            sub.ID,
		Name:          sub.Name,
		Result:        message,
		SuccessCount:  uint32(len(processedProxies)),
		FilteredCount: uint32(filteredCount),
		SkippedCount:  uint32(parseResult.skipped),
	}, true
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
		if (r.Type == "Http" && r.URL != r.Path) || (r.Type == "Manual" && !exists) {
			data, _ := json.MarshalIndent(rules, "", "  ")
			if err := storage.AtomicWriteFile(GetPath(r.Path), data, 0644); err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
		}
	}
	if r.Format == "binary" {
		if r.Type == "Http" {
			resp, err := http.Get(r.URL)
			if err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
			if err := storage.AtomicWriteFile(GetPath(r.Path), data, 0644); err != nil {
				return taskResult(false, r.ID, r.Tag, err.Error()), false
			}
		}
	}
	r.UpdateTime = time.Now().UnixMilli()
	return taskResult(true, r.ID, r.Tag, fmt.Sprintf("Ruleset [%s] updated successfully.", r.Tag)), true
}

func (s *appRuntimeService) notifyReferencedResourcesChanged(domain syncstate.Domain, ids []string) {
	if s.kernel != nil {
		s.kernel.ReferencedResourcesChanged(domain, ids)
	}
}

func mustRead(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

func (s *appRuntimeService) updateSubscription(id string) ([]*appv1.TaskResult, *commonv1.MutationState, error) {
	s.subscriptionsMu.Lock()
	items, err := loadSubscriptions()
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, nil, err
	}
	defaultUserAgent := s.config.Current().UserAgent
	for i := range items {
		if items[i].ID == id {
			before := cloneSubscription(items[i])
			result, changed := updateSubscriptionAt(items, i, defaultUserAgent)
			if changed {
				if err := saveSubscriptions(items); err != nil {
					s.subscriptionsMu.Unlock()
					return nil, nil, err
				}
			}
			configChanged := changed && !subscriptionConfigEqual(before, items[i])
			state := s.state.Mutation(syncstate.DomainSubscriptions, subscriptionIDs(items), id)
			operation := syncstate.OperationRuntime
			if changed {
				if configChanged {
					state = s.state.Advance(syncstate.DomainSubscriptions, subscriptionIDs(items), []string{id}, nil, false, id)
					operation = syncstate.OperationUpsert
				} else {
					state = s.state.AdvanceRuntime(syncstate.DomainSubscriptions, subscriptionIDs(items), id)
				}
			}
			s.subscriptionsMu.Unlock()
			if changed {
				s.publishResourceChanged(syncstate.DomainSubscriptions, operation, []string{id}, state)
				s.notifyReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{id})
			}
			return []*appv1.TaskResult{result}, state, nil
		}
	}
	s.subscriptionsMu.Unlock()
	return nil, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("subscription %q not found", id))
}

func (s *appRuntimeService) updateAllSubscriptions() ([]*appv1.TaskResult, *commonv1.MutationState, error) {
	s.subscriptionsMu.Lock()
	items, err := loadSubscriptions()
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, nil, err
	}
	defaultUserAgent := s.config.Current().UserAgent
	results := make([]*appv1.TaskResult, 0, len(items))
	changedIDs := make([]string, 0, len(items))
	configChangedIDs := make([]string, 0, len(items))
	for i := range items {
		if items[i].Disabled {
			continue
		}
		before := cloneSubscription(items[i])
		result, didChange := updateSubscriptionAt(items, i, defaultUserAgent)
		results = append(results, result)
		if didChange {
			changedIDs = append(changedIDs, items[i].ID)
			if !subscriptionConfigEqual(before, items[i]) {
				configChangedIDs = append(configChangedIDs, items[i].ID)
			}
		}
	}
	if len(changedIDs) > 0 {
		if err := saveSubscriptions(items); err != nil {
			s.subscriptionsMu.Unlock()
			return nil, nil, err
		}
	}
	state := s.state.Mutation(syncstate.DomainSubscriptions, subscriptionIDs(items), "")
	operation := syncstate.OperationRuntime
	if len(changedIDs) > 0 {
		if len(configChangedIDs) > 0 {
			state = s.state.Advance(syncstate.DomainSubscriptions, subscriptionIDs(items), configChangedIDs, nil, false, "")
			operation = syncstate.OperationUpsert
		} else {
			state = s.state.AdvanceRuntime(syncstate.DomainSubscriptions, subscriptionIDs(items))
		}
	}
	s.subscriptionsMu.Unlock()
	if len(changedIDs) > 0 {
		s.publishResourceChanged(syncstate.DomainSubscriptions, operation, changedIDs, state)
		s.notifyReferencedResourcesChanged(syncstate.DomainSubscriptions, changedIDs)
	}
	return results, state, nil
}

func updateRulesetLocked(id string) ([]*appv1.TaskResult, bool, error) {
	items, err := loadRulesets()
	if err != nil {
		return nil, false, err
	}
	for i := range items {
		if items[i].ID == id {
			result, changed := updateRulesetAt(items, i)
			if changed {
				if err := saveRulesets(items); err != nil {
					return nil, false, err
				}
			}
			return []*appv1.TaskResult{result}, changed, nil
		}
	}
	return nil, false, connect.NewError(connect.CodeNotFound, fmt.Errorf("ruleset %q not found", id))
}

func (s *appRuntimeService) updateRuleset(id string) ([]*appv1.TaskResult, *commonv1.MutationState, error) {
	s.rulesetsMu.Lock()
	results, changed, err := updateRulesetLocked(id)
	items, loadErr := loadRulesets()
	if err == nil && loadErr != nil {
		err = loadErr
	}
	var state *commonv1.MutationState
	if err == nil {
		if changed {
			state = s.state.AdvanceRuntime(syncstate.DomainRuleSets, rulesetIDs(items), id)
		} else {
			state = s.state.Mutation(syncstate.DomainRuleSets, rulesetIDs(items), id)
		}
	}
	s.rulesetsMu.Unlock()
	if err == nil && changed {
		s.publishResourceChanged(syncstate.DomainRuleSets, syncstate.OperationRuntime, []string{id}, state)
		s.notifyReferencedResourcesChanged(syncstate.DomainRuleSets, []string{id})
	}
	return results, state, err
}

func updateAllRulesetsLocked() ([]*appv1.TaskResult, bool, error) {
	items, err := loadRulesets()
	if err != nil {
		return nil, false, err
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
			return nil, false, err
		}
	}
	return results, changed, nil
}

func (s *appRuntimeService) updateAllRulesets() ([]*appv1.TaskResult, *commonv1.MutationState, error) {
	s.rulesetsMu.Lock()
	results, changed, err := updateAllRulesetsLocked()
	items, loadErr := loadRulesets()
	if err == nil && loadErr != nil {
		err = loadErr
	}
	var state *commonv1.MutationState
	if err == nil {
		if changed {
			state = s.state.AdvanceRuntime(syncstate.DomainRuleSets, rulesetIDs(items))
		} else {
			state = s.state.Mutation(syncstate.DomainRuleSets, rulesetIDs(items), "")
		}
	}
	s.rulesetsMu.Unlock()
	if err == nil && changed {
		s.publishResourceChanged(syncstate.DomainRuleSets, syncstate.OperationRuntime, nil, state)
		s.notifyReferencedResourcesChanged(syncstate.DomainRuleSets, rulesetIDs(items))
	}
	return results, state, err
}

func (s *appRuntimeService) runScheduledTask(ctx context.Context, id string, publishCompletion bool) (scheduledTaskLog, error) {
	s.mu.Lock()
	if s.runningTask[id] {
		s.mu.Unlock()
		now := time.Now().UnixMilli()
		result := []*appv1.TaskResult{taskResult(false, id, "", "Skipped: task is already running")}
		log := s.recordTaskLog(id, "", now, now, result)
		s.publish("scheduledTaskFinished", id, publishCompletion && s.scheduledTaskNotificationEnabled(id))
		return log, nil
	}
	s.runningTask[id] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.runningTask, id)
		s.mu.Unlock()
	}()

	s.scheduledTasksMu.Lock()
	tasks, err := loadScheduledTasks()
	s.scheduledTasksMu.Unlock()
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
			r, _, err := s.updateSubscription(subID)
			if err != nil {
				results = append(results, taskResult(false, subID, "", err.Error()))
				continue
			}
			results = append(results, r...)
		}
	case scheduledTaskUpdateRuleset:
		for _, rulesetID := range task.Rulesets {
			r, _, err := s.updateRuleset(rulesetID)
			if err != nil {
				results = append(results, taskResult(false, rulesetID, "", err.Error()))
				continue
			}
			results = append(results, r...)
		}
	case scheduledTaskUpdateAllSubscription:
		results, _, err = s.updateAllSubscriptions()
	case scheduledTaskUpdateAllRuleset:
		results, _, err = s.updateAllRulesets()
	default:
		results = []*appv1.TaskResult{taskResult(false, task.ID, task.Name, "unsupported scheduled task type: "+task.Type)}
	}
	if err != nil {
		results = append(results, taskResult(false, task.ID, task.Name, err.Error()))
	}
	end := time.Now().UnixMilli()
	var taskState *commonv1.MutationState
	s.scheduledTasksMu.Lock()
	latestTasks, loadErr := loadScheduledTasks()
	if loadErr == nil {
		for i := range latestTasks {
			if latestTasks[i].ID == task.ID {
				latestTasks[i].LastTime = end
				if saveErr := saveScheduledTasks(latestTasks); saveErr == nil {
					taskState = s.state.AdvanceRuntime(syncstate.DomainScheduledTasks, scheduledTaskIDs(latestTasks), task.ID)
				}
				break
			}
		}
	}
	s.scheduledTasksMu.Unlock()
	log := s.recordTaskLog(task.ID, task.Name, start, end, results)
	if taskState != nil {
		s.publishResourceChanged(syncstate.DomainScheduledTasks, syncstate.OperationRuntime, []string{task.ID}, taskState)
	}
	s.publish("scheduledTaskFinished", task.ID, publishCompletion && task.Notification)
	_ = ctx
	return log, nil
}

func (s *appRuntimeService) scheduledTaskNotificationEnabled(id string) bool {
	s.scheduledTasksMu.Lock()
	defer s.scheduledTasksMu.Unlock()
	tasks, err := loadScheduledTasks()
	if err != nil {
		return false
	}
	for _, task := range tasks {
		if task.ID == id {
			return task.Notification
		}
	}
	return false
}

func (s *appRuntimeService) recordTaskLog(id string, name string, start int64, end int64, results []*appv1.TaskResult) scheduledTaskLog {
	s.scheduledTasksMu.Lock()
	tasks, tasksErr := loadScheduledTasks()
	s.scheduledTasksMu.Unlock()

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

	if tasksErr == nil {
		s.taskLogs = trimScheduledTaskLogs(s.taskLogs, tasks)
	}
	_ = saveScheduledTaskLogs(s.taskLogs)
	return log
}

func (s *appRuntimeService) ListSubscriptions(ctx context.Context, req *connect.Request[appv1.ListSubscriptionsRequest]) (*connect.Response[appv1.ListSubscriptionsResponse], error) {
	s.subscriptionsMu.Lock()
	subscriptions, err := loadSubscriptions()
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	items, err := subscriptionsToJSON(subscriptions)
	state := s.state.Snapshot(syncstate.DomainSubscriptions, subscriptionIDs(subscriptions))
	s.subscriptionsMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.ListSubscriptionsResponse{SubscriptionsJson: items, State: state}), nil
}

func (s *appRuntimeService) CreateSubscription(ctx context.Context, req *connect.Request[appv1.CreateSubscriptionRequest]) (*connect.Response[appv1.CreateSubscriptionResponse], error) {
	item, err := decodeSubscription(req.Msg.GetSubscriptionJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	item.Upload = 0
	item.Download = 0
	item.Total = 0
	item.Expire = 0
	item.UpdateTime = 0
	item.Proxies = nil
	s.subscriptionsMu.Lock()
	items, err := loadSubscriptions()
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	for _, existing := range items {
		if existing.ID == item.ID {
			s.subscriptionsMu.Unlock()
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("subscription %q already exists", item.ID))
		}
	}
	items = append(items, item)
	if err := saveSubscriptions(items); err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainSubscriptions, subscriptionIDs(items), []string{item.ID}, nil, true, item.ID)
	data, err := json.Marshal(item)
	s.subscriptionsMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	s.publishResourceChanged(syncstate.DomainSubscriptions, syncstate.OperationUpsert, []string{item.ID}, state)
	s.notifyReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{item.ID})
	return connect.NewResponse(&appv1.CreateSubscriptionResponse{SubscriptionJson: string(data), State: state}), nil
}

func (s *appRuntimeService) UpdateSubscriptionConfig(ctx context.Context, req *connect.Request[appv1.UpdateSubscriptionConfigRequest]) (*connect.Response[appv1.UpdateSubscriptionConfigResponse], error) {
	requested, err := decodeSubscription(req.Msg.GetSubscriptionJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	s.subscriptionsMu.Lock()
	items, err := loadSubscriptions()
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	idx := -1
	for index := range items {
		if items[index].ID == requested.ID {
			idx = index
			break
		}
	}
	if idx == -1 {
		s.subscriptionsMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("subscription %q not found", requested.ID))
	}
	if err := s.state.CheckItem(syncstate.DomainSubscriptions, subscriptionIDs(items), requested.ID, req.Msg.GetExpectedRevision(), true); err != nil {
		s.subscriptionsMu.Unlock()
		return nil, err
	}
	existing := items[idx]
	requested.Upload = existing.Upload
	requested.Download = existing.Download
	requested.Total = existing.Total
	requested.Expire = existing.Expire
	requested.UpdateTime = existing.UpdateTime
	requested.Proxies = existing.Proxies
	requested.Updating = false
	if subscriptionConfigEqual(existing, requested) {
		state := s.state.Mutation(syncstate.DomainSubscriptions, subscriptionIDs(items), requested.ID)
		data, marshalErr := json.Marshal(existing)
		s.subscriptionsMu.Unlock()
		if marshalErr != nil {
			return nil, asConnectError(marshalErr)
		}
		return connect.NewResponse(&appv1.UpdateSubscriptionConfigResponse{SubscriptionJson: string(data), State: state}), nil
	}
	items[idx] = requested
	if err := saveSubscriptions(items); err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainSubscriptions, subscriptionIDs(items), []string{requested.ID}, nil, false, requested.ID)
	data, err := json.Marshal(requested)
	s.subscriptionsMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	s.publishResourceChanged(syncstate.DomainSubscriptions, syncstate.OperationUpsert, []string{requested.ID}, state)
	s.notifyReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{requested.ID})
	return connect.NewResponse(&appv1.UpdateSubscriptionConfigResponse{SubscriptionJson: string(data), State: state}), nil
}

func (s *appRuntimeService) DeleteSubscription(ctx context.Context, req *connect.Request[appv1.DeleteSubscriptionRequest]) (*connect.Response[appv1.DeleteSubscriptionResponse], error) {
	s.subscriptionsMu.Lock()
	items, err := loadSubscriptions()
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	found := false
	for _, item := range items {
		if item.ID == req.Msg.GetId() {
			found = true
			break
		}
	}
	if !found {
		s.subscriptionsMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("subscription %q not found", req.Msg.GetId()))
	}
	if err := s.state.CheckItem(syncstate.DomainSubscriptions, subscriptionIDs(items), req.Msg.GetId(), req.Msg.GetExpectedRevision(), true); err != nil {
		s.subscriptionsMu.Unlock()
		return nil, err
	}
	if err := deleteSubscription(req.Msg.GetId()); err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	current, err := loadSubscriptions()
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainSubscriptions, subscriptionIDs(current), nil, []string{req.Msg.GetId()}, true, req.Msg.GetId())
	s.subscriptionsMu.Unlock()
	s.publishResourceChanged(syncstate.DomainSubscriptions, syncstate.OperationDelete, []string{req.Msg.GetId()}, state)
	s.notifyReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{req.Msg.GetId()})
	return connect.NewResponse(&appv1.DeleteSubscriptionResponse{State: state}), nil
}

func (s *appRuntimeService) ReorderSubscriptions(ctx context.Context, req *connect.Request[appv1.ReorderSubscriptionsRequest]) (*connect.Response[appv1.ReorderSubscriptionsResponse], error) {
	s.subscriptionsMu.Lock()
	items, err := loadSubscriptions()
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	currentIDs := subscriptionIDs(items)
	if err := s.state.CheckOrder(syncstate.DomainSubscriptions, currentIDs, req.Msg.GetExpectedOrderRevision(), true); err != nil {
		s.subscriptionsMu.Unlock()
		return nil, err
	}
	if err := validateRuntimeOrderIDs("subscription", currentIDs, req.Msg.GetIds()); err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	if stringSlicesEqual(currentIDs, req.Msg.GetIds()) {
		state := s.state.Mutation(syncstate.DomainSubscriptions, currentIDs, "")
		s.subscriptionsMu.Unlock()
		return connect.NewResponse(&appv1.ReorderSubscriptionsResponse{Ids: currentIDs, State: state}), nil
	}
	byID := make(map[string]subscription, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	reordered := make([]subscription, 0, len(items))
	for _, id := range req.Msg.GetIds() {
		reordered = append(reordered, byID[id])
	}
	if err := saveSubscriptions(reordered); err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainSubscriptions, req.Msg.GetIds(), nil, nil, true, "")
	s.subscriptionsMu.Unlock()
	s.publishResourceChanged(syncstate.DomainSubscriptions, syncstate.OperationReorder, nil, state)
	return connect.NewResponse(&appv1.ReorderSubscriptionsResponse{Ids: append([]string(nil), req.Msg.GetIds()...), State: state}), nil
}

func (s *appRuntimeService) UpdateSubscription(ctx context.Context, req *connect.Request[appv1.UpdateSubscriptionRequest]) (*connect.Response[appv1.UpdateSubscriptionResponse], error) {
	results, state, err := s.updateSubscription(req.Msg.GetId())
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.UpdateSubscriptionResponse{Results: results, State: state}), nil
}

func (s *appRuntimeService) UpdateAllSubscriptions(ctx context.Context, req *connect.Request[appv1.UpdateAllSubscriptionsRequest]) (*connect.Response[appv1.UpdateAllSubscriptionsResponse], error) {
	results, state, err := s.updateAllSubscriptions()
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.UpdateAllSubscriptionsResponse{Results: results, State: state}), nil
}

func (s *appRuntimeService) GetSubscriptionContent(ctx context.Context, req *connect.Request[appv1.GetSubscriptionContentRequest]) (*connect.Response[appv1.GetSubscriptionContentResponse], error) {
	s.subscriptionsMu.Lock()
	_, items, err := findSubscription(req.Msg.GetId())
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	content, err := readText(subscriptionContentPath(req.Msg.GetId()))
	if os.IsNotExist(err) {
		content = ""
		err = nil
	}
	revision := s.state.ExpectedItem(syncstate.DomainSubscriptions, subscriptionIDs(items), req.Msg.GetId())
	s.subscriptionsMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.GetSubscriptionContentResponse{Content: content, Revision: revision}), nil
}

func (s *appRuntimeService) SaveSubscriptionContent(ctx context.Context, req *connect.Request[appv1.SaveSubscriptionContentRequest]) (*connect.Response[appv1.SaveSubscriptionContentResponse], error) {
	s.subscriptionsMu.Lock()
	_, items, err := findSubscription(req.Msg.GetId())
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := s.state.CheckItem(syncstate.DomainSubscriptions, subscriptionIDs(items), req.Msg.GetId(), req.Msg.GetExpectedRevision(), true); err != nil {
		s.subscriptionsMu.Unlock()
		return nil, err
	}
	item, changed, err := saveSubscriptionContent(req.Msg.GetId(), req.Msg.GetContent())
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	current, err := loadSubscriptions()
	if err != nil {
		s.subscriptionsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Mutation(syncstate.DomainSubscriptions, subscriptionIDs(current), req.Msg.GetId())
	if changed {
		state = s.state.Advance(syncstate.DomainSubscriptions, subscriptionIDs(current), []string{req.Msg.GetId()}, nil, false, req.Msg.GetId())
	}
	s.subscriptionsMu.Unlock()
	if changed {
		s.publishResourceChanged(syncstate.DomainSubscriptions, syncstate.OperationUpsert, []string{req.Msg.GetId()}, state)
		s.notifyReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{req.Msg.GetId()})
	}
	return connect.NewResponse(&appv1.SaveSubscriptionContentResponse{SubscriptionJson: item, State: state}), nil
}

func (s *appRuntimeService) ListRuleSets(ctx context.Context, req *connect.Request[appv1.ListRuleSetsRequest]) (*connect.Response[appv1.ListRuleSetsResponse], error) {
	s.rulesetsMu.Lock()
	rulesets, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	items, err := rulesetsToJSON(rulesets)
	state := s.state.Snapshot(syncstate.DomainRuleSets, rulesetIDs(rulesets))
	hub := string(mustRead(GetPath(rulesetHubPath)))
	s.rulesetsMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.ListRuleSetsResponse{RulesetsJson: items, HubJson: hub, State: state}), nil
}

func decodeRuleSet(raw string) (ruleset, error) {
	var item ruleset
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		return item, err
	}
	if item.ID == "" {
		return item, invalidArgumentError{message: "id is required"}
	}
	if err := validateSourceType(item.Type, "ruleset"); err != nil {
		return item, err
	}
	if item.Format == "" {
		item.Format = "binary"
	}
	return item, nil
}

func (s *appRuntimeService) CreateRuleSet(ctx context.Context, req *connect.Request[appv1.CreateRuleSetRequest]) (*connect.Response[appv1.CreateRuleSetResponse], error) {
	item, err := decodeRuleSet(req.Msg.GetRulesetJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	s.rulesetsMu.Lock()
	items, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	for _, existing := range items {
		if existing.ID == item.ID {
			s.rulesetsMu.Unlock()
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("ruleset %q already exists", item.ID))
		}
	}
	item.Path = managedRulesetPath(item.ID, item.Format)
	if _, err := ensureDefaultManualRuleSetContent(item); err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	item.Updating = false
	items = append(items, item)
	if err := saveRulesets(items); err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainRuleSets, rulesetIDs(items), []string{item.ID}, nil, true, item.ID)
	data, err := json.Marshal(item)
	s.rulesetsMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	s.publishResourceChanged(syncstate.DomainRuleSets, syncstate.OperationUpsert, []string{item.ID}, state)
	s.notifyReferencedResourcesChanged(syncstate.DomainRuleSets, []string{item.ID})
	return connect.NewResponse(&appv1.CreateRuleSetResponse{RulesetJson: string(data), State: state}), nil
}

func (s *appRuntimeService) UpdateRuleSetConfig(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetConfigRequest]) (*connect.Response[appv1.UpdateRuleSetConfigResponse], error) {
	requested, err := decodeRuleSet(req.Msg.GetRulesetJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	s.rulesetsMu.Lock()
	items, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	idx := -1
	for index := range items {
		if items[index].ID == requested.ID {
			idx = index
			break
		}
	}
	if idx == -1 {
		s.rulesetsMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ruleset %q not found", requested.ID))
	}
	if err := s.state.CheckItem(syncstate.DomainRuleSets, rulesetIDs(items), requested.ID, req.Msg.GetExpectedRevision(), true); err != nil {
		s.rulesetsMu.Unlock()
		return nil, err
	}
	existing := items[idx]
	requested.Path = existing.Path
	requested.Count = existing.Count
	requested.UpdateTime = existing.UpdateTime
	requested.Updating = false
	if requested.Format != existing.Format || requested.Path == "" {
		requested.Path = managedRulesetPath(requested.ID, requested.Format)
		migrateRulesetFile(existing.Path, requested.Path)
	}
	if rulesetConfigEqual(existing, requested) {
		state := s.state.Mutation(syncstate.DomainRuleSets, rulesetIDs(items), requested.ID)
		data, marshalErr := json.Marshal(existing)
		s.rulesetsMu.Unlock()
		if marshalErr != nil {
			return nil, asConnectError(marshalErr)
		}
		return connect.NewResponse(&appv1.UpdateRuleSetConfigResponse{RulesetJson: string(data), State: state}), nil
	}
	if _, err := ensureDefaultManualRuleSetContent(requested); err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	items[idx] = requested
	if err := saveRulesets(items); err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainRuleSets, rulesetIDs(items), []string{requested.ID}, nil, false, requested.ID)
	data, err := json.Marshal(requested)
	s.rulesetsMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	s.publishResourceChanged(syncstate.DomainRuleSets, syncstate.OperationUpsert, []string{requested.ID}, state)
	s.notifyReferencedResourcesChanged(syncstate.DomainRuleSets, []string{requested.ID})
	return connect.NewResponse(&appv1.UpdateRuleSetConfigResponse{RulesetJson: string(data), State: state}), nil
}

func (s *appRuntimeService) DeleteRuleSet(ctx context.Context, req *connect.Request[appv1.DeleteRuleSetRequest]) (*connect.Response[appv1.DeleteRuleSetResponse], error) {
	s.rulesetsMu.Lock()
	items, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	found := false
	for _, item := range items {
		if item.ID == req.Msg.GetId() {
			found = true
			break
		}
	}
	if !found {
		s.rulesetsMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ruleset %q not found", req.Msg.GetId()))
	}
	if err := s.state.CheckItem(syncstate.DomainRuleSets, rulesetIDs(items), req.Msg.GetId(), req.Msg.GetExpectedRevision(), true); err != nil {
		s.rulesetsMu.Unlock()
		return nil, err
	}
	if err := deleteRuleset(req.Msg.GetId()); err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	current, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainRuleSets, rulesetIDs(current), nil, []string{req.Msg.GetId()}, true, req.Msg.GetId())
	s.rulesetsMu.Unlock()
	s.publishResourceChanged(syncstate.DomainRuleSets, syncstate.OperationDelete, []string{req.Msg.GetId()}, state)
	s.notifyReferencedResourcesChanged(syncstate.DomainRuleSets, []string{req.Msg.GetId()})
	return connect.NewResponse(&appv1.DeleteRuleSetResponse{State: state}), nil
}

func (s *appRuntimeService) ReorderRuleSets(ctx context.Context, req *connect.Request[appv1.ReorderRuleSetsRequest]) (*connect.Response[appv1.ReorderRuleSetsResponse], error) {
	s.rulesetsMu.Lock()
	items, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	currentIDs := rulesetIDs(items)
	if err := s.state.CheckOrder(syncstate.DomainRuleSets, currentIDs, req.Msg.GetExpectedOrderRevision(), true); err != nil {
		s.rulesetsMu.Unlock()
		return nil, err
	}
	if err := validateRuntimeOrderIDs("ruleset", currentIDs, req.Msg.GetIds()); err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	if stringSlicesEqual(currentIDs, req.Msg.GetIds()) {
		state := s.state.Mutation(syncstate.DomainRuleSets, currentIDs, "")
		s.rulesetsMu.Unlock()
		return connect.NewResponse(&appv1.ReorderRuleSetsResponse{Ids: currentIDs, State: state}), nil
	}
	byID := make(map[string]ruleset, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	reordered := make([]ruleset, 0, len(items))
	for _, id := range req.Msg.GetIds() {
		reordered = append(reordered, byID[id])
	}
	if err := saveRulesets(reordered); err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainRuleSets, req.Msg.GetIds(), nil, nil, true, "")
	s.rulesetsMu.Unlock()
	s.publishResourceChanged(syncstate.DomainRuleSets, syncstate.OperationReorder, nil, state)
	return connect.NewResponse(&appv1.ReorderRuleSetsResponse{Ids: append([]string(nil), req.Msg.GetIds()...), State: state}), nil
}

func (s *appRuntimeService) UpdateRuleSet(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetRequest]) (*connect.Response[appv1.UpdateRuleSetResponse], error) {
	results, state, err := s.updateRuleset(req.Msg.GetId())
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.UpdateRuleSetResponse{Results: results, State: state}), nil
}

func (s *appRuntimeService) UpdateAllRuleSets(ctx context.Context, req *connect.Request[appv1.UpdateAllRuleSetsRequest]) (*connect.Response[appv1.UpdateAllRuleSetsResponse], error) {
	results, state, err := s.updateAllRulesets()
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.UpdateAllRuleSetsResponse{Results: results, State: state}), nil
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
	s.rulesetsMu.Lock()
	items, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	changed := !bytes.Equal(mustRead(GetPath(rulesetHubPath)), []byte(body))
	if changed {
		err = storage.AtomicWriteFile(GetPath(rulesetHubPath), []byte(body), 0644)
	}
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Mutation(syncstate.DomainRuleSets, rulesetIDs(items), "")
	if changed {
		state = s.state.AdvanceRuntime(syncstate.DomainRuleSets, rulesetIDs(items))
	}
	s.rulesetsMu.Unlock()
	if changed {
		s.publishResourceChanged(syncstate.DomainRuleSets, syncstate.OperationRuntime, nil, state)
	}
	return connect.NewResponse(&appv1.UpdateRuleSetHubResponse{HubJson: body, State: state}), nil
}

func loadRuleSetHubCache() (rulesetHub, error) {
	var hub rulesetHub
	data, err := os.ReadFile(GetPath(rulesetHubPath))
	if err != nil {
		return hub, err
	}
	if err := json.Unmarshal(data, &hub); err != nil {
		return hub, err
	}
	return hub, nil
}

func previewRuleSetHub(index int, format string) (string, error) {
	if index < 0 {
		return "", invalidArgumentError{message: "ruleset hub index is out of range"}
	}
	hub, err := loadRuleSetHubCache()
	if err != nil {
		return "", err
	}
	if index >= len(hub.List) {
		return "", invalidArgumentError{message: "ruleset hub index is out of range"}
	}

	suffix := ""
	switch format {
	case "source":
		suffix = ".json"
	case "binary":
		suffix = ".srs"
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported ruleset format %q", format)}
	}

	item := hub.List[index]
	baseURL := ""
	switch item.Type {
	case "geosite":
		baseURL = hub.Geosite
	case "geoip":
		baseURL = hub.Geoip
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported ruleset hub type %q", item.Type)}
	}
	if item.Name == "" || baseURL == "" {
		return "", invalidArgumentError{message: "invalid ruleset hub item"}
	}

	_, body, err := ruleSetHubHTTPRequest(http.MethodGet, baseURL+item.Name+suffix, nil, "", false, 15)
	return body, err
}

func (s *appRuntimeService) PreviewRuleSetHub(ctx context.Context, req *connect.Request[appv1.PreviewRuleSetHubRequest]) (*connect.Response[appv1.PreviewRuleSetHubResponse], error) {
	content, err := previewRuleSetHub(int(req.Msg.GetIndex()), req.Msg.GetFormat())
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.PreviewRuleSetHubResponse{Content: content}), nil
}

func (s *appRuntimeService) GetRuleSetContent(ctx context.Context, req *connect.Request[appv1.GetRuleSetContentRequest]) (*connect.Response[appv1.GetRuleSetContentResponse], error) {
	s.rulesetsMu.Lock()
	defer s.rulesetsMu.Unlock()
	r, items, err := findRuleset(req.Msg.GetId())
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
	return connect.NewResponse(&appv1.GetRuleSetContentResponse{
		Content:  content,
		Revision: s.state.ExpectedItem(syncstate.DomainRuleSets, rulesetIDs(items), req.Msg.GetId()),
	}), nil
}

func (s *appRuntimeService) SaveRuleSetContent(ctx context.Context, req *connect.Request[appv1.SaveRuleSetContentRequest]) (*connect.Response[appv1.SaveRuleSetContentResponse], error) {
	s.rulesetsMu.Lock()
	items, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	found := false
	for _, item := range items {
		if item.ID == req.Msg.GetId() {
			found = true
			break
		}
	}
	if !found {
		s.rulesetsMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ruleset %q not found", req.Msg.GetId()))
	}
	if err := s.state.CheckItem(syncstate.DomainRuleSets, rulesetIDs(items), req.Msg.GetId(), req.Msg.GetExpectedRevision(), true); err != nil {
		s.rulesetsMu.Unlock()
		return nil, err
	}
	item, changed, err := saveRuleSetContent(req.Msg.GetId(), req.Msg.GetContent())
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	current, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Mutation(syncstate.DomainRuleSets, rulesetIDs(current), req.Msg.GetId())
	if changed {
		state = s.state.Advance(syncstate.DomainRuleSets, rulesetIDs(current), []string{req.Msg.GetId()}, nil, false, req.Msg.GetId())
	}
	s.rulesetsMu.Unlock()
	if changed {
		s.publishResourceChanged(syncstate.DomainRuleSets, syncstate.OperationUpsert, []string{req.Msg.GetId()}, state)
		s.notifyReferencedResourcesChanged(syncstate.DomainRuleSets, []string{req.Msg.GetId()})
	}
	return connect.NewResponse(&appv1.SaveRuleSetContentResponse{RulesetJson: item, State: state}), nil
}

func (s *appRuntimeService) ClearRuleSetContent(ctx context.Context, req *connect.Request[appv1.ClearRuleSetContentRequest]) (*connect.Response[appv1.ClearRuleSetContentResponse], error) {
	s.rulesetsMu.Lock()
	items, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	found := false
	for _, item := range items {
		if item.ID == req.Msg.GetId() {
			found = true
			break
		}
	}
	if !found {
		s.rulesetsMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("ruleset %q not found", req.Msg.GetId()))
	}
	if err := s.state.CheckItem(syncstate.DomainRuleSets, rulesetIDs(items), req.Msg.GetId(), req.Msg.GetExpectedRevision(), true); err != nil {
		s.rulesetsMu.Unlock()
		return nil, err
	}
	item, changed, err := saveRuleSetContent(req.Msg.GetId(), defaultSourceRuleSetContent)
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	current, err := loadRulesets()
	if err != nil {
		s.rulesetsMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Mutation(syncstate.DomainRuleSets, rulesetIDs(current), req.Msg.GetId())
	if changed {
		state = s.state.Advance(syncstate.DomainRuleSets, rulesetIDs(current), []string{req.Msg.GetId()}, nil, false, req.Msg.GetId())
	}
	s.rulesetsMu.Unlock()
	if changed {
		s.publishResourceChanged(syncstate.DomainRuleSets, syncstate.OperationUpsert, []string{req.Msg.GetId()}, state)
		s.notifyReferencedResourcesChanged(syncstate.DomainRuleSets, []string{req.Msg.GetId()})
	}
	return connect.NewResponse(&appv1.ClearRuleSetContentResponse{RulesetJson: item, State: state}), nil
}

func (s *appRuntimeService) ListScheduledTasks(ctx context.Context, req *connect.Request[appv1.ListScheduledTasksRequest]) (*connect.Response[appv1.ListScheduledTasksResponse], error) {
	s.scheduledTasksMu.Lock()
	tasks, err := loadScheduledTasks()
	if err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	items, err := scheduledTasksToJSON(tasks)
	state := s.state.Snapshot(syncstate.DomainScheduledTasks, scheduledTaskIDs(tasks))
	s.scheduledTasksMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.ListScheduledTasksResponse{TasksJson: items, State: state}), nil
}

func decodeScheduledTask(raw string) (scheduledTask, error) {
	var task scheduledTask
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		return task, err
	}
	if task.ID == "" {
		return task, invalidArgumentError{message: "id is required"}
	}
	if !isSupportedScheduledTaskType(task.Type) {
		return task, invalidArgumentError{message: "unsupported scheduled task type: " + task.Type}
	}
	task.LogLimit = normalizeScheduledTaskLogLimit(task.LogLimit)
	return task, nil
}

func (s *appRuntimeService) CreateScheduledTask(ctx context.Context, req *connect.Request[appv1.CreateScheduledTaskRequest]) (*connect.Response[appv1.CreateScheduledTaskResponse], error) {
	task, err := decodeScheduledTask(req.Msg.GetTaskJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	task.LastTime = 0
	s.scheduledTasksMu.Lock()
	tasks, err := loadScheduledTasks()
	if err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	for _, existing := range tasks {
		if existing.ID == task.ID {
			s.scheduledTasksMu.Unlock()
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("scheduled task %q already exists", task.ID))
		}
	}
	tasks = append(tasks, task)
	if err := saveScheduledTasks(tasks); err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainScheduledTasks, scheduledTaskIDs(tasks), []string{task.ID}, nil, true, task.ID)
	data, err := json.Marshal(task)
	s.scheduledTasksMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	s.restartScheduler()
	s.publishResourceChanged(syncstate.DomainScheduledTasks, syncstate.OperationUpsert, []string{task.ID}, state)
	return connect.NewResponse(&appv1.CreateScheduledTaskResponse{TaskJson: string(data), State: state}), nil
}

func (s *appRuntimeService) UpdateScheduledTask(ctx context.Context, req *connect.Request[appv1.UpdateScheduledTaskRequest]) (*connect.Response[appv1.UpdateScheduledTaskResponse], error) {
	requested, err := decodeScheduledTask(req.Msg.GetTaskJson())
	if err != nil {
		return nil, asConnectError(err)
	}
	s.scheduledTasksMu.Lock()
	tasks, err := loadScheduledTasks()
	if err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	idx := -1
	for index := range tasks {
		if tasks[index].ID == requested.ID {
			idx = index
			break
		}
	}
	if idx == -1 {
		s.scheduledTasksMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("scheduled task %q not found", requested.ID))
	}
	if err := s.state.CheckItem(syncstate.DomainScheduledTasks, scheduledTaskIDs(tasks), requested.ID, req.Msg.GetExpectedRevision(), true); err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, err
	}
	existing := tasks[idx]
	requested.LastTime = existing.LastTime
	if scheduledTaskConfigEqual(existing, requested) {
		state := s.state.Mutation(syncstate.DomainScheduledTasks, scheduledTaskIDs(tasks), requested.ID)
		data, marshalErr := json.Marshal(existing)
		s.scheduledTasksMu.Unlock()
		if marshalErr != nil {
			return nil, asConnectError(marshalErr)
		}
		return connect.NewResponse(&appv1.UpdateScheduledTaskResponse{TaskJson: string(data), State: state}), nil
	}
	tasks[idx] = requested
	if err := saveScheduledTasks(tasks); err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainScheduledTasks, scheduledTaskIDs(tasks), []string{requested.ID}, nil, false, requested.ID)
	data, err := json.Marshal(requested)
	s.scheduledTasksMu.Unlock()
	if err != nil {
		return nil, asConnectError(err)
	}
	s.mu.Lock()
	s.taskLogs = trimScheduledTaskLogs(s.taskLogs, tasks)
	_ = saveScheduledTaskLogs(s.taskLogs)
	s.mu.Unlock()
	s.restartScheduler()
	s.publishResourceChanged(syncstate.DomainScheduledTasks, syncstate.OperationUpsert, []string{requested.ID}, state)
	return connect.NewResponse(&appv1.UpdateScheduledTaskResponse{TaskJson: string(data), State: state}), nil
}

func (s *appRuntimeService) DeleteScheduledTask(ctx context.Context, req *connect.Request[appv1.DeleteScheduledTaskRequest]) (*connect.Response[appv1.DeleteScheduledTaskResponse], error) {
	s.scheduledTasksMu.Lock()
	tasks, err := loadScheduledTasks()
	if err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	found := false
	for _, task := range tasks {
		if task.ID == req.Msg.GetId() {
			found = true
			break
		}
	}
	if !found {
		s.scheduledTasksMu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("scheduled task %q not found", req.Msg.GetId()))
	}
	if err := s.state.CheckItem(syncstate.DomainScheduledTasks, scheduledTaskIDs(tasks), req.Msg.GetId(), req.Msg.GetExpectedRevision(), true); err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, err
	}
	if err := deleteJSONItem(scheduledTasksPath, req.Msg.GetId()); err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	current, err := loadScheduledTasks()
	if err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainScheduledTasks, scheduledTaskIDs(current), nil, []string{req.Msg.GetId()}, true, req.Msg.GetId())
	s.scheduledTasksMu.Unlock()
	s.mu.Lock()
	s.taskLogs = removeScheduledTaskLogs(s.taskLogs, req.Msg.GetId())
	_ = saveScheduledTaskLogs(s.taskLogs)
	s.mu.Unlock()
	s.restartScheduler()
	s.publishResourceChanged(syncstate.DomainScheduledTasks, syncstate.OperationDelete, []string{req.Msg.GetId()}, state)
	return connect.NewResponse(&appv1.DeleteScheduledTaskResponse{State: state}), nil
}

func (s *appRuntimeService) ReorderScheduledTasks(ctx context.Context, req *connect.Request[appv1.ReorderScheduledTasksRequest]) (*connect.Response[appv1.ReorderScheduledTasksResponse], error) {
	s.scheduledTasksMu.Lock()
	tasks, err := loadScheduledTasks()
	if err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	currentIDs := scheduledTaskIDs(tasks)
	if err := s.state.CheckOrder(syncstate.DomainScheduledTasks, currentIDs, req.Msg.GetExpectedOrderRevision(), true); err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, err
	}
	if err := validateRuntimeOrderIDs("scheduled task", currentIDs, req.Msg.GetIds()); err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	if stringSlicesEqual(currentIDs, req.Msg.GetIds()) {
		state := s.state.Mutation(syncstate.DomainScheduledTasks, currentIDs, "")
		s.scheduledTasksMu.Unlock()
		return connect.NewResponse(&appv1.ReorderScheduledTasksResponse{Ids: currentIDs, State: state}), nil
	}
	byID := make(map[string]scheduledTask, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	reordered := make([]scheduledTask, 0, len(tasks))
	for _, id := range req.Msg.GetIds() {
		reordered = append(reordered, byID[id])
	}
	if err := saveScheduledTasks(reordered); err != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainScheduledTasks, req.Msg.GetIds(), nil, nil, true, "")
	s.scheduledTasksMu.Unlock()
	s.publishResourceChanged(syncstate.DomainScheduledTasks, syncstate.OperationReorder, nil, state)
	return connect.NewResponse(&appv1.ReorderScheduledTasksResponse{Ids: append([]string(nil), req.Msg.GetIds()...), State: state}), nil
}

func (s *appRuntimeService) RunScheduledTask(ctx context.Context, req *connect.Request[appv1.RunScheduledTaskRequest]) (*connect.Response[appv1.RunScheduledTaskResponse], error) {
	log, err := s.runScheduledTask(ctx, req.Msg.GetId(), false)
	if err != nil {
		return nil, asConnectError(err)
	}
	s.scheduledTasksMu.Lock()
	tasks, stateErr := loadScheduledTasks()
	if stateErr != nil {
		s.scheduledTasksMu.Unlock()
		return nil, asConnectError(stateErr)
	}
	state := s.state.Mutation(syncstate.DomainScheduledTasks, scheduledTaskIDs(tasks), req.Msg.GetId())
	s.scheduledTasksMu.Unlock()
	return connect.NewResponse(&appv1.RunScheduledTaskResponse{
		Results:   taskResultsToProto(log.Results),
		StartTime: log.StartTime,
		EndTime:   log.EndTime,
		State:     state,
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
