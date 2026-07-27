package profile

import (
	"context"
	"os"
	"reflect"
	"testing"

	"guiforcores/bridge/storage"
	commonv1 "guiforcores/gen/common/v1"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
	"gopkg.in/yaml.v3"
)

func profileExpected(state *commonv1.ResourceState, id string) *commonv1.ExpectedRevision {
	return &commonv1.ExpectedRevision{
		InstanceId: state.GetInstanceId(),
		Revision:   state.GetItemRevisions()[id],
	}
}

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
			Id:                "rule",
			Enable:            true,
			Action:            profilev1.DnsRuleAction_DNS_RULE_ACTION_EVALUATE,
			Inbound:           []string{"inbound-id"},
			QueryType:         []string{"A", "65"},
			PreferredBy:       []string{"local", "resolved"},
			SourceIpCidr:      []string{"10.0.0.0/8"},
			SourceIpIsPrivate: true,
			SourcePort:        []uint32{0, 53},
			SourcePortRange:   []string{"1000:2000"},
			MatchResponse:     true,
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
	if !reflect.DeepEqual(rule.GetSourceIpCidr(), []string{"10.0.0.0/8"}) {
		t.Fatalf("source_ip_cidr = %#v", rule.GetSourceIpCidr())
	}
	if !rule.GetSourceIpIsPrivate() {
		t.Fatal("source_ip_is_private was lost")
	}
	if !reflect.DeepEqual(rule.GetSourcePort(), []uint32{0, 53}) {
		t.Fatalf("source_port = %#v", rule.GetSourcePort())
	}
	if !reflect.DeepEqual(rule.GetSourcePortRange(), []string{"1000:2000"}) {
		t.Fatalf("source_port_range = %#v", rule.GetSourcePortRange())
	}
	if rule.GetActionOptions().RewriteTtl == nil || rule.GetActionOptions().GetRewriteTtl() != 0 {
		t.Fatalf("rewrite_ttl presence was lost: %#v", rule.GetActionOptions())
	}
	if rule.GetRaw() == "" || rule.GetRaw()[0] != '{' {
		t.Fatalf("raw = %q", rule.GetRaw())
	}
}

type recordingProfileChanges struct {
	changes [][]string
}

func (r *recordingProfileChanges) ProfilesChanged(ids []string) {
	r.changes = append(r.changes, append([]string(nil), ids...))
}

func TestProfilePersistenceKeepsTunAutoRedirect(t *testing.T) {
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

func TestProfilePersistenceKeepsBridgeOutbound(t *testing.T) {
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

func TestProfilePersistenceKeepsRoutePreferredBy(t *testing.T) {
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

func TestCreateProfileRejectsDuplicateIDs(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	if _, err := service.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{
		Profile: &profilev1.Profile{Id: "duplicate", Name: "First"},
	})); err != nil {
		t.Fatal(err)
	}
	_, err := service.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{
		Profile: &profilev1.Profile{Id: "duplicate", Name: "Second"},
	}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("duplicate create code = %v", connect.CodeOf(err))
	}

	profiles, loadErr := service.loadProfiles()
	if loadErr != nil {
		t.Fatalf("load profiles: %v", loadErr)
	}
	if len(profiles) != 1 || profiles[0].GetName() != "First" {
		t.Fatalf("duplicate create changed stored data: %#v", profiles)
	}
}

func TestProfileEntityConflictsDoNotOverwriteNewerData(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	if err := service.saveProfiles([]*profilev1.Profile{{Id: "first", Name: "First"}, {Id: "second", Name: "Second"}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.ListProfiles(context.Background(), connect.NewRequest(&profilev1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	firstRevision := profileExpected(snapshot.Msg.GetState(), "first")
	secondRevision := profileExpected(snapshot.Msg.GetState(), "second")

	firstUpdate, err := service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "first", Name: "First from client A"},
		ExpectedRevision: firstRevision,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if firstUpdate.Msg.GetState().GetItemRevision() <= firstRevision.GetRevision() {
		t.Fatalf("item revision did not advance: %#v", firstUpdate.Msg.GetState())
	}

	_, err = service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "first", Name: "stale overwrite"},
		ExpectedRevision: firstRevision,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale update code = %v", connect.CodeOf(err))
	}
	stored, err := service.GetProfile(context.Background(), connect.NewRequest(&profilev1.GetProfileRequest{Id: "first"}))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Msg.GetProfile().GetName() != "First from client A" {
		t.Fatalf("stale update overwrote profile: %#v", stored.Msg.GetProfile())
	}

	if _, err := service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "second", Name: "Second from client B"},
		ExpectedRevision: secondRevision,
	})); err != nil {
		t.Fatalf("editing a different entity should succeed: %v", err)
	}
}

