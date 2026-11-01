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
	corePath, _ := selectedConfigTestCore(t)

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
