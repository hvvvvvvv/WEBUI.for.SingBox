package parser

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const testUUID = "123e4567-e89b-12d3-a456-426614174000"

func TestParseBase64URILists(t *testing.T) {
	t.Parallel()

	// The non-ASCII fragment makes the standard and URL-safe alphabets differ,
	// while the odd payload length exercises padded and raw encodings.
	plain := strings.Join([]string{
		ssURI("aes-128-gcm", "base64-password", "ss.example.com", 8388, "Rocket🚀"),
		"socks5://base64-user:base64-pass@socks.example.com:1080#Base64%20SOCKS",
	}, "\n")
	encodings := []struct {
		name   string
		encode func([]byte) string
	}{
		{name: "standard padded", encode: base64.StdEncoding.EncodeToString},
		{name: "standard raw", encode: base64.RawStdEncoding.EncodeToString},
		{name: "URL-safe padded", encode: base64.URLEncoding.EncodeToString},
		{name: "URL-safe raw", encode: base64.RawURLEncoding.EncodeToString},
	}

	for _, tc := range encodings {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse("\ufeff  " + tc.encode([]byte(plain)) + "\n")
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if result.Total != 2 || result.Skipped != 0 || len(result.Outbounds) != 2 {
				t.Fatalf("Parse() counts = total %d, skipped %d, outbounds %d; want 2, 0, 2", result.Total, result.Skipped, len(result.Outbounds))
			}
			assertOutbound(t, outboundWithTag(t, result, "Rocket🚀"), outboundWant{
				typeName: "shadowsocks",
				server:   "ss.example.com",
				port:     8388,
				fields: map[string]any{
					"method":   "aes-128-gcm",
					"password": "base64-password",
				},
			})
			assertOutbound(t, outboundWithTag(t, result, "Base64 SOCKS"), outboundWant{
				typeName: "socks",
				server:   "socks.example.com",
				port:     1080,
				fields: map[string]any{
					"username": "base64-user",
					"password": "base64-pass",
				},
			})
		})
	}
}

