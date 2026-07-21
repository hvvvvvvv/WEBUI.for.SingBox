package config

import (
	"reflect"
	"strings"
	"testing"

	profilev1 "guiforcores/gen/profile/v1"
)

func TestGenerateDNSRuleQueryType(t *testing.T) {
	generator := &configGenerator{}
	dns, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{
		{
			Enable:    true,
			Domain:    []string{"example.com"},
			Action:    profilev1.DnsRuleAction_DNS_RULE_ACTION_REJECT,
			QueryType: []string{"A", "AAAA"},
		},
		{
			Enable:       true,
			DomainSuffix: []string{".example.org"},
			Action:       profilev1.DnsRuleAction_DNS_RULE_ACTION_REJECT,
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

func TestGenerateDNSRawOverridesStructuredFields(t *testing.T) {
	generator := &configGenerator{}
	dns, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{
		{
			Enable:    true,
			Raw:       `{"action":"reject","invert":false,"query_type":["HTTPS"],"disable_cache":false,"nested":{"winner":"inline"}}`,
			Invert:    true,
			Action:    profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE_OPTIONS,
			QueryType: []string{"A", "AAAA"},
			ActionOptions: &profilev1.DnsActionOptions{
				DisableCache: true,
				ClientSubnet: "198.51.100.0/24",
			},
		},
	}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("generate DNS: %v", err)
	}

	rule := dns["rules"].([]any)[0].(map[string]any)
	if got := rule["query_type"]; !reflect.DeepEqual(got, []any{"HTTPS"}) {
		t.Fatalf("inline query_type should win, got %#v", got)
	}
	if rule["action"] != "reject" || rule["disable_cache"] != false {
		t.Fatalf("raw action fields should win, got %#v", rule)
	}
	if rule["client_subnet"] != "198.51.100.0/24" {
		t.Fatalf("non-conflicting structured field should remain, got %#v", rule)
	}
	nested := rule["nested"].(map[string]any)
	if nested["winner"] != "inline" {
		t.Fatalf("raw nested fields should be preserved, got %#v", nested)
	}
	if rule["invert"] != false {
		t.Fatalf("inline invert should win, got %#v", rule)
	}
}

func TestGenerateDNSInlineRemovesFakeIPMarkerAfterOverride(t *testing.T) {
	generator := &configGenerator{}
	dns, err := generator.generateDNS(&profilev1.Dns{
		Servers: []*profilev1.DnsServer{
			{Id: "fake-id", Type: profilev1.DnsServerType_DNS_SERVER_TYPE_FAKEIP, Tag: "fakeip"},
		},
		Rules: []*profilev1.DnsRule{
			{
				Enable:        true,
				Raw:           `{"__is_fake_ip":true,"domain":["example.com"]}`,
				Action:        profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE,
				ActionOptions: &profilev1.DnsActionOptions{Server: "fake-id"},
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

func TestGenerateDNSSkipsFakeIPRuleBeforeResolvingMissingServer(t *testing.T) {
	generator := &configGenerator{}
	dns, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{{
		Enable:        true,
		Action:        profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE,
		ActionOptions: &profilev1.DnsActionOptions{Server: "missing-fake-server"},
		Raw:           `{"__is_fake_ip":true,"domain":["example.com"]}`,
	}}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Fake-IP rule should be skipped before reference validation: %v", err)
	}
	if got := len(dns["rules"].([]any)); got != 0 {
		t.Fatalf("generated %d Fake-IP rules without a Fake-IP server", got)
	}
}

func TestGenerateDNSStructuredMatches(t *testing.T) {
	generator := &configGenerator{}
	dns, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{{
		Enable:                   true,
		Action:                   profilev1.DnsRuleAction_DNS_RULE_ACTION_RESPOND,
		Invert:                   true,
		Inbound:                  []string{"in-1"},
		ClashMode:                "direct",
		IpVersion:                4,
		QueryType:                []string{"A", "65"},
		Network:                  []string{"udp"},
		Protocol:                 []string{"dns"},
		PreferredBy:              []string{"local", "resolved"},
		Domain:                   []string{"example.com"},
		DomainSuffix:             []string{".example.org"},
		DomainKeyword:            []string{"keyword"},
		DomainRegex:              []string{"^regex"},
		RuleSet:                  []string{"rs-1"},
		RuleSetIpCidrMatchSource: true,
		MatchResponse:            true,
		IpAcceptAny:              true,
		IpCidr:                   []string{"192.0.2.0/24"},
		IpIsPrivate:              true,
		ResponseRcode:            "NOERROR",
		ResponseAnswer:           []string{"example.com. 60 IN A 192.0.2.1"},
		ResponseNs:               []string{"example.com. 60 IN NS ns.example.com."},
		ResponseExtra:            []string{"ns.example.com. 60 IN A 192.0.2.2"},
		ProcessName:              []string{"browser"},
		ProcessPath:              []string{"/usr/bin/browser"},
		ProcessPathRegex:         []string{"/browser$"},
	}}},
		[]*profilev1.RuleSet{{Id: "rs-1", Tag: "ruleset-tag"}},
		[]*profilev1.Inbound{{Id: "in-1", Tag: "inbound-tag", Enable: true}},
		nil,
	)
	if err != nil {
		t.Fatalf("generate DNS: %v", err)
	}

	rule := dns["rules"].([]any)[0].(map[string]any)
	checks := map[string]any{
		"action":                        "respond",
		"invert":                        true,
		"inbound":                       []any{"inbound-tag"},
		"clash_mode":                    "direct",
		"ip_version":                    int32(4),
		"query_type":                    []any{"A", uint64(65)},
		"network":                       []any{"udp"},
		"protocol":                      []any{"dns"},
		"preferred_by":                  []any{"local", "resolved"},
		"domain":                        []any{"example.com"},
		"rule_set":                      []any{"ruleset-tag"},
		"rule_set_ip_cidr_match_source": true,
		"match_response":                true,
		"ip_accept_any":                 true,
		"ip_is_private":                 true,
		"response_rcode":                "NOERROR",
		"process_name":                  []any{"browser"},
	}
	for key, want := range checks {
		if got := rule[key]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %#v, want %#v; rule=%#v", key, got, want, rule)
		}
	}
}

func TestGenerateDNSActionOptionIsolation(t *testing.T) {
	generator := &configGenerator{}
	ttl := uint32(0)
	allOptions := func() *profilev1.DnsActionOptions {
		return &profilev1.DnsActionOptions{
			Server: "dns-1", DisableCache: true, DisableOptimisticCache: true,
			RewriteTtl: &ttl, Timeout: "2s", ClientSubnet: "198.51.100.0/24",
			Method: "default", NoDrop: true, Rcode: "NXDOMAIN",
			Answer: []string{"answer"}, Ns: []string{"ns"}, Extra: []string{"extra"},
		}
	}
	dns, err := generator.generateDNS(&profilev1.Dns{
		Servers: []*profilev1.DnsServer{{Id: "dns-1", Tag: "dns-tag", Type: profilev1.DnsServerType_DNS_SERVER_TYPE_LOCAL}},
		Rules: []*profilev1.DnsRule{
			{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE, ActionOptions: allOptions()},
			{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_EVALUATE, ActionOptions: allOptions()},
			{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE_OPTIONS, ActionOptions: allOptions()},
			{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_RESPOND, ActionOptions: allOptions()},
			{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_REJECT, ActionOptions: allOptions()},
			{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_PREDEFINED, ActionOptions: allOptions()},
		},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("generate DNS: %v", err)
	}

	rules := dns["rules"].([]any)
	for _, index := range []int{0, 1} {
		rule := rules[index].(map[string]any)
		if rule["server"] != "dns-tag" || rule["rewrite_ttl"] != uint32(0) || rule["disable_cache"] != true {
			t.Fatalf("query action fields missing: %#v", rule)
		}
		if _, exists := rule["method"]; exists {
			t.Fatalf("reject options leaked into query action: %#v", rule)
		}
	}
	routeOptions := rules[2].(map[string]any)
	if _, exists := routeOptions["server"]; exists {
		t.Fatalf("route-options must not output server: %#v", routeOptions)
	}
	respond := rules[3].(map[string]any)
	if len(respond) != 1 || respond["action"] != "respond" {
		t.Fatalf("respond options leaked: %#v", respond)
	}
	reject := rules[4].(map[string]any)
	if reject["method"] != "default" || reject["no_drop"] != true {
		t.Fatalf("reject options missing: %#v", reject)
	}
	if _, exists := reject["disable_cache"]; exists {
		t.Fatalf("query options leaked into reject: %#v", reject)
	}
	predefined := rules[5].(map[string]any)
	if predefined["rcode"] != "NXDOMAIN" || !reflect.DeepEqual(predefined["answer"], []any{"answer"}) {
		t.Fatalf("predefined options missing: %#v", predefined)
	}
	if _, exists := predefined["server"]; exists {
		t.Fatalf("server leaked into predefined: %#v", predefined)
	}
}

func TestGenerateDNSMatchValidation(t *testing.T) {
	generator := &configGenerator{}
	tests := []struct {
		name string
		rule *profilev1.DnsRule
		want string
	}{
		{name: "IP version", rule: &profilev1.DnsRule{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_RESPOND, IpVersion: 5}, want: "ip_version"},
		{name: "numeric query type", rule: &profilev1.DnsRule{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_RESPOND, QueryType: []string{"65536"}}, want: "query_type[0]"},
		{name: "response dependency", rule: &profilev1.DnsRule{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_RESPOND, IpCidr: []string{"192.0.2.0/24"}}, want: "match_response"},
		{name: "inline raw required", rule: &profilev1.DnsRule{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_INLINE}, want: ".raw is required"},
		{name: "inline action required", rule: &profilev1.DnsRule{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_INLINE, Raw: `{}`}, want: ".raw.action is required"},
		{name: "reject no drop conflict", rule: &profilev1.DnsRule{Enable: true, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_REJECT, ActionOptions: &profilev1.DnsActionOptions{Method: "drop", NoDrop: true}}, want: "no_drop"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{test.rule}}, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want path containing %q", err, test.want)
			}
		})
	}
}

func TestGenerateDNSPredefinedDefaultsRcode(t *testing.T) {
	generator := &configGenerator{}
	dns, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{{
		Enable: true,
		Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_PREDEFINED,
	}}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("generate DNS: %v", err)
	}
	rule := dns["rules"].([]any)[0].(map[string]any)
	if rule["rcode"] != "NOERROR" {
		t.Fatalf("default predefined rcode = %#v", rule["rcode"])
	}
}

