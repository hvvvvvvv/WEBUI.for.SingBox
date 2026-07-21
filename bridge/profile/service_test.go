package profile

import (
	"context"
	"os"
	"reflect"
	"testing"

	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
	"gopkg.in/yaml.v3"
)

func TestProfilesIgnoreUnknownFieldsOnReadAndWrite(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	legacy := `
- id: profile
  name: Profile
  dns:
    rules:
      - id: rule
        enable: true
        domain:
          - example.com
        action: 1
        legacy_field: ignored
        querytype:
          - A
          - AAAA
`
	if err := os.MkdirAll(paths.Resolve("data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Resolve(profilesFilePath), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	profiles, err := service.loadProfiles()
	if err != nil {
		t.Fatalf("load legacy profile: %v", err)
	}
	got := profiles[0].GetDns().GetRules()[0].GetQueryType()
	if len(got) != 2 || got[0] != "A" || got[1] != "AAAA" {
		t.Fatalf("unexpected query types: %#v", got)
	}
	if err := service.saveProfiles(profiles); err != nil {
		t.Fatalf("save profiles: %v", err)
	}
	written, err := os.ReadFile(paths.Resolve(profilesFilePath))
	if err != nil {
		t.Fatal(err)
	}
	var persisted []map[string]any
	if err := yaml.Unmarshal(written, &persisted); err != nil {
		t.Fatalf("decode written profiles: %v", err)
	}
	dns := persisted[0]["dns"].(map[string]any)
	rules := dns["rules"].([]any)
	if _, ok := rules[0].(map[string]any)["legacy_field"]; ok {
		t.Fatalf("unknown DNS rule field was written back:\n%s", written)
	}
}

func TestLegacyMigrationDiscardsUnknownFields(t *testing.T) {
	raw := []byte(`
- id: profile
  name: Profile
  dns:
    strategy: default
    rules:
      - id: rule
        type: domain
        enable: true
        payload: example.com
        action: route
        strategy: prefer_ipv4
        future_field: ignored
`)
	profiles, err := migrateLegacyProfilesYAML(raw)
	if err != nil {
		t.Fatalf("migrate legacy profile: %v", err)
	}
	if len(profiles) != 1 || len(profiles[0].GetDns().GetRules()) != 1 {
		t.Fatalf("valid profile data was not retained: %#v", profiles)
	}
}

func TestNormalizeProfileRawForYAMLKeepsMarshaledProfilesParseable(t *testing.T) {
	profiles := []*profilev1.Profile{
		{
			Id: "profile",
			Dns: &profilev1.Dns{
				Rules: []*profilev1.DnsRule{
					{
						Id:     "rule",
						Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_INLINE,
						Raw:    " {\n  \"query_type\": [\n    \"A\",\n    \"AAAA\"\n  ]\n}",
					},
				},
			},
		},
	}

	profilesForYAML := cloneProfiles(profiles)
	normalizeProfilePayloadsForYAML(profilesForYAML)

	payload, err := yaml.Marshal(profilesForYAML)
	if err != nil {
		t.Fatalf("marshal profiles: %v", err)
	}

	var decoded []*profilev1.Profile
	if err := yaml.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal marshaled profiles: %v\n%s", err, payload)
	}

	got := decoded[0].GetDns().GetRules()[0].GetRaw()
	if got[0] != '{' {
		t.Fatalf("raw should start with JSON object, got %q", got)
	}
}

func TestStructuredDNSRuleYAMLRoundTrip(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	ttl := uint32(0)
	profiles := []*profilev1.Profile{{
		Id: "profile",
		Dns: &profilev1.Dns{Rules: []*profilev1.DnsRule{{
			Id:            "rule",
			Enable:        true,
			Action:        profilev1.DnsRuleAction_DNS_RULE_ACTION_EVALUATE,
			Inbound:       []string{"inbound-id"},
			QueryType:     []string{"A", "65"},
			PreferredBy:   []string{"local", "resolved"},
			MatchResponse: true,
			ResponseAnswer: []string{
				"example.com. 60 IN A 192.0.2.1",
			},
			ActionOptions: &profilev1.DnsActionOptions{
				Server:     "dns-id",
				RewriteTtl: &ttl,
			},
			Raw: "{\n  \"custom\": true\n}",
		}}},
	}}

	if err := service.saveProfiles(profiles); err != nil {
		t.Fatalf("save profiles: %v", err)
	}
	loaded, err := service.loadProfiles()
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	rule := loaded[0].GetDns().GetRules()[0]
	if rule.GetAction() != profilev1.DnsRuleAction_DNS_RULE_ACTION_EVALUATE {
		t.Fatalf("action = %v", rule.GetAction())
	}
	if len(rule.GetInbound()) != 1 || rule.GetInbound()[0] != "inbound-id" {
		t.Fatalf("inbound = %#v", rule.GetInbound())
	}
	if !reflect.DeepEqual(rule.GetQueryType(), []string{"A", "65"}) {
		t.Fatalf("query_type = %#v", rule.GetQueryType())
	}
	if rule.GetActionOptions().RewriteTtl == nil || rule.GetActionOptions().GetRewriteTtl() != 0 {
		t.Fatalf("rewrite_ttl presence was lost: %#v", rule.GetActionOptions())
	}
	if rule.GetRaw() == "" || rule.GetRaw()[0] != '{' {
		t.Fatalf("raw = %q", rule.GetRaw())
	}
}

func TestSaveProfilesPersistsRequestedOrder(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())

	service := NewService(paths, nil)
	_, err := service.SaveProfiles(context.Background(), connect.NewRequest(&profilev1.SaveProfilesRequest{
		Profiles: []*profilev1.Profile{
			{Id: "second", Name: "Second"},
			{Id: "first", Name: "First"},
		},
	}))
	if err != nil {
		t.Fatalf("SaveProfiles returned error: %v", err)
	}

	profiles, err := service.loadProfiles()
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].GetId() != "second" || profiles[1].GetId() != "first" {
		t.Fatalf("profiles order was not persisted: %q, %q", profiles[0].GetId(), profiles[1].GetId())
	}
}