func TestProfileOrderingConflictsOnlyWithOrderingChanges(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	if err := service.saveProfiles([]*profilev1.Profile{{Id: "first", Name: "First"}, {Id: "second", Name: "Second"}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.ListProfiles(context.Background(), connect.NewRequest(&profilev1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	orderRevision := &commonv1.ExpectedRevision{
		InstanceId: snapshot.Msg.GetState().GetInstanceId(),
		Revision:   snapshot.Msg.GetState().GetOrderRevision(),
	}

	if _, err := service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "first", Name: "Edited"},
		ExpectedRevision: profileExpected(snapshot.Msg.GetState(), "first"),
	})); err != nil {
		t.Fatal(err)
	}
	reordered, err := service.ReorderProfiles(context.Background(), connect.NewRequest(&profilev1.ReorderProfilesRequest{
		Ids:                   []string{"second", "first"},
		ExpectedOrderRevision: orderRevision,
	}))
	if err != nil {
		t.Fatalf("content edit should not conflict with ordering: %v", err)
	}

	_, err = service.ReorderProfiles(context.Background(), connect.NewRequest(&profilev1.ReorderProfilesRequest{
		Ids:                   []string{"first", "second"},
		ExpectedOrderRevision: orderRevision,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale order code = %v; state = %#v", connect.CodeOf(err), reordered.Msg.GetState())
	}

	latest, err := service.ListProfiles(context.Background(), connect.NewRequest(&profilev1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	beforeCreate := &commonv1.ExpectedRevision{
		InstanceId: latest.Msg.GetState().GetInstanceId(), Revision: latest.Msg.GetState().GetOrderRevision(),
	}
	if _, err := service.CreateProfile(context.Background(), connect.NewRequest(&profilev1.CreateProfileRequest{
		Profile: &profilev1.Profile{Id: "third", Name: "Third"},
	})); err != nil {
		t.Fatal(err)
	}
	_, err = service.ReorderProfiles(context.Background(), connect.NewRequest(&profilev1.ReorderProfilesRequest{
		Ids: []string{"first", "second"}, ExpectedOrderRevision: beforeCreate,
	}))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("create/order conflict code = %v", connect.CodeOf(err))
	}
}

func TestProfileNoOpAndServerRestartVersionBehavior(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, nil)
	if err := service.saveProfiles([]*profilev1.Profile{{Id: "profile", Name: "Profile"}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.ListProfiles(context.Background(), connect.NewRequest(&profilev1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	handler := &recordingProfileChanges{}
	service.SetChangeHandler(handler)
	response, err := service.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "profile", Name: "Profile"},
		ExpectedRevision: profileExpected(snapshot.Msg.GetState(), "profile"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetState().GetStateRevision() != snapshot.Msg.GetState().GetStateRevision() {
		t.Fatalf("no-op update advanced state: before=%d after=%d", snapshot.Msg.GetState().GetStateRevision(), response.Msg.GetState().GetStateRevision())
	}
	if len(handler.changes) != 0 {
		t.Fatalf("no-op update notified kernel: %#v", handler.changes)
	}

	restarted := NewService(paths, nil)
	_, err = restarted.UpdateProfile(context.Background(), connect.NewRequest(&profilev1.UpdateProfileRequest{
		Profile:          &profilev1.Profile{Id: "profile", Name: "After restart"},
		ExpectedRevision: profileExpected(snapshot.Msg.GetState(), "profile"),
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("old instance revision code = %v", connect.CodeOf(err))
	}
}