func TestGenerateRouteStructuredMatchesAndRawOverride(t *testing.T) {
	generator := &configGenerator{}
	inbounds := []*profilev1.Inbound{{Id: "in-1", Tag: "mixed-in", Enable: true}}
	outbounds := []*profilev1.Outbound{{Id: "out-1", Tag: "proxy"}}
	ruleSets := []*profilev1.RuleSet{{
		Id: "rs-1", Tag: "geoip-cn", Type: profilev1.RulesetType_RULESET_TYPE_INLINE, Rules: "[]",
	}}
	route, err := generator.generateRoute(&profilev1.Route{
		RuleSet: ruleSets,
		Rules: []*profilev1.RouteRule{{
			Enable:            true,
			Invert:            true,
			Action:            profilev1.RuleAction_RULE_ACTION_ROUTE,
			Inbound:           []string{"in-1"},
			IpVersion:         6,
			Network:           []string{"tcp", "udp"},
			PreferredBy:       []string{"tailscale", "wireguard"},
			Protocol:          []string{"tls"},
			Domain:            []string{"structured.example"},
			DomainSuffix:      []string{".example.org"},
			DomainKeyword:     []string{"keyword"},
			DomainRegex:       []string{"^regex"},
			IpCidr:            []string{"192.0.2.0/24"},
			SourceIpCidr:      []string{"10.0.0.0/8"},
			SourceIpIsPrivate: true,
			IpIsPrivate:       true,
			SourcePort:        []uint32{0, 53},
			SourcePortRange:   []string{"1000:2000"},
			Port:              []uint32{80, 443},
			PortRange:         []string{"8000:9000"},
			ProcessName:       []string{"curl"},
			ProcessPath:       []string{"/usr/bin/curl"},
			ProcessPathRegex:  []string{"^/usr/bin/"},
			ClashMode:         "direct",
			RuleSet:           []string{"rs-1"},
			ActionOptions:     &profilev1.ActionOptions{Outbound: "out-1"},
			Raw:               `{"domain":["raw.example"],"preferred_by":["bridge"],"custom":{"nested":true}}`,
		}},
	}, inbounds, outbounds, nil)
	if err != nil {
		t.Fatalf("generate route: %v", err)
	}

	rule := route["rules"].([]any)[0].(map[string]any)
	for key, want := range map[string]any{
		"ip_version":           int32(6),
		"source_ip_is_private": true,
		"ip_is_private":        true,
		"clash_mode":           "direct",
		"action":               "route",
		"outbound":             "proxy",
		"invert":               true,
	} {
		if got := rule[key]; got != want {
			t.Fatalf("%s = %#v, want %#v in %#v", key, got, want, rule)
		}
	}
	for key, want := range map[string][]any{
		"inbound":      {"mixed-in"},
		"domain":       {"raw.example"},
		"preferred_by": {"bridge"},
		"source_port":  {uint32(0), uint32(53)},
		"port":         {uint32(80), uint32(443)},
		"rule_set":     {"geoip-cn"},
	} {
		if got := rule[key]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
	if got := rule["custom"].(map[string]any)["nested"]; got != true {
		t.Fatalf("raw custom object was not merged: %#v", rule)
	}
}

func TestGenerateRoutePreferredByPresence(t *testing.T) {
	generator := &configGenerator{}
	tests := []struct {
		name       string
		values     []string
		wantField  bool
		wantValues []any
	}{
		{name: "empty"},
		{
			name:       "all documented values",
			values:     []string{"tailscale", "wireguard", "bridge"},
			wantField:  true,
			wantValues: []any{"tailscale", "wireguard", "bridge"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route, err := generator.generateRoute(&profilev1.Route{Rules: []*profilev1.RouteRule{{
				Enable: true, Action: profilev1.RuleAction_RULE_ACTION_REJECT,
				PreferredBy: test.values,
			}}}, nil, nil, nil)
			if err != nil {
				t.Fatalf("generate route: %v", err)
			}

			rule := route["rules"].([]any)[0].(map[string]any)
			got, ok := rule["preferred_by"]
			if ok != test.wantField || (ok && !reflect.DeepEqual(got, test.wantValues)) {
				t.Fatalf("preferred_by = %#v, presence %v, want %#v/%v", got, ok, test.wantValues, test.wantField)
			}
		})
	}
}

func TestGenerateRouteIPVersionPresence(t *testing.T) {
	generator := &configGenerator{}
	for _, test := range []struct {
		name      string
		version   int32
		wantField bool
	}{
		{name: "unrestricted", version: 0},
		{name: "IPv4", version: 4, wantField: true},
		{name: "IPv6", version: 6, wantField: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			route, err := generator.generateRoute(&profilev1.Route{Rules: []*profilev1.RouteRule{{
				Enable: true, Action: profilev1.RuleAction_RULE_ACTION_REJECT, IpVersion: test.version,
			}}}, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			rule := route["rules"].([]any)[0].(map[string]any)
			got, ok := rule["ip_version"]
			if ok != test.wantField || (ok && got != test.version) {
				t.Fatalf("ip_version = %#v, presence %v, want %d/%v", got, ok, test.version, test.wantField)
			}
		})
	}
}

func TestGenerateRouteActionsAndOptionIsolation(t *testing.T) {
	generator := &configGenerator{}
	outbounds := []*profilev1.Outbound{{Id: "out-1", Tag: "proxy"}}
	dns := &profilev1.Dns{Servers: []*profilev1.DnsServer{{Id: "dns-1", Tag: "resolver"}}}
	ttl := uint32(0)

	tests := []struct {
		name      string
		action    profilev1.RuleAction
		options   *profilev1.ActionOptions
		want      map[string]any
		forbidden []string
	}{
		{
			name: "route", action: profilev1.RuleAction_RULE_ACTION_ROUTE,
			options:   &profilev1.ActionOptions{Outbound: "out-1", OverrideAddress: "example.com"},
			want:      map[string]any{"action": "route", "outbound": "proxy", "override_address": "example.com"},
			forbidden: []string{"method", "sniffer", "server"},
		},
		{
			name: "bypass", action: profilev1.RuleAction_RULE_ACTION_BYPASS,
			options:   &profilev1.ActionOptions{OverrideAddress: "example.com", TlsRecordFragment: true},
			want:      map[string]any{"action": "bypass", "override_address": "example.com", "tls_record_fragment": true},
			forbidden: []string{"outbound", "method", "server"},
		},
		{
			name: "route-options", action: profilev1.RuleAction_RULE_ACTION_ROUTE_OPTIONS,
			options: &profilev1.ActionOptions{
				Outbound: "out-1", OverrideAddress: "example.net", OverridePort: 443,
				NetworkStrategy: "fallback", NetworkType: []string{"wifi"},
				FallbackNetworkType: []string{"cellular"}, FallbackDelay: "300ms",
				UdpDisableDomainUnmapping: true, UdpConnect: true, UdpTimeout: "30s",
				TlsFragment: true, TlsFragmentFallbackDelay: "500ms",
				TlsSpoof: "allowed.example", TlsSpoofMethod: "wrong-sequence",
			},
			want: map[string]any{
				"action": "route-options", "override_address": "example.net", "override_port": uint32(443),
				"network_strategy": "fallback", "network_type": []any{"wifi"},
				"fallback_network_type": []any{"cellular"}, "fallback_delay": "300ms",
				"udp_disable_domain_unmapping": true, "udp_connect": true, "udp_timeout": "30s",
				"tls_fragment": true, "tls_fragment_fallback_delay": "500ms",
				"tls_spoof": "allowed.example", "tls_spoof_method": "wrong-sequence",
			},
			forbidden: []string{"outbound", "method", "server"},
		},
		{
			name: "reject", action: profilev1.RuleAction_RULE_ACTION_REJECT,
			options:   &profilev1.ActionOptions{Outbound: "out-1", Method: "reply", NoDrop: true, OverrideAddress: "leak"},
			want:      map[string]any{"action": "reject", "method": "reply", "no_drop": true},
			forbidden: []string{"outbound", "override_address", "sniffer", "server"},
		},
		{
			name: "hijack-dns", action: profilev1.RuleAction_RULE_ACTION_HIJACK_DNS,
			options:   &profilev1.ActionOptions{Outbound: "out-1", Method: "reply"},
			want:      map[string]any{"action": "hijack-dns"},
			forbidden: []string{"outbound", "method", "server"},
		},
		{
			name: "sniff", action: profilev1.RuleAction_RULE_ACTION_SNIFF,
			options:   &profilev1.ActionOptions{Outbound: "out-1", Sniffer: []string{"tls", "http"}, Timeout: "500ms"},
			want:      map[string]any{"action": "sniff", "sniffer": []any{"tls", "http"}, "timeout": "500ms"},
			forbidden: []string{"outbound", "method", "server", "strategy"},
		},
		{
			name: "resolve", action: profilev1.RuleAction_RULE_ACTION_RESOLVE,
			options: &profilev1.ActionOptions{
				Outbound: "out-1", Server: "dns-1", Strategy: "ipv4_only",
				DisableCache: true, DisableOptimisticCache: true, RewriteTtl: &ttl,
				Timeout: "2s", ClientSubnet: "198.51.100.0/24",
			},
			want: map[string]any{
				"action": "resolve", "server": "resolver", "strategy": "ipv4_only",
				"disable_cache": true, "disable_optimistic_cache": true,
				"rewrite_ttl": uint32(0), "timeout": "2s", "client_subnet": "198.51.100.0/24",
			},
			forbidden: []string{"outbound", "method", "sniffer", "override_address"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route, err := generator.generateRoute(&profilev1.Route{Rules: []*profilev1.RouteRule{{
				Enable: true, Action: test.action, ActionOptions: test.options,
			}}}, nil, outbounds, dns)
			if err != nil {
				t.Fatal(err)
			}
			rule := route["rules"].([]any)[0].(map[string]any)
			for key, want := range test.want {
				if got := rule[key]; !reflect.DeepEqual(got, want) {
					t.Fatalf("%s = %#v, want %#v in %#v", key, got, want, rule)
				}
			}
			for _, key := range test.forbidden {
				if _, ok := rule[key]; ok {
					t.Fatalf("option %q leaked into %s action: %#v", key, test.name, rule)
				}
			}
		})
	}
}

func TestGenerateRouteActionValidation(t *testing.T) {
	generator := &configGenerator{}
	tests := []struct {
		name    string
		rule    *profilev1.RouteRule
		message string
	}{
		{
			name:    "route outbound required",
			rule:    &profilev1.RouteRule{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_ROUTE},
			message: "outbound is required",
		},
		{
			name:    "TLS fragmentation conflict",
			rule:    &profilev1.RouteRule{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_ROUTE_OPTIONS, ActionOptions: &profilev1.ActionOptions{TlsFragment: true, TlsRecordFragment: true}},
			message: "mutually exclusive",
		},
		{
			name:    "drop no_drop conflict",
			rule:    &profilev1.RouteRule{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_REJECT, ActionOptions: &profilev1.ActionOptions{Method: "drop", NoDrop: true}},
			message: "no_drop",
		},
		{
			name:    "inline raw required",
			rule:    &profilev1.RouteRule{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_INLINE},
			message: ".raw is required",
		},
		{
			name:    "inline raw action required",
			rule:    &profilev1.RouteRule{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_INLINE, Raw: `{}`},
			message: ".raw.action is required",
		},
		{
			name:    "invalid raw object",
			rule:    &profilev1.RouteRule{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_REJECT, Raw: `[]`},
			message: "json value must be an object",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := generator.generateRoute(&profilev1.Route{Rules: []*profilev1.RouteRule{test.rule}}, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want message containing %q", err, test.message)
			}
		})
	}
}

