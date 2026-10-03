package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"

	"connectrpc.com/connect"
)

func decodeHTTPClientFixture(t *testing.T, text string) map[string]any {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal([]byte(text), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestRuleSetHTTPClientMigration(t *testing.T) {
	tests := []struct {
		name   string
		config string
		client string
	}{
		{
			name:   "proxy keeps chosen tag",
			config: `{"outbounds":[{"type":"socks","tag":"proxy"}],"route":{"rule_set":[{"type":"remote","download_detour":"proxy"}]}}`,
			client: `{"detour":"proxy"}`,
		},
		{
			name:   "direct selector remains a selector",
			config: `{"outbounds":[{"type":"selector","tag":"direct-group","outbounds":["direct"]},{"type":"direct","tag":"direct"}],"route":{"rule_set":[{"type":"remote","download_detour":"direct-group"}]}}`,
			client: `{"detour":"direct-group"}`,
		},
		{
			name:   "urltest remains a detour",
			config: `{"outbounds":[{"type":"urltest","tag":"auto"}],"route":{"rule_set":[{"type":"remote","download_detour":"auto"}]}}`,
			client: `{"detour":"auto"}`,
		},
		{
			name:   "empty direct dials explicitly",
			config: `{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rule_set":[{"type":"remote","download_detour":"direct"}]}}`,
			client: `{"engine":"go"}`,
		},
		{
			name:   "direct fallback is a valid legacy reference",
			config: `{"route":{"rule_set":[{"type":"remote","download_detour":"direct"}]}}`,
			client: `{"engine":"go"}`,
		},
		{
			name:   "endpoint detour",
			config: `{"endpoints":[{"type":"wireguard","tag":"vpn"}],"route":{"rule_set":[{"type":"remote","download_detour":"vpn"}]}}`,
			client: `{"detour":"vpn"}`,
		},
		{
			name:   "numeric endpoint tag",
			config: `{"endpoints":[{"type":"wireguard"}],"route":{"rule_set":[{"type":"remote","download_detour":"0"}]}}`,
			client: `{"detour":"0"}`,
		},
		{
			name:   "outbound beats same named endpoint",
			config: `{"outbounds":[{"type":"direct","tag":"vpn"}],"endpoints":[{"type":"wireguard","tag":"vpn"}],"route":{"rule_set":[{"type":"remote","download_detour":"vpn"}]}}`,
			client: `{"engine":"go"}`,
		},
		{
			name:   "virtual direct beats same named endpoint",
			config: `{"endpoints":[{"type":"wireguard","tag":"direct"}],"route":{"rule_set":[{"type":"remote","download_detour":"direct"}]}}`,
			client: `{"engine":"go"}`,
		},
		{
			name:   "modern empty object beats invalid legacy field",
			config: `{"route":{"rule_set":[{"type":"remote","http_client":{},"download_detour":false}]}}`,
			client: `{}`,
		},
		{
			name:   "modern options are preserved",
			config: `{"route":{"rule_set":[{"type":"remote","http_client":{"engine":"go","version":1,"headers":{"X-Test":"value"},"tls":{"server_name":"rules.example"}},"download_detour":"missing"}]}}`,
			client: `{"engine":"go","version":1,"headers":{"X-Test":"value"},"tls":{"server_name":"rules.example"}}`,
		},
		{
			name:   "shared reference beats legacy field",
			config: `{"http_clients":[{"tag":"custom","engine":"go"}],"route":{"rule_set":[{"type":"remote","http_client":"custom","download_detour":"missing"}]}}`,
			client: `{"value":"custom"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := decodeHTTPClientFixture(t, tc.config)
			if err := normalizeRuleSetHTTPClients(config); err != nil {
				t.Fatal(err)
			}
			route := config["route"].(map[string]any)
			rule := route["rule_set"].([]any)[0].(map[string]any)
			want := any(decodeHTTPClientFixture(t, tc.client))
			if wrapped, ok := want.(map[string]any)["value"]; ok {
				want = wrapped
			}
			if !reflect.DeepEqual(rule["http_client"], want) {
				t.Fatalf("client = %#v, want %#v", rule["http_client"], want)
			}
			if _, exists := rule["download_detour"]; exists {
				t.Fatal("legacy download field remains")
			}
			if _, exists := route["default_http_client"]; exists {
				t.Fatal("added a default when every rule has an explicit client")
			}
			before, _ := json.Marshal(config)
			if err := normalizeRuleSetHTTPClients(config); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(config)
			if string(before) != string(after) {
				t.Fatal("normalization is not idempotent")
			}
		})
	}
}

func TestRuleSetHTTPClientPreservesDirectDialSettings(t *testing.T) {
	config := decodeHTTPClientFixture(t, `{
		"outbounds":[{"type":"direct","tag":"chosen","bind_interface":"eth0","inet4_bind_address":"127.0.0.1","inet6_bind_address":"::1","bind_address_no_port":true,"protect_path":"/tmp/protect","routing_mark":42,"reuse_addr":true,"netns":"ns","connect_timeout":"5s","tcp_fast_open":false,"tcp_multi_path":true,"disable_tcp_keep_alive":true,"tcp_keep_alive":"30s","tcp_keep_alive_interval":"10s","udp_fragment":false,"domain_resolver":{"server":"dns","strategy":"prefer_ipv4"},"network_strategy":"fallback","network_type":["wifi"],"fallback_network_type":["cellular"],"fallback_delay":"500ms","domain_strategy":"prefer_ipv6","override_address":"old.example","override_port":443,"proxy_protocol":1}],
		"route":{"rule_set":[{"type":"remote","download_detour":"chosen"}]}
	}`)
	outbound := config["outbounds"].([]any)[0].(map[string]any)
	if err := normalizeRuleSetHTTPClients(config); err != nil {
		t.Fatal(err)
	}
	client := config["route"].(map[string]any)["rule_set"].([]any)[0].(map[string]any)["http_client"].(map[string]any)
	for _, field := range httpClientDirectDialFields {
		if !reflect.DeepEqual(client[field], outbound[field]) {
			t.Fatalf("lost dial field %s: %#v", field, client[field])
		}
	}
	for _, field := range []string{"type", "tag", "detour", "override_address", "override_port", "proxy_protocol"} {
		if _, exists := client[field]; exists {
			t.Fatalf("copied outbound-only field %s", field)
		}
	}
	client["domain_resolver"].(map[string]any)["strategy"] = "ipv4_only"
	client["network_type"].([]any)[0] = "ethernet"
	if outbound["domain_resolver"].(map[string]any)["strategy"] != "prefer_ipv4" || outbound["network_type"].([]any)[0] != "wifi" {
		t.Fatal("client dial options alias the selected outbound")
	}
}

func TestRuleSetHTTPClientDefaults(t *testing.T) {
	tests := []struct {
		name   string
		config string
		tag    string
		client string
	}{
		{"final endpoint", `{"outbounds":[{"type":"direct","tag":"first"}],"endpoints":[{"type":"wireguard","tag":"vpn"}],"route":{"final":"vpn","rule_set":[{"type":"remote"}]}}`, ruleSetDefaultHTTPClientTag, `{"tag":"webui-rule-set-default","detour":"vpn"}`},
		{"first unnamed outbound", `{"outbounds":[{"type":"socks"}],"route":{"rule_set":[{"type":"remote","http_client":""}]}}`, ruleSetDefaultHTTPClientTag, `{"tag":"webui-rule-set-default","detour":"0"}`},
		{"no outbound", `{"route":{"rule_set":[{"type":"remote","http_client":null,"download_detour":""}]}}`, ruleSetDefaultHTTPClientTag, `{"tag":"webui-rule-set-default","engine":"go"}`},
		{"explicit shared default wins", `{"outbounds":[{"type":"direct","tag":"direct"},{"type":"socks","tag":"proxy"}],"http_clients":[{"tag":"first","engine":"go"},{"tag":"custom","detour":"proxy"}],"route":{"final":"direct","default_http_client":"custom","rule_set":[{"type":"remote"}]}}`, "custom", `{"tag":"custom","detour":"proxy"}`},
		{"first shared wins", `{"http_clients":[{"tag":"first","engine":"go"}],"route":{"rule_set":[{"type":"remote"}]}}`, "first", `{"tag":"first","engine":"go"}`},
		{"unnamed first shared avoids collisions", `{"http_clients":[{"engine":"go"},{"tag":"webui-rule-set-default","engine":"go"},{"tag":"webui-rule-set-default-2","engine":"go"}],"route":{"rule_set":[{"type":"remote"}]}}`, "webui-rule-set-default-3", `{"tag":"webui-rule-set-default-3","engine":"go"}`},
		{"duplicate shared tag matches core", `{"http_clients":[{"tag":"custom","detour":"missing"},{"tag":"custom","engine":"go"}],"route":{"rule_set":[{"type":"remote"}]}}`, "custom", `{"tag":"custom","engine":"go"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := decodeHTTPClientFixture(t, tc.config)
			if err := normalizeRuleSetHTTPClients(config); err != nil {
				t.Fatal(err)
			}
			route := config["route"].(map[string]any)
			if route["default_http_client"] != tc.tag {
				t.Fatalf("default = %v, want %s", route["default_http_client"], tc.tag)
			}
			var client any
			for _, value := range config["http_clients"].([]any) {
				if value.(map[string]any)["tag"] == tc.tag {
					client = value
				}
			}
			if !reflect.DeepEqual(client, decodeHTTPClientFixture(t, tc.client)) {
				t.Fatalf("default client = %#v", client)
			}
			rule := route["rule_set"].([]any)[0].(map[string]any)
			if _, exists := rule["http_client"]; exists {
				t.Fatal("default consumer still has an empty explicit client")
			}
			before, _ := json.Marshal(config)
			if err := normalizeRuleSetHTTPClients(config); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(config)
			if string(before) != string(after) {
				t.Fatal("default client changes on repeated normalization")
			}
		})
	}
}

func TestRuleSetHTTPClientErrors(t *testing.T) {
	tests := []struct{ name, config, path string }{
		{"route type", `{"route":false}`, "route"},
		{"rule set array", `{"route":{"rule_set":{}}}`, "route.rule_set"},
		{"rule set object", `{"route":{"rule_set":[1]}}`, "route.rule_set[0]"},
		{"rule set type", `{"route":{"rule_set":[{"type":true}]}}`, "route.rule_set[0].type"},
		{"modern client type", `{"route":{"rule_set":[{"type":"remote","http_client":false}]}}`, "route.rule_set[0].http_client"},
		{"legacy field type", `{"route":{"rule_set":[{"type":"remote","download_detour":false}]}}`, "route.rule_set[0].download_detour"},
		{"missing selected outbound", `{"route":{"rule_set":[{"type":"remote","download_detour":"gone"}]}}`, "route.rule_set[0].download_detour"},
		{"missing shared reference", `{"route":{"rule_set":[{"type":"remote","http_client":"gone"}]}}`, "route.rule_set[0].http_client"},
		{"inline detour type", `{"route":{"rule_set":[{"type":"remote","http_client":{"detour":3}}]}}`, "route.rule_set[0].http_client.detour"},
		{"inline missing detour", `{"route":{"rule_set":[{"type":"remote","http_client":{"detour":"gone"}}]}}`, "route.rule_set[0].http_client.detour"},
		{"shared detour missing", `{"http_clients":[{"tag":"custom","detour":"gone"}],"route":{"rule_set":[{"type":"remote","http_client":"custom"}]}}`, "http_clients[0].detour"},
		{"explicit default missing", `{"route":{"default_http_client":"gone","rule_set":[{"type":"remote"}]}}`, "route.default_http_client"},
		{"active default missing even with explicit client", `{"route":{"default_http_client":"gone","rule_set":[{"type":"remote","http_client":{}}]}}`, "route.default_http_client"},
		{"default type even with explicit client", `{"route":{"default_http_client":false,"rule_set":[{"type":"remote","http_client":{}}]}}`, "route.default_http_client"},
		{"active first shared missing detour", `{"http_clients":[{"tag":"first","detour":"gone"}],"route":{"rule_set":[{"type":"remote","http_client":{}}]}}`, "http_clients[0].detour"},
		{"final missing", `{"route":{"final":"gone","rule_set":[{"type":"remote"}]}}`, "route.final"},
		{"explicit final direct does not create fallback", `{"route":{"final":"direct","rule_set":[{"type":"remote"}]}}`, "route.final"},
		{"outbound array type", `{"outbounds":false,"route":{"rule_set":[{"type":"remote"}]}}`, "outbounds"},
		{"endpoint tag type", `{"endpoints":[{"type":"wireguard","tag":false}],"route":{"rule_set":[{"type":"remote"}]}}`, "endpoints[0].tag"},
		{"shared array type", `{"http_clients":{},"route":{"rule_set":[{"type":"remote"}]}}`, "http_clients"},
		{"shared entry type", `{"http_clients":[null],"route":{"rule_set":[{"type":"remote"}]}}`, "http_clients[0]"},
		{"shared tag type", `{"http_clients":[{"tag":false}],"route":{"rule_set":[{"type":"remote"}]}}`, "http_clients[0].tag"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := normalizeRuleSetHTTPClients(decodeHTTPClientFixture(t, tc.config))
			var invalid invalidArgumentError
			if err == nil || !errors.As(err, &invalid) || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("error = %v, want invalid argument at %s", err, tc.path)
			}
		})
	}
}

