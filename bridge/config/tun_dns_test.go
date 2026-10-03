package config

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestGenerateTunDNSModeAcrossPlatforms(t *testing.T) {
	for _, platform := range []string{"linux", "windows", "darwin"} {
		for _, autoRoute := range []bool{false, true} {
			for _, mode := range []string{"", "disabled", "native", "hijack"} {
				t.Run(fmt.Sprintf("%s/auto_route=%t/mode=%s", platform, autoRoute, mode), func(t *testing.T) {
					tun := &profilev1.TunInboundConfig{
						DnsMode: mode, AutoRoute: autoRoute,
						Stack:        profilev1.TunStack_TUN_STACK_SYSTEM,
						RouteAddress: []string{"192.0.2.0/24"}, RouteExcludeAddress: []string{"198.51.100.0/24"},
					}
					inbounds, err := generateInbounds([]*profilev1.Inbound{{
						Type: profilev1.InboundType_INBOUND_TYPE_TUN, Enable: true, Tun: tun,
					}}, platform)
					if err != nil {
						t.Fatal(err)
					}
					item := inbounds[0].(map[string]any)
					wantMode := mode
					if wantMode == "" {
						wantMode = "hijack"
					}
					if item["dns_mode"] != wantMode || tun.GetDnsMode() != mode {
						t.Fatalf("expected generated mode %q without changing input, got %#v / %q", wantMode, item, tun.GetDnsMode())
					}
					for _, key := range []string{"endpoint_independent_nat", "dns_address", "udp_timeout", "udp_disable_domain_unmapping"} {
						if _, exists := item[key]; exists {
							t.Fatalf("unexpected TUN field %q: %#v", key, item)
						}
					}
					if item["stack"] != "system" || item["auto_route"] != autoRoute ||
						!reflect.DeepEqual(item["route_address"], []any{"192.0.2.0/24"}) ||
						!reflect.DeepEqual(item["route_exclude_address"], []any{"198.51.100.0/24"}) {
						t.Fatalf("existing TUN options changed: %#v", item)
					}
				})
			}
		}
	}
}

func TestGenerateTunDNSModeValidation(t *testing.T) {
	for _, platform := range []string{"linux", "windows", "darwin"} {
		for _, autoRoute := range []bool{false, true} {
			for _, mode := range []string{"invalid", "HIJACK", " hijack ", " "} {
				t.Run(fmt.Sprintf("%s/auto_route=%t/mode=%q", platform, autoRoute, mode), func(t *testing.T) {
					_, err := generateInbounds([]*profilev1.Inbound{nil, {
						Type: profilev1.InboundType_INBOUND_TYPE_TUN, Enable: false,
					}, {
						Type: profilev1.InboundType_INBOUND_TYPE_TUN, Enable: true,
						Tun: &profilev1.TunInboundConfig{DnsMode: mode, AutoRoute: autoRoute},
					}}, platform)
					if _, ok := err.(invalidArgumentError); !ok || !strings.Contains(err.Error(), "inbounds[2].tun.dns_mode") {
						t.Fatalf("expected located InvalidArgument, got %v", err)
					}
				})
			}
		}
	}
	if _, err := generateInbounds([]*profilev1.Inbound{nil, {
		Type: profilev1.InboundType_INBOUND_TYPE_TUN, Enable: false,
		Tun: &profilev1.TunInboundConfig{DnsMode: "invalid"},
	}, {
		Type: profilev1.InboundType_INBOUND_TYPE_TUN, Enable: true,
	}, {
		Type: profilev1.InboundType_INBOUND_TYPE_DIRECT, Enable: true,
		Direct: &profilev1.DirectInboundConfig{}, Tun: &profilev1.TunInboundConfig{DnsMode: "invalid"},
	}}, "linux"); err != nil {
		t.Fatalf("validation should only inspect enabled structured TUN inputs: %v", err)
	}
}