func TestGenerateRouteReferenceErrors(t *testing.T) {
	generator := &configGenerator{}
	enabledInbound := []*profilev1.Inbound{{Id: "in-1", Tag: "in", Enable: true}}
	disabledInbound := []*profilev1.Inbound{{Id: "in-1", Tag: "in", Enable: false}}
	outbounds := []*profilev1.Outbound{{Id: "out-1", Tag: "out"}}
	dns := &profilev1.Dns{Servers: []*profilev1.DnsServer{{Id: "dns-1", Tag: "dns"}}}
	ruleSets := []*profilev1.RuleSet{{Id: "rs-1", Tag: "rs", Type: profilev1.RulesetType_RULESET_TYPE_INLINE, Rules: "[]"}}

	tests := []struct {
		name      string
		route     *profilev1.Route
		inbounds  []*profilev1.Inbound
		outbounds []*profilev1.Outbound
		dns       *profilev1.Dns
		want      []string
	}{
		{
			name:  "missing rule inbound",
			route: &profilev1.Route{Rules: []*profilev1.RouteRule{{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_REJECT, Inbound: []string{"missing"}}}},
			want:  []string{"route.rules[0].inbound[0]", "missing"},
		},
		{
			name:     "disabled rule inbound",
			route:    &profilev1.Route{Rules: []*profilev1.RouteRule{{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_REJECT, Inbound: []string{"in-1"}}}},
			inbounds: disabledInbound,
			want:     []string{"route.rules[0].inbound[0]", "disabled", "in-1"},
		},
		{
			name:     "missing rule set",
			route:    &profilev1.Route{RuleSet: ruleSets, Rules: []*profilev1.RouteRule{{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_REJECT, RuleSet: []string{"missing"}}}},
			inbounds: enabledInbound,
			want:     []string{"route.rules[0].rule_set[0]", "missing"},
		},
		{
			name:      "missing action outbound",
			route:     &profilev1.Route{Rules: []*profilev1.RouteRule{{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_ROUTE, ActionOptions: &profilev1.ActionOptions{Outbound: "missing"}}}},
			outbounds: outbounds,
			want:      []string{"route.rules[0].action_options.outbound", "missing"},
		},
		{
			name:  "missing resolve server",
			route: &profilev1.Route{Rules: []*profilev1.RouteRule{{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_RESOLVE, ActionOptions: &profilev1.ActionOptions{Server: "missing"}}}},
			dns:   dns,
			want:  []string{"route.rules[0].action_options.server", "missing"},
		},
		{
			name:      "missing route final",
			route:     &profilev1.Route{Final: "missing"},
			outbounds: outbounds,
			want:      []string{"route.final", "missing"},
		},
		{
			name:  "missing default resolver",
			route: &profilev1.Route{DefaultDomainResolver: &profilev1.RouteDefaultDomainResolver{Server: "missing"}},
			dns:   dns,
			want:  []string{"route.default_domain_resolver.server", "missing"},
		},
		{
			name:      "missing rule set download detour",
			route:     &profilev1.Route{RuleSet: []*profilev1.RuleSet{{Type: profilev1.RulesetType_RULESET_TYPE_REMOTE, DownloadDetour: "missing"}}},
			outbounds: outbounds,
			want:      []string{"route.rule_set[0].download_detour", "missing"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := generator.generateRoute(test.route, test.inbounds, test.outbounds, test.dns)
			if err == nil {
				t.Fatal("expected reference error")
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestGenerateDNSReferenceErrors(t *testing.T) {
	generator := &configGenerator{}
	dnsServers := []*profilev1.DnsServer{{Id: "dns-1", Tag: "dns", Type: profilev1.DnsServerType_DNS_SERVER_TYPE_LOCAL}}
	outbounds := []*profilev1.Outbound{{Id: "out-1", Tag: "out"}}
	inbounds := []*profilev1.Inbound{{Id: "in-1", Tag: "in", Enable: true}}
	ruleSets := []*profilev1.RuleSet{{Id: "rs-1", Tag: "rs"}}

	tests := []struct {
		name string
		dns  *profilev1.Dns
		want string
	}{
		{name: "final", dns: &profilev1.Dns{Servers: dnsServers, Final: "missing"}, want: "dns.final"},
		{name: "server detour", dns: &profilev1.Dns{Servers: []*profilev1.DnsServer{{Type: profilev1.DnsServerType_DNS_SERVER_TYPE_LOCAL, Detour: "missing"}}}, want: "dns.servers[0].detour"},
		{name: "server domain resolver", dns: &profilev1.Dns{Servers: []*profilev1.DnsServer{{Type: profilev1.DnsServerType_DNS_SERVER_TYPE_LOCAL, DomainResolver: "missing"}}}, want: "dns.servers[0].domain_resolver"},
		{name: "rule server", dns: &profilev1.Dns{Servers: dnsServers, Rules: []*profilev1.DnsRule{{Enable: true, Domain: []string{"example.com"}, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_ROUTE, ActionOptions: &profilev1.DnsActionOptions{Server: "missing"}}}}, want: "dns.rules[0].action_options.server"},
		{name: "rule inbound", dns: &profilev1.Dns{Rules: []*profilev1.DnsRule{{Enable: true, Inbound: []string{"missing"}, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_REJECT}}}, want: "dns.rules[0].inbound[0]"},
		{name: "rule set", dns: &profilev1.Dns{Rules: []*profilev1.DnsRule{{Enable: true, RuleSet: []string{"missing"}, Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_REJECT}}}, want: "dns.rules[0].rule_set[0]"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := generator.generateDNS(test.dns, ruleSets, inbounds, outbounds)
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "missing") {
				t.Fatalf("error = %v, want path %q and missing ID", err, test.want)
			}
		})
	}
}

func TestGenerateDNSRejectsDisabledInboundReference(t *testing.T) {
	generator := &configGenerator{}
	_, err := generator.generateDNS(
		&profilev1.Dns{Rules: []*profilev1.DnsRule{{
			Enable: true, Inbound: []string{"in-1"},
			Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_REJECT,
		}}},
		nil,
		[]*profilev1.Inbound{{Id: "in-1", Tag: "in", Enable: false}},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "dns.rules[0].inbound[0]") || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled inbound path error, got %v", err)
	}
}

func TestGenerateRouteSkipsInternalInsertionPointByID(t *testing.T) {
	generator := &configGenerator{}
	route, err := generator.generateRoute(&profilev1.Route{Rules: []*profilev1.RouteRule{{
		Id: ruleTypeInsertionPoint, Enable: true, Action: profilev1.RuleAction_RULE_ACTION_ROUTE,
	}}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("insertion point should be skipped before route action validation: %v", err)
	}
	if got := len(route["rules"].([]any)); got != 0 {
		t.Fatalf("generated %d rules for insertion point", got)
	}
}

func TestGenerateOutboundReferenceErrors(t *testing.T) {
	tests := []struct {
		name      string
		generator *configGenerator
		proxy     *profilev1.ProxyRef
		want      string
	}{
		{
			name:      "local outbound",
			generator: &configGenerator{},
			proxy:     &profilev1.ProxyRef{Id: "missing", Type: "Built-in"},
			want:      "missing outbound ID",
		},
		{
			name:      "subscription",
			generator: &configGenerator{subscriptions: map[string]subscriptionMeta{}, subscriptionProxies: map[string][]map[string]any{}},
			proxy:     &profilev1.ProxyRef{Id: "missing", Type: "Subscription"},
			want:      `subscription "missing" not found`,
		},
		{
			name: "subscription node",
			generator: &configGenerator{
				subscriptions:       map[string]subscriptionMeta{"sub-1": {ID: "sub-1"}},
				subscriptionProxies: map[string][]map[string]any{"sub-1": {{"tag": "node"}}},
			},
			proxy: &profilev1.ProxyRef{Id: "missing-node", Type: "sub-1"},
			want:  "missing subscription node",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.generator.generateOutbounds([]*profilev1.Outbound{{
				Id: "group", Tag: "group", Type: profilev1.OutboundType_OUTBOUND_TYPE_SELECTOR,
				Outbounds: []*profilev1.ProxyRef{test.proxy},
			}})
			if err == nil || !strings.Contains(err.Error(), "outbounds[0].outbounds[0]") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want path and %q", err, test.want)
			}
		})
	}
}

func TestGenerateOutboundsResolveManagedIDs(t *testing.T) {
	generator := &configGenerator{
		subscriptions: map[string]subscriptionMeta{
			"sub-1": {ID: "sub-1", Proxies: []subscriptionProxyMeta{{ID: "node-id", Tag: "node-tag"}}},
		},
		subscriptionProxies: map[string][]map[string]any{
			"sub-1": {{"type": "direct", "tag": "node-tag"}},
		},
	}
	outbounds := []*profilev1.Outbound{
		{Id: "target-id", Tag: "target-tag", Type: profilev1.OutboundType_OUTBOUND_TYPE_SELECTOR},
		{
			Id: "group-id", Tag: "group-tag", Type: profilev1.OutboundType_OUTBOUND_TYPE_SELECTOR,
			Outbounds: []*profilev1.ProxyRef{
				{Id: "target-id", Type: "Built-in", Tag: "stale-local-tag"},
				{Id: "node-id", Type: "sub-1", Tag: "stale-node-tag"},
			},
		},
	}

	generated, err := generator.generateOutbounds(outbounds)
	if err != nil {
		t.Fatal(err)
	}
	group := generated[1].(map[string]any)
	if got := group["outbounds"]; !reflect.DeepEqual(got, []any{"target-tag", "node-tag"}) {
		t.Fatalf("outbound IDs were not resolved to current tags: %#v", got)
	}
}

func TestGenerateInlineActionUsesRawAction(t *testing.T) {
	generator := &configGenerator{}

	t.Run("route", func(t *testing.T) {
		route, err := generator.generateRoute(&profilev1.Route{Rules: []*profilev1.RouteRule{
			{
				Enable: true,
				Raw:    `{"action":"reject","method":"drop","invert":true}`,
				Invert: false,
				Action: profilev1.RuleAction_RULE_ACTION_INLINE,
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
				Enable: true,
				Raw:    `{"action":"reject","method":"drop","invert":false}`,
				Invert: true,
				Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_INLINE,
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

func TestGenerateDNSRejectsInvalidRaw(t *testing.T) {
	generator := &configGenerator{}
	_, err := generator.generateDNS(&profilev1.Dns{Rules: []*profilev1.DnsRule{
		{
			Enable: true,
			Raw:    `{invalid`,
			Action: profilev1.DnsRuleAction_DNS_RULE_ACTION_INLINE,
		},
	}}, nil, nil, nil)
	if err == nil {
		t.Fatal("invalid raw should return an error")
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
			}, "linux")

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

func TestGenerateTunAutoRedirectByPlatformAndAutoRoute(t *testing.T) {
	tests := []struct {
		name         string
		platformOS   string
		autoRoute    bool
		autoRedirect bool
		wantField    bool
	}{
		{name: "linux enabled true", platformOS: "linux", autoRoute: true, autoRedirect: true, wantField: true},
		{name: "linux enabled false", platformOS: "linux", autoRoute: true, autoRedirect: false, wantField: true},
		{name: "linux auto route disabled", platformOS: "linux", autoRoute: false, autoRedirect: true},
		{name: "windows", platformOS: "windows", autoRoute: true, autoRedirect: true},
		{name: "darwin", platformOS: "darwin", autoRoute: true, autoRedirect: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generated := generateInbounds([]*profilev1.Inbound{
				{
					Type:   profilev1.InboundType_INBOUND_TYPE_TUN,
					Tag:    "tun-in",
					Enable: true,
					Tun: &profilev1.TunInboundConfig{
						AutoRoute:    tt.autoRoute,
						AutoRedirect: tt.autoRedirect,
					},
				},
			}, tt.platformOS)
			item := generated[0].(map[string]any)
			got, ok := item["auto_redirect"]
			if ok != tt.wantField {
				t.Fatalf("auto_redirect presence = %v, want %v: %#v", ok, tt.wantField, item)
			}
			if ok && got != tt.autoRedirect {
				t.Fatalf("auto_redirect = %#v, want %v", got, tt.autoRedirect)
			}
		})
	}
}

func TestGenerateBridgeOutbound(t *testing.T) {
	generator := &configGenerator{}
	generated, err := generator.generateOutbounds([]*profilev1.Outbound{
		{
			Type:       profilev1.OutboundType_OUTBOUND_TYPE_BRIDGE,
			Tag:        "bridge-out",
			Interface:  "eth0",
			BridgeName: "custom-bridge",
		},
		{
			Type: profilev1.OutboundType_OUTBOUND_TYPE_BRIDGE,
			Tag:  "bridge-defaults",
		},
	})
	if err != nil {
		t.Fatalf("generate bridge outbounds: %v", err)
	}

	configured := generated[0].(map[string]any)
	if configured["type"] != "bridge" || configured["tag"] != "bridge-out" {
		t.Fatalf("unexpected bridge metadata: %#v", configured)
	}
	if configured["interface"] != "eth0" || configured["bridge_name"] != "custom-bridge" {
		t.Fatalf("bridge fields were not generated: %#v", configured)
	}
	for _, key := range []string{"iproute2_table_index", "iproute2_rule_index"} {
		if _, ok := configured[key]; ok {
			t.Fatalf("unsupported field %q was generated: %#v", key, configured)
		}
	}

	defaults := generated[1].(map[string]any)
	if _, ok := defaults["interface"]; ok {
		t.Fatalf("empty interface should be omitted: %#v", defaults)
	}
	if _, ok := defaults["bridge_name"]; ok {
		t.Fatalf("empty bridge_name should be omitted: %#v", defaults)
	}
}
