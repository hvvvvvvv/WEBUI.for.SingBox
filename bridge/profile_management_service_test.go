package bridge

import (
	"testing"

	configv1 "guiforcores/gen/profile/v1"

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
