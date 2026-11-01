package config

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

func defaultHTTPClientProfile() *profilev1.Profile {
	return &profilev1.Profile{
		Outbounds: []*profilev1.Outbound{
			{Id: "proxy-id", Tag: "proxy-group", Type: profilev1.OutboundType_OUTBOUND_TYPE_SELECTOR,
				Outbounds: []*profilev1.ProxyRef{{Id: "direct", Type: "Built-in"}}},
			{Id: "direct-id", Tag: "local-direct", Type: profilev1.OutboundType_OUTBOUND_TYPE_DIRECT},
		},
		Route: &profilev1.Route{
			Final: "direct-id", DefaultHttpClient: "proxy-id",
			RuleSet: []*profilev1.RuleSet{
				{Tag: "inherited", Type: profilev1.RulesetType_RULESET_TYPE_REMOTE, Format: profilev1.RulesetFormat_RULESET_FORMAT_SOURCE, Url: "https://example.test/inherited.json"},
				{Tag: "explicit", Type: profilev1.RulesetType_RULESET_TYPE_REMOTE, Format: profilev1.RulesetFormat_RULESET_FORMAT_SOURCE, Url: "https://example.test/explicit.json", DownloadDetour: "direct-id"},
			},
		},
	}
}

func TestProfileDefaultHTTPClientUsesOutboundIDAndKeepsPerRuleOverrides(t *testing.T) {
	profile := defaultHTTPClientProfile()
	original := proto.Clone(profile)
	config, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	route := config["route"].(map[string]any)
	if route["default_http_client"] != "proxy-group" || route["final"] != "local-direct" {
		t.Fatalf("default HTTP client was confused with the final outbound: %#v", route)
	}
	client := config["http_clients"].([]any)[0].(map[string]any)
	if client["tag"] != "proxy-group" || client["detour"] != "proxy-group" {
		t.Fatalf("outbound ID was not resolved to an HTTP client: %#v", client)
	}
	rules := route["rule_set"].([]any)
	if _, exists := rules[0].(map[string]any)["http_client"]; exists {
		t.Fatalf("inherited rule set unexpectedly overrides the default: %#v", rules[0])
	}
	explicit := rules[1].(map[string]any)["http_client"].(map[string]any)
	if explicit["engine"] != "go" || explicit["detour"] != nil {
		t.Fatalf("explicit direct download was not preserved: %#v", explicit)
	}
	if !proto.Equal(profile, original) {
		t.Fatal("generation modified the persisted profile")
	}
}

func TestProfileDefaultHTTPClientLegacyClearAndMissingReferences(t *testing.T) {
	for _, tc := range []struct{ id, wantTag, wantDetour string }{
		{"direct-id", "local-direct", ""},
		{"", ruleSetDefaultHTTPClientTag, ""},
	} {
		t.Run(tc.id, func(t *testing.T) {
			profile := defaultHTTPClientProfile()
			profile.Route.DefaultHttpClient = tc.id
			config, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(profile, nil)
			if err != nil {
				t.Fatal(err)
			}
			route := config["route"].(map[string]any)
			client := config["http_clients"].([]any)[0].(map[string]any)
			if route["default_http_client"] != tc.wantTag || client["engine"] != "go" || client["detour"] != nil {
				t.Fatalf("cleared or direct policy changed: route=%#v client=%#v", route, client)
			}
		})
	}
	profile := defaultHTTPClientProfile()
	profile.Route.DefaultHttpClient = "missing"
	if _, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(profile, nil); err == nil || !strings.Contains(err.Error(), "route.default_http_client") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing default HTTP client reference error = %v", err)
	}
}

func TestProfileDefaultHTTPClientAllowsExplicitMixinClient(t *testing.T) {
	profile := defaultHTTPClientProfile()
	profile.Mixin = &profilev1.Mixin{Priority: profilev1.MixinPriority_MIXIN_PRIORITY_MIXIN,
		Config: `{"http_clients":[{"tag":"custom","engine":"go"}],"route":{"default_http_client":"custom"}}`}
	config, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(profile, &kernelv1.GenerateConfigOptions{EnableMixinProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	if config["route"].(map[string]any)["default_http_client"] != "custom" {
		t.Fatalf("mixin default HTTP client lost: %#v", config)
	}
}
