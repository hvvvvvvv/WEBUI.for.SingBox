package routerplugin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	profilev1 "guiforcores/gen/profile/v1"
)

// check validates core schema without creating a TUN device, binding ports or
// applying routes/firewall rules on the machine executing the test.
func TestRouterPluginOutputWithRealCore(t *testing.T) {
	binary := os.Getenv("SINGBOX_NATIVE_TEST_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_NATIVE_TEST_BINARY to run actual sing-box schema checks")
	}
	for _, explicitExit := range []bool{false, true} {
		name := "automatic exit"
		if explicitExit {
			name = "explicit exit"
		}
		t.Run(name, func(t *testing.T) {
			configuration := map[string]any{
				"inbounds":  []any{map[string]any{"type": "tun", "tag": "tun-in", "interface_name": "webui-test", "address": []any{"172.18.0.1/30"}, "mtu": 9000, "stack": "system", "dns_mode": "hijack"}},
				"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
				"dns":       map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "local"}}, "final": "local"},
				"route":     map[string]any{"rules": []any{}, "final": "direct"},
			}
			if explicitExit {
				configuration["route"].(map[string]any)["default_interface"] = "eth0"
			}
			generated, err := Apply(configuration, []*profilev1.RouterPlugin{
				testPlugin("tun", "tun-routing", tunRoutingConfig{Inbound: "tun-in", StrictRoute: true, IncludeInterface: []string{"br-lan"}, ExcludeAddress: []string{"192.168.1.0/24"}}),
				testPlugin("client", "source-routing", sourceRoutingConfig{Inbound: "tun-in", SourceIPCIDR: []string{"192.168.2.0/24"}, Outbound: "direct"}),
				testPlugin("dns", "dns-hijack", dnsHijackConfig{Inbound: "tun-in"}),
			})
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(generated)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, binary, "check", "--disable-color", "-c", file).CombinedOutput()
			if err != nil {
				t.Fatalf("core rejected plugins: %v\n%s\n%s", err, output, data)
			}
		})
	}
}
