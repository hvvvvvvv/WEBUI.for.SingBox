package config

import (
	"reflect"
	"testing"

	profilev1 "guiforcores/gen/profile/v1"
)

func TestGenerateDNSRuleQueryType(t *testing.T) {
	generator := &configGenerator{}
	dns, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{
		{
			Type:      profilev1.RuleType_RULE_TYPE_DOMAIN,
			Enable:    true,
			Payload:   "example.com",
			Action:    profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE,
			QueryType: []string{"A", "AAAA"},
		},
		{
			Type:    profilev1.RuleType_RULE_TYPE_DOMAIN_SUFFIX,
			Enable:  true,
			Payload: ".example.org",
			Action:  profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE,
		},
	}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("generate DNS: %v", err)
	}

	rules := dns["rules"].([]any)
	first := rules[0].(map[string]any)
	if got := first["query_type"]; !reflect.DeepEqual(got, []any{"A", "AAAA"}) {
		t.Fatalf("unexpected query_type: %#v", got)
	}
	if _, ok := rules[1].(map[string]any)["query_type"]; ok {
		t.Fatalf("empty query_type should be omitted: %#v", rules[1])
	}
}

func TestGenerateDNSStructuredActionFieldsOverrideInline(t *testing.T) {
	generator := &configGenerator{}
	dns, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{
		{
			Type:         profilev1.RuleType_RULE_TYPE_INLINE,
			Enable:       true,
			Payload:      `{"action":"reject","invert":false,"query_type":["HTTPS"],"server":"inline","disable_cache":false,"nested":{"winner":"inline"}}`,
			Invert:       true,
			Action:       profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE_OPTIONS,
			Server:       `{"server":"structured","nested":{"winner":"structured","preserved":true}}`,
			DisableCache: true,
			ClientSubnet: "198.51.100.0/24",
			QueryType:    []string{"A", "AAAA"},
		},
	}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("generate DNS: %v", err)
	}

	rule := dns["rules"].([]any)[0].(map[string]any)
	if got := rule["query_type"]; !reflect.DeepEqual(got, []any{"HTTPS"}) {
		t.Fatalf("inline query_type should win, got %#v", got)
	}
	if rule["action"] != "route-options" || rule["server"] != "structured" || rule["disable_cache"] != true {
		t.Fatalf("structured action fields should win, got %#v", rule)
	}
	if rule["client_subnet"] != "198.51.100.0/24" {
		t.Fatalf("non-conflicting structured field should remain, got %#v", rule)
	}
	nested := rule["nested"].(map[string]any)
	if nested["winner"] != "structured" || nested["preserved"] != true {
		t.Fatalf("structured action options should win without dropping other fields, got %#v", nested)
	}
	if rule["invert"] != false {
		t.Fatalf("inline invert should win, got %#v", rule)
	}
}

func TestGenerateDNSInlineRemovesFakeIPMarkerAfterOverride(t *testing.T) {
	generator := &configGenerator{}
	dns, err := generator.generateDNS(&profilev1.Dns{
		Servers: []*profilev1.DnsServer{
			{Type: profilev1.DnsServerType_DNS_SERVER_TYPE_FAKEIP, Tag: "fakeip"},
		},
		Rules: []*profilev1.DnsRule{
			{
				Type:    profilev1.RuleType_RULE_TYPE_INLINE,
				Enable:  true,
				Payload: `{"__is_fake_ip":true,"domain":["example.com"]}`,
				Action:  profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE,
			},
		},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("generate DNS: %v", err)
	}

	rule := dns["rules"].([]any)[0].(map[string]any)
	if _, ok := rule["__is_fake_ip"]; ok {
		t.Fatalf("internal Fake-IP marker should be omitted, got %#v", rule)
	}
	if got := rule["domain"]; !reflect.DeepEqual(got, []any{"example.com"}) {
		t.Fatalf("inline fields should be preserved, got %#v", rule)
	}
}

func TestGenerateRouteStructuredActionFieldsOverrideInline(t *testing.T) {
	generator := &configGenerator{}
	route, err := generator.generateRoute(&profilev1.Route{Rules: []*profilev1.RouteRule{
		{
			Type:     profilev1.RuleType_RULE_TYPE_INLINE,
			Enable:   true,
			Payload:  `{"action":"route","method":"inline","invert":false,"domain":["example.com"]}`,
			Invert:   true,
			Action:   profilev1.RuleAction_RULE_ACTION_REJECT,
			Outbound: "drop",
		},
	}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("generate route: %v", err)
	}

	rule := route["rules"].([]any)[0].(map[string]any)
	if rule["action"] != "reject" || rule["method"] != "drop" {
		t.Fatalf("structured action should win, got %#v", rule)
	}
	if rule["invert"] != false {
		t.Fatalf("inline invert should win, got %#v", rule)
	}
	if got := rule["domain"]; !reflect.DeepEqual(got, []any{"example.com"}) {
		t.Fatalf("inline match fields were not preserved, got %#v", rule)
	}
}

func TestGenerateInlineActionUsesPayloadAction(t *testing.T) {
	generator := &configGenerator{}

	t.Run("route", func(t *testing.T) {
		route, err := generator.generateRoute(&profilev1.Route{Rules: []*profilev1.RouteRule{
			{
				Type:    profilev1.RuleType_RULE_TYPE_INLINE,
				Enable:  true,
				Payload: `{"action":"reject","method":"drop","invert":true}`,
				Invert:  false,
				Action:  profilev1.RuleAction_RULE_ACTION_INLINE,
			},
		}}, nil, nil, nil)
		if err != nil {
			t.Fatalf("generate route: %v", err)
		}
		rule := route["rules"].([]any)[0].(map[string]any)
		if rule["action"] != "reject" || rule["method"] != "drop" {
			t.Fatalf("route inline action should come from payload, got %#v", rule)
		}
		if rule["invert"] != true {
			t.Fatalf("inline invert should win, got %#v", rule)
		}
	})

	t.Run("dns", func(t *testing.T) {
		dns, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{
			{
				Type:    profilev1.RuleType_RULE_TYPE_INLINE,
				Enable:  true,
				Payload: `{"action":"reject","method":"drop","invert":false}`,
				Invert:  true,
				Action:  profilev1.DnsRuleAction_DNS_RULE_ACTION_INLINE,
			},
		}}, nil, nil, nil)
		if err != nil {
			t.Fatalf("generate DNS: %v", err)
		}
		rule := dns["rules"].([]any)[0].(map[string]any)
		if rule["action"] != "reject" || rule["method"] != "drop" {
			t.Fatalf("DNS inline action should come from payload, got %#v", rule)
		}
		if rule["invert"] != false {
			t.Fatalf("inline invert should win, got %#v", rule)
		}
	})
}

