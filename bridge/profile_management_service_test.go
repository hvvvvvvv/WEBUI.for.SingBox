package bridge

import (
	"context"
	"testing"

	configv1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
	"gopkg.in/yaml.v3"
)

func TestNormalizeProfilePayloadsForYAMLKeepsMarshaledProfilesParseable(t *testing.T) {
	profiles := []*configv1.Profile{
		{
			Id: "profile",
			Dns: &configv1.Dns{
				Rules: []*configv1.DnsRule{
					{
						Id:      "rule",
						Type:    configv1.RuleType_RULE_TYPE_INLINE,
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

	var decoded []*configv1.Profile
	if err := yaml.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal marshaled profiles: %v\n%s", err, payload)
	}

	got := decoded[0].GetDns().GetRules()[0].GetPayload()
	if got[0] != '{' {
		t.Fatalf("payload should start with JSON object, got %q", got)
	}
}

func TestSaveProfilesPersistsRequestedOrder(t *testing.T) {
	withTempBasePath(t)

	service := &profileManagementService{}
	_, err := service.SaveProfiles(context.Background(), connect.NewRequest(&configv1.SaveProfilesRequest{
		Profiles: []*configv1.Profile{
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

func TestSaveProfilesRejectsDuplicateIDs(t *testing.T) {
	withTempBasePath(t)

	service := &profileManagementService{}
	_, err := service.SaveProfiles(context.Background(), connect.NewRequest(&configv1.SaveProfilesRequest{
		Profiles: []*configv1.Profile{
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