func TestParseURIProtocols(t *testing.T) {
	t.Parallel()

	vmessJSON, err := json.Marshal(map[string]any{
		"v":    "2",
		"ps":   "VMess WS",
		"add":  "vmess.example.com",
		"port": "443",
		"id":   testUUID,
		"aid":  "0",
		"scy":  "auto",
		"net":  "ws",
		"type": "none",
		"host": "cdn.example.com",
		"path": "/vmess",
		"tls":  "tls",
		"sni":  "origin.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	quantumultVMess := "Quantum VMess = vmess, quantum.example.com, 443, auto, \"" + testUUID + "\", obfs=wss, obfs-path=\"/quantum\", obfs-header=\"Host: quantum-cdn.example.com\", tls-verification=false"
	shadowrocketVMessCredentials := base64.RawURLEncoding.EncodeToString([]byte("auto:" + testUUID + "@shadow.example.com:443"))

	tests := []struct {
		name string
		raw  string
		want outboundWant
	}{
		{
			name: "Shadowsocks SIP002",
			raw:  ssURI("chacha20-ietf-poly1305", "ss-password", "ss.example.com", 8443, "SS SIP002"),
			want: outboundWant{
				tag: "SS SIP002", typeName: "shadowsocks", server: "ss.example.com", port: 8443,
				fields: map[string]any{"method": "chacha20-ietf-poly1305", "password": "ss-password"},
			},
		},
		{
			name: "Shadowsocks legacy full payload",
			raw:  "ss://" + base64.RawStdEncoding.EncodeToString([]byte("aes-256-gcm:legacy-password@legacy-ss.example.com:8389")) + "#Legacy%20SS",
			want: outboundWant{
				tag: "Legacy SS", typeName: "shadowsocks", server: "legacy-ss.example.com", port: 8389,
				fields: map[string]any{"method": "aes-256-gcm", "password": "legacy-password"},
			},
		},
		{
			name: "VMess WebSocket TLS",
			raw:  "vmess://" + base64.RawStdEncoding.EncodeToString(vmessJSON),
			want: outboundWant{
				tag: "VMess WS", typeName: "vmess", server: "vmess.example.com", port: 443,
				fields: map[string]any{
					"uuid":                   testUUID,
					"security":               "auto",
					"tls.enabled":            true,
					"tls.server_name":        "origin.example.com",
					"transport.type":         "ws",
					"transport.path":         "/vmess",
					"transport.headers.Host": "cdn.example.com",
				},
			},
		},
		{
			name: "VMess Quantumult URI",
			raw:  "vmess://" + base64.RawStdEncoding.EncodeToString([]byte(quantumultVMess)),
			want: outboundWant{
				tag: "Quantum VMess", typeName: "vmess", server: "quantum.example.com", port: 443,
				fields: map[string]any{
					"uuid":                   testUUID,
					"security":               "auto",
					"tls.enabled":            true,
					"tls.insecure":           true,
					"transport.type":         "ws",
					"transport.path":         "/quantum",
					"transport.headers.Host": "quantum-cdn.example.com",
				},
			},
		},
		{
			name: "VMess Shadowrocket URI",
			raw:  "vmess://" + shadowrocketVMessCredentials + "?remark=Shadowrocket%20VMess&tls=1&sni=shadow-sni.example.com&obfs=websocket&path=%2Fshadow&host=shadow-cdn.example.com",
			want: outboundWant{
				tag: "Shadowrocket VMess", typeName: "vmess", server: "shadow.example.com", port: 443,
				fields: map[string]any{
					"uuid":                   testUUID,
					"security":               "auto",
					"tls.enabled":            true,
					"tls.server_name":        "shadow-sni.example.com",
					"transport.type":         "ws",
					"transport.path":         "/shadow",
					"transport.headers.Host": "shadow-cdn.example.com",
				},
			},
		},
		{
			name: "VLESS Reality WebSocket",
			raw:  "vless://" + testUUID + "@vless.example.com:443?encryption=none&security=reality&sni=reality.example.com&fp=chrome&pbk=PUBLIC-KEY&sid=abcd1234&type=ws&host=cdn.example.com&path=%2Fvless#VLESS%20Reality",
			want: outboundWant{
				tag: "VLESS Reality", typeName: "vless", server: "vless.example.com", port: 443,
				fields: map[string]any{
					"uuid":                   testUUID,
					"tls.enabled":            true,
					"tls.server_name":        "reality.example.com",
					"tls.reality.enabled":    true,
					"tls.reality.public_key": "PUBLIC-KEY",
					"tls.reality.short_id":   "abcd1234",
					"tls.utls.enabled":       true,
					"tls.utls.fingerprint":   "chrome",
					"transport.type":         "ws",
					"transport.path":         "/vless",
					"transport.headers.Host": "cdn.example.com",
				},
			},
		},
		{
			name: "Trojan gRPC",
			raw:  "trojan://trojan-password@trojan.example.com:443?security=tls&sni=trojan-sni.example.com&type=grpc&serviceName=trojan-service#Trojan%20gRPC",
			want: outboundWant{
				tag: "Trojan gRPC", typeName: "trojan", server: "trojan.example.com", port: 443,
				fields: map[string]any{
					"password":               "trojan-password",
					"tls.enabled":            true,
					"tls.server_name":        "trojan-sni.example.com",
					"transport.type":         "grpc",
					"transport.service_name": "trojan-service",
				},
			},
		},
		{
			name: "Hysteria 1",
			raw:  "hysteria://hy.example.com:443?auth=hy-auth&sni=hy-sni.example.com&insecure=1&upmbps=12&downmbps=34&alpn=h3#Hysteria%201",
			want: outboundWant{
				tag: "Hysteria 1", typeName: "hysteria", server: "hy.example.com", port: 443,
				fields: map[string]any{
					"auth_str":        "hy-auth",
					"up_mbps":         12,
					"down_mbps":       34,
					"tls.enabled":     true,
					"tls.server_name": "hy-sni.example.com",
					"tls.insecure":    true,
				},
			},
		},
		{
			name: "Hysteria 2 obfuscation",
			raw:  "hysteria2://hy2-password@hy2.example.com:8443?sni=hy2-sni.example.com&insecure=1&obfs=salamander&obfs-password=obfs-secret#Hysteria%202",
			want: outboundWant{
				tag: "Hysteria 2", typeName: "hysteria2", server: "hy2.example.com", port: 8443,
				fields: map[string]any{
					"password":        "hy2-password",
					"tls.enabled":     true,
					"tls.server_name": "hy2-sni.example.com",
					"tls.insecure":    true,
					"obfs.type":       "salamander",
					"obfs.password":   "obfs-secret",
				},
			},
		},
		{
			name: "TUIC",
			raw:  "tuic://" + testUUID + ":tuic-password@tuic.example.com:443?sni=tuic-sni.example.com&alpn=h3&congestion_control=bbr&udp_relay_mode=native#TUIC",
			want: outboundWant{
				tag: "TUIC", typeName: "tuic", server: "tuic.example.com", port: 443,
				fields: map[string]any{
					"uuid":               testUUID,
					"password":           "tuic-password",
					"congestion_control": "bbr",
					"udp_relay_mode":     "native",
					"tls.enabled":        true,
					"tls.server_name":    "tuic-sni.example.com",
				},
			},
		},
		{
			name: "AnyTLS",
			raw:  "anytls://anytls-password@anytls.example.com:443?sni=anytls-sni.example.com&insecure=1#AnyTLS",
			want: outboundWant{
				tag: "AnyTLS", typeName: "anytls", server: "anytls.example.com", port: 443,
				fields: map[string]any{
					"password":        "anytls-password",
					"tls.enabled":     true,
					"tls.server_name": "anytls-sni.example.com",
					"tls.insecure":    true,
				},
			},
		},
		{
			name: "HTTP authentication",
			raw:  "http://http-user:http-password@http.example.com:8080#HTTP",
			want: outboundWant{
				tag: "HTTP", typeName: "http", server: "http.example.com", port: 8080,
				fields: map[string]any{"username": "http-user", "password": "http-password"},
			},
		},
		{
			name: "HTTPS enables TLS",
			raw:  "https://https-user:https-password@https.example.com:443#HTTPS",
			want: outboundWant{
				tag: "HTTPS", typeName: "http", server: "https.example.com", port: 443,
				fields: map[string]any{"username": "https-user", "password": "https-password", "tls.enabled": true},
			},
		},
		{
			name: "SOCKS5 authentication",
			raw:  "socks5://socks-user:socks-password@socks.example.com:1080#SOCKS5",
			want: outboundWant{
				tag: "SOCKS5", typeName: "socks", server: "socks.example.com", port: 1080,
				fields: map[string]any{"username": "socks-user", "password": "socks-password", "version": "5"},
			},
		},
		{
			name: "SOCKS5 IPv6 endpoint",
			raw:  "socks5://ipv6-user:ipv6-password@[2001:db8::1]:1081#SOCKS5%20IPv6",
			want: outboundWant{
				tag: "SOCKS5 IPv6", typeName: "socks", server: "2001:db8::1", port: 1081,
				fields: map[string]any{"username": "ipv6-user", "password": "ipv6-password", "version": "5"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(tc.raw)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if result.Total != 1 || result.Skipped != 0 || len(result.Outbounds) != 1 {
				t.Fatalf("Parse() counts = total %d, skipped %d, outbounds %d; want 1, 0, 1", result.Total, result.Skipped, len(result.Outbounds))
			}
			assertOutbound(t, result.Outbounds[0], tc.want)
		})
	}
}

func TestParseClashAndMihomoDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want []outboundWant
	}{
		{
			name: "Clash YAML",
			raw: `
proxies:
  - name: Clash SS
    type: ss
    server: clash-ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: clash-password
  - name: Clash VLESS
    type: vless
    server: clash-vless.example.com
    port: 443
    uuid: 123e4567-e89b-12d3-a456-426614174000
    network: ws
    tls: true
    servername: clash-sni.example.com
    ws-opts:
      path: /clash
      headers:
        Host: clash-cdn.example.com
`,
			want: []outboundWant{
				{tag: "Clash SS", typeName: "shadowsocks", server: "clash-ss.example.com", port: 8388, fields: map[string]any{"method": "aes-256-gcm", "password": "clash-password"}},
				{tag: "Clash VLESS", typeName: "vless", server: "clash-vless.example.com", port: 443, fields: map[string]any{
					"uuid":                   testUUID,
					"tls.enabled":            true,
					"tls.server_name":        "clash-sni.example.com",
					"transport.type":         "ws",
					"transport.path":         "/clash",
					"transport.headers.Host": "clash-cdn.example.com",
				}},
			},
		},
		{
			name: "Mihomo JSON",
			raw: `{
  "proxies": [
    {
      "name": "Mihomo Hy2",
      "type": "hysteria2",
      "server": "mihomo-hy2.example.com",
      "port": 8443,
      "password": "mihomo-password",
      "sni": "mihomo-sni.example.com",
      "skip-cert-verify": true,
      "obfs": "salamander",
      "obfs-password": "mihomo-obfs"
    }
  ]
}`,
			want: []outboundWant{
				{tag: "Mihomo Hy2", typeName: "hysteria2", server: "mihomo-hy2.example.com", port: 8443, fields: map[string]any{
					"password":        "mihomo-password",
					"tls.enabled":     true,
					"tls.server_name": "mihomo-sni.example.com",
					"tls.insecure":    true,
					"obfs.type":       "salamander",
					"obfs.password":   "mihomo-obfs",
				}},
			},
		},
		{
			name: "JSON5 Clash object",
			raw: `{
  // Exercise JSON5-only hexadecimal numbers and a continued string.
  proxies: [
    {
      name: 'JSON5 \
HTTP',
      type: 'http',
      server: 'json5.example.com',
      port: 0x1f90,
      username: 'json5-user',
      password: 'json5-password',
    },
  ],
}`,
			want: []outboundWant{
				{tag: "JSON5 HTTP", typeName: "http", server: "json5.example.com", port: 8080, fields: map[string]any{
					"username": "json5-user",
					"password": "json5-password",
				}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(tc.raw)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if result.Skipped != 0 || len(result.Outbounds) != len(tc.want) {
				t.Fatalf("Parse() counts = total %d, skipped %d, outbounds %d; want skipped 0 and %d outbounds", result.Total, result.Skipped, len(result.Outbounds), len(tc.want))
			}
			for _, want := range tc.want {
				assertOutbound(t, outboundWithTag(t, result, want.tag), want)
			}
		})
	}
}

func TestParseFullClashYAMLWithLeadingGlobalKeys(t *testing.T) {
	t.Parallel()

	raw := `mixed-port: 7890
mode: rule
dns:
  enable: true
proxies:
  - name: Full Clash SS
    type: ss
    server: full-clash.example.com
    port: 8388
    cipher: aes-128-gcm
    password: full-clash-password
proxy-groups:
  - name: Proxy
    type: select
    proxies: [Full Clash SS]
`
	inputs := map[string]string{
		"plain":          raw,
		"base64 wrapped": base64.RawStdEncoding.EncodeToString([]byte(raw)),
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			result, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if result.Total != 1 || result.Skipped != 0 || len(result.Outbounds) != 1 {
				t.Fatalf("Parse() counts = total %d, skipped %d, outbounds %d; want 1, 0, 1", result.Total, result.Skipped, len(result.Outbounds))
			}
			assertOutbound(t, result.Outbounds[0], outboundWant{
				tag: "Full Clash SS", typeName: "shadowsocks", server: "full-clash.example.com", port: 8388,
				fields: map[string]any{"method": "aes-128-gcm", "password": "full-clash-password"},
			})
		})
	}
}

func TestParseClientLineFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want outboundWant
	}{
		{
			name: "Quantumult X",
			raw:  `shadowsocks=qx.example.com:8388, method=aes-128-gcm, password="qx,password", obfs=tls, obfs-host=qx-obfs.example.com, udp-relay=true, tag=QX SS`,
			want: outboundWant{tag: "QX SS", typeName: "shadowsocks", server: "qx.example.com", port: 8388, fields: map[string]any{
				"method": "aes-128-gcm", "password": "qx,password", "plugin": "obfs-local", "plugin_opts": "obfs=tls;obfs-host=qx-obfs.example.com",
			}},
		},
		{
			name: "Loon",
			raw:  "Loon SS = Shadowsocks, loon.example.com, 8388, aes-256-gcm, loon-password, udp=true",
			want: outboundWant{tag: "Loon SS", typeName: "shadowsocks", server: "loon.example.com", port: 8388, fields: map[string]any{
				"method": "aes-256-gcm", "password": "loon-password",
			}},
		},
		{
			name: "Surge",
			raw:  "Surge HTTP = http, surge.example.com, 8080, username=surge-user, password=surge-password",
			want: outboundWant{tag: "Surge HTTP", typeName: "http", server: "surge.example.com", port: 8080, fields: map[string]any{
				"username": "surge-user", "password": "surge-password",
			}},
		},
		{
			name: "Surge HTTPS",
			raw:  "Surge HTTPS = https, surge-tls.example.com, 443, username=surge-user, password=surge-password, sni=surge-sni.example.com",
			want: outboundWant{tag: "Surge HTTPS", typeName: "http", server: "surge-tls.example.com", port: 443, fields: map[string]any{
				"username": "surge-user", "password": "surge-password", "tls.enabled": true, "tls.server_name": "surge-sni.example.com",
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(tc.raw)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if result.Total != 1 || result.Skipped != 0 || len(result.Outbounds) != 1 {
				t.Fatalf("Parse() counts = total %d, skipped %d, outbounds %d; want 1, 0, 1", result.Total, result.Skipped, len(result.Outbounds))
			}
			assertOutbound(t, result.Outbounds[0], tc.want)
		})
	}
}