func TestGenerateTunDNSModeRPCInvalidArgument(t *testing.T) {
	service := NewService(storage.NewPaths(t.TempDir()), "test")
	_, err := service.GenerateConfig(context.Background(), connect.NewRequest(&kernelv1.GenerateConfigRequest{
		Profile: &profilev1.Profile{Inbounds: []*profilev1.Inbound{nil, {
			Type: profilev1.InboundType_INBOUND_TYPE_TUN, Enable: true,
			Tun: &profilev1.TunInboundConfig{DnsMode: "invalid"},
		}}},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "inbounds[1].tun.dns_mode") {
		t.Fatalf("expected located RPC InvalidArgument, got %v", err)
	}
}

func TestTunDNSModeDoesNotDisableHijackDNSRules(t *testing.T) {
	config, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(&profilev1.Profile{
		Inbounds: []*profilev1.Inbound{{
			Type: profilev1.InboundType_INBOUND_TYPE_TUN, Enable: true,
			Tun: &profilev1.TunInboundConfig{DnsMode: "disabled"},
		}},
		Route: &profilev1.Route{Rules: []*profilev1.RouteRule{{Enable: true, Action: profilev1.RuleAction_RULE_ACTION_HIJACK_DNS}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rules := config["route"].(map[string]any)["rules"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["action"] != "hijack-dns" {
		t.Fatalf("disabled DNS mode changed explicit hijack-dns rule: %#v", rules)
	}
}

func TestTunDNSModeCompositionPreservesExistingPriority(t *testing.T) {
	for _, test := range []struct {
		name     string
		priority profilev1.MixinPriority
		script   string
		wantMode any
		custom   bool
	}{
		{name: "GUI priority", priority: profilev1.MixinPriority_MIXIN_PRIORITY_GUI, wantMode: "native"},
		{name: "mixin priority accepts custom mode and fields", priority: profilev1.MixinPriority_MIXIN_PRIORITY_MIXIN, wantMode: "custom-mixin", custom: true},
		{name: "script overrides mixin mode", priority: profilev1.MixinPriority_MIXIN_PRIORITY_MIXIN, script: `function onGenerate(config) { config.inbounds[0].dns_mode = "custom-script"; return config; }`, wantMode: "custom-script", custom: true},
		{name: "script removes mode without defaulting", priority: profilev1.MixinPriority_MIXIN_PRIORITY_MIXIN, script: `function onGenerate(config) { delete config.inbounds[0].dns_mode; return config; }`, custom: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(&profilev1.Profile{
				Inbounds: []*profilev1.Inbound{{
					Type: profilev1.InboundType_INBOUND_TYPE_TUN, Enable: true,
					Tun: &profilev1.TunInboundConfig{DnsMode: "native"},
				}},
				Mixin:  &profilev1.Mixin{Priority: test.priority, Config: `{"inbounds":[{"type":"tun","dns_mode":"custom-mixin","endpoint_independent_nat":true,"dns_address":["192.0.2.2"]}]}`},
				Script: &profilev1.Script{Code: test.script},
			}, &kernelv1.GenerateConfigOptions{EnableMixinProcessing: true, EnableScriptProcessing: true})
			if err != nil {
				t.Fatal(err)
			}
			item := config["inbounds"].([]any)[0].(map[string]any)
			if item["dns_mode"] != test.wantMode {
				t.Fatalf("DNS mode = %#v, want %#v", item["dns_mode"], test.wantMode)
			}
			if test.custom && (item["endpoint_independent_nat"] != true || !reflect.DeepEqual(item["dns_address"], []any{"192.0.2.2"})) {
				t.Fatalf("custom TUN fields were cleaned after composition: %#v", item)
			}
			if test.wantMode == nil {
				if _, exists := item["dns_mode"]; exists {
					t.Fatalf("script removal was replaced by a default: %#v", item)
				}
			}
		})
	}
}
