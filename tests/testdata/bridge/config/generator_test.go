package config

import (
	"reflect"
	"strings"
	"testing"

	profilev1 "guiforcores/gen/profile/v1"
)

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