func TestGenerateDNSRejectsInvalidInlinePayload(t *testing.T) {
	generator := &configGenerator{}
	_, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{
		{
			Type:    profilev1.RuleType_RULE_TYPE_INLINE,
			Enable:  true,
			Payload: `{invalid`,
			Action:  profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE,
		},
	}}, nil, nil, nil)
	if err == nil {
		t.Fatal("invalid inline payload should return an error")
	}
}

func TestGenerateExperimentalUsesManagedCoreAPI(t *testing.T) {
	experimental := generateExperimental(&profilev1.Experimental{
		CacheFile: &profilev1.CacheFileExperimental{
			Enabled:     true,
			Path:        "cache.db",
			CacheId:     "cache-id",
			StoreFakeip: true,
			StoreRdrc:   true,
			RdrcTimeout: "7d",
		},
	}, nil)

	clashAPI, ok := experimental["clash_api"].(map[string]any)
	if !ok {
		t.Fatalf("expected generated clash_api object, got %#v", experimental["clash_api"])
	}
	if controller := clashAPI["external_controller"]; controller != CoreAPIController {
		t.Fatalf("expected managed controller %q, got %#v", CoreAPIController, controller)
	}
	secret, ok := clashAPI["secret"].(string)
	if !ok || len(secret) != 64 {
		t.Fatalf("expected generated 64-char secret, got %#v", clashAPI["secret"])
	}

	cacheFile, ok := experimental["cache_file"].(map[string]any)
	if !ok {
		t.Fatalf("expected generated cache_file object, got %#v", experimental["cache_file"])
	}
	if cacheFile["path"] != "cache.db" || cacheFile["cache_id"] != "cache-id" {
		t.Fatalf("expected profile cache_file values to be preserved, got %#v", cacheFile)
	}
}

func TestGenerateInboundsDirectNetwork(t *testing.T) {
	tests := []struct {
		name    string
		network profilev1.InboundNetwork
		want    string
	}{
		{
			name:    "tcp",
			network: profilev1.InboundNetwork_INBOUND_NETWORK_TCP,
			want:    "tcp",
		},
		{
			name:    "udp",
			network: profilev1.InboundNetwork_INBOUND_NETWORK_UDP,
			want:    "udp",
		},
		{
			name:    "unspecified defaults to udp",
			network: profilev1.InboundNetwork_INBOUND_NETWORK_UNSPECIFIED,
			want:    "udp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbounds := generateInbounds([]*profilev1.Inbound{
				{
					Id:     "direct-in",
					Type:   profilev1.InboundType_INBOUND_TYPE_DIRECT,
					Tag:    "direct-in",
					Enable: true,
					Direct: &profilev1.DirectInboundConfig{
						Listen: &profilev1.InboundListen{
							Listen:       "127.0.0.1",
							ListenPort:   20123,
							TcpFastOpen:  true,
							TcpMultiPath: true,
							UdpFragment:  true,
						},
						Network: tt.network,
					},
				},
			})

			if len(inbounds) != 1 {
				t.Fatalf("expected one inbound, got %#v", inbounds)
			}
			item, ok := inbounds[0].(map[string]any)
			if !ok {
				t.Fatalf("expected inbound map, got %#v", inbounds[0])
			}
			if item["type"] != "direct" || item["tag"] != "direct-in" {
				t.Fatalf("expected direct inbound metadata, got %#v", item)
			}
			if item["network"] != tt.want {
				t.Fatalf("expected network %q, got %#v", tt.want, item["network"])
			}
			if _, ok := item["users"]; ok {
				t.Fatalf("direct inbound should not include users, got %#v", item)
			}
			if item["listen"] != "127.0.0.1" || item["listen_port"] != int32(20123) {
				t.Fatalf("expected listen fields to be preserved, got %#v", item)
			}
			if item["tcp_fast_open"] != true || item["tcp_multi_path"] != true || item["udp_fragment"] != true {
				t.Fatalf("expected listen options to be preserved, got %#v", item)
			}
		})
	}
}
