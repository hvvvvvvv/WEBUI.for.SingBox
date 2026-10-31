package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"guiforcores/bridge/storage"
)

func pluginForTest(id, source string, triggers ...string) Plugin {
	if len(triggers) == 0 {
		triggers = []string{OnSubscribe}
	}
	return Plugin{ID: id, Name: id, Version: "v1.0.0", Triggers: triggers, Source: source}
}

func serviceForTest(t *testing.T) *Service {
	t.Helper()
	service, err := NewService(storage.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func saveForTest(t *testing.T, service *Service, plugin Plugin) Snapshot {
	t.Helper()
	result, err := service.Save(plugin, service.List().Revision)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func jsonForTest(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStorageRevisionCloneReorderAndRestart(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service, err := NewService(paths)
	if err != nil {
		t.Fatal(err)
	}
	if initial := service.List(); initial.Revision != 0 || initial.Plugins == nil || len(initial.Plugins) != 0 {
		t.Fatalf("invalid initial snapshot: %#v", initial)
	}
	first := pluginForTest("first", "throw new Error('must not run during installation')")
	first.Configuration = map[string]any{"nested": map[string]any{"list": []any{"original"}}}
	saved := saveForTest(t, service, first)
	first.Configuration["nested"].(map[string]any)["list"].([]any)[0] = "caller change"
	saved.Plugins[0].Configuration["nested"].(map[string]any)["list"].([]any)[0] = "snapshot change"
	saved.Plugins[0].Triggers[0] = OnManual
	if actual := service.List().Plugins[0]; actual.Configuration["nested"].(map[string]any)["list"].([]any)[0] != "original" || actual.Triggers[0] != OnSubscribe {
		t.Fatal("service retained caller-owned objects")
	}
	if _, err := service.Save(first, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	second := pluginForTest("second", "function onSubscribe(p) { return p }")
	saveForTest(t, service, second)
	if _, err := service.Reorder([]string{"second", "second"}, 2); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid reorder accepted: %v", err)
	}
	ordered, err := service.Reorder([]string{"second", "first"}, 2)
	if err != nil || ordered.Revision != 3 || ordered.Plugins[0].ID != "second" {
		t.Fatalf("reorder failed: %#v %v", ordered, err)
	}
	restarted, err := NewService(paths)
	if err != nil {
		t.Fatal(err)
	}
	if jsonForTest(t, restarted.List()) != jsonForTest(t, service.List()) {
		t.Fatal("restart lost the revision or installed order")
	}
	if _, err := restarted.Delete("missing", 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing plugin: %v", err)
	}
	deleted, err := restarted.Delete("first", 3)
	if err != nil || deleted.Revision != 4 || len(deleted.Plugins) != 1 || deleted.Plugins[0].ID != "second" {
		t.Fatalf("delete failed: %#v %v", deleted, err)
	}
	updated := deleted.Plugins[0]
	updated.Version = "v1.0.1"
	updated.Source = "function onSubscribe(p) { p.reverse(); return p }"
	result := saveForTest(t, restarted, updated)
	if len(result.Plugins) != 1 || result.Plugins[0].Version != "v1.0.1" || result.Revision != 5 {
		t.Fatal("source update appended or failed to advance revision")
	}
}

func TestStorageConcurrentStaleWritersAndFailedWriteAreAtomic(t *testing.T) {
	service := serviceForTest(t)
	var group sync.WaitGroup
	results := make(chan error, 12)
	for index := 0; index < cap(results); index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			_, err := service.Save(pluginForTest(fmt.Sprint("plugin", index), "function onSubscribe(p) { return p }"), 0)
			results <- err
		}(index)
	}
	group.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || service.List().Revision != 1 || len(service.List().Plugins) != 1 {
		t.Fatal("concurrent saves lost an update")
	}
	before := jsonForTest(t, service.List())
	// A regular file cannot be used as the destination's parent directory.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.path = filepath.Join(blocker, "plugins.json")
	if _, err := service.Save(pluginForTest("new", "function onSubscribe(p) { return p }"), 1); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if jsonForTest(t, service.List()) != before || service.programs["new"] != nil {
		t.Fatal("failed write changed state or compiled source")
	}
}

func TestValidationAndStorageBounds(t *testing.T) {
	for _, mutate := range []func(*Plugin){
		func(p *Plugin) { p.ID = "../escape" },
		func(p *Plugin) { p.Name = " " },
		func(p *Plugin) { p.Version = "" },
		func(p *Plugin) { p.Triggers = []string{"on::startup"} },
		func(p *Plugin) { p.Triggers = []string{OnSubscribe, OnSubscribe} },
		func(p *Plugin) { p.Source = "function {" },
		func(p *Plugin) { p.Source = strings.Repeat(" ", MaxSourceBytes+1) },
		func(p *Plugin) { p.Configuration = map[string]any{"large": strings.Repeat("x", MaxConfigurationBytes)} },
		func(p *Plugin) { p.Configuration = map[string]any{"unsupported": make(chan int)} },
	} {
		plugin := pluginForTest("test", "function onSubscribe(p) { return p }")
		mutate(&plugin)
		if err := ValidatePlugin(plugin); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid plugin accepted: %v", err)
		}
	}
	service := serviceForTest(t)
	for index := 0; index < MaxPlugins; index++ {
		saveForTest(t, service, pluginForTest(fmt.Sprint("plugin", index), "function onSubscribe(p) { return p }"))
	}
	if _, err := service.Save(pluginForTest("overflow", "function onSubscribe(p) { return p }"), service.List().Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("plugin count limit not enforced: %v", err)
	}
	for _, raw := range []string{"null", `{"plugins":null}`, `{"plugins":[],"unexpected":true}`, strings.Repeat(" ", MaxStorageBytes+1)} {
		paths := storage.NewPaths(t.TempDir())
		if err := storage.AtomicWriteFile(paths.Resolve("data/plugins.json"), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewService(paths); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid store accepted: %v", err)
		}
	}
}
