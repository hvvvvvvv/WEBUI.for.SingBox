package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

// This checks the generated configuration against the fixed core without
// creating a TUN device or changing the machine's routes and DNS settings.
func TestTunDNSModesWithSingBox114(t *testing.T) {
	corePath := os.Getenv("SING_BOX_114_PATH")
	if corePath == "" {
		t.Skip("set SING_BOX_114_PATH to a sing-box 1.14.0 binary to run core checks")
	}
	version, err := runHTTPClientCoreCommand(corePath, "version")
	if err != nil || strings.SplitN(string(version), "\n", 2)[0] != "sing-box version 1.14.0" {
		t.Fatalf("expected sing-box 1.14.0, got %s (error=%v)", version, err)
	}

	for _, mode := range []string{"", "disabled", "native", "hijack"} {
		for _, autoRoute := range []bool{false, true} {
			for _, autoRedirect := range []bool{false, true} {
				name := fmt.Sprintf("mode=%s/auto_route=%t/auto_redirect=%t", mode, autoRoute, autoRedirect)
				t.Run(name, func(t *testing.T) {
					config, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(&profilev1.Profile{
						Inbounds: []*profilev1.Inbound{{
							Type:   profilev1.InboundType_INBOUND_TYPE_TUN,
							Tag:    "tun-in",
							Enable: true,
							Tun: &profilev1.TunInboundConfig{
								InterfaceName: "tun-test",
								Address:       []string{"172.18.0.1/30", "fdfe:dcba:9876::1/126"},
								Mtu:           9000,
								AutoRoute:     autoRoute,
								AutoRedirect:  autoRedirect,
								StrictRoute:   true,
								Stack:         profilev1.TunStack_TUN_STACK_MIXED,
								DnsMode:       mode,
							},
						}},
					}, nil)
					if err != nil {
						t.Fatal(err)
					}
					inbounds := config["inbounds"].([]any)
					if len(inbounds) != 1 {
						t.Fatalf("expected one TUN inbound, got %#v", inbounds)
					}
					tun := inbounds[0].(map[string]any)
					wantMode := mode
					if wantMode == "" {
						wantMode = "hijack"
					}
					if tun["dns_mode"] != wantMode {
						t.Fatalf("dns_mode = %#v, want %q", tun["dns_mode"], wantMode)
					}
					for _, field := range []string{"endpoint_independent_nat", "dns_address"} {
						if _, exists := tun[field]; exists {
							t.Fatalf("generated TUN contains unsupported field %q: %#v", field, tun)
						}
					}
					redirect, hasRedirect := tun["auto_redirect"]
					if wantRedirect := runtime.GOOS == "linux" && autoRoute; hasRedirect != wantRedirect {
						t.Fatalf("auto_redirect presence = %t, want %t", hasRedirect, wantRedirect)
					}
					if hasRedirect && redirect != autoRedirect {
						t.Fatalf("auto_redirect = %#v, want %t", redirect, autoRedirect)
					}
					data, err := json.Marshal(config)
					if err != nil {
						t.Fatal(err)
					}
					configPath := filepath.Join(t.TempDir(), "config.json")
					if err := os.WriteFile(configPath, data, 0o600); err != nil {
						t.Fatal(err)
					}
					output, err := runHTTPClientCoreCommand(corePath, "check", "-c", configPath)
					if err != nil {
						t.Fatalf("core rejected TUN config: error=%v, output=%s, config=%s", err, output, data)
					}
					if strings.Contains(strings.ToLower(string(output)), "deprecated") {
						t.Fatalf("core emitted a deprecation warning: %s", output)
					}
				})
			}
		}
	}
}
