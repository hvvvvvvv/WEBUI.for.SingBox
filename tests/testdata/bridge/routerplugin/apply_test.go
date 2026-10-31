package routerplugin

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	profilev1 "guiforcores/gen/profile/v1"
)

func testPlugin(id, kind string, config any) *profilev1.RouterPlugin {
	switch value := config.(type) {
	case tunRoutingConfig:
		if value.IncludeInterface == nil {
			value.IncludeInterface = []string{}
		}
		if value.ExcludeAddress == nil {
			value.ExcludeAddress = []string{}
		}
		config = value
	case sourceRoutingConfig:
		if value.SourceIPCIDR == nil {
			value.SourceIPCIDR = []string{}
		}
		config = value
	}
	data, err := json.Marshal(map[string]any{"apiVersion": APIVersion, "id": "community." + kind, "name": "Example", "description": "", "version": "1.2.3-beta.1+build.02", "kind": kind, "config": config})
	if err != nil {
		panic(err)
	}
	return &profilev1.RouterPlugin{Id: id, Enabled: true, ManifestJson: string(data)}
}

func testConfig() map[string]any {
	return map[string]any{
		"inbounds": []any{
			map[string]any{"tag": "lan-tun", "type": "tun", "interface_name": "tun0", "auto_route": false, "strict_route": false, "address": []any{"172.18.0.1/30"}, "mtu": 9000},
			map[string]any{"tag": "other-tun", "type": "tun", "interface_name": "tun1", "auto_route": false},
			map[string]any{"tag": "mixed", "type": "mixed", "listen_port": 20122},
		},
		"outbounds":    []any{map[string]any{"tag": "proxy", "type": "selector", "outbounds": []any{"direct"}}, map[string]any{"tag": "direct", "type": "direct"}},
		"route":        map[string]any{"rules": []any{map[string]any{"domain_suffix": []any{"existing.test"}, "action": "route", "outbound": "direct"}}, "final": "proxy", "find_process": true},
		"experimental": map[string]any{"cache_file": map[string]any{"enabled": true}},
		"services":     []any{map[string]any{"type": "api", "listen": "127.0.0.1", "listen_port": 20123, "secret": "must remain unchanged"}},
	}
}

func configJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestApplyRoutesDNSBeforeClientPoliciesAndPreservesOriginal(t *testing.T) {
	config := testConfig()
	before := configJSON(t, config)
	plugins := []*profilev1.RouterPlugin{
		testPlugin("source-a", "source-routing", sourceRoutingConfig{Inbound: "lan-tun", SourceIPCIDR: []string{"192.168.1.20/24"}, Outbound: "proxy"}),
		testPlugin("dns-other", "dns-hijack", dnsHijackConfig{Inbound: "other-tun"}),
		testPlugin("source-b", "source-routing", sourceRoutingConfig{Inbound: "other-tun", SourceIPCIDR: []string{"fd00::/64"}, Outbound: "direct"}),
		testPlugin("dns-lan", "dns-hijack", dnsHijackConfig{Inbound: "lan-tun"}),
	}
	next, err := Apply(config, plugins)
	if err != nil {
		t.Fatal(err)
	}
	if configJSON(t, config) != before {
		t.Fatal("Apply mutated caller's configuration")
	}
	rules := next["route"].(map[string]any)["rules"].([]any)
	if len(rules) != 5 {
		t.Fatalf("rules=%#v", rules)
	}
	for index, inbound := range []string{"other-tun", "lan-tun"} {
		rule := rules[index].(map[string]any)
		if rule["action"] != "hijack-dns" || !reflect.DeepEqual(rule["port"], []any{53}) || !reflect.DeepEqual(rule["inbound"], []any{inbound}) {
			t.Fatalf("DNS policy escaped inbound/port scope: %#v", rule)
		}
	}
	if rules[2].(map[string]any)["outbound"] != "proxy" || rules[3].(map[string]any)["outbound"] != "direct" {
		t.Fatal("client rule order changed")
	}
	if !reflect.DeepEqual(rules[2].(map[string]any)["source_ip_cidr"], []any{"192.168.1.0/24"}) {
		t.Fatal("CIDR was not normalized")
	}
	if configJSON(t, rules[4]) != configJSON(t, config["route"].(map[string]any)["rules"].([]any)[0]) {
		t.Fatal("original rules changed")
	}
	for _, field := range []string{"inbounds", "outbounds", "services", "experimental"} {
		if configJSON(t, next[field]) != configJSON(t, config[field]) {
			t.Fatalf("unrelated %s changed", field)
		}
	}
}

