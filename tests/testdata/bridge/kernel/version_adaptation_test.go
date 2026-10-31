package kernel

import "testing"

func TestTUNStackCompatibilityUsesInstalledVersion(t *testing.T) {
	// sing-box's documented contract keeps legacy stack selection in 1.14,
	// deprecates it in 1.15, and removes it in 1.17. Alpha can run either version.
	for _, version := range []string{"1.14.2", "v1.14.9", "1.15.0-alpha.1", "1.15.0", "1.17.0", "2.0.0-alpha.1"} {
		t.Run(version, func(t *testing.T) {
			tun := map[string]any{"type": "tun", "stack": "mixed", "auto_route": true}
			mixed := map[string]any{"type": "mixed", "listen_port": 2080}
			config := map[string]any{"inbounds": []any{tun, mixed}}
			adaptConfigToCoreVersion(config, version)
			_, hasStack := tun["stack"]
			if legacy := version == "1.14.2" || version == "v1.14.9"; hasStack != legacy {
				t.Fatalf("version %s retains stack=%v, want %v", version, hasStack, legacy)
			}
			if tun["auto_route"] != true || mixed["listen_port"] != 2080 {
				t.Fatal("version adaptation changed unrelated inbound options")
			}
		})
	}
}
