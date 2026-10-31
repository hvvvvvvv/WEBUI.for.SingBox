package plugins

import (
	"context"
	"errors"
	"testing"
)

func TestSettingDefaultsOverridesAndResetReachBackend(t *testing.T) {
	s := serviceForTest(t)
	p := pluginForTest("settings", `function onSubscribe(p) { p[0].tag = configuration.enabled ? configuration.name : 'disabled'; return p }`)
	p.ConfigurationSchema = []Setting{
		{Key: "enabled", Title: "Enabled", Component: "Switch", Value: true},
		{Key: "name", Title: "Name", Component: "Input", Value: "default"},
	}
	input := []map[string]any{{"type": "direct", "tag": "input"}}
	for _, tc := range []struct {
		values map[string]any
		want   string
	}{
		{nil, "default"}, {map[string]any{"name": "edited"}, "edited"},
		{map[string]any{"enabled": false}, "disabled"}, {map[string]any{}, "default"},
	} {
		p.Configuration = tc.values
		saveForTest(t, s, p)
		result, err := s.ApplySubscription(context.Background(), input, nil)
		if err != nil || result[0]["tag"] != tc.want {
			t.Fatalf("settings did not reach backend: %v %v", result, err)
		}
	}
	if input[0]["tag"] != "input" {
		t.Fatal("execution changed caller input")
	}
}

func TestSettingSchemaRejectsUnsafeControlsAndInvalidOverrides(t *testing.T) {
	for _, setting := range []Setting{
		{Key: "a", Title: "A", Component: "script", Value: "alert(1)"},
		{Key: "a", Title: "A", Component: "Switch", Value: "true"},
		{Key: "a", Title: "A", Component: "Radio", Value: "unknown", Options: []string{"Default,default"}},
	} {
		p := pluginForTest("settings", "function onSubscribe(p) { return p }")
		p.ConfigurationSchema = []Setting{setting}
		if err := ValidatePlugin(p); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsafe schema accepted: %+v %v", setting, err)
		}
	}
	s := serviceForTest(t)
	p := pluginForTest("settings", "function onSubscribe(p) { return p }")
	p.ConfigurationSchema = []Setting{{Key: "mode", Title: "Mode", Component: "Radio", Value: "0", Options: []string{"No,0", "Yes,1"}}}
	saveForTest(t, s, p)
	before := jsonForTest(t, s.List())
	p.Configuration = map[string]any{"mode": "2"}
	if _, err := s.Save(p, s.List().Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid override accepted: %v", err)
	}
	if jsonForTest(t, s.List()) != before {
		t.Fatal("failed setting save changed installed plugin")
	}
}

func TestSettingSchemaSnapshotsDoNotShareServiceState(t *testing.T) {
	s := serviceForTest(t)
	p := pluginForTest("settings", `function onSubscribe(p) { p[0].tag = configuration.mode; return p }`)
	p.ConfigurationSchema = []Setting{{Key: "mode", Title: "Mode", Component: "Radio", Value: "0", Options: []string{"No,0", "Yes,1"}}}
	saved, err := s.Save(p, s.List().Revision)
	if err != nil {
		t.Fatal(err)
	}
	before := jsonForTest(t, s.List())
	for _, snapshot := range []Snapshot{saved, s.List()} {
		snapshot.Plugins[0].ConfigurationSchema[0].Value = "1"
		snapshot.Plugins[0].ConfigurationSchema[0].Options[0] = "Changed,2"
	}
	p.ConfigurationSchema[0].Value = "1"
	if jsonForTest(t, s.List()) != before {
		t.Fatal("schema snapshot modified service state")
	}
	result, err := s.ApplySubscription(context.Background(), []map[string]any{{"type": "direct", "tag": "input"}}, nil)
	if err != nil || result[0]["tag"] != "0" {
		t.Fatalf("snapshot mutation affected execution: %v %v", result, err)
	}
}