func TestProducerAdvancedTransportAndProtocolFields(t *testing.T) {
	t.Parallel()

	result, err := Parse(`proxies:
  - name: Hysteria Ports
    type: hysteria
    server: hy-ports.example.com
    port: 443
    ports: 1000-2000
    auth-str: auth
    up: 12
    down: 34
    sni: hy-ports-sni.example.com
  - name: VMess H2 Mux
    type: vmess
    server: vmess-h2.example.com
    port: 443
    uuid: 123e4567-e89b-12d3-a456-426614174000
    cipher: auto
    network: h2
    udp: false
    tls: true
    h2-opts:
      host: [h2-cdn.example.com]
      path: /h2
    smux:
      enabled: true
      protocol: h2mux
      max-streams: 8
  - name: VLESS HTTPUpgrade
    type: vless
    server: upgrade.example.com
    port: 443
    uuid: 123e4567-e89b-12d3-a456-426614174000
    network: httpupgrade
    tls: true
    httpupgrade-opts:
      host: upgrade-cdn.example.com
      path: /upgrade
`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if result.Skipped != 0 || len(result.Outbounds) != 3 {
		t.Fatalf("Parse() counts = total %d, skipped %d, outbounds %d; want 3, 0, 3", result.Total, result.Skipped, len(result.Outbounds))
	}

	hysteria := outboundWithTag(t, result, "Hysteria Ports")
	assertPathValue(t, hysteria, "type", "hysteria")
	assertPathValue(t, hysteria, "server", "hy-ports.example.com")
	assertPathValue(t, hysteria, "server_ports", []string{"1000:2000"})
	assertPathValue(t, hysteria, "up_mbps", 12)
	assertPathValue(t, hysteria, "down_mbps", 34)
	if _, exists := hysteria["server_port"]; exists {
		t.Fatalf("Hysteria port-hopping outbound must not retain conflicting server_port: %#v", hysteria)
	}

	assertOutbound(t, outboundWithTag(t, result, "VMess H2 Mux"), outboundWant{
		tag: "VMess H2 Mux", typeName: "vmess", server: "vmess-h2.example.com", port: 443,
		fields: map[string]any{
			"network":               "tcp",
			"transport.type":        "http",
			"transport.host":        []string{"h2-cdn.example.com"},
			"transport.path":        "/h2",
			"multiplex.enabled":     true,
			"multiplex.protocol":    "h2mux",
			"multiplex.max_streams": 8,
		},
	})
	assertOutbound(t, outboundWithTag(t, result, "VLESS HTTPUpgrade"), outboundWant{
		tag: "VLESS HTTPUpgrade", typeName: "vless", server: "upgrade.example.com", port: 443,
		fields: map[string]any{
			"transport.type": "httpupgrade",
			"transport.host": "upgrade-cdn.example.com",
			"transport.path": "/upgrade",
		},
	})
}

func TestHysteria2ProducerUsesStableBandwidthSchema(t *testing.T) {
	t.Parallel()

	result, err := Parse(`proxies:
  - name: Stable Hysteria2
    type: hysteria2
    server: hy2-stable.example.com
    port: 443
    password: hy2-password
    up: 100 Mbps
    down: 1Gbps
    hop-interval: 30
    hop-interval-max: 60s
    obfs:
      type: salamander
      password: obfs-password
`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if result.Skipped != 0 || len(result.Outbounds) != 1 {
		t.Fatalf("Parse() counts = total %d, skipped %d, outbounds %d; want 1, 0, 1", result.Total, result.Skipped, len(result.Outbounds))
	}
	outbound := result.Outbounds[0]
	assertPathValue(t, outbound, "up_mbps", 100)
	assertPathValue(t, outbound, "down_mbps", 1000)
	assertPathValue(t, outbound, "hop_interval", "30s")
	assertPathValue(t, outbound, "obfs.type", "salamander")
	for _, field := range []string{"up", "down", "hop_interval_max"} {
		if _, exists := outbound[field]; exists {
			t.Errorf("stable Hysteria2 outbound must not contain 1.13-incompatible field %q: %#v", field, outbound)
		}
	}
}

func TestProducerRejectsStableSchemaConflicts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node Node
	}{
		{
			name: "Hysteria2 Gecko obfuscation",
			node: Node{
				"name": "Gecko", "type": "hysteria2", "server": "gecko.example.com", "port": 443,
				"password": "password", "obfs": map[string]any{"type": "gecko", "password": "obfs-password"},
			},
		},
		{
			name: "Hysteria2 unsupported randomized hop interval",
			node: Node{
				"name": "Random hop", "type": "hysteria2", "server": "hop.example.com", "port": 443,
				"password": "password", "hop-interval": "15-30",
			},
		},
		{
			name: "TUIC UDP modes",
			node: Node{
				"name": "TUIC conflict", "type": "tuic", "server": "tuic.example.com", "port": 443,
				"uuid": testUUID, "password": "password", "udp-relay-mode": "native", "udp-over-stream": true,
			},
		},
		{
			name: "Shadowsocks UDP over TCP and multiplex",
			node: Node{
				"name": "SS conflict", "type": "ss", "server": "ss.example.com", "port": 8388,
				"cipher": "aes-128-gcm", "password": "password", "udp-over-tcp": true,
				"smux": map[string]any{"enabled": true},
			},
		},
		{
			name: "multiplex connection and stream limits",
			node: Node{
				"name": "Mux conflict", "type": "vmess", "server": "vmess.example.com", "port": 443,
				"uuid": testUUID,
				"smux": map[string]any{"enabled": true, "max-connections": 2, "max-streams": 8},
			},
		},
		{
			name: "Mihomo certificate fingerprint is not uTLS",
			node: Node{
				"name": "Pinned TLS", "type": "trojan", "server": "pinned.example.com", "port": 443,
				"password": "password", "fingerprint": "certificate-sha256-hash",
			},
		},
		{
			name: "Naive QUIC and insecure concurrency",
			node: Node{
				"name": "Naive conflict", "type": "naive", "server": "naive.example.com", "port": 443,
				"username": "user", "password": "password", "quic": true, "insecure-concurrency": 2,
			},
		},
		{
			name: "unsupported Mihomo Shadowsocks cipher",
			node: Node{
				"name": "SS CCM", "type": "ss", "server": "ss-ccm.example.com", "port": 8388,
				"cipher": "aes-128-ccm", "password": "password",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if outbound, err := produceNode(test.node); err == nil {
				t.Fatalf("produceNode() = %#v, want an incompatible-node error", outbound)
			}
		})
	}
}

