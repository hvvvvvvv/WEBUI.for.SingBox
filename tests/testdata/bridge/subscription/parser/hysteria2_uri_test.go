package parser

import (
	"reflect"
	"testing"
)

// These are Hysteria's documented authority port lists, independent of the
// nonstandard query-string aliases accepted by older subscriptions.
func TestHysteria2URIPortHopping(t *testing.T) {
	for _, tc := range []struct {
		name     string
		uri      string
		server   string
		password string
		ports    []string
		port     int
	}{
		{"mixed list", "hy2://password@127.0.0.1:443,5000-6000/#hop", "127.0.0.1", "password", []string{"443:443", "5000:6000"}, 0},
		{"single range", "hysteria2://password@proxy.example:5000-6000/#hop", "proxy.example", "password", []string{"5000:6000"}, 0},
		{"IPv6 and encoded credentials", "hy2://user%40name:p%2Fss%3A%25@[2001:db8::1]:443,8443/?sni=proxy.example#hop", "2001:db8::1", "user@name:p/ss:%", []string{"443:443", "8443:8443"}, 0},
		{"authority precedes query", "hy2://password@127.0.0.1:443,8443/?ports=5000-6000#hop", "127.0.0.1", "password", []string{"443:443", "8443:8443"}, 0},
		{"legacy query", "hy2://password@127.0.0.1:443/?ports=5000-6000#hop", "127.0.0.1", "password", []string{"5000:6000"}, 0},
		{"single port", "hy2://password@127.0.0.1:8443/#hop", "127.0.0.1", "password", nil, 8443},
		{"default port", "hy2://password@proxy.example/#hop", "proxy.example", "password", nil, 443},
		{"IPv6 default port", "hy2://password@[2001:db8::1]/#hop", "2001:db8::1", "password", nil, 443},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse(tc.uri)
			if err != nil || result.Skipped != 0 || len(result.Outbounds) != 1 {
				t.Fatalf("Parse() = %#v, %v", result, err)
			}
			out := result.Outbounds[0]
			if out["server"] != tc.server || out["password"] != tc.password || out["tag"] != "hop" {
				t.Fatalf("URI components changed: %#v", out)
			}
			if tc.ports != nil {
				if !reflect.DeepEqual(out["server_ports"], tc.ports) {
					t.Fatalf("server_ports = %#v, want %#v", out["server_ports"], tc.ports)
				}
				if _, exists := out["server_port"]; exists {
					t.Fatalf("hopping outbound still has server_port: %#v", out)
				}
			} else if out["server_port"] != tc.port {
				t.Fatalf("server_port = %#v, want %d", out["server_port"], tc.port)
			}
		})
	}
}
