package config

import (
	"reflect"
	"strings"
	"testing"

	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestOptimisticDNSCacheGeneration(t *testing.T) {
	g := &configGenerator{}
	for _, tc := range []struct {
		name       string
		optimistic *profilev1.DnsOptimisticCache
		want       any
	}{
		{name: "legacy profile omits optimistic"},
		{name: "enabled custom age", optimistic: &profilev1.DnsOptimisticCache{Enabled: true, Timeout: "12h"}, want: map[string]any{"enabled": true, "timeout": "12h"}},
		{name: "enabled default age", optimistic: &profilev1.DnsOptimisticCache{Enabled: true}, want: map[string]any{"enabled": true}},
		{name: "disabled omits unused age", optimistic: &profilev1.DnsOptimisticCache{Timeout: "7d"}, want: map[string]any{"enabled": false}},
		{name: "disabled ignores invalid saved age", optimistic: &profilev1.DnsOptimisticCache{Timeout: "invalid"}, want: map[string]any{"enabled": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := g.generateDNS(&profilev1.Dns{Optimistic: tc.optimistic}, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result["optimistic"], tc.want) {
				t.Fatalf("generated optimistic = %#v, want %#v", result["optimistic"], tc.want)
			}
		})
	}
	for _, dns := range []*profilev1.Dns{
		{DisableCache: true, Optimistic: &profilev1.DnsOptimisticCache{Enabled: true}},
		{DisableExpire: true, Optimistic: &profilev1.DnsOptimisticCache{Enabled: true}},
	} {
		if _, err := g.generateDNS(dns, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "dns.optimistic") {
			t.Fatalf("conflicting cache settings must fail before core startup: %v", err)
		}
	}
}

func TestClearingDefaultResolverOmitsInvalidEmptyObject(t *testing.T) {
	s := NewService(storage.NewPaths(t.TempDir()), "test")
	profile := &profilev1.Profile{
		Route: &profilev1.Route{DefaultDomainResolver: &profilev1.RouteDefaultDomainResolver{Server: "local-id"}},
		Dns:   &profilev1.Dns{Servers: []*profilev1.DnsServer{{Id: "local-id", Tag: "local", Type: profilev1.DnsServerType_DNS_SERVER_TYPE_LOCAL}}},
	}
	configured, err := s.Generate(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	if configured["route"].(map[string]any)["default_domain_resolver"].(map[string]any)["server"] != "local" {
		t.Fatal("selected resolver was not resolved to its runtime tag")
	}
	profile.Route.DefaultDomainResolver.Server = ""
	cleared, err := s.Generate(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolver, exists := cleared["route"].(map[string]any)["default_domain_resolver"]; exists {
		t.Fatalf("clearing the UI selector generated an invalid empty resolver instead of omitting it: %#v", resolver)
	}
}

func TestFakeIPCannotBeDefaultDNSServer(t *testing.T) {
	g := &configGenerator{}
	dns := &profilev1.Dns{
		Servers: []*profilev1.DnsServer{
			{Id: "local-id", Tag: "local", Type: profilev1.DnsServerType_DNS_SERVER_TYPE_LOCAL},
			{Id: "fake-id", Tag: "fake", Type: profilev1.DnsServerType_DNS_SERVER_TYPE_FAKEIP},
		},
		Final: "fake-id",
	}
	if _, err := g.generateDNS(dns, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "dns.final") {
		t.Fatalf("fakeip default DNS must fail before core startup, got %v", err)
	}
	dns.Final = "local-id"
	config, err := g.generateDNS(dns, nil, nil, nil)
	if err != nil || config["final"] != "local" || len(config["servers"].([]any)) != 2 {
		t.Fatalf("valid default DNS must retain fakeip for explicit rules: config=%#v, err=%v", config, err)
	}
}
