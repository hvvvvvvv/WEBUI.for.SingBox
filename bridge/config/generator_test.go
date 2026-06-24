package config

import (
	"testing"

	profilev1 "guiforcores/gen/profile/v1"
)

func TestGenerateExperimentalUsesManagedCoreAPI(t *testing.T) {
	experimental := generateExperimental(&profilev1.Experimental{
		CacheFile: &profilev1.CacheFileExperimental{
			Enabled:     true,
			Path:        "cache.db",
			CacheId:     "cache-id",
			StoreFakeip: true,
			StoreRdrc:   true,
			RdrcTimeout: "7d",
		},
	}, nil)

	clashAPI, ok := experimental["clash_api"].(map[string]any)
	if !ok {
		t.Fatalf("expected generated clash_api object, got %#v", experimental["clash_api"])
	}
	if controller := clashAPI["external_controller"]; controller != CoreAPIController {
		t.Fatalf("expected managed controller %q, got %#v", CoreAPIController, controller)
	}
	secret, ok := clashAPI["secret"].(string)
	if !ok || len(secret) != 64 {
		t.Fatalf("expected generated 64-char secret, got %#v", clashAPI["secret"])
	}

	cacheFile, ok := experimental["cache_file"].(map[string]any)
	if !ok {
		t.Fatalf("expected generated cache_file object, got %#v", experimental["cache_file"])
	}
	if cacheFile["path"] != "cache.db" || cacheFile["cache_id"] != "cache-id" {
		t.Fatalf("expected profile cache_file values to be preserved, got %#v", cacheFile)
	}
}
