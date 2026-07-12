package profile

import (
	"context"
	"os"
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
        type: 4
        enable: true
        payload: example.com
        action: 1
        strategy: 2
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
	if _, ok := rules[0].(map[string]any)["strategy"]; ok {
		t.Fatalf("legacy DNS rule strategy was written back:\n%s", written)
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

func TestNormalizeProfilePayloadsForYAMLKeepsMarshaledProfilesParseable(t *testing.T) {
	profiles := []*profilev1.Profile{
		{
			Id: "profile",
			Dns: &profilev1.Dns{
				Rules: []*profilev1.DnsRule{
					{
						Id:      "rule",
						Type:    profilev1.RuleType_RULE_TYPE_INLINE,
						Payload: " {\n  \"query_type\": [\n    \"A\",\n    \"AAAA\"\n  ]\n}",
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

	got := decoded[0].GetDns().GetRules()[0].GetPayload()
	if got[0] != '{' {
		t.Fatalf("payload should start with JSON object, got %q", got)
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