func TestTUNRoutingOnlyChangesSelectedInboundAndKeepsExplicitExit(t *testing.T) {
	for _, exit := range []string{"", "eth0"} {
		t.Run("exit="+exit, func(t *testing.T) {
			config := testConfig()
			config["route"].(map[string]any)["default_interface"] = exit
			before := configJSON(t, config)
			plugin := testPlugin("tun", "tun-routing", tunRoutingConfig{Inbound: "lan-tun", StrictRoute: true, IncludeInterface: []string{"br-lan"}, ExcludeAddress: []string{"192.168.2.0/24"}})
			next, err := Apply(config, []*profilev1.RouterPlugin{plugin})
			if err != nil {
				t.Fatal(err)
			}
			tun := next["inbounds"].([]any)[0].(map[string]any)
			if tun["auto_route"] != true || tun["auto_redirect"] != false || tun["strict_route"] != true {
				t.Fatalf("unexpected TUN settings: %#v", tun)
			}
			if !reflect.DeepEqual(tun["include_interface"], []any{"br-lan"}) || !reflect.DeepEqual(tun["route_exclude_address"], []any{"192.168.2.0/24"}) {
				t.Fatal("TUN filters were lost")
			}
			route := next["route"].(map[string]any)
			if route["auto_detect_interface"] != (exit == "") || route["default_interface"] != exit || route["find_process"] != false {
				t.Fatalf("incorrect router exit selection: %#v", route)
			}
			if configJSON(t, next["inbounds"].([]any)[1:]) != configJSON(t, config["inbounds"].([]any)[1:]) || configJSON(t, config) != before {
				t.Fatal("unrelated inbound/caller was mutated")
			}
		})
	}
}

func TestApplyRejectsBrokenReferencesAndConflictsWithoutPartialChanges(t *testing.T) {
	cases := []struct {
		name   string
		modify func(map[string]any)
		second *profilev1.RouterPlugin
	}{
		{name: "missing TUN", second: testPlugin("second", "dns-hijack", dnsHijackConfig{Inbound: "missing"})},
		{name: "non TUN", second: testPlugin("second", "dns-hijack", dnsHijackConfig{Inbound: "mixed"})},
		{name: "missing outbound", second: testPlugin("second", "source-routing", sourceRoutingConfig{Inbound: "lan-tun", SourceIPCIDR: []string{"192.168.1.0/24"}, Outbound: "missing"})},
		{name: "duplicate TUN configuration", second: testPlugin("second", "tun-routing", tunRoutingConfig{Inbound: "lan-tun"})},
		{name: "exit loop", modify: func(config map[string]any) { config["route"].(map[string]any)["default_interface"] = "tun0" }},
		{name: "ambiguous inbound", modify: func(config map[string]any) {
			config["inbounds"] = append(config["inbounds"].([]any), map[string]any{"tag": "lan-tun", "type": "tun"})
		}},
		{name: "own interface", second: testPlugin("second", "tun-routing", tunRoutingConfig{Inbound: "other-tun", IncludeInterface: []string{"tun1"}})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := testConfig()
			if tc.modify != nil {
				tc.modify(config)
			}
			before := configJSON(t, config)
			plugins := []*profilev1.RouterPlugin{testPlugin("first", "tun-routing", tunRoutingConfig{Inbound: "lan-tun", StrictRoute: true})}
			if tc.second != nil {
				plugins = append(plugins, tc.second)
			}
			if _, err := Apply(config, plugins); err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if configJSON(t, config) != before {
				t.Fatal("a failing later plugin left partial changes")
			}
		})
	}
}

