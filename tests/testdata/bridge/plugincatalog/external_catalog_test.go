package plugincatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"guiforcores/bridge/plugins"
	"guiforcores/bridge/storage"
)

// Integration tests consume a separate checkout; plugin sources are never
// stored in the application repository or linked into its executable.
func externalPlugin(t *testing.T, id string) plugins.Plugin {
	t.Helper()
	directory := os.Getenv("WEBUI_PLUGIN_REPO")
	if directory == "" {
		t.Skip("set WEBUI_PLUGIN_REPO to an independent plugin repository checkout")
	}
	items, err := Read(func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(directory, filepath.FromSlash(name)))
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range items {
		if plugin.ID == id {
			return plugin
		}
	}
	t.Fatalf("external plugin %q not found", id)
	return plugins.Plugin{}
}

func externalOrganizer(t *testing.T, mode string) plugins.Plugin {
	t.Helper()
	plugin := externalPlugin(t, "plugin-subscription-organizer")
	plugin.Configuration = map[string]any{"Enabled": true, "RenameMode": mode}
	if mode == "dedupe" {
		plugin.Configuration["RenameMode"] = "none"
		plugin.Configuration["RemoveDuplicates"] = true
	}
	return plugin
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

func TestExternalRenamersPreserveDuplicateReferencesAndExistingSuffixes(t *testing.T) {
	for _, id := range []string{"index", "flags", "beautify"} {
		t.Run(id, func(t *testing.T) {
			plugin := externalOrganizer(t, id)
			plugin.Configuration["EnableSubscriptionName"] = false
			plugin.Configuration["EnableNationalEmoji"] = false
			plugin.Configuration["EnableAddEmoji"] = false
			plugin.Configuration["EnableIndexProxyName"] = "2"
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
			byServer := map[string]map[string]any{}
			var group map[string]any
			for _, proxy := range result {
				if proxy["type"] == "selector" {
					group = proxy
				}
				if server, ok := proxy["server"].(string); ok {
					byServer[server] = proxy
				}
			}
			if group["default"] != byServer["suffix.test"]["tag"] || byServer["fourth.test"]["detour"] != byServer["suffix.test"]["tag"] || group["outbounds"].([]any)[0] != byServer["first.test"]["tag"] {
				t.Fatal("renaming redirected an original reference to the wrong duplicate")
			}
			for index := 0; index < 3; index++ {
				if byServer[input[index]["server"].(string)]["password"] != input[index]["password"] {
					t.Fatal("renaming changed connection settings")
				}
			}
			if string(jsonBytes(t, input)) != before {
				t.Fatal("renaming mutated original cache")
			}
		})
	}
}

func TestExternalModifyNameRegionKeywordAndIndexOptions(t *testing.T) {
	plugin := externalOrganizer(t, "flags")
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

func TestExternalBeautifyIsIdempotentIncludingManualCachedRun(t *testing.T) {
	plugin := externalOrganizer(t, "beautify")
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
	if first[0]["tag"] != "My subscription | 🇭🇰 香港 01" {
		t.Fatalf("unexpected beautified name: %#v", first[0]["tag"])
	}
	second, err := service.ApplySubscription(context.Background(), first, metadata)
	if err != nil || string(jsonBytes(t, second)) != string(jsonBytes(t, first)) {
		t.Fatalf("automatic beautify is not idempotent: %#v %v", second, err)
	}
	manual, _, err := service.RunSubscription(context.Background(), plugin.ID, second, metadata)
	if err != nil || string(jsonBytes(t, manual)) != string(jsonBytes(t, first)) {
		t.Fatalf("manual cached beautify changed names again: %#v %v", manual, err)
	}
	assertReferencesExist(t, manual)
}

func TestExternalDedupePreservesCredentialsTLSAndRemapsReferences(t *testing.T) {
	service := installedPlugin(t, externalOrganizer(t, "dedupe"))
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

func TestExternalVisualSettingsChangeNamesAndPreserveNotices(t *testing.T) {
	p := externalOrganizer(t, "beautify")
	p.Configuration = map[string]any{"Enabled": true, "RenameMode": "beautify", "EnableSubscriptionName": false, "EnableNationalEmoji": false, "EnableCityName": true}
	s := installedPlugin(t, p)
	input := []map[string]any{{"type": "direct", "tag": "JP 东京 IEPL 2x"}, {"type": "direct", "tag": "剩余流量: 200 GB"}}
	result, err := s.ApplySubscription(context.Background(), input, map[string]any{"name": "Airport"})
	if err != nil {
		t.Fatal(err)
	}
	if result[0]["tag"] != "日本 01 东京 | IEPL 2x" || result[1]["tag"] != "剩余流量: 200 GB" {
		t.Fatalf("visual defaults/city/keywords failed: %#v", result)
	}
	p.Configuration["EnableCityName"] = false
	p.Configuration["ReservedKeywords"] = ""
	p.Configuration["EnableUnifyRegionName"] = "统一为英文"
	if _, err := s.Save(p, s.List().Revision); err != nil {
		t.Fatal(err)
	}
	result, err = s.ApplySubscription(context.Background(), input, nil)
	if err != nil || result[0]["tag"] != "Japan 01" {
		t.Fatalf("disabling options failed: %#v %v", result, err)
	}

	name := externalOrganizer(t, "flags")
	name.Configuration = map[string]any{"Enabled": true, "RenameMode": "flags", "EnableAddEmoji": false, "EnableIndexProxyName": "0"}
	s = installedPlugin(t, name)
	result, err = s.ApplySubscription(context.Background(), []map[string]any{{"type": "direct", "tag": "same"}, {"type": "direct", "tag": "same"}}, nil)
	if err != nil || result[0]["tag"] != "same" || result[1]["tag"] != "same" {
		t.Fatalf("No index option still adds indices: %#v %v", result, err)
	}
}
