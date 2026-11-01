package config

import (
	"reflect"
	"testing"

	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestDefaultModeGenerationAndNativeProtection(t *testing.T) {
	service := NewService(storage.NewPaths(t.TempDir()), "test")
	for _, mode := range []string{"", "global", "rule", "direct", "Global"} {
		t.Run(mode, func(t *testing.T) {
			root, err := service.Generate(&profilev1.Profile{Experimental: &profilev1.Experimental{DefaultMode: mode}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			experimental := root["experimental"].(map[string]any)
			if mode == "" {
				if _, exists := experimental["clash_api"]; exists {
					t.Fatal("legacy empty default unexpectedly changed")
				}
			} else {
				want := mode
				if mode == "Global" {
					want = "global"
				}
				if !reflect.DeepEqual(experimental["clash_api"], map[string]any{"default_mode": want}) {
					t.Fatalf("unexpected legacy API fields: %v", experimental)
				}
			}
			if len(NativeAPISecret(root)) != 64 {
				t.Fatal("native credential missing")
			}
		})
	}
	for _, mode := range []any{"unsupported", false, 1, []any{"rule"}} {
		root := map[string]any{"experimental": map[string]any{"clash_api": map[string]any{"default_mode": mode}}}
		if err := EnforceNativeAPIConfig(root); err == nil {
			t.Fatalf("accepted invalid mode %v", mode)
		}
	}
	root := map[string]any{"experimental": map[string]any{"clash_api": map[string]any{"default_mode": "Direct", "external_controller": "0.0.0.0:9090", "external_ui": "/private", "secret": "old", "external_ui_download_url": "https://example.com"}}}
	if err := EnforceNativeAPIConfig(root); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(root["experimental"].(map[string]any)["clash_api"], map[string]any{"default_mode": "direct"}) {
		t.Fatalf("unsafe Clash fields retained: %v", root["experimental"])
	}
}

func TestFinalizationPreservesLoggingAndManagesOnlyCachePath(t *testing.T) {
	for _, level := range []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"} {
		log := map[string]any{"disabled": true, "output": "custom.log", "level": level, "timestamp": true}
		root := map[string]any{"log": log, "experimental": map[string]any{"cache_file": map[string]any{"path": "custom.db", "cache_id": "profile-cache"}}}
		FinalizeGeneratedConfig(root)
		if !reflect.DeepEqual(root["log"], map[string]any{"disabled": true, "output": "custom.log", "level": level, "timestamp": true}) {
			t.Fatalf("log setting overwritten: %v", root["log"])
		}
		cache := root["experimental"].(map[string]any)["cache_file"].(map[string]any)
		if cache["path"] != coreGeneratedCacheFilePath || cache["cache_id"] != "profile-cache" {
			t.Fatalf("cache finalization: %v", cache)
		}
	}
	root := map[string]any{}
	FinalizeGeneratedConfig(root)
	if root["log"].(map[string]any)["level"] != "info" {
		t.Fatal("empty profiles must retain the prior info level default")
	}
}
