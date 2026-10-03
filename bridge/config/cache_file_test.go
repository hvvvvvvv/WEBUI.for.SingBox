package config

import (
	"testing"

	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestGenerateCacheFileUsesCoreDNSPersistenceDefault(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cacheFile *profilev1.CacheFileExperimental
		wantDNS   bool
	}{
		{name: "missing cache settings"},
		{name: "enabled cache with default DNS persistence", cacheFile: &profilev1.CacheFileExperimental{Enabled: true}},
		{name: "explicitly disabled DNS persistence", cacheFile: &profilev1.CacheFileExperimental{Enabled: true, StoreDns: false}},
		{name: "enabled DNS persistence", cacheFile: &profilev1.CacheFileExperimental{Enabled: true, StoreDns: true}, wantDNS: true},
		{name: "disabled cache retains selected DNS persistence", cacheFile: &profilev1.CacheFileExperimental{StoreDns: true}, wantDNS: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			experimental := generateExperimental(&profilev1.Experimental{CacheFile: tc.cacheFile}, nil)
			cacheFile := experimental["cache_file"].(map[string]any)
			value, exists := cacheFile["store_dns"]
			if exists != tc.wantDNS || exists && value != true {
				t.Fatalf("store_dns = %#v, exists = %v; want true only when enabled = %v", value, exists, tc.wantDNS)
			}
			for _, legacy := range []string{"store_rdrc", "rdrc_timeout"} {
				if _, exists := cacheFile[legacy]; exists {
					t.Fatalf("legacy cache field %q was generated: %#v", legacy, cacheFile)
				}
			}
			if tc.cacheFile != nil && cacheFile["enabled"] != tc.cacheFile.GetEnabled() {
				t.Fatalf("cache enabled = %#v, want %v", cacheFile["enabled"], tc.cacheFile.GetEnabled())
			}
		})
	}

	cacheFile := generateExperimental(&profilev1.Experimental{CacheFile: &profilev1.CacheFileExperimental{
		Enabled: true, Path: "custom-cache.db", CacheId: "profile-cache", StoreFakeip: true,
	}}, nil)["cache_file"].(map[string]any)
	if cacheFile["path"] != "custom-cache.db" || cacheFile["cache_id"] != "profile-cache" || cacheFile["store_fakeip"] != true {
		t.Fatalf("other cache settings changed: %#v", cacheFile)
	}

	dns, err := (&configGenerator{}).generateDNS(&profilev1.Dns{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := dns["independent_cache"]; exists {
		t.Fatalf("legacy independent_cache was generated: %#v", dns)
	}
}

func TestCustomCacheConfigurationRemainsUnmodified(t *testing.T) {
	for _, tc := range []struct {
		name       string
		structured bool
		mixin      string
		script     string
		wantDNS    bool
		wantLegacy bool
	}{
		{
			name: "mixin disables structured persistence", structured: true,
			mixin: `{"experimental":{"cache_file":{"store_dns":false}}}`,
		},
		{
			name:  "mixin enables persistence",
			mixin: `{"experimental":{"cache_file":{"store_dns":true}}}`, wantDNS: true,
		},
		{
			name: "script disables structured persistence", structured: true,
			script: `function onGenerate(config) { config.experimental.cache_file.store_dns = false; return config; }`,
		},
		{
			name:   "script enables persistence",
			script: `function onGenerate(config) { config.experimental.cache_file.store_dns = true; return config; }`, wantDNS: true,
		},
		{
			name:    "mixin legacy fields pass through",
			mixin:   `{"experimental":{"cache_file":{"store_dns":true,"store_rdrc":true,"rdrc_timeout":"7d"}},"dns":{"independent_cache":true}}`,
			wantDNS: true, wantLegacy: true,
		},
		{
			name: "script runs after mixin and preserves legacy fields", structured: true,
			mixin: `{"experimental":{"cache_file":{"store_dns":true}}}`,
			script: `function onGenerate(config) {
				config.experimental.cache_file.store_dns = false;
				config.experimental.cache_file.store_rdrc = true;
				config.experimental.cache_file.rdrc_timeout = "7d";
				config.dns.independent_cache = true;
				return config;
			}`,
			wantLegacy: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := &profilev1.Profile{
				Experimental: &profilev1.Experimental{CacheFile: &profilev1.CacheFileExperimental{Enabled: true, StoreDns: tc.structured}},
				Mixin:        &profilev1.Mixin{Priority: profilev1.MixinPriority_MIXIN_PRIORITY_MIXIN, Config: tc.mixin},
				Script:       &profilev1.Script{Code: tc.script},
			}
			generated, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(profile, &kernelv1.GenerateConfigOptions{
				EnableMixinProcessing: true, EnableScriptProcessing: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			cacheFile := generated["experimental"].(map[string]any)["cache_file"].(map[string]any)
			if value, exists := cacheFile["store_dns"]; !exists || value != tc.wantDNS {
				t.Fatalf("custom store_dns = %#v, exists = %v; want %v", value, exists, tc.wantDNS)
			}
			if tc.wantLegacy {
				if cacheFile["store_rdrc"] != true || cacheFile["rdrc_timeout"] != "7d" {
					t.Fatalf("custom legacy cache settings changed: %#v", cacheFile)
				}
				if generated["dns"].(map[string]any)["independent_cache"] != true {
					t.Fatalf("custom independent_cache changed: %#v", generated["dns"])
				}
			}
		})
	}
}
