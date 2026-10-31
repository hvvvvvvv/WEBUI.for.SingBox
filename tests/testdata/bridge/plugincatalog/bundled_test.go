package plugincatalog

import (
	"context"
	"strings"
	"testing"

	"guiforcores/bridge/plugins"
	"guiforcores/bridge/storage"
)

func bundledPlugin(t *testing.T, id string) plugins.Plugin {
	t.Helper()
	items, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range items {
		if plugin.ID == id {
			return plugin
		}
	}
	t.Fatalf("bundled plugin %q not found", id)
	return plugins.Plugin{}
}

func installedPlugin(t *testing.T, plugin plugins.Plugin) *plugins.Service {
	t.Helper()
	service, err := plugins.NewService(storage.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(plugin, 0); err != nil {
		t.Fatal(err)
	}
	return service
}

func assertReferencesExist(t *testing.T, proxies []map[string]any) {
	t.Helper()
	seen := make(map[string]bool)
	for _, proxy := range proxies {
		tag := proxy["tag"].(string)
		if seen[tag] {
			t.Fatalf("duplicate tag %q", tag)
		}
		seen[tag] = true
	}
	for _, proxy := range proxies {
		for _, field := range []string{"detour", "default"} {
			if tag, exists := proxy[field].(string); exists && !seen[tag] {
				t.Fatalf("%s.%s has dangling reference %q", proxy["tag"], field, tag)
			}
		}
		if members, ok := proxy["outbounds"].([]any); ok {
			for _, member := range members {
				if !seen[member.(string)] {
					t.Fatalf("%s has dangling group member %q", proxy["tag"], member)
				}
			}
		}
	}
}

func TestBundledRenamersPreserveDuplicateReferencesAndExistingSuffixes(t *testing.T) {
	for _, id := range []string{"plugin-index-the-proxy-name", "plugin-modify-proxy-name", "plugin-beautiful-node-name"} {
		t.Run(id, func(t *testing.T) {
			plugin := bundledPlugin(t, id)
			plugin.Configuration["EnableSubscriptionName"] = false
			plugin.Configuration["EnableNationalEmoji"] = false
			plugin.Configuration["EnableAddEmoji"] = false
			service := installedPlugin(t, plugin)
			input := []map[string]any{
				{"type": "shadowsocks", "tag": "same", "server": "first.test", "password": "one"},
				{"type": "shadowsocks", "tag": "same", "server": "second.test", "password": "two"},
				{"type": "shadowsocks", "tag": "same [2]", "server": "suffix.test", "password": "three"},
				{"type": "selector", "tag": "group", "default": "same [2]", "outbounds": []any{"same", "same [2]"}},
				{"type": "shadowsocks", "tag": "detoured", "detour": "same [2]", "server": "fourth.test"},
			}
			before := string(jsonBytes(t, input))
			result, err := service.ApplySubscription(context.Background(), input, map[string]any{"name": "Subscription"})
			if err != nil {
				t.Fatal(err)
			}
			assertReferencesExist(t, result)
			if result[3]["default"] != result[2]["tag"] || result[4]["detour"] != result[2]["tag"] || result[3]["outbounds"].([]any)[0] != result[0]["tag"] {
				t.Fatal("renaming redirected an original reference to the wrong duplicate")
			}
			for index := 0; index < 3; index++ {
				if result[index]["server"] != input[index]["server"] || result[index]["password"] != input[index]["password"] {
					t.Fatal("renaming changed connection settings")
				}
			}
			if string(jsonBytes(t, input)) != before {
				t.Fatal("renaming mutated original cache")
			}
		})
	}
}

func TestBundledModifyNameRegionKeywordAndIndexOptions(t *testing.T) {
	plugin := bundledPlugin(t, "plugin-modify-proxy-name")
	plugin.Configuration["EnableRemoveKeywords"] = true
	plugin.Configuration["RemoveKeywords"] = `^ISP\s+`
	plugin.Configuration["EnableIndexProxyName"] = "1"
	service := installedPlugin(t, plugin)
	input := []map[string]any{
		{"type": "direct", "tag": "ISP HK01"},
		{"type": "direct", "tag": "JP01", "detour": "ISP HK01"},
		{"type": "selector", "tag": "choice", "outbounds": []any{"ISP HK01", "JP01"}, "default": "JP01"},
	}
	result, err := service.ApplySubscription(context.Background(), input, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result[0]["tag"] != "🇭🇰 HK01 1" || result[1]["tag"] != "🇯🇵 JP01 2" {
		t.Fatalf("region/keyword/index options not applied: %#v", result)
	}
	assertReferencesExist(t, result)
	if result[1]["detour"] != result[0]["tag"] || result[2]["default"] != result[1]["tag"] {
		t.Fatal("modify-name did not remap outbound references")
	}
}

func TestBundledBeautifyIsIdempotentIncludingManualCachedRun(t *testing.T) {
	plugin := bundledPlugin(t, "plugin-beautiful-node-name")
	plugin.Configuration["EnableUnifyRegionName"] = "统一为中文"
	service := installedPlugin(t, plugin)
	input := []map[string]any{
		{"type": "direct", "tag": "  Hong   Kong  01 "},
		{"type": "direct", "tag": "  Hong   Kong  01 "},
		{"type": "selector", "tag": " group ", "outbounds": []any{"  Hong   Kong  01 "}, "default": "  Hong   Kong  01 "},
	}
	metadata := map[string]any{"name": "My subscription"}
	first, err := service.ApplySubscription(context.Background(), input, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if first[0]["tag"] != "[My subscription] 🇭🇰 香港 01" {
		t.Fatalf("unexpected beautified name: %#v", first[0]["tag"])
	}
	second, err := service.ApplySubscription(context.Background(), first, metadata)
	if err != nil || string(jsonBytes(t, second)) != string(jsonBytes(t, first)) {
		t.Fatalf("automatic beautify is not idempotent: %#v %v", second, err)
	}
	manual, logs, err := service.RunSubscription(context.Background(), plugin.ID, second, metadata)
	if err != nil || string(jsonBytes(t, manual)) != string(jsonBytes(t, first)) || len(logs) != 1 || !strings.Contains(logs[0], "3 nodes") {
		t.Fatalf("manual cached beautify changed names again or failed to log: %#v %#v %v", manual, logs, err)
	}
	assertReferencesExist(t, manual)
}

func TestBundledDedupePreservesCredentialsTLSAndRemapsReferences(t *testing.T) {
	service := installedPlugin(t, bundledPlugin(t, "plugin-duplicate-proxies-removal"))
	input := []map[string]any{
		{"type": "trojan", "tag": "first", "server": "same.test", "server_port": 443, "password": "secret", "tls": map[string]any{"enabled": true, "server_name": "same.test"}},
		{"password": "secret", "type": "trojan", "tag": "duplicate", "tls": map[string]any{"server_name": "same.test", "enabled": true}, "server": "same.test", "server_port": 443},
		{"type": "trojan", "tag": "different-password", "server": "same.test", "server_port": 443, "password": "different", "tls": map[string]any{"enabled": true, "server_name": "same.test"}},
		{"type": "trojan", "tag": "different-tls", "server": "same.test", "server_port": 443, "password": "secret", "tls": map[string]any{"enabled": true, "server_name": "other.test"}},
		{"type": "shadowsocks", "tag": "different-protocol", "server": "same.test", "server_port": 443, "password": "secret", "method": "aes-256-gcm"},
		{"type": "selector", "tag": "choice", "outbounds": []any{"first", "duplicate", "different-password"}, "default": "duplicate"},
		{"type": "direct", "tag": "detoured", "detour": "duplicate"},
	}
	before := string(jsonBytes(t, input))
	result, err := service.ApplySubscription(context.Background(), input, map[string]any{})
	if err != nil || len(result) != 6 {
		t.Fatalf("dedupe failed or removed differing connection settings: %#v %v", result, err)
	}
	assertReferencesExist(t, result)
	for index, tag := range []string{"first", "different-password", "different-tls", "different-protocol", "choice", "detoured"} {
		if result[index]["tag"] != tag {
			t.Fatalf("dedupe did not preserve stable order: %#v", result)
		}
	}
	if result[4]["default"] != "first" || result[5]["detour"] != "first" || string(jsonBytes(t, result[4]["outbounds"])) != `["first","different-password"]` {
		t.Fatal("dedupe left removed node references")
	}
	if string(jsonBytes(t, input)) != before {
		t.Fatal("dedupe mutated original input")
	}
	second, err := service.ApplySubscription(context.Background(), result, map[string]any{})
	if err != nil || string(jsonBytes(t, second)) != string(jsonBytes(t, result)) {
		t.Fatalf("dedupe is not idempotent: %#v %v", second, err)
	}
}