func TestDisabledDraftsAndExistingProfilesStayUnchanged(t *testing.T) {
	config := testConfig()
	before := configJSON(t, config)
	template := testPlugin("draft", "source-routing", sourceRoutingConfig{SourceIPCIDR: []string{}})
	template.Enabled = false
	for _, plugins := range [][]*profilev1.RouterPlugin{nil, {}, {template}} {
		next, err := Apply(config, plugins)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(next, config) || configJSON(t, config) != before {
			t.Fatal("inactive plugins affected existing configuration")
		}
	}
	template.Enabled = true
	if err := Validate([]*profilev1.RouterPlugin{template}); err == nil {
		t.Fatal("an incomplete template can be enabled")
	}
}

func TestManifestRejectsExecutableAndMalformedData(t *testing.T) {
	valid := testPlugin("test", "dns-hijack", dnsHijackConfig{Inbound: "lan-tun"}).ManifestJson
	for name, raw := range map[string]string{
		"JS":                   `function onGenerate(config){return config}`,
		"script field":         strings.Replace(valid, `"kind":"dns-hijack"`, `"kind":"dns-hijack","script":"while(true){}"`, 1),
		"shell config":         strings.Replace(valid, `"inbound":"lan-tun"`, `"inbound":"lan-tun","command":"sh"`, 1),
		"unknown kind":         strings.Replace(valid, `"dns-hijack"`, `"javascript"`, 1),
		"duplicate key":        strings.Replace(valid, `"inbound":"lan-tun"`, `"inbound":"lan-tun","inbound":"other-tun"`, 1),
		"case alias":           strings.Replace(valid, `"kind":`, `"Kind":`, 1),
		"null":                 strings.Replace(valid, `"inbound":"lan-tun"`, `"inbound":null`, 1),
		"trailing":             valid + ` {}`,
		"old API":              strings.Replace(valid, APIVersion, "webui.router/v2", 1),
		"leading zero version": strings.Replace(valid, "1.2.3-beta.1+build.02", "1.2.3-beta.01", 1),
		"missing prerelease":   strings.Replace(valid, "1.2.3-beta.1+build.02", "1.2.3-", 1),
		"too large":            strings.Repeat(" ", MaxManifestBytes) + valid,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest(raw); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	if _, err := ParseManifest(valid); err != nil {
		t.Fatal(err)
	}
	for _, configuration := range []any{sourceRoutingConfig{SourceIPCIDR: []string{"not-a-cidr"}}, sourceRoutingConfig{SourceIPCIDR: []string{"192.168.1.1/24", "192.168.1.2/24"}}} {
		plugin := testPlugin("disabled", "source-routing", configuration)
		plugin.Enabled = false
		if err := Validate([]*profilev1.RouterPlugin{plugin}); err == nil {
			t.Fatal("disabled malformed payload bypassed validation")
		}
	}
}

func TestInstanceAndListLimits(t *testing.T) {
	plugin := testPlugin("one", "dns-hijack", dnsHijackConfig{Inbound: "lan-tun"})
	if err := Validate([]*profilev1.RouterPlugin{plugin, plugin}); err == nil {
		t.Fatal("duplicate instance IDs accepted")
	}
	if err := Validate([]*profilev1.RouterPlugin{nil}); err == nil {
		t.Fatal("nil instance accepted")
	}
	var many []*profilev1.RouterPlugin
	for index := 0; index <= MaxPlugins; index++ {
		many = append(many, testPlugin(fmt.Sprint("id", index), "dns-hijack", dnsHijackConfig{Inbound: "lan-tun"}))
	}
	if err := Validate(many); err == nil {
		t.Fatal("instance bound ignored")
	}
	if err := Validate(many[:MaxPlugins]); err != nil {
		t.Fatal(err)
	}
	interfaces := make([]string, MaxInterfaces+1)
	for index := range interfaces {
		interfaces[index] = fmt.Sprint("lan", index)
	}
	if _, err := ParseManifest(testPlugin("one", "tun-routing", tunRoutingConfig{IncludeInterface: interfaces}).ManifestJson); err == nil {
		t.Fatal("interface bound ignored")
	}
	cidrs := make([]string, MaxCIDRs+1)
	for index := range cidrs {
		cidrs[index] = fmt.Sprintf("10.%d.%d.0/24", index/256, index%256)
	}
	if _, err := ParseManifest(testPlugin("one", "source-routing", sourceRoutingConfig{SourceIPCIDR: cidrs}).ManifestJson); err == nil {
		t.Fatal("CIDR bound ignored")
	}
}

func TestEmbeddedCatalogIsOfflineDataAndReturnsIndependentTemplates(t *testing.T) {
	first, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 {
		t.Fatalf("catalog entries=%d", len(first))
	}
	for _, manifest := range first {
		if _, err := ParseManifest(configJSON(t, manifest)); err != nil {
			t.Fatal(err)
		}
		instance := &profilev1.RouterPlugin{Id: manifest.ID, ManifestJson: configJSON(t, manifest)}
		if err := Validate([]*profilev1.RouterPlugin{instance}); err != nil {
			t.Fatal(err)
		}
		instance.Enabled = true
		if err := Validate([]*profilev1.RouterPlugin{instance}); err == nil {
			t.Fatal("template should require choosing an inbound")
		}
	}
	first[0].Config[0] = 'x'
	second, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Config[0] != '{' {
		t.Fatal("catalog callers share mutable configuration")
	}
}

func TestApplyPreservesLargeIntegerConfigurationValues(t *testing.T) {
	config := testConfig()
	config["unrelated_integer"] = uint64(9007199254740993)
	next, err := Apply(config, []*profilev1.RouterPlugin{testPlugin("dns", "dns-hijack", dnsHijackConfig{Inbound: "lan-tun"})})
	if err != nil {
		t.Fatal(err)
	}
	if configJSON(t, next["unrelated_integer"]) != "9007199254740993" {
		t.Fatal("copy rounded a large integer")
	}
}

func TestTUNPluginPreservesExistingInterfaceAndManagementExclusions(t *testing.T) {
	config := testConfig()
	tun := config["inbounds"].([]any)[0].(map[string]any)
	tun["include_interface"] = []any{"br-protected"}
	tun["route_exclude_address"] = []any{"192.168.1.0/24", "10.0.0.0/8"}
	for _, added := range [][]string{nil, {"192.168.1.17/24", "fd00::/64"}} {
		plugin := testPlugin("tun", "tun-routing", tunRoutingConfig{Inbound: "lan-tun", ExcludeAddress: added})
		next, err := Apply(config, []*profilev1.RouterPlugin{plugin})
		if err != nil {
			t.Fatal(err)
		}
		actual := next["inbounds"].([]any)[0].(map[string]any)
		if !reflect.DeepEqual(actual["include_interface"], []any{"br-protected"}) {
			t.Fatal("empty plugin list cleared the user's interface restriction")
		}
		want := []any{"192.168.1.0/24", "10.0.0.0/8"}
		if len(added) > 0 {
			want = append(want, "fd00::/64")
		}
		if !reflect.DeepEqual(actual["route_exclude_address"], want) {
			t.Fatalf("management exclusions changed: %#v", actual["route_exclude_address"])
		}
	}
}

func TestManifestRequiresAllProtocolFields(t *testing.T) {
	valid := testPlugin("one", "tun-routing", tunRoutingConfig{}).ManifestJson
	var manifest map[string]any
	if err := json.Unmarshal([]byte(valid), &manifest); err != nil {
		t.Fatal(err)
	}
	for name, value := range manifest {
		delete(manifest, name)
		if _, err := ParseManifest(configJSON(t, manifest)); err == nil {
			t.Fatalf("missing manifest.%s accepted", name)
		}
		manifest[name] = value
	}
	configuration := manifest["config"].(map[string]any)
	for name, value := range configuration {
		delete(configuration, name)
		if _, err := ParseManifest(configJSON(t, manifest)); err == nil {
			t.Fatalf("missing config.%s accepted", name)
		}
		configuration[name] = value
	}
}

func TestAggregateManifestAndGeneratedConfigBounds(t *testing.T) {
	cidrs := make([]string, MaxCIDRs)
	for index := range cidrs {
		cidrs[index] = fmt.Sprintf("2001:0db8:0000:0000:0000:0000:%04x:0000/112", index)
	}
	var instances []*profilev1.RouterPlugin
	for index := 0; index < MaxPlugins; index++ {
		plugin := testPlugin(fmt.Sprint("instance", index), "source-routing", sourceRoutingConfig{SourceIPCIDR: cidrs})
		plugin.Enabled = false
		if _, err := ParseManifest(plugin.ManifestJson); err != nil {
			t.Fatal(err)
		}
		instances = append(instances, plugin)
	}
	if err := Validate(instances); err == nil || !strings.Contains(err.Error(), "total manifest bytes") {
		t.Fatalf("aggregate size limit not enforced: %v", err)
	}
	config := testConfig()
	config["oversized"] = strings.Repeat("x", MaxConfigBytes)
	before := configJSON(t, config)
	if _, err := Apply(config, []*profilev1.RouterPlugin{testPlugin("dns", "dns-hijack", dnsHijackConfig{Inbound: "lan-tun"})}); err == nil || !strings.Contains(err.Error(), "configuration exceeds") {
		t.Fatalf("configuration size limit not enforced: %v", err)
	}
	if configJSON(t, config) != before {
		t.Fatal("size rejection changed original config")
	}
}

func TestAutoRedirectRejectsConflictingMarksWithoutMutatingInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		set  func(map[string]any)
	}{
		{"default numeric", "route.default_mark", func(config map[string]any) {
			config["route"].(map[string]any)["default_mark"] = 2023
		}},
		{"default hex", "route.default_mark", func(config map[string]any) {
			config["route"].(map[string]any)["default_mark"] = "0x2023"
		}},
		{"outbound", "outbounds[1].routing_mark", func(config map[string]any) {
			config["outbounds"].([]any)[1].(map[string]any)["routing_mark"] = 1
		}},
		{"DNS", "dns.servers[0].routing_mark", func(config map[string]any) {
			config["dns"] = map[string]any{"servers": []any{map[string]any{"type": "udp", "server": "1.1.1.1", "routing_mark": "0x100"}}}
		}},
		{"HTTP client", "http_clients[0].routing_mark", func(config map[string]any) {
			config["http_clients"] = []any{map[string]any{"tag": "downloads", "routing_mark": 1}}
		}},
		{"endpoint", "endpoints[0].routing_mark", func(config map[string]any) {
			config["endpoints"] = []any{map[string]any{"type": "wireguard", "tag": "wg", "routing_mark": 1}}
		}},
		{"nested dialer", "http_clients[0].domain_resolver.routing_mark", func(config map[string]any) {
			config["http_clients"] = []any{map[string]any{"domain_resolver": map[string]any{"routing_mark": 1}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := testConfig()
			tc.set(config)
			before := configJSON(t, config)
			plugin := testPlugin("tun", "tun-routing", tunRoutingConfig{Inbound: "lan-tun", AutoRedirect: true})
			next, err := Apply(config, []*profilev1.RouterPlugin{plugin})
			if err == nil || !strings.Contains(err.Error(), "auto_redirect") || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("expected an actionable mark conflict for %s, got %v", tc.path, err)
			}
			if next != nil || configJSON(t, config) != before {
				t.Fatal("failed preflight returned a partial config or changed the input")
			}
		})
	}
}

