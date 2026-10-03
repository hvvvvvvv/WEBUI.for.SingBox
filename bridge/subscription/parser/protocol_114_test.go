package parser

import (
	"reflect"
	"strings"
	"testing"
)

func TestSnell114Conversion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		fields Node
		want   map[string]any
	}{
		{"v4 defaults", Node{}, map[string]any{"version": 4, "psk": "snell-secret"}},
		{"v4 no obfs", Node{"obfs": "none"}, map[string]any{"obfs_mode": "none"}},
		{"v4 HTTP nested", Node{"obfs-opts": map[string]any{"mode": "http", "host": "front.example"}}, map[string]any{"obfs_mode": "http", "obfs_host": "front.example"}},
		{"v4 TLS flat", Node{"obfs_mode": "tls", "obfs_host": "front.example"}, map[string]any{"obfs_mode": "tls", "obfs_host": "front.example"}},
		{"v4 obfs object", Node{"obfs": map[string]any{"type": "http", "host": "front.example"}}, map[string]any{"obfs_mode": "http", "obfs_host": "front.example"}},
		{"v5 non QUIC", Node{"version": 5, "quic": false, "obfs": "http"}, map[string]any{"version": 4, "obfs_mode": "http"}},
		{"v6 default", Node{"version": "6", "psk": "0123456789ab"}, map[string]any{"version": 6, "psk": "0123456789ab"}},
		{"v6 explicit default", Node{"version": 6, "psk": "0123456789ab", "mode": "default"}, map[string]any{"mode": "default"}},
		{"v6 unshaped", Node{"version": 6, "psk": "0123456789ab", "mode": "unshaped"}, map[string]any{"mode": "unshaped"}},
		{"v6 unsafe raw", Node{"version": 6, "psk": "0123456789ab", "mode": "unsafe-raw"}, map[string]any{"mode": "unsafe-raw"}},
		{"v6 max key lengths", Node{"version": 6, "psk": strings.Repeat("s", 255), "user-key": strings.Repeat("u", 255)}, map[string]any{"psk": strings.Repeat("s", 255), "userkey": strings.Repeat("u", 255)}},
		{"v6 byte length", Node{"version": 6, "psk": "密钥密钥"}, map[string]any{"psk": "密钥密钥"}},
		{"v6 PSK whitespace minimum", Node{"version": 6, "psk": " abcdefghij "}, map[string]any{"psk": " abcdefghij "}},
		{"v6 PSK whitespace preserved", Node{"version": 6, "psk": " abcdefghijk "}, map[string]any{"psk": " abcdefghijk "}},
		{"v6 PSK whitespace maximum", Node{"version": 6, "psk": " " + strings.Repeat("s", 253) + " "}, map[string]any{"psk": " " + strings.Repeat("s", 253) + " "}},
		{"userkey whitespace preserved", Node{"userkey": " user-secret "}, map[string]any{"userkey": " user-secret "}},
		{"userkey whitespace maximum", Node{"userkey": " " + strings.Repeat("u", 253) + " "}, map[string]any{"userkey": " " + strings.Repeat("u", 253) + " "}},
		{"common options", Node{"user-key": "user-secret", "reuse": true, "udp": false, "detour": "upstream", "interface-name": "eth0", "routing-mark": 123, "tfo": true, "mptcp": true, "connect-timeout": "8s"}, map[string]any{"userkey": "user-secret", "reuse": true, "network": "tcp", "detour": "upstream", "bind_interface": "eth0", "routing_mark": 123, "tcp_fast_open": true, "tcp_multi_path": true, "connect_timeout": "8s"}},
		{"UDP and disabled reuse", Node{"network": "udp", "reuse": false}, map[string]any{"network": "udp", "reuse": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := protocol114Node("snell", tc.fields)
			out, err := produceNode(node)
			if err != nil {
				t.Fatalf("produceNode() error = %v", err)
			}
			assertPathValue(t, out, "type", "snell")
			assertPathValue(t, out, "tag", "protocol-114")
			assertPathValue(t, out, "server", "proxy.example")
			assertPathValue(t, out, "server_port", 443)
			for key, value := range tc.want {
				assertPathValue(t, out, key, value)
			}
			for _, key := range []string{"password", "obfs", "quic", "domain_strategy"} {
				if _, ok := out[key]; ok {
					t.Fatalf("unexpected source field %q in outbound", key)
				}
			}
		})
	}
}

