package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"
)

// This invokes only version and check. In particular, no TUN or system DNS is
// started on the test host. CI can supply stable and alpha executables equally.
func TestInstalledCoreVersionAdaptationWithRealCore(t *testing.T) {
	core := os.Getenv("SING_BOX_COMPAT_BINARY")
	if core == "" {
		t.Skip("set SING_BOX_COMPAT_BINARY for stable/alpha core compatibility checks")
	}
	command := func(t *testing.T, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, core, args...)
		platform.SetCmdWindowHidden(cmd)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("core %v failed: %v\n%s", args, err, output)
		}
		return output
	}
	versionText := command(t, "version")
	matches := coreVersionRE.FindSubmatch(versionText)
	if len(matches) != 2 {
		t.Fatalf("unrecognized core version: %s", versionText)
	}
	version := string(matches[1])
	if err := requireCompatibleCoreVersion(version); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"", "disabled", "native", "hijack"} {
		for _, autoRoute := range []bool{false, true} {
			for _, autoRedirect := range []bool{false, true} {
				t.Run(fmt.Sprintf("dns=%s/auto_route=%t/auto_redirect=%t", mode, autoRoute, autoRedirect), func(t *testing.T) {
					root := t.TempDir()
					profile := &profilev1.Profile{Inbounds: []*profilev1.Inbound{{
						Enable: true, Tag: "fixture-tun", Type: profilev1.InboundType_INBOUND_TYPE_TUN,
						Tun: &profilev1.TunInboundConfig{Address: []string{"172.30.0.1/30"}, DnsMode: mode, Stack: profilev1.TunStack_TUN_STACK_MIXED, AutoRoute: autoRoute, AutoRedirect: autoRedirect, StrictRoute: true, Mtu: 9000},
					}}}
					generated, err := config.NewService(storage.NewPaths(root), "test").Generate(profile, nil)
					if err != nil {
						t.Fatal(err)
					}
					adaptConfigToCoreVersion(generated, version)
					tun := generated["inbounds"].([]any)[0].(map[string]any)
					_, hasStack := tun["stack"]
					if strings.HasPrefix(version, "1.14.") != hasStack {
						t.Fatalf("wrong stack adaptation for %s: %v", version, tun)
					}
					wantMode := mode
					if wantMode == "" {
						wantMode = "hijack"
					}
					if tun["dns_mode"] != wantMode {
						t.Fatalf("DNS mode changed during adaptation: %v", tun)
					}
					data, err := json.Marshal(generated)
					if err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(root, "config.json")
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					output := command(t, "check", "--disable-color", "-c", path, "-D", root)
					if strings.Contains(strings.ToLower(string(output)), "deprecated") {
						t.Fatalf("adapted config emitted deprecation warning: %s", output)
					}
				})
			}
		}
	}
}
