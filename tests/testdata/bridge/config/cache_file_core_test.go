package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestCacheDNSPersistenceWithSingBox114(t *testing.T) {
	corePath := os.Getenv("SING_BOX_114_PATH")
	if corePath == "" {
		t.Skip("set SING_BOX_114_PATH to a sing-box 1.14.0 binary to run core checks")
	}
	version, err := runHTTPClientCoreCommand(corePath, "version")
	if err != nil || strings.SplitN(string(version), "\n", 2)[0] != "sing-box version 1.14.0" {
		t.Fatalf("expected sing-box 1.14.0, got %s (error=%v)", version, err)
	}

	for _, tc := range []struct {
		name     string
		storeDNS bool
	}{
		{name: "core default"},
		{name: "persist DNS cache", storeDNS: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(&profilev1.Profile{
				Experimental: &profilev1.Experimental{CacheFile: &profilev1.CacheFileExperimental{
					Enabled: true, Path: filepath.Join(t.TempDir(), "cache.db"), StoreDns: tc.storeDNS,
				}},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			output, err := runHTTPClientCoreCommand(corePath, "check", "-c", configPath)
			if err != nil {
				t.Fatalf("core rejected cache config: error=%v, output=%s, config=%s", err, output, data)
			}
			if strings.Contains(strings.ToLower(string(output)), "deprecated") {
				t.Fatalf("core emitted a deprecation warning: %s", output)
			}
		})
	}
}