func TestSnell114RejectsUnsupportedNodes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		fields Node
		reason string
	}{
		{"missing version", Node{"version": nil}, "version"},
		{"invalid version", Node{"version": "unknown"}, "version"},
		{"old version", Node{"version": 3}, "version"},
		{"future version", Node{"version": 7}, "version"},
		{"v5 QUIC", Node{"version": 5, "quic": true}, "QUIC"},
		{"QUIC string", Node{"quic": "true"}, "QUIC"},
		{"QUIC network", Node{"network": "quic"}, "QUIC"},
		{"QUIC transport", Node{"transport": map[string]any{"type": "quic"}}, "QUIC"},
		{"missing psk", Node{"password": ""}, "credentials"},
		{"v6 short psk", Node{"version": 6, "psk": "0123456789a"}, "key length"},
		{"v6 long psk", Node{"version": 6, "psk": strings.Repeat("s", 256)}, "key length"},
		{"v6 long psk with whitespace", Node{"version": 6, "psk": " " + strings.Repeat("s", 254) + " "}, "key length"},
		{"v6 UTF8 bytes", Node{"version": 6, "psk": strings.Repeat("密", 86)}, "key length"},
		{"long userkey", Node{"userkey": strings.Repeat("u", 256)}, "key length"},
		{"long userkey with whitespace", Node{"userkey": " " + strings.Repeat("u", 254) + " "}, "key length"},
		{"v4 traffic shaping", Node{"mode": "default"}, "version 6"},
		{"v6 old obfs", Node{"version": 6, "psk": "0123456789ab", "obfs": "none"}, "version 4"},
		{"v6 old obfs host", Node{"version": 6, "psk": "0123456789ab", "obfs_host": "front.example"}, "version 4"},
		{"v6 old nested obfs", Node{"version": 6, "psk": "0123456789ab", "obfs-opts": map[string]any{"mode": "http"}}, "version 4"},
		{"invalid shaping mode", Node{"version": 6, "psk": "0123456789ab", "mode": "unknown"}, "shaping mode"},
		{"unsupported obfs", Node{"obfs": "shadow-tls"}, "obfuscation"},
		{"unsupported plugin", Node{"plugin": "shadow-tls"}, "chain"},
		{"ShadowTLS chain", Node{"shadow-tls-password": "chain-secret"}, "chain"},
		{"Restls chain", Node{"restls-opts": map[string]any{"password": "chain-secret"}}, "chain"},
		{"JLS chain", Node{"jls": true}, "chain"},
		{"external TLS", Node{"tls": true}, "TLS transport"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := produceNode(protocol114Node("snell", tc.fields))
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("produceNode() = %v, %v; want reason containing %q", out, err, tc.reason)
			}
			for _, secret := range []string{"snell-secret", "chain-secret", "front.example"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error discloses source data: %v", err)
				}
			}
		})
	}
}

func TestHysteria2114HopIntervals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		fields Node
		min    string
		max    string
	}{
		{"core default", Node{}, "", ""},
		{"seconds", Node{"hop-interval": 30}, "30s", ""},
		{"fractional seconds", Node{"hop-interval": 5.5}, "5.5s", ""},
		{"duration", Node{"hop_interval": "1m"}, "1m", ""},
		{"five seconds boundary", Node{"hop-interval": "5s"}, "5s", ""},
		{"explicit bounds", Node{"hop-interval": "5s", "hop_interval_max": 30}, "5s", "30s"},
		{"random seconds", Node{"hop-interval": "15-30"}, "15s", "30s"},
		{"spaced random seconds", Node{"hop-interval": " 15 - 30 "}, "15s", "30s"},
		{"fixed seconds range", Node{"hop-interval": "5-5"}, "5s", "5s"},
		{"matching explicit max", Node{"hop-interval": "15-60", "hop-interval-max": "1m"}, "15s", "60s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := produceNode(protocol114Node("hysteria2", tc.fields))
			if err != nil {
				t.Fatalf("produceNode() error = %v", err)
			}
			for key, want := range map[string]string{"hop_interval": tc.min, "hop_interval_max": tc.max} {
				got, exists := out[key]
				if (want == "" && exists) || (want != "" && got != want) {
					t.Fatalf("%s = %v (exists %t); want %q", key, got, exists, want)
				}
			}
		})
	}
}

func TestHysteria2114RejectsInvalidHopIntervals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		fields Node
	}{
		{"max only", Node{"hop-interval-max": "30s"}},
		{"below five seconds", Node{"hop-interval": "4.999s"}},
		{"range below five seconds", Node{"hop-interval": "4-30"}},
		{"inverted bounds", Node{"hop-interval": "30s", "hop-interval-max": "15s"}},
		{"inverted range", Node{"hop-interval": "30-15"}},
		{"conflicting range max", Node{"hop-interval": "15-30", "hop-interval-max": "60s"}},
		{"invalid minimum", Node{"hop-interval": "invalid"}},
		{"invalid maximum", Node{"hop-interval": "15s", "hop-interval-max": "invalid"}},
		{"zero minimum", Node{"hop-interval": 0}},
		{"zero maximum", Node{"hop-interval": 15, "hop-interval-max": 0}},
		{"negative minimum", Node{"hop-interval": -15}},
		{"range duration strings", Node{"hop-interval": "15s-30s"}},
		{"multiple bounds", Node{"hop-interval": "15-30-60"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := produceNode(protocol114Node("hysteria2", tc.fields)); err == nil {
				t.Fatal("produceNode() accepted invalid hop interval")
			}
		})
	}
}

