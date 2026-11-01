//go:build ignore

// Generate the isolated Linux namespace fixture with the production generator.
// Usage (repository root): go run tests/compat/router-transparent.go OUTPUT_DIR
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
	"guiforcores/bridge/config"
	"guiforcores/bridge/storage"
	"guiforcores/gen/native/daemon"
	"guiforcores/gen/native/daemon/daemonconnect"
	profilev1 "guiforcores/gen/profile/v1"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "mode" {
		if len(os.Args) != 4 {
			return fmt.Errorf("usage: probe mode CONFIG_PATH rule|global|direct")
		}
		return setMode(os.Args[2], os.Args[3])
	}
	return generate()
}

// The native core API speaks gRPC-Web, not Connect's JSON protocol. Use the
// same generated client and bearer credential as the real lifecycle tests.
func setMode(configPath, mode string) error {
	if mode != "rule" && mode != "global" && mode != "direct" {
		return fmt.Errorf("invalid core mode")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var generated struct {
		Services []struct {
			Type   string `json:"type"`
			Tag    string `json:"tag"`
			Listen string `json:"listen"`
			Port   uint16 `json:"listen_port"`
			Secret string `json:"secret"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &generated); err != nil {
		return err
	}
	for _, service := range generated.Services {
		if service.Type != "api" || service.Tag != config.CoreAPIServiceTag {
			continue
		}
		if !net.ParseIP(service.Listen).IsLoopback() || service.Port == 0 || service.Secret == "" {
			return fmt.Errorf("fixture native API must have a loopback listener and credential")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		transport := &http.Transport{Proxy: nil}
		defer transport.CloseIdleConnections()
		client := daemonconnect.NewStartedServiceClient(&http.Client{Transport: transport, Timeout: 3 * time.Second},
			"http://"+net.JoinHostPort(service.Listen, strconv.Itoa(int(service.Port))), connect.WithGRPCWeb())
		request := connect.NewRequest(&daemon.ClashMode{Mode: mode})
		request.Header().Set("Authorization", "Bearer "+service.Secret)
		if _, err := client.SetClashMode(ctx, request); err != nil {
			return fmt.Errorf("set native core mode: %w", err)
		}
		statusRequest := connect.NewRequest(&emptypb.Empty{})
		statusRequest.Header().Set("Authorization", "Bearer "+service.Secret)
		status, err := client.GetClashModeStatus(ctx, statusRequest)
		if err != nil {
			return fmt.Errorf("read native core mode: %w", err)
		}
		if !strings.EqualFold(status.Msg.GetCurrentMode(), mode) {
			return fmt.Errorf("native mode did not change to %s", mode)
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"mode": mode})
	}
	return fmt.Errorf("generated fixture has no managed native API service")
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
