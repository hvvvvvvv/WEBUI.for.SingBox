//go:build ignore

// Generate the isolated Linux namespace fixture with the production generator.
// Usage (repository root): go run tests/compat/router-transparent.go OUTPUT_DIR
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"guiforcores/bridge/config"
	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run tests/compat/router-transparent.go OUTPUT_DIR")
	}
	root, err := filepath.Abs(os.Args[1])
	if err != nil {
		return err
	}
	data, err := os.ReadFile("tests/fixtures/router-default-profile.json")
	if err != nil {
		return err
	}
	profile := new(profilev1.Profile)
	if err := protojson.Unmarshal(data, profile); err != nil {
		return err
	}
	if profile.Dns == nil || profile.Route == nil || len(profile.Route.RuleSet) != 6 {
		return fmt.Errorf("expected the real six-rule-set QuickStart fixture")
	}
	for _, inbound := range profile.Inbounds {
		inbound.Enable = inbound.Type == profilev1.InboundType_INBOUND_TYPE_TUN
		if inbound.Enable {
			inbound.Tun.InterfaceName = "webui-test-tun"
			inbound.Tun.Address = []string{"172.30.0.1/30"}
			inbound.Tun.AutoRoute, inbound.Tun.StrictRoute = true, true
			inbound.Tun.AutoRedirect = false // preserve the template's actual TUN path
			inbound.Tun.RouteExcludeAddress = []string{"10.204.0.0/24"}
		}
	}
	profile.Route.AutoDetectInterface = false
	profile.Route.DefaultInterface = "wan0"
	profile.Route.FindProcess = false
	for _, group := range profile.Outbounds {
		if group.Type == profilev1.OutboundType_OUTBOUND_TYPE_URLTEST {
			group.Url = "http://10.204.0.2:8081/generate_204"
		}
	}
	for _, ruleSet := range profile.Route.RuleSet {
		ruleSet.Url = "http://10.204.0.2:8081/rules/" + ruleSet.Tag + ".json"
		ruleSet.Format = profilev1.RulesetFormat_RULESET_FORMAT_SOURCE
		ruleSet.UpdateInterval = "24h"
	}
	// Preserve DNS IDs, rules and detours, but use real controlled TCP/UDP DNS
	// instead of public encrypted resolvers. This is not a TLS/DoH acceptance test.
	for _, server := range profile.Dns.Servers {
		if server.Type == profilev1.DnsServerType_DNS_SERVER_TYPE_FAKEIP {
			continue
		}
		server.Type = profilev1.DnsServerType_DNS_SERVER_TYPE_UDP
		if strings.HasPrefix(server.Id, "Remote-") {
			server.Type = profilev1.DnsServerType_DNS_SERVER_TYPE_TCP
		}
		server.Server, server.ServerPort = "10.204.0.2", "1053"
		server.DomainResolver, server.Path = "", ""
	}
	profile.Dns.DisableCache = true // observe each controlled DNS change immediately
	profile.Experimental.CacheFile.Path = filepath.Join(root, "cache.db")
	if err := os.MkdirAll(filepath.Join(root, "data", "subscribes"), 0700); err != nil {
		return err
	}
	metadata := "- id: fixture-subscription\n  proxies:\n    - id: fixture-node-id\n      tag: fixture-node\n"
	if err := os.WriteFile(filepath.Join(root, "data", "subscribes.yaml"), []byte(metadata), 0600); err != nil {
		return err
	}
	nodes := []byte(`[{"type":"http","tag":"fixture-node","server":"10.204.0.2","server_port":3128}]`)
	if err := os.WriteFile(filepath.Join(root, "data", "subscribes", "fixture-subscription.json"), nodes, 0600); err != nil {
		return err
	}
	generated, err := config.NewService(storage.NewPaths(root), "namespace-test").Generate(profile, nil)
	if err != nil {
		return err
	}
	generated["log"] = map[string]any{"level": "debug", "timestamp": true}
	for _, value := range generated["inbounds"].([]any) {
		inbound := value.(map[string]any)
		if inbound["type"] == "tun" {
			// Test topology only: intercept the LAN, not fixture control traffic.
			inbound["include_interface"] = []string{"lan0"}
		}
	}
	data, err = json.MarshalIndent(generated, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), data, 0600); err != nil {
		return err
	}
	redacted, err := json.MarshalIndent(config.RedactGeneratedConfig(generated), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "config.redacted.json"), redacted, 0600)
}
