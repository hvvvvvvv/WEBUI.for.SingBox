package parser

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SING_BOX_114_PATH enables acceptance checks against the fixed 1.14.0 core.
// Ordinary unit tests do not download or require a core binary.
func TestConvertedSubscriptionWithSingBox114(t *testing.T) {
	corePath := os.Getenv("SING_BOX_114_PATH")
	if corePath == "" {
		t.Skip("set SING_BOX_114_PATH to a sing-box 1.14.0 binary to run core checks")
	}
	runCore := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, corePath, args...)
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "ENABLE_DEPRECATED_MISSING_DOMAIN_RESOLVER=") {
				command.Env = append(command.Env, env)
			}
		}
		return command.CombinedOutput()
	}
	version, err := runCore("version")
	if err != nil || strings.SplitN(string(version), "\n", 2)[0] != "sing-box version 1.14.0" {
		t.Fatalf("expected sing-box 1.14.0, got %s (error=%v)", version, err)
	}
	tests := []struct {
		name        string
		body        string
		global      bool
		wantFailure bool
	}{
		{
			name: "explicit-node-resolver",
			body: `proxies: [{name: proxy, type: socks5, server: proxy.example.org, port: 1080, domain-resolver: dns-primary, domain-strategy: prefer-ipv4}]`,
		},
		{
			name:   "global-default-inheritance",
			body:   `proxies: [{name: proxy, type: socks5, server: proxy.example.org, port: 1080, ip-version: ipv4}]`,
			global: true,
		},
		{
			name:        "core-rejects-missing-required-resolver",
			body:        `proxies: [{name: proxy, type: socks5, server: proxy.example.org, port: 1080, ip-version: ipv4}]`,
			wantFailure: true,
		},
		{
			name:   "snell-hopping-and-gecko",
			global: true,
			body: `proxies:
  - {name: snell4-none, type: snell, server: proxy.example.org, port: 443, psk: password, version: 4, obfs: none}
  - {name: snell4-http, type: snell, server: proxy.example.org, port: 443, psk: password, version: 4, obfs-opts: {mode: http, host: example.org}, udp: false}
  - {name: snell4-tls, type: snell, server: proxy.example.org, port: 443, psk: password, version: 4, obfs: tls, obfs-host: example.org}
  - {name: snell5, type: snell, server: proxy.example.org, port: 443, psk: password, version: 5, quic: false, reuse: true}
  - {name: snell6-default, type: snell, server: proxy.example.org, port: 443, psk: password-123, version: 6, userkey: user, mode: default}
  - {name: snell6-unshaped, type: snell, server: proxy.example.org, port: 443, psk: password-123, version: 6, mode: unshaped, reuse: true}
  - {name: snell6-raw, type: snell, server: proxy.example.org, port: 443, psk: password-123, version: 6, mode: unsafe-raw}
  - {name: hy2-fixed, type: hysteria2, server: proxy.example.org, port: 443, password: password, up: 100 Mbps, down: 1Gbps, hop-interval: 5s}
  - {name: hy2-gecko-default, type: hysteria2, server: proxy.example.org, port: 443, password: password, ports: '443,444-450', hop-interval: 15-30, obfs: {type: gecko, password: secret}}
  - {name: hy2-gecko-size, type: hysteria2, server: proxy.example.org, port: 443, password: password, ports: '443-450', hop-interval: 15, hop-interval-max: 30, obfs: {type: gecko, password: secret, min_packet_size: 512, max_packet_size: 2048}}
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := Parse(tc.body)
			if err != nil || parsed.Skipped != 0 {
				t.Fatalf("failed to convert core fixture: result=%#v, error=%v", parsed, err)
			}
			route := map[string]any{"final": parsed.Outbounds[0]["tag"]}
			if tc.global {
				route["default_domain_resolver"] = map[string]any{"server": "dns-primary", "strategy": "prefer_ipv6"}
			}
			config := map[string]any{
				"log": map[string]any{"disabled": true},
				"dns": map[string]any{
					"servers": []map[string]any{
						{"type": "udp", "tag": "dns-primary", "server": "1.1.1.1"},
						{"type": "udp", "tag": "dns-secondary", "server": "8.8.8.8"},
					},
					"final": "dns-primary",
				},
				"inbounds":  []map[string]any{{"type": "mixed", "listen": "127.0.0.1", "listen_port": 1080}},
				"outbounds": parsed.Outbounds,
				"route":     route,
			}
			data, err := json.MarshalIndent(config, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			output, checkErr := runCore("check", "-c", configPath)
			if tc.wantFailure {
				if checkErr == nil || !strings.Contains(string(output), "missing") || !strings.Contains(string(output), "domain_resolver") {
					t.Fatalf("core should report the missing resolver: error=%v, output=%s", checkErr, output)
				}
			} else if checkErr != nil {
				t.Fatalf("core rejected converted config: error=%v, output=%s", checkErr, output)
			}
		})
	}
}