func TestAutoRedirectOnlyAcceptsRecognizedZeroMarks(t *testing.T) {
	plugin := testPlugin("tun", "tun-routing", tunRoutingConfig{Inbound: "lan-tun", AutoRedirect: true})
	for _, mark := range []any{0, float64(0), json.Number("0"), "0", "0x0", "0X0000", "0b0", "000"} {
		t.Run("zero "+fmt.Sprint(mark), func(t *testing.T) {
			config := testConfig()
			config["route"].(map[string]any)["default_mark"] = mark
			config["outbounds"].([]any)[1].(map[string]any)["routing_mark"] = mark
			before := configJSON(t, config)
			next, err := Apply(config, []*profilev1.RouterPlugin{plugin})
			if err != nil {
				t.Fatal(err)
			}
			if actual, exists := next["route"].(map[string]any)["default_mark"]; !exists || configJSON(t, actual) != configJSON(t, mark) {
				t.Fatal("preflight removed or changed the default mark")
			}
			if actual, exists := next["outbounds"].([]any)[1].(map[string]any)["routing_mark"]; !exists || configJSON(t, actual) != configJSON(t, mark) {
				t.Fatal("preflight removed or changed the dialer mark")
			}
			if configJSON(t, config) != before {
				t.Fatal("successful preflight changed the input")
			}
		})
	}
	for _, mark := range []any{nil, false, "", "unknown", "0xnot-hex", "4294967296", json.Number("1e-999"), []any{0}, map[string]any{"value": 0}} {
		t.Run("unknown "+fmt.Sprint(mark), func(t *testing.T) {
			config := testConfig()
			config["route"].(map[string]any)["default_mark"] = mark
			before := configJSON(t, config)
			if _, err := Apply(config, []*profilev1.RouterPlugin{plugin}); err == nil || !strings.Contains(err.Error(), "route.default_mark") {
				t.Fatalf("unknown mark was accepted as zero: %v", err)
			}
			if configJSON(t, config) != before {
				t.Fatal("unknown mark rejection changed the input")
			}
		})
	}
}