func TestProducerDistinguishesSSHPrivateKeyPathAndContent(t *testing.T) {
	t.Parallel()

	pathOutbound, err := produceNode(Node{
		"name": "SSH path", "type": "ssh", "server": "ssh.example.com", "port": 22,
		"username": "root", "private-key": "/home/user/.ssh/id_ed25519",
	})
	if err != nil {
		t.Fatalf("produceNode(path) error = %v", err)
	}
	assertPathValue(t, pathOutbound, "private_key_path", "/home/user/.ssh/id_ed25519")
	if _, exists := pathOutbound["private_key"]; exists {
		t.Fatalf("SSH path was also emitted as inline key content: %#v", pathOutbound)
	}

	const keyContent = "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-data\n-----END OPENSSH PRIVATE KEY-----"
	contentOutbound, err := produceNode(Node{
		"name": "SSH content", "type": "ssh", "server": "ssh.example.com", "port": 22,
		"username": "root", "private-key": keyContent,
	})
	if err != nil {
		t.Fatalf("produceNode(content) error = %v", err)
	}
	assertPathValue(t, contentOutbound, "private_key", keyContent)
	if _, exists := contentOutbound["private_key_path"]; exists {
		t.Fatalf("SSH key content was also emitted as a path: %#v", contentOutbound)
	}
}