func TestRuleSetHTTPClientsOnlyTouchNeededClients(t *testing.T) {
	for _, text := range []string{
		`{}`,
		`{"route":null}`,
		`{"route":{"rule_set":[]}}`,
		`{"route":{"rule_set":[{"type":"inline","rules":[]},{"type":"local","format":"binary"}]},"http_clients":false}`,
		`{"http_clients":[{"tag":"active","engine":"go"},{"tag":"unused","detour":"gone"}],"route":{"rule_set":[{"type":"remote","http_client":{}}]}}`,
		`{"http_clients":[{"detour":"gone"}],"route":{"rule_set":[{"type":"remote","http_client":{}}]}}`,
	} {
		config := decodeHTTPClientFixture(t, text)
		before, _ := json.Marshal(config)
		if err := normalizeRuleSetHTTPClients(config); err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(config)
		if string(before) != string(after) {
			t.Fatalf("changed a config with no default-client consumers: %s", after)
		}
	}
}

func TestRuleSetHTTPClientsAcrossGenerationEntrypoints(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, "test")
	profile := &profilev1.Profile{
		Outbounds: []*profilev1.Outbound{{Id: "outbound-id", Tag: "download-direct", Type: profilev1.OutboundType_OUTBOUND_TYPE_DIRECT}},
		Route: &profilev1.Route{
			Final: "outbound-id",
			RuleSet: []*profilev1.RuleSet{{
				Type: profilev1.RulesetType_RULESET_TYPE_REMOTE, Tag: "rules", Format: profilev1.RulesetFormat_RULESET_FORMAT_SOURCE,
				Url: "https://example.org/rules.json", DownloadDetour: "outbound-id", UpdateInterval: "2h",
			}},
		},
	}
	assertClient := func(config map[string]any) {
		t.Helper()
		rule := config["route"].(map[string]any)["rule_set"].([]any)[0].(map[string]any)
		if _, exists := rule["download_detour"]; exists {
			t.Fatal("generation entrypoint emitted legacy download field")
		}
		if !reflect.DeepEqual(rule["http_client"], map[string]any{"engine": "go"}) || rule["url"] != "https://example.org/rules.json" || rule["update_interval"] != "2h" {
			t.Fatalf("unexpected migrated rule set: %#v", rule)
		}
	}
	generated, err := service.Generate(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertClient(generated)
	preview, err := service.GenerateConfig(context.Background(), connect.NewRequest(&kernelv1.GenerateConfigRequest{Profile: profile}))
	if err != nil {
		t.Fatal(err)
	}
	assertClient(preview.Msg.Config.AsMap())
	exported, err := service.GenerateConfigFile(context.Background(), connect.NewRequest(&kernelv1.GenerateConfigFileRequest{Profile: profile}))
	if err != nil {
		t.Fatal(err)
	}
	assertClient(exported.Msg.Config.AsMap())
	written, err := os.ReadFile(paths.Resolve(CoreConfigFilePath))
	if err != nil {
		t.Fatal(err)
	}
	assertClient(decodeHTTPClientFixture(t, string(written)))
	if profile.Route.RuleSet[0].DownloadDetour != "outbound-id" {
		t.Fatal("generation changed the stored outbound ID")
	}
	profile.Script = &profilev1.Script{Code: `function onGenerate(config) { config.outbounds = []; return config; }`}
	_, err = service.Generate(profile, &kernelv1.GenerateConfigOptions{EnableScriptProcessing: true})
	if err == nil || !strings.Contains(err.Error(), "route.rule_set[0].download_detour") {
		t.Fatalf("failed to detect script removing selected outbound: %v", err)
	}
}
