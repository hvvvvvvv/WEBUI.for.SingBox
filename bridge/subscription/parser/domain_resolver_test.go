package parser

import (
	"reflect"
	"testing"
)

func TestNodeDomainResolverConversion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input Node
		want  any
	}{
		{name: "absent resolver", input: Node{"domain-strategy": "prefer-ipv4"}},
		{name: "empty resolver", input: Node{"domain_resolver": "", "domain_strategy": "ipv6_only"}},
		{name: "blank resolver", input: Node{"domain-resolver": " \t", "ip-version": "4"}},
		{name: "string alone", input: Node{"domain_resolver": "node-dns"}, want: "node-dns"},
		{name: "string and legacy strategy", input: Node{"domain-resolver": "node-dns", "domain-strategy": "prefer-ipv6"}, want: map[string]any{"server": "node-dns", "strategy": "prefer_ipv6"}},
		{name: "unrecognized legacy strategy", input: Node{"domain_resolver": "node-dns", "domain_strategy": "unsupported"}, want: "node-dns"},
		{name: "object gains strategy and preserves options", input: Node{"domain_resolver": map[string]any{"server": "node-dns", "timeout": "2s", "disable_cache": true, "client_subnet": "192.0.2.0/24"}, "ip_version": 6}, want: map[string]any{"server": "node-dns", "timeout": "2s", "disable_cache": true, "client_subnet": "192.0.2.0/24", "strategy": "ipv6_only"}},
		{name: "modern strategy wins", input: Node{"domain_resolver": map[string]any{"server": "node-dns", "strategy": "prefer_ipv4"}, "domain_strategy": "ipv6"}, want: map[string]any{"server": "node-dns", "strategy": "prefer_ipv4"}},
		{name: "explicit empty strategy wins", input: Node{"domain_resolver": map[string]any{"server": "node-dns", "strategy": ""}, "domain_strategy": "ipv6"}, want: map[string]any{"server": "node-dns", "strategy": ""}},
		{name: "object without server", input: Node{"domain-resolver": map[string]any{"timeout": "3s"}, "domain_strategy": "ipv4"}, want: map[string]any{"timeout": "3s"}},
		{name: "object with empty server", input: Node{"domain-resolver": map[string]any{"server": ""}, "domain_strategy": "ipv4"}, want: map[string]any{"server": ""}},
		{name: "explicit empty object", input: Node{"domain_resolver": map[string]any{}, "domain_strategy": "ipv4"}, want: map[string]any{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.input["type"] = "direct"
			tc.input["name"] = "resolver test"
			out, err := produceNode(tc.input)
			if err != nil {
				t.Fatalf("produceNode() error = %v", err)
			}
			resolver, present := out["domain_resolver"]
			if present != (tc.want != nil) || !reflect.DeepEqual(resolver, tc.want) {
				t.Fatalf("domain_resolver = %#v (present %v), want %#v", resolver, present, tc.want)
			}
			if _, present := out["domain_strategy"]; present {
				t.Fatalf("deprecated domain_strategy emitted: %#v", out)
			}
		})
	}
}

func TestNodeDomainResolverStrategyAliases(t *testing.T) {
	t.Parallel()
	strategies := map[string]string{
		"4": "ipv4_only", "ipv4": "ipv4_only", "IPv4Only": "ipv4_only", "ipv4-only": "ipv4_only",
		"6": "ipv6_only", "ipv6": "ipv6_only", "IPv6Only": "ipv6_only", "ipv6_only": "ipv6_only",
		"preferipv4": "prefer_ipv4", "prefer-ipv4": "prefer_ipv4", "prefer_ipv4": "prefer_ipv4",
		"preferipv6": "prefer_ipv6", "prefer ipv6": "prefer_ipv6", "prefer_ipv6": "prefer_ipv6",
	}
	for _, alias := range []string{"domain-strategy", "domain_strategy", "ip-version", "ip_version"} {
		for input, want := range strategies {
			t.Run(alias+"/"+input, func(t *testing.T) {
				out := map[string]any{}
				applyDomainResolver(Node{"domain_resolver": "node-dns", alias: input}, out)
				if !reflect.DeepEqual(out["domain_resolver"], map[string]any{"server": "node-dns", "strategy": want}) {
					t.Fatalf("domain_resolver = %#v", out["domain_resolver"])
				}
			})
		}
	}
}

func TestNodeDomainResolverDoesNotMutateSourceObject(t *testing.T) {
	t.Parallel()
	source := map[string]any{"server": "node-dns", "timeout": "2s"}
	for _, strategy := range []string{"ipv4", "ipv6"} {
		out := map[string]any{}
		applyDomainResolver(Node{"domain_resolver": source, "domain_strategy": strategy}, out)
		if _, present := source["strategy"]; present {
			t.Fatalf("source resolver mutated: %#v", source)
		}
	}
}

func TestNodeDomainResolverHasNoAddressOrDetourInference(t *testing.T) {
	t.Parallel()
	for _, server := range []string{"proxy.example.com", "192.0.2.1", "2001:db8::1"} {
		out, err := produceNode(Node{"name": "node", "type": "http", "server": server, "port": 443, "domain_strategy": "ipv6", "detour": "another-proxy"})
		if err != nil {
			t.Fatal(err)
		}
		if _, present := out["domain_resolver"]; present {
			t.Fatalf("resolver synthesized for %q: %#v", server, out)
		}
		if _, present := out["domain_strategy"]; present {
			t.Fatalf("legacy strategy emitted for %q: %#v", server, out)
		}
	}
}