func TestProducerNormalizesUTLSAndAnyTLSDurations(t *testing.T) {
	t.Parallel()

	outbound, err := produceNode(Node{
		"name": "AnyTLS normalized", "type": "anytls", "server": "anytls.example.com", "port": 443,
		"password": "password", "client-fingerprint": "iOS",
		"idle-session-check-interval": 30, "idle-session-timeout": "60",
	})
	if err != nil {
		t.Fatalf("produceNode() error = %v", err)
	}
	assertPathValue(t, outbound, "tls.utls.fingerprint", "ios")
	assertPathValue(t, outbound, "idle_session_check_interval", "30s")
	assertPathValue(t, outbound, "idle_session_timeout", "60s")
}

func TestProducerNormalizesHeaderScalarsToStrings(t *testing.T) {
	t.Parallel()

	headers := stringMap(map[string]any{
		"X-Number": 123,
		"X-Bool":   true,
		"X-List":   []any{"first", 2, false},
		"X-Nested": map[string]any{"invalid": true},
	})
	want := map[string]any{
		"X-Number": "123",
		"X-Bool":   "true",
		"X-List":   []string{"first", "2", "false"},
	}
	if !reflect.DeepEqual(headers, want) {
		t.Fatalf("stringMap() = %#v, want %#v", headers, want)
	}
}

func TestParsePartialSuccessSkipsUnsupportedAndInvalid(t *testing.T) {
	t.Parallel()

	ss := ssURI("aes-128-gcm", "kept-password", "kept.example.com", 8388, "Kept SS")
	ssr := ssrURI("ssr.example.com", 443, "auth_sha1_v4", "aes-256-cfb", "tls1.2_ticket_auth", "ssr-password", "Skipped SSR")
	wireGuard := "wireguard://WG-PRIVATE-KEY@wg.example.com:51820?publickey=WG-PUBLIC-KEY&address=10.0.0.2%2F32#Skipped%20WireGuard"
	snell := "Skipped Snell = snell, snell.example.com, 443, psk=snell-password, version=4"
	raw := strings.Join([]string{ss, ssr, wireGuard, snell, "not-a-proxy://invalid-candidate"}, "\n")

	result, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(result.Outbounds) != 1 {
		t.Fatalf("len(Outbounds) = %d, want 1", len(result.Outbounds))
	}
	if result.Total != 5 || result.Skipped != 4 || len(result.Issues) != 4 {
		t.Fatalf("Parse() counts = total %d, skipped %d, issues %d; want 5, 4, 4", result.Total, result.Skipped, len(result.Issues))
	}
	assertOutbound(t, result.Outbounds[0], outboundWant{
		tag: "Kept SS", typeName: "shadowsocks", server: "kept.example.com", port: 8388,
		fields: map[string]any{"method": "aes-128-gcm", "password": "kept-password"},
	})
	if !issuesMention(result.Issues, "unsupported") {
		t.Fatalf("Issues = %#v, want an unsupported-protocol diagnostic", result.Issues)
	}
	for _, issue := range result.Issues {
		if strings.TrimSpace(issue.Parser) == "" || strings.TrimSpace(issue.Reason) == "" {
			t.Errorf("Issue = %#v, want a parser category and sanitized reason", issue)
		}
	}
	indices := map[int]bool{}
	for _, issue := range result.Issues {
		indices[issue.Index] = true
	}
	for _, want := range []int{2, 3, 4, 5} {
		if !indices[want] {
			t.Errorf("Issues = %#v, want original candidate index %d", result.Issues, want)
		}
	}
}

