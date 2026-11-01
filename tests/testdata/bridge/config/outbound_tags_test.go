package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	profilev1 "guiforcores/gen/profile/v1"
)

func TestSameSubscriptionRepeatedTagsKeepDistinctNodesAndReferences(t *testing.T) {
	const notice = "套餐到期：长期有效"
	g := &configGenerator{
		subscriptions: map[string]subscriptionMeta{"sub": {ID: "sub", Proxies: []subscriptionProxyMeta{
			{ID: "first", Tag: "Hong Kong"}, {ID: "second", Tag: "Hong Kong"}, {ID: "third", Tag: "Hong Kong"},
			{ID: "notice-one", Tag: notice}, {ID: "notice-two", Tag: notice},
			{ID: "nested", Tag: "nested"}, {ID: "chain", Tag: "chain"},
		}}},
		subscriptionProxies: map[string][]map[string]any{"sub": {
			{"type": "socks", "tag": "Hong Kong", "server": "192.0.2.1", "server_port": 1080, "username": "user", "password": "one"},
			{"type": "socks", "tag": "Hong Kong", "server": "192.0.2.2", "server_port": 1080, "username": "user", "password": "two"},
			{"type": "socks", "tag": "Hong Kong", "server": "192.0.2.1", "server_port": 1080, "username": "user", "password": "different"},
			{"type": "socks", "tag": notice, "server": "192.0.2.3", "server_port": 1080},
			{"type": "socks", "tag": notice, "server": "192.0.2.3", "server_port": 1080},
			{"type": "selector", "tag": "nested", "outbounds": []any{"Hong Kong", notice}, "default": "Hong Kong"},
			{"type": "socks", "tag": "chain", "server": "192.0.2.4", "server_port": 1080, "detour": "Hong Kong"},
		}},
	}
	before, err := json.Marshal(g.subscriptionProxies)
	if err != nil {
		t.Fatal(err)
	}
	outbounds := []*profilev1.Outbound{
		// Request the second occurrence first: raw tag-only references must still
		// target the first source node, not the first group visited by generation.
		{Id: "picked", Tag: "picked", Type: profilev1.OutboundType_OUTBOUND_TYPE_SELECTOR, Outbounds: []*profilev1.ProxyRef{{Type: "sub", Id: "second"}, {Type: "sub", Id: "third"}}},
		{Id: "all", Tag: "all", Type: profilev1.OutboundType_OUTBOUND_TYPE_SELECTOR, Outbounds: []*profilev1.ProxyRef{{Type: "Subscription", Id: "sub"}}},
		{Id: "reserved", Tag: "Hong Kong [sub]", Type: profilev1.OutboundType_OUTBOUND_TYPE_DIRECT},
	}
	generate := func(profiles []*profilev1.Outbound) ([]any, map[string]map[string]any) {
		t.Helper()
		items, err := g.generateOutbounds(profiles)
		if err != nil {
			t.Fatal(err)
		}
		byTag := make(map[string]map[string]any, len(items))
		for _, value := range items {
			item := value.(map[string]any)
			tag := item["tag"].(string)
			if _, exists := byTag[tag]; exists {
				t.Fatalf("duplicate generated outbound tag %q", tag)
			}
			byTag[tag] = item
		}
		return items, byTag
	}
	items, first := generate(outbounds)
	if len(first) != 10 {
		t.Fatalf("same-name subscription nodes were discarded: got %d outbounds, want 10", len(first))
	}
	all := first["all"]["outbounds"].([]any)
	if len(all) != 7 || all[0] == all[1] || all[0] == all[2] || all[3] == all[4] {
		t.Fatalf("subscription members did not get unique identities: %#v", all)
	}
	picked := first["picked"]["outbounds"].([]any)
	if !reflect.DeepEqual(picked, []any{all[1], all[2]}) || first[picked[0].(string)]["server"] != "192.0.2.2" || first[picked[1].(string)]["password"] != "different" {
		t.Fatalf("explicit IDs selected the wrong same-name node: %#v", picked)
	}
	if first["chain"]["detour"] != all[0] || first["nested"]["default"] != all[0] || !reflect.DeepEqual(first["nested"]["outbounds"], []any{all[0], all[3]}) {
		t.Fatalf("ambiguous source names did not retain first-node reference semantics: chain=%#v nested=%#v", first["chain"], first["nested"])
	}
	_, second := generate([]*profilev1.Outbound{outbounds[2], outbounds[1], outbounds[0]})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("profile group reordering changed node identities: first=%#v second=%#v", first, second)
	}
	after, err := json.Marshal(g.subscriptionProxies)
	if err != nil || string(after) != string(before) {
		t.Fatalf("generation modified the subscription cache: %s (%v)", after, err)
	}

	t.Run("real core accepts all repeated names", func(t *testing.T) {
		core := os.Getenv("SING_BOX_114_PATH")
		if core == "" {
			t.Skip("set SING_BOX_114_PATH for a real core check")
		}
		payload, err := json.Marshal(map[string]any{"outbounds": items, "route": map[string]any{"final": "all"}})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := runHTTPClientCoreCommand(core, "check", "-c", path); err != nil {
			t.Fatalf("core rejected unique generated tags: %v\n%s", err, output)
		}
	})
}

