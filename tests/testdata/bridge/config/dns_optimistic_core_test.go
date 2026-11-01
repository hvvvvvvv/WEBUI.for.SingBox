package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestOptimisticDNSConfigurationWithRealCores(t *testing.T) {
	for _, core := range []struct {
		env     string
		version string
	}{
		{env: "SING_BOX_114_PATH", version: "1.14.0"},
		{env: "SINGBOX_NATIVE_TEST_BINARY"},
	} {
		name := core.version
		if name == "" {
			name = "selected-native-core"
		}
		t.Run(name, func(t *testing.T) {
			corePath := os.Getenv(core.env)
			if corePath == "" {
				t.Skipf("set %s to a compatible sing-box binary to run core checks", core.env)
			}
			version, err := runHTTPClientCoreCommand(corePath, "version")
			versionLine := strings.TrimSpace(strings.SplitN(string(version), "\n", 2)[0])
			if err != nil || !strings.HasPrefix(versionLine, "sing-box version ") || (core.version != "" && versionLine != "sing-box version "+core.version) {
				t.Fatalf("expected sing-box %s, got %s (error=%v)", core.version, version, err)
			}
			t.Log(versionLine)
			for _, tc := range []struct {
				name       string
				optimistic *profilev1.DnsOptimisticCache
				wantError  bool
			}{
				{name: "legacy profile"},
				{name: "disabled default", optimistic: &profilev1.DnsOptimisticCache{Timeout: "3d"}},
				{name: "disabled invalid saved timeout", optimistic: &profilev1.DnsOptimisticCache{Timeout: "invalid"}},
				{name: "enabled custom timeout", optimistic: &profilev1.DnsOptimisticCache{Enabled: true, Timeout: "12h"}},
				{name: "empty timeout uses core default", optimistic: &profilev1.DnsOptimisticCache{Enabled: true}},
				{name: "invalid timeout is diagnosed", optimistic: &profilev1.DnsOptimisticCache{Enabled: true, Timeout: "invalid"}, wantError: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					generated, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(&profilev1.Profile{
						Dns: &profilev1.Dns{
							Servers: []*profilev1.DnsServer{{Id: "local", Tag: "local", Type: profilev1.DnsServerType_DNS_SERVER_TYPE_LOCAL}},
							Final:   "local", Optimistic: tc.optimistic,
						},
					}, nil)
					if err != nil {
						t.Fatal(err)
					}
					data, err := json.Marshal(generated)
					if err != nil {
						t.Fatal(err)
					}
					configPath := filepath.Join(t.TempDir(), "config.json")
					if err := os.WriteFile(configPath, data, 0o600); err != nil {
						t.Fatal(err)
					}
					output, err := runHTTPClientCoreCommand(corePath, "check", "--disable-color", "-c", configPath)
					if tc.wantError {
						if err == nil || !strings.Contains(string(output), "dns.optimistic.timeout") || !strings.Contains(string(output), "invalid duration") {
							t.Fatalf("invalid cache duration must identify its field: error=%v, output=%s", err, output)
						}
						return
					}
					if err != nil {
						t.Fatalf("core rejected generated DNS config: error=%v, output=%s, config=%s", err, output, data)
					}
				})
			}
		})
	}
}