func TestHysteria2114GeckoPacketSizes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		fields Node
		want   map[string]any
	}{
		{"defaults", Node{"obfs": "gecko", "obfs-password": "obfs-secret"}, map[string]any{"type": "gecko", "password": "obfs-secret"}},
		{"top password whitespace preserved", Node{"obfs": "gecko", "obfs-password": " obfs-secret "}, map[string]any{"type": "gecko", "password": " obfs-secret "}},
		{"nested password whitespace preserved", Node{"obfs": map[string]any{"type": "gecko", "password": " obfs-secret "}}, map[string]any{"type": "gecko", "password": " obfs-secret "}},
		{"zero defaults", Node{"obfs": map[string]any{"type": "gecko", "password": "obfs-secret", "min_packet_size": 0, "max_packet_size": 0}}, map[string]any{"type": "gecko", "password": "obfs-secret"}},
		{"nested bounds", Node{"obfs": map[string]any{"type": "gecko", "password": "obfs-secret", "min_packet_size": 1, "max_packet_size": 2048}}, map[string]any{"type": "gecko", "password": "obfs-secret", "min_packet_size": 1, "max_packet_size": 2048}},
		{"top level bounds", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-min-packet-size": "600", "obfs-max-packet-size": 1400}, map[string]any{"type": "gecko", "password": "obfs-secret", "min_packet_size": 600, "max_packet_size": 1400}},
		{"equal bounds", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-min-packet-size": 1200, "obfs-max-packet-size": 1200}, map[string]any{"type": "gecko", "password": "obfs-secret", "min_packet_size": 1200, "max_packet_size": 1200}},
		{"nested wins", Node{"obfs": map[string]any{"type": "gecko", "password": "obfs-secret", "min_packet_size": 600}, "obfs-min-packet-size": 1800}, map[string]any{"type": "gecko", "password": "obfs-secret", "min_packet_size": 600}},
		{"salamander retained", Node{"obfs": "salamander", "obfs-password": "obfs-secret"}, map[string]any{"type": "salamander", "password": "obfs-secret"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := produceNode(protocol114Node("hysteria2", tc.fields))
			if err != nil {
				t.Fatalf("produceNode() error = %v", err)
			}
			if !reflect.DeepEqual(out["obfs"], tc.want) {
				t.Fatalf("obfs = %#v; want %#v", out["obfs"], tc.want)
			}
		})
	}
}

func TestHysteria2114RejectsInvalidGecko(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		fields Node
	}{
		{"missing password", Node{"obfs": "gecko"}},
		{"negative minimum", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-min-packet-size": -1}},
		{"invalid minimum", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-min-packet-size": "invalid"}},
		{"fractional size", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-max-packet-size": 1200.5}},
		{"maximum too large", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-max-packet-size": 2049}},
		{"inverted bounds", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-min-packet-size": 1400, "obfs-max-packet-size": 1200}},
		{"default maximum conflict", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-min-packet-size": 1400}},
		{"default minimum conflict", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-max-packet-size": 511}},
		{"zero minimum default conflict", Node{"obfs": "gecko", "obfs-password": "obfs-secret", "obfs-min-packet-size": 0, "obfs-max-packet-size": 511}},
		{"packet size without Gecko", Node{"obfs": "salamander", "obfs-password": "obfs-secret", "obfs-max-packet-size": 1200}},
		{"packet size without obfs", Node{"obfs-min-packet-size": 512}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := produceNode(protocol114Node("hysteria2", tc.fields))
			if err == nil {
				t.Fatal("produceNode() accepted invalid Gecko configuration")
			}
			if strings.Contains(err.Error(), "obfs-secret") || strings.Contains(err.Error(), "hy2-secret") {
				t.Fatalf("error discloses credentials: %v", err)
			}
		})
	}
}

func TestHysteria2114MixedServerPorts(t *testing.T) {
	t.Parallel()
	out, err := produceNode(protocol114Node("hysteria2", Node{"ports": []string{"443", "444-450", "500:600"}}))
	if err != nil {
		t.Fatalf("produceNode() error = %v", err)
	}
	if _, exists := out["server_port"]; exists {
		t.Fatal("hopping outbound retained server_port")
	}
	if want := []string{"443:443", "444:450", "500:600"}; !reflect.DeepEqual(out["server_ports"], want) {
		t.Fatalf("server_ports = %#v; want %#v", out["server_ports"], want)
	}
}

func protocol114Node(protocol string, overrides Node) Node {
	node := Node{"name": "protocol-114", "type": protocol, "server": "proxy.example", "port": 443}
	if protocol == "snell" {
		node["version"], node["password"] = 4, "snell-secret"
	} else {
		node["password"] = "hy2-secret"
	}
	for key, value := range overrides {
		node[key] = value
	}
	return node
}
