package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
)

func seedTestConfig(t *testing.T) (map[string]any, []any) {
	t.Helper()
	manifest, err := readRuleSetSeedManifest()
	if err != nil {
		t.Fatal(err)
	}
	rules := make([]any, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		rules = append(rules, map[string]any{
			"type": "remote", "tag": file.File, "format": "binary",
			"url":         "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/" + file.Path,
			"http_client": map[string]any{"detour": "user-selected-proxy"},
		})
	}
	return map[string]any{"route": map[string]any{"rule_set": rules}}, rules
}

func TestRuntimeRuleSetSeedsAreVerifiedReusedAndRepaired(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app with spaces")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	service := NewService(storage.NewPaths(root), "test")
	config, rules := seedTestConfig(t)
	// A renamed default still denotes the same resource, never a disk pathname.
	rules[0].(map[string]any)["tag"] = "../../renamed-default"
	rules[0].(map[string]any)["update_interval"] = "48h"
	if err := service.PrepareRuleSetBootstrap(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	manifest, _ := readRuleSetSeedManifest()
	oldTime := time.Unix(1600000000, 0)
	for i, file := range manifest.Files {
		rule := rules[i].(map[string]any)
		name, ok := rule["initial_path"].(string)
		if !ok || !filepath.IsAbs(name) || filepath.Base(name) != file.File || !strings.HasPrefix(name, service.paths.Resolve("data/rule-set-bootstrap")+"/") {
			t.Fatalf("invalid managed seed path: %v", rule)
		}
		data, err := os.ReadFile(name)
		sum := sha256.Sum256(data)
		if err != nil || int64(len(data)) != file.Bytes || hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Fatalf("seed does not match bundled source: %s %v", file.File, err)
		}
		if !reflect.DeepEqual(rule["http_client"], map[string]any{"detour": "user-selected-proxy"}) {
			t.Fatal("bootstrap changed user download policy")
		}
		if i == 0 && rule["update_interval"] != "48h" || i != 0 && rule["update_interval"] != "24h" {
			t.Fatalf("incorrect update interval: %v", rule)
		}
		if err := os.Chtimes(name, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.PrepareRuleSetBootstrap(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	for _, value := range rules {
		info, err := os.Stat(value.(map[string]any)["initial_path"].(string))
		if err != nil || !info.ModTime().Equal(oldTime) {
			t.Fatalf("valid seed rewritten on flash: %v", err)
		}
	}
	path := rules[0].(map[string]any)["initial_path"].(string)
	if err := os.WriteFile(path, []byte("interrupted or corrupt seed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.PrepareRuleSetBootstrap(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != manifest.Files[0].SHA256 {
		t.Fatal("corrupt managed seed not restored")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := service.PrepareRuleSetBootstrap(context.Background(), config); err != nil {
		t.Fatalf("deleted managed seed was not restored: %v", err)
	}
}

func TestRuleSetSeedSupportsExternalDataButRejectsManagedSymlinks(t *testing.T) {
	root, external := t.TempDir(), t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "data")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	service := NewService(storage.NewPaths(root), "test")
	config, _ := seedTestConfig(t)
	if err := service.PrepareRuleSetBootstrap(context.Background(), config); err != nil {
		t.Fatalf("external data storage should remain supported: %v", err)
	}
	otherRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(otherRoot, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(otherRoot, "data", "rule-set-bootstrap")); err != nil {
		t.Fatal(err)
	}
	config, _ = seedTestConfig(t)
	if err := NewService(storage.NewPaths(otherRoot), "test").PrepareRuleSetBootstrap(context.Background(), config); err == nil {
		t.Fatal("managed extraction directory followed an unexpected symlink")
	}
}

func TestRuleSetSeedDoesNotReplaceUserSourcesOrExplicitPaths(t *testing.T) {
	cases := map[string]map[string]any{
		"custom-url":   {"url": "https://example.test/category-ads-all.srs"},
		"custom-path":  {"initial_path": "/my/custom.srs"},
		"explicit-off": {"initial_path": ""},
		"source":       {"format": "source"},
		"local":        {"type": "local"},
		"inline":       {"type": "inline"},
		"pinned":       {"url": "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/fixed-commit/geo/geosite/cn.srs"},
		"query":        {"url": "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs?custom=1"},
		"other-repo":   {"url": "https://raw.githubusercontent.com/someone/custom/sing/geo/geosite/cn.srs"},
	}
	for name, changes := range cases {
		t.Run(name, func(t *testing.T) {
			config, rules := seedTestConfig(t)
			rule := rules[0].(map[string]any)
			for key, value := range changes {
				rule[key] = value
			}
			config["route"].(map[string]any)["rule_set"] = []any{rule}
			before := deepCopyMap(rule)
			root := t.TempDir()
			if err := NewService(storage.NewPaths(root), "test").PrepareRuleSetBootstrap(context.Background(), config); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, rule) {
				t.Fatalf("user configuration was replaced: %v", rule)
			}
			if _, err := os.Stat(filepath.Join(root, "data", "rule-set-bootstrap")); !os.IsNotExist(err) {
				t.Fatalf("unneeded managed data was created: %v", err)
			}
		})
	}
}

func TestRuleSetSeedInstallationFailsBeforeConfigMutation(t *testing.T) {
	root := t.TempDir()
	service := NewService(storage.NewPaths(root), "test")
	config, rules := seedTestConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.PrepareRuleSetBootstrap(ctx, config); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not honored: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "rule-set-bootstrap"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.PrepareRuleSetBootstrap(context.Background(), config); err == nil || !strings.Contains(err.Error(), "initial rule-set directory") {
		t.Fatalf("unwritable seed directory did not stop preparation: %v", err)
	}
	for _, value := range rules {
		if _, exists := value.(map[string]any)["initial_path"]; exists {
			t.Fatal("failed preparation left a runtime reference to unavailable data")
		}
	}
}

func TestRuntimeSeedsDoNotLeakIntoPreviewAndSharing(t *testing.T) {
	root := t.TempDir()
	defaultProfileSubscription(t, root, map[string]any{"type": "http", "tag": "fixture-node", "server": "127.0.0.1", "server_port": 1080})
	service := NewService(storage.NewPaths(root), "test")
	profile := defaultQuickStartProfile(t)
	assertPortable := func() {
		t.Helper()
		preview, err := service.Generate(profile, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range preview["route"].(map[string]any)["rule_set"].([]any) {
			if _, exists := value.(map[string]any)["initial_path"]; exists {
				t.Fatal("preview/export contains a device-local initial path")
			}
		}
	}
	assertPortable()
	if _, err := os.Stat(filepath.Join(root, "data", "rule-set-bootstrap")); !os.IsNotExist(err) {
		t.Fatalf("preview extracted runtime files: %v", err)
	}
	response, err := service.GenerateConfigFile(context.Background(), connect.NewRequest(&kernelv1.GenerateConfigFileRequest{Profile: profile}))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range response.Msg.GetConfig().AsMap()["route"].(map[string]any)["rule_set"].([]any) {
		path, ok := value.(map[string]any)["initial_path"].(string)
		if !ok {
			t.Fatal("runtime config omitted default seed")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("runtime references unavailable seed: %v", err)
		}
	}
	assertPortable()
}