func TestParseAssignsUniqueStableOutboundTags(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		ssURI("aes-128-gcm", "password-1", "one.example.com", 8388, "Duplicate"),
		ssURI("aes-128-gcm", "password-2", "two.example.com", 8388, "Duplicate 2"),
		ssURI("aes-128-gcm", "password-3", "three.example.com", 8388, "Duplicate"),
		ssURI("aes-128-gcm", "password-4", "four.example.com", 8388, "Duplicate"),
	}, "\n")
	result, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := make([]string, 0, len(result.Outbounds))
	for _, outbound := range result.Outbounds {
		got = append(got, stringValue(outbound["tag"]))
	}
	want := []string{"Duplicate", "Duplicate 2", "Duplicate 3", "Duplicate 4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outbound tags = %#v, want %#v", got, want)
	}
}

func TestParseAllFailureAndDiagnosticsAreSanitized(t *testing.T) {
	t.Parallel()

	const (
		ssrSecret     = "SSR-SUPER-SECRET"
		unknownSecret = "UNKNOWN-SUPER-SECRET"
		unknownScheme = "SCHEME-SUPER-SECRET"
	)
	ssr := ssrURI("ssr.example.com", 443, "auth_sha1_v4", "aes-256-cfb", "tls1.2_ticket_auth", ssrSecret, "Unsupported SSR")
	unknown := unknownScheme + "://" + unknownSecret + "@private.example.com:443#Private"
	raw := strings.Join([]string{ssr, unknown}, "\n")

	result, err := Parse(raw)
	if err == nil {
		t.Fatal("Parse() error = nil, want failure")
	}
	var parseErr *Error
	if !errors.As(err, &parseErr) {
		t.Fatalf("Parse() error type = %T, want *Error", err)
	}
	if len(result.Outbounds) != 0 || result.Total != 2 || result.Skipped != 2 || len(result.Issues) != 2 {
		t.Fatalf("Parse() counts = outbounds %d, total %d, skipped %d, issues %d; want 0, 2, 2, 2", len(result.Outbounds), result.Total, result.Skipped, len(result.Issues))
	}
	if parseErr.Total != result.Total || len(parseErr.Issues) != len(result.Issues) {
		t.Fatalf("Error diagnostics = total %d, issues %d; result = total %d, issues %d", parseErr.Total, len(parseErr.Issues), result.Total, len(result.Issues))
	}

	diagnostics := err.Error() + " " + fmt.Sprint(result.Issues) + " " + fmt.Sprint(parseErr.Issues)
	for _, secret := range []string{ssrSecret, unknownSecret, unknownScheme, ssr, unknown, raw} {
		if strings.Contains(diagnostics, secret) {
			t.Errorf("diagnostics leaked source secret or candidate %q: %s", secret, diagnostics)
		}
	}
	if !issuesMention(result.Issues, "unsupported") {
		t.Fatalf("Issues = %#v, want an unsupported-protocol diagnostic", result.Issues)
	}
	for _, issue := range result.Issues {
		if strings.TrimSpace(issue.Parser) == "" || strings.TrimSpace(issue.Reason) == "" {
			t.Errorf("Issue = %#v, want a parser category and sanitized reason", issue)
		}
	}
}

func TestParseRejectsIncompleteRealityInsteadOfDowngradingTLS(t *testing.T) {
	result, err := Parse("vless://" + testUUID + "@reality.example.com:443?security=reality&sni=reality.example.com#Incomplete%20Reality")
	if err == nil {
		t.Fatal("Parse() error = nil, want incomplete Reality failure")
	}
	if len(result.Outbounds) != 0 || result.Total != 1 || result.Skipped != 1 {
		t.Fatalf("Parse() result = %#v, want one skipped Reality candidate", result)
	}
	if !issuesMention(result.Issues, "Reality public key") {
		t.Fatalf("Issues = %#v, want missing Reality public key diagnostic", result.Issues)
	}
}

func TestParseEmptySubscription(t *testing.T) {
	t.Parallel()

	result, err := Parse("\ufeff \r\n\t")
	if err == nil {
		t.Fatal("Parse() error = nil, want no-candidates failure")
	}
	var parseErr *Error
	if !errors.As(err, &parseErr) {
		t.Fatalf("Parse() error type = %T, want *Error", err)
	}
	if result.Total != 0 || result.Skipped != 0 || len(result.Outbounds) != 0 || len(result.Issues) != 0 {
		t.Fatalf("Parse() result = %#v, want an empty diagnostic result", result)
	}
	if parseErr.Total != 0 || len(parseErr.Issues) != 0 {
		t.Fatalf("Parse() error = %#v, want empty no-candidates diagnostics", parseErr)
	}
}

func TestParseRejectsCandidateCountOverLimit(t *testing.T) {
	lines := make([]string, maxSubscriptionCandidates+1)
	for index := range lines {
		lines[index] = "unsupported-candidate"
	}
	result, err := Parse(strings.Join(lines, "\n"))
	if err == nil {
		t.Fatal("Parse() error = nil, want candidate-limit failure")
	}
	if len(result.Outbounds) != 0 || result.Total != maxSubscriptionCandidates+1 {
		t.Fatalf("Parse() result = %#v, want no outbounds and total %d", result, maxSubscriptionCandidates+1)
	}
	if !issuesMention(result.Issues, candidateLimitReason) {
		t.Fatalf("Issues = %#v, want candidate limit diagnostic", result.Issues)
	}
}