func TestSaveProfilesPersistsTunAutoRedirect(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	profiles := []*profilev1.Profile{
		{
			Id: "profile",
			Inbounds: []*profilev1.Inbound{
				{
					Type:   profilev1.InboundType_INBOUND_TYPE_TUN,
					Enable: true,
					Tun:    &profilev1.TunInboundConfig{AutoRoute: true, AutoRedirect: true},
				},
			},
		},
	}

	if err := service.saveProfiles(profiles); err != nil {
		t.Fatalf("save profiles: %v", err)
	}
	loaded, err := service.loadProfiles()
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	if !loaded[0].GetInbounds()[0].GetTun().GetAutoRedirect() {
		t.Fatal("TUN auto_redirect was not persisted")
	}
}

func TestSaveProfilesPersistsBridgeOutbound(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	profiles := []*profilev1.Profile{
		{
			Id: "profile",
			Outbounds: []*profilev1.Outbound{
				{
					Type:       profilev1.OutboundType_OUTBOUND_TYPE_BRIDGE,
					Tag:        "bridge-out",
					Interface:  "eth0",
					BridgeName: "custom-bridge",
				},
			},
		},
	}

	if err := service.saveProfiles(profiles); err != nil {
		t.Fatalf("save profiles: %v", err)
	}
	loaded, err := service.loadProfiles()
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	bridge := loaded[0].GetOutbounds()[0]
	if bridge.GetType() != profilev1.OutboundType_OUTBOUND_TYPE_BRIDGE ||
		bridge.GetInterface() != "eth0" || bridge.GetBridgeName() != "custom-bridge" {
		t.Fatalf("Bridge outbound was not persisted: %#v", bridge)
	}
}

func TestSaveProfilesPersistsRoutePreferredBy(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	want := []string{"tailscale", "wireguard", "bridge"}
	profiles := []*profilev1.Profile{{
		Id: "profile",
		Route: &profilev1.Route{Rules: []*profilev1.RouteRule{{
			Id: "rule", Enable: true, PreferredBy: want,
		}}},
	}}

	if err := service.saveProfiles(profiles); err != nil {
		t.Fatalf("save profiles: %v", err)
	}
	loaded, err := service.loadProfiles()
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	got := loaded[0].GetRoute().GetRules()[0].GetPreferredBy()
	if len(got) != len(want) {
		t.Fatalf("preferred_by = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("preferred_by = %#v, want %#v", got, want)
		}
	}
}

func TestSaveProfilesRejectsDuplicateIDs(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())

	service := NewService(paths, nil)
	_, err := service.SaveProfiles(context.Background(), connect.NewRequest(&profilev1.SaveProfilesRequest{
		Profiles: []*profilev1.Profile{
			{Id: "duplicate", Name: "First"},
			{Id: "duplicate", Name: "Second"},
		},
	}))
	if err == nil {
		t.Fatal("expected duplicate id error")
	}

	profiles, loadErr := service.loadProfiles()
	if loadErr != nil {
		t.Fatalf("load profiles: %v", loadErr)
	}
	if len(profiles) != 0 {
		t.Fatalf("failed SaveProfiles should not persist data, got %d profiles", len(profiles))
	}
}