func TestSubscriptionTagCollisionsPreserveSelectedNodes(t *testing.T) {
	g := &configGenerator{
		subscriptions: map[string]subscriptionMeta{
			"sub-a": {ID: "sub-a", Proxies: []subscriptionProxyMeta{{ID: "node-a", Tag: "Hong Kong"}}},
			"sub-b": {ID: "sub-b", Proxies: []subscriptionProxyMeta{{ID: "node-b", Tag: "Hong Kong"}}},
		},
		subscriptionProxies: map[string][]map[string]any{
			"sub-a": {
				{"type": "socks", "tag": "Hong Kong", "server": "192.0.2.1", "server_port": 1080},
				{"type": "socks", "tag": "chain", "server": "192.0.2.3", "server_port": 1080, "detour": "Hong Kong"},
				{"type": "direct", "tag": "direct"},
				{"type": "selector", "tag": "nested", "outbounds": []any{"Hong Kong"}, "default": "Hong Kong"},
			},
			"sub-b": {{"type": "socks", "tag": "Hong Kong", "server": "192.0.2.2", "server_port": 1080}},
		},
	}
	outbounds := []*profilev1.Outbound{
		{Id: "a", Tag: "group-a", Type: profilev1.OutboundType_OUTBOUND_TYPE_SELECTOR, Outbounds: []*profilev1.ProxyRef{{Type: "Subscription", Id: "sub-a"}}},
		{Id: "b", Tag: "group-b", Type: profilev1.OutboundType_OUTBOUND_TYPE_SELECTOR, Outbounds: []*profilev1.ProxyRef{{Type: "sub-b", Id: "node-b"}}},
		{Id: "builtins", Tag: "builtins", Type: profilev1.OutboundType_OUTBOUND_TYPE_SELECTOR, Outbounds: []*profilev1.ProxyRef{{Type: "Built-in", Id: "direct"}}},
		{Id: "reserved", Tag: "Hong Kong [sub-a]", Type: profilev1.OutboundType_OUTBOUND_TYPE_DIRECT},
	}
	generate := func(profiles []*profilev1.Outbound) map[string]map[string]any {
		t.Helper()
		items, err := g.generateOutbounds(profiles)
		if err != nil {
			t.Fatal(err)
		}
		byTag := map[string]map[string]any{}
		for _, value := range items {
			item := value.(map[string]any)
			tag := item["tag"].(string)
			if _, exists := byTag[tag]; exists {
				t.Fatalf("duplicate outbound tag %q", tag)
			}
			byTag[tag] = item
		}
		return byTag
	}
	first := generate(outbounds)
	selected := func(group string) map[string]any {
		return first[first[group]["outbounds"].([]any)[0].(string)]
	}
	if selected("group-a")["server"] != "192.0.2.1" || selected("group-b")["server"] != "192.0.2.2" {
		t.Fatalf("groups no longer select their own subscription: %#v", first)
	}
	if first["chain"]["detour"] != selected("group-a")["tag"] {
		t.Fatalf("chained dialer lost its subscription-local target: %#v", first["chain"])
	}
	if first["nested"]["default"] != selected("group-a")["tag"] || first["nested"]["outbounds"].([]any)[0] != selected("group-a")["tag"] {
		t.Fatalf("raw subscription group lost its selected node: %#v", first["nested"])
	}
	if first["group-a"]["outbounds"].([]any)[2] == "direct" || selected("builtins")["tag"] != "direct" {
		t.Fatal("subscription node overwrote the built-in direct outbound")
	}
	reversed := []*profilev1.Outbound{outbounds[3], outbounds[2], outbounds[1], outbounds[0]}
	if second := generate(reversed); !reflect.DeepEqual(first, second) {
		t.Fatalf("reordering profile groups changed node identities:\n%#v\n%#v", first, second)
	}
	if g.subscriptionProxies["sub-a"][0]["tag"] != "Hong Kong" || g.subscriptionProxies["sub-a"][1]["detour"] != "Hong Kong" {
		t.Fatal("configuration generation modified the stored subscription")
	}
}

func TestRejectDuplicateProfileOutboundTags(t *testing.T) {
	g := &configGenerator{}
	_, err := g.generateOutbounds([]*profilev1.Outbound{
		{Id: "one", Tag: "direct", Type: profilev1.OutboundType_OUTBOUND_TYPE_DIRECT},
		{Id: "two", Tag: "direct", Type: profilev1.OutboundType_OUTBOUND_TYPE_BLOCK},
	})
	if err == nil {
		t.Fatal("conflicting explicit outbound tags would silently replace a different outbound")
	}
}