func TestParseRejectsAggregateInlineDocumentCountOverLimit(t *testing.T) {
	countPerLine := maxSubscriptionCandidates/2 + 1
	candidates := make([]any, countPerLine)
	for index := range candidates {
		candidates[index] = map[string]any{"type": "direct", "name": "Direct"}
	}
	encoded, err := json.Marshal(candidates)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Parse(string(encoded) + "\n" + string(encoded))
	if err == nil {
		t.Fatal("Parse() error = nil, want aggregate candidate-limit failure")
	}
	wantTotal := countPerLine * 2
	if len(result.Outbounds) != 0 || result.Total != wantTotal {
		t.Fatalf("Parse() result has %d outbounds and total %d, want 0 and %d", len(result.Outbounds), result.Total, wantTotal)
	}
	if !issuesMention(result.Issues, candidateLimitReason) {
		t.Fatalf("Issues = %#v, want aggregate candidate limit diagnostic", result.Issues)
	}
}

type outboundWant struct {
	tag      string
	typeName string
	server   string
	port     int
	fields   map[string]any
}

func ssURI(method, password, host string, port int, tag string) string {
	credentials := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + password))
	return "ss://" + credentials + "@" + host + ":" + strconv.Itoa(port) + "#" + tag
}

func ssrURI(host string, port int, protocol, method, obfs, password, tag string) string {
	password64 := base64.RawURLEncoding.EncodeToString([]byte(password))
	tag64 := base64.RawURLEncoding.EncodeToString([]byte(tag))
	payload := fmt.Sprintf("%s:%d:%s:%s:%s:%s/?remarks=%s", host, port, protocol, method, obfs, password64, url.QueryEscape(tag64))
	return "ssr://" + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func outboundWithTag(t *testing.T, result Result, tag string) map[string]any {
	t.Helper()
	for _, outbound := range result.Outbounds {
		if outbound["tag"] == tag {
			return outbound
		}
	}
	t.Fatalf("no outbound tagged %q in %#v", tag, result.Outbounds)
	return nil
}

func assertOutbound(t *testing.T, got map[string]any, want outboundWant) {
	t.Helper()
	if want.tag != "" {
		assertPathValue(t, got, "tag", want.tag)
	}
	assertPathValue(t, got, "type", want.typeName)
	assertPathValue(t, got, "server", want.server)
	assertPathValue(t, got, "server_port", want.port)
	for path, value := range want.fields {
		assertPathValue(t, got, path, value)
	}
}

func assertPathValue(t *testing.T, object map[string]any, path string, want any) {
	t.Helper()
	got, ok := lookupPath(object, strings.Split(path, ".")...)
	if !ok {
		t.Errorf("outbound field %q is missing in %#v", path, object)
		return
	}
	if !semanticEqual(got, want) {
		t.Errorf("outbound field %q = %#v (%T), want %#v (%T)", path, got, got, want, want)
	}
}

func lookupPath(object map[string]any, parts ...string) (any, bool) {
	var current any = object
	for _, part := range parts {
		mapped, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapped[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func semanticEqual(got, want any) bool {
	if gotNumber, ok := numericValue(got); ok {
		if wantNumber, ok := numericValue(want); ok {
			return gotNumber == wantNumber
		}
	}
	gotStrings, gotOK := stringSlice(got)
	wantStrings, wantOK := stringSlice(want)
	if gotOK && wantOK {
		return reflect.DeepEqual(gotStrings, wantStrings)
	}
	gotBytes, gotOK := byteSlice(got)
	wantBytes, wantOK := byteSlice(want)
	if gotOK && wantOK {
		return reflect.DeepEqual(gotBytes, wantBytes)
	}
	return reflect.DeepEqual(got, want)
}

func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	case float32:
		return float64(number), true
	case float64:
		return number, true
	case json.Number:
		converted, err := number.Float64()
		return converted, err == nil
	default:
		return 0, false
	}
}

func stringSlice(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return values, true
	case []any:
		result := make([]string, len(values))
		for index, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			result[index] = text
		}
		return result, true
	default:
		return nil, false
	}
}

func byteSlice(value any) ([]byte, bool) {
	switch values := value.(type) {
	case []byte:
		return values, true
	case []int:
		result := make([]byte, len(values))
		for index, value := range values {
			if value < 0 || value > 255 {
				return nil, false
			}
			result[index] = byte(value)
		}
		return result, true
	case []any:
		result := make([]byte, len(values))
		for index, value := range values {
			number, ok := numericValue(value)
			if !ok || number < 0 || number > 255 || number != float64(byte(number)) {
				return nil, false
			}
			result[index] = byte(number)
		}
		return result, true
	default:
		return nil, false
	}
}

func issuesMention(issues []Issue, substring string) bool {
	for _, issue := range issues {
		if strings.Contains(strings.ToLower(issue.Parser+" "+issue.Reason), strings.ToLower(substring)) {
			return true
		}
	}
	return false
}
