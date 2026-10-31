package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"guiforcores/bridge/storage"
)

func TestPluginConflictSaveChecksBothDirectionsAndKeepsDisabledDrafts(t *testing.T) {
	for _, declaring := range []string{"first", "second"} {
		t.Run(declaring, func(t *testing.T) {
			s := serviceForTest(t)
			first := pluginForTest("first", "function onSubscribe(p){return p}")
			second := pluginForTest("second", "function onSubscribe(p){return p}")
			first.Name, second.Name = "First display name", "Second display name"
			if declaring == "first" {
				first.Conflicts = []string{second.ID}
			} else {
				second.Conflicts = []string{first.ID}
			}
			saveForTest(t, s, first)
			before := jsonForTest(t, s.List())
			if _, err := s.Save(second, s.List().Revision); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), first.Name) || !strings.Contains(err.Error(), second.Name) {
				t.Fatalf("conflict not rejected with names: %v", err)
			}
			if jsonForTest(t, s.List()) != before {
				t.Fatal("failed save changed state")
			}
			second.Disabled = true
			saveForTest(t, s, second)
			first.Disabled = true
			saveForTest(t, s, first)
			second.Disabled = false
			saveForTest(t, s, second)
		})
	}
}

func TestConflictingLegacyStorageLoadsButPipelineFailsBeforeExecuting(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	first := pluginForTest("first", `throw Error('must not execute first'); function onSubscribe(p){p[0].tag='changed';return p}`)
	second := pluginForTest("second", "function onSubscribe(p){return p}")
	first.Conflicts = []string{second.ID}
	loaded := Snapshot{Revision: 7, Plugins: []Plugin{first, second}}
	raw, _ := json.Marshal(loaded)
	if err := storage.AtomicWriteFile(paths.Resolve("data/plugins.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewService(paths)
	if err != nil {
		t.Fatalf("legacy conflict blocked application startup: %v", err)
	}
	input := []map[string]any{{"type": "direct", "tag": "original"}}
	if result, err := s.ApplySubscription(context.Background(), input, nil); !errors.Is(err, ErrInvalid) || result != nil || strings.Contains(err.Error(), "must not execute") {
		t.Fatalf("pipeline did not preflight conflict: %#v %v", result, err)
	}
	if input[0]["tag"] != "original" {
		t.Fatal("conflicting pipeline changed input")
	}
	actual, _ := os.ReadFile(paths.Resolve("data/plugins.json"))
	if string(actual) != string(raw) {
		t.Fatal("conflict rejection changed persisted storage")
	}
	first.Disabled = true
	saveForTest(t, s, first)
	if _, err := s.ApplySubscription(context.Background(), input, nil); err != nil {
		t.Fatalf("disabling conflicted plugin failed to restore execution: %v", err)
	}
}

func TestConflictMetadataIsValidatedAndCloned(t *testing.T) {
	s := serviceForTest(t)
	p := pluginForTest("first", "function onSubscribe(p){return p}")
	p.Conflicts = []string{"second"}
	saved := saveForTest(t, s, p)
	p.Conflicts[0] = "caller-mutation"
	saved.Plugins[0].Conflicts[0] = "snapshot-mutation"
	if s.List().Plugins[0].Conflicts[0] != "second" {
		t.Fatal("conflict slice aliases service state")
	}
	for _, conflicts := range [][]string{{"first"}, {"second", "second"}, {"../invalid"}, make([]string, MaxPlugins+1)} {
		p.Conflicts = conflicts
		if err := ValidatePlugin(p); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid conflicts accepted: %#v %v", conflicts, err)
		}
	}
}

func TestTextAreaSettingIsBoundedTextAndNeverEvaluated(t *testing.T) {
	s := serviceForTest(t)
	p := pluginForTest("text-area", `function onSubscribe(p){p[0].tag=configuration.hosts;return p}`)
	p.ConfigurationSchema = []Setting{{Key: "hosts", Title: "Hosts", Component: "TextArea", Value: "throw Error('not JavaScript')\n127.0.0.1 example.test"}}
	saveForTest(t, s, p)
	output, err := s.ApplySubscription(context.Background(), []map[string]any{{"type": "direct", "tag": "original"}}, nil)
	if err != nil || output[0]["tag"] != p.ConfigurationSchema[0].Value {
		t.Fatalf("multiline text was not preserved: %#v %v", output, err)
	}
	p.Configuration = map[string]any{"hosts": strings.Repeat("x", 16385)}
	if err := ValidatePlugin(p); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized TextArea accepted: %v", err)
	}
	p.Configuration = map[string]any{"hosts": true}
	if err := ValidatePlugin(p); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-string TextArea accepted: %v", err)
	}
}