func TestAutoRedirectChecksFinalConfigurationIncludingExistingTUNs(t *testing.T) {
	config := testConfig()
	tun := config["inbounds"].([]any)[0].(map[string]any)
	tun["auto_redirect"] = true
	config["route"].(map[string]any)["default_mark"] = 100
	before := configJSON(t, config)
	dnsPlugin := testPlugin("dns", "dns-hijack", dnsHijackConfig{Inbound: "lan-tun"})
	if _, err := Apply(config, []*profilev1.RouterPlugin{dnsPlugin}); err == nil || !strings.Contains(err.Error(), "route.default_mark") {
		t.Fatalf("existing auto_redirect TUN bypassed validation: %v", err)
	}
	if configJSON(t, config) != before {
		t.Fatal("existing TUN rejection changed the input")
	}
	// A plugin may intentionally disable auto_redirect to retain manual marks.
	tunPlugin := testPlugin("tun", "tun-routing", tunRoutingConfig{Inbound: "lan-tun", AutoRedirect: false})
	next, err := Apply(config, []*profilev1.RouterPlugin{dnsPlugin, tunPlugin})
	if err != nil {
		t.Fatal(err)
	}
	if configJSON(t, next["route"].(map[string]any)["default_mark"]) != "100" {
		t.Fatal("manual routing mark was removed")
	}
	if configJSON(t, config) != before {
		t.Fatal("disabling auto_redirect changed the input")
	}
}

func TestAutoRedirectRejectsMultipleTUNsInSameNamespace(t *testing.T) {
	plugin := testPlugin("tun", "tun-routing", tunRoutingConfig{Inbound: "lan-tun", AutoRedirect: true})
	for _, namespace := range []string{"", "router-lan"} {
		t.Run(namespace, func(t *testing.T) {
			config := testConfig()
			inbounds := config["inbounds"].([]any)
			inbounds[0].(map[string]any)["netns"] = namespace
			inbounds[1].(map[string]any)["netns"] = namespace
			inbounds[1].(map[string]any)["auto_redirect"] = true
			before := configJSON(t, config)
			if _, err := Apply(config, []*profilev1.RouterPlugin{plugin}); err == nil || !strings.Contains(err.Error(), "same network namespace") {
				t.Fatalf("conflicting auto_redirect TUNs accepted: %v", err)
			}
			if configJSON(t, config) != before {
				t.Fatal("namespace rejection changed input")
			}
			inbounds[1].(map[string]any)["netns"] = "separate-router"
			if _, err := Apply(config, []*profilev1.RouterPlugin{plugin}); err != nil {
				t.Fatalf("distinct namespace rejected: %v", err)
			}
		})
	}
}
