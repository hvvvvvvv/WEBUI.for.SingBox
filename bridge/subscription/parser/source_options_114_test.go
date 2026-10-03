package parser

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func TestResolverAndDialOptionsAcrossInputs(t *testing.T) {
	t.Parallel()
	options := "domain_resolver=node-dns&ip_version=prefer-ipv6&bind_interface=eth0&connect_timeout=2s&routing_mark=123&tcp_multi_path=true&udp_fragment=false&tcp_fast_open=true&detour=another-proxy"
	vmess, err := json.Marshal(map[string]any{"add": "vmess.example.com", "port": 443, "id": testUUID, "domain_resolver": map[string]any{"server": "node-dns", "timeout": "3s"}, "domain_strategy": "ipv6"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		raw      string
		resolver any
		dial     bool
	}{
		{name: "HTTP URI", raw: "http://proxy.example.com:8080?" + options + "#node", resolver: map[string]any{"server": "node-dns", "strategy": "prefer_ipv6"}, dial: true},
		{name: "Hysteria2 URI", raw: "hy2://password@proxy.example.com:443?" + options + "#node", resolver: map[string]any{"server": "node-dns", "strategy": "prefer_ipv6"}, dial: true},
		{name: "Surge HTTP", raw: "node = http, proxy.example.com, 8080, domain-resolver=node-dns, ip-version=prefer-ipv6, bind-interface=eth0, connect-timeout=2s, routing-mark=123, tcp-multi-path=true, udp-fragment=false, tcp-fast-open=true, detour=another-proxy", resolver: map[string]any{"server": "node-dns", "strategy": "prefer_ipv6"}, dial: true},
		{name: "Surge resolver object", raw: "node = http, proxy.example.com, 8080, domain-resolver={server: 'node-dns', timeout: '3s'}, domain-strategy=ipv6", resolver: map[string]any{"server": "node-dns", "timeout": "3s", "strategy": "ipv6_only"}},
		{name: "URI resolver object", raw: "http://proxy.example.com:8080?" + url.Values{"domain_resolver": {`{"server":"node-dns","timeout":"3s"}`}, "domain_strategy": {"ipv6"}}.Encode(), resolver: map[string]any{"server": "node-dns", "timeout": "3s", "strategy": "ipv6_only"}},
		{name: "Loon direct", raw: "node = direct, domain_resolver=node-dns, domain_strategy=ipv4", resolver: map[string]any{"server": "node-dns", "strategy": "ipv4_only"}},
		{name: "YAML", raw: "proxies:\n  - name: node\n    type: http\n    server: proxy.example.com\n    port: 8080\n    domain-resolver:\n      server: node-dns\n      timeout: 3s\n    domain-strategy: ipv6\n", resolver: map[string]any{"server": "node-dns", "timeout": "3s", "strategy": "ipv6_only"}},
		{name: "JSON5", raw: "{proxies: [{name: 'node', type: 'http', server: 'proxy.example.com', port: 8080, domain_resolver: {server: 'node-dns', timeout: '3s'}, domain_strategy: 'ipv6'}]}", resolver: map[string]any{"server": "node-dns", "timeout": "3s", "strategy": "ipv6_only"}},
		{name: "VMess JSON payload", raw: "vmess://" + base64.RawStdEncoding.EncodeToString(vmess), resolver: map[string]any{"server": "node-dns", "timeout": "3s", "strategy": "ipv6_only"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(tc.raw)
			if err != nil || result.Skipped != 0 || len(result.Outbounds) != 1 {
				t.Fatalf("Parse() = %#v, %v", result, err)
			}
			out := result.Outbounds[0]
			if !reflect.DeepEqual(out["domain_resolver"], tc.resolver) {
				t.Fatalf("domain_resolver = %#v, want %#v", out["domain_resolver"], tc.resolver)
			}
			if _, present := out["domain_strategy"]; present {
				t.Fatalf("legacy domain_strategy emitted: %#v", out)
			}
			if tc.dial {
				for key, want := range map[string]any{"bind_interface": "eth0", "connect_timeout": "2s", "routing_mark": 123, "tcp_multi_path": true, "udp_fragment": false, "tcp_fast_open": true, "detour": "another-proxy"} {
					if !reflect.DeepEqual(out[key], want) {
						t.Fatalf("%s = %#v, want %#v", key, out[key], want)
					}
				}
			}
		})
	}
}

func TestHysteria2NewOptionsAcrossInputs(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, raw string }{
		{name: "URI", raw: "hy2://password@hy2.example.com:443?obfs=gecko&obfs-password=gecko-password&obfs_min_packet_size=600&obfs_max_packet_size=1400&hop_interval=15-30&hop_interval_max=30s&up_mbps=80&down_mbps=120#migrated"},
		{name: "Surge", raw: "migrated = hysteria2, hy2.example.com, 443, password=password, obfs=gecko, obfs-password=gecko-password, obfs-min-packet-size=600, obfs-max-packet-size=1400, hop-interval=15-30, hop-interval-max=30s, up=80, down=120"},
		{name: "YAML nested Gecko", raw: "proxies:\n  - name: migrated\n    type: hysteria2\n    server: hy2.example.com\n    port: 443\n    password: password\n    obfs:\n      type: gecko\n      password: gecko-password\n      min_packet_size: 600\n      max_packet_size: 1400\n    hop-interval: 15-30\n    hop-interval-max: 30s\n    up: 80\n    down: 120\n"},
		{name: "JSON5 nested Gecko", raw: "{proxies: [{name: 'migrated', type: 'hysteria2', server: 'hy2.example.com', port: 443, password: 'password', obfs: {type: 'gecko', password: 'gecko-password', min_packet_size: 600, max_packet_size: 1400}, hop_interval: '15-30', hop_interval_max: '30s', up: 80, down: 120}]}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(tc.raw)
			if err != nil || result.Skipped != 0 || len(result.Outbounds) != 1 {
				t.Fatalf("Parse() = %#v, %v", result, err)
			}
			out := result.Outbounds[0]
			assertOutbound(t, out, outboundWant{tag: "migrated", typeName: "hysteria2", server: "hy2.example.com", port: 443, fields: map[string]any{"hop_interval": "15s", "hop_interval_max": "30s", "up_mbps": 80, "down_mbps": 120, "obfs.type": "gecko", "obfs.password": "gecko-password", "obfs.min_packet_size": 600, "obfs.max_packet_size": 1400}})
		})
	}
}

func TestSnellOptionsAcrossPlatformInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, raw string
		version   int
		fields    map[string]any
	}{
		{name: "v4", raw: "snell-v4 = snell, snell.example.com, 443, psk=password, version=4, user-key=user, reuse=false, obfs=tls, obfs-host=cdn.example.com, udp=false", version: 4, fields: map[string]any{"userkey": "user", "reuse": false, "obfs_mode": "tls", "obfs_host": "cdn.example.com", "network": "tcp"}},
		{name: "v5 non QUIC", raw: "snell-v5 = snell, snell.example.com, 443, psk=password, version=5, quic=false, reuse=true, obfs=http, obfs-host=cdn.example.com", version: 4, fields: map[string]any{"reuse": true, "obfs_mode": "http", "obfs_host": "cdn.example.com"}},
		{name: "v6", raw: "snell-v6 = snell, snell.example.com, 443, psk=123456789012, version=6, userkey=user, mode=unsafe-raw, reuse=true, network=tcp", version: 6, fields: map[string]any{"userkey": "user", "mode": "unsafe-raw", "reuse": true, "network": "tcp"}},
		{name: "v4 obfs options alias", raw: "snell-v4 = snell, snell.example.com, 443, psk=password, version=4, obfs-options={mode:'http', host:'cdn.example.com'}", version: 4, fields: map[string]any{"obfs_mode": "http", "obfs_host": "cdn.example.com"}},
		{name: "quoted v6 credentials", raw: `snell-v6 = snell, snell.example.com, 443, psk=" abcdefghijk ", version=6, userkey=" user "`, version: 6, fields: map[string]any{"psk": " abcdefghijk ", "userkey": " user "}},
		{name: "quoted positional v6 PSK", raw: `snell-v6 = snell, snell.example.com, 443, " abcdefghijk ", version=6`, version: 6, fields: map[string]any{"psk": " abcdefghijk "}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(tc.raw)
			if err != nil || result.Skipped != 0 || len(result.Outbounds) != 1 {
				t.Fatalf("Parse() = %#v, %v", result, err)
			}
			if result.Outbounds[0]["version"] != tc.version {
				t.Fatalf("version = %#v, want %d", result.Outbounds[0]["version"], tc.version)
			}
			for field, want := range tc.fields {
				if !reflect.DeepEqual(result.Outbounds[0][field], want) {
					t.Fatalf("%s = %#v, want %#v", field, result.Outbounds[0][field], want)
				}
			}
		})
	}
}

func TestSourceOptionsPreserveSnellQUICAndInvalidHysteria2Intervals(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		"snell = snell, snell.example.com, 443, psk=password, version=5, quic=true",
		"snell = snell, snell.example.com, 443, psk=password, version=4, transport=quic",
		"snell = snell, snell.example.com, 443, psk=password, version=5, use-quic=true",
		"snell = snell, snell.example.com, 443, psk=password, version=5, quic-mode=true",
		"snell = snell, snell.example.com, 443, psk=password, version=4, plugin=shadow-tls",
		"snell = snell, snell.example.com, 443, psk=password, version=4, shadow-tls-password=secret",
		"snell = snell, snell.example.com, 443, psk=password, version=4, restls-script=unsupported",
		"snell = snell, snell.example.com, 443, psk=password, version=4, jls-mode=unsupported",
		"snell = snell, snell.example.com, 443, psk=123456789012, version=6, obfs-options={mode:'http'}",
		"hy2 = hysteria2, hy2.example.com, 443, password=password, hop-interval-max=30s",
		"hy2://password@hy2.example.com:443?" + url.Values{"hop_interval": {"15-30"}, "hop_interval_max": {"45s"}}.Encode(),
	} {
		result, err := Parse(line)
		if err == nil || result.Skipped != 1 || len(result.Outbounds) != 0 {
			t.Fatalf("invalid source accepted: result %#v, error %v", result, err)
		}
	}
}
