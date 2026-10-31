package kernel

import (
	"context"
	kernelv1 "guiforcores/gen/kernel/v1"
	"net/url"
	"path/filepath"
	"testing"
)

func TestCoreRequestProxyDerivesUsableInboundsOnly(t *testing.T) {
	for _, tc := range []struct {
		name, listen, want string
		port               any
		extra              map[string]any
	}{
		{name: "mixed wildcard", listen: "0.0.0.0", port: float64(7890), want: "127.0.0.1:7890"},
		{name: "IPv6 wildcard", listen: "::", port: 7890, want: "[::1]:7890"},
		{name: "LAN address", listen: "192.168.1.1", port: 7890, want: "192.168.1.1:7890"},
		{name: "random port", port: 0},
		{name: "TLS inbound", port: 7890, extra: map[string]any{"tls": map[string]any{"enabled": true}}},
		{name: "other namespace", port: 7890, extra: map[string]any{"netns": "other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inbound := map[string]any{"type": "mixed", "listen": tc.listen, "listen_port": tc.port, "users": []any{map[string]any{"username": "a@b", "password": "p:s"}}}
			for key, value := range tc.extra {
				inbound[key] = value
			}
			raw := requestProxyFromConfig(map[string]any{"inbounds": []any{map[string]any{"type": "tun"}, inbound}})
			if tc.want == "" {
				if raw != "" {
					t.Fatal("unusable inbound accepted")
				}
				return
			}
			parsed, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			password, _ := parsed.User.Password()
			if parsed.Host != tc.want || parsed.User.Username() != "a@b" || password != "p:s" {
				t.Fatalf("wrong proxy: %s", raw)
			}
		})
	}
}

func TestCoreRequestProxyUsesRunningSnapshotAndExpiresWithInstance(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config := map[string]any{"inbounds": []any{map[string]any{"type": "http", "listen_port": 7890}}}
	service := &Service{status: kernelv1.CoreStatus_CORE_STATUS_RUNNING, corePID: 42, nativeAPI: &nativeAPIInstance{pid: 42, ctx: ctx, cancel: cancel}, requestProxyURL: requestProxyFromConfig(config)}
	config["inbounds"].([]any)[0].(map[string]any)["listen_port"] = 9999
	actual, err := service.CoreRequestProxy()
	if err != nil || actual != "http://127.0.0.1:7890" {
		t.Fatalf("editable configuration changed live proxy: %q %v", actual, err)
	}
	cancel()
	if _, err := service.CoreRequestProxy(); err == nil {
		t.Fatal("terminated instance still usable")
	}
	service.revokeNativeAPILocked()
	if service.requestProxyURL != "" {
		t.Fatal("revoked instance kept proxy")
	}
}

func TestCoreRequestProxyRejectsNonManagedConfigurationArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, args := range [][]string{{"run", "-c", path}, {"run", "--config=" + path}, {"run", "--config", path, "-D", filepath.Dir(path)}} {
		if !usesManagedProxyConfig(args, path) {
			t.Fatalf("managed configuration rejected: %v", args)
		}
	}
	for _, args := range [][]string{{"run"}, {"run", "-c", "config.json"}, {"run", "-c", path, "-c", path}, {"run", "-c", path, "-C", filepath.Dir(path)}, {"run", "-c", path + ".other"}, {"run", "-c" + path}} {
		if usesManagedProxyConfig(args, path) {
			t.Fatalf("uncertain configuration accepted: %v", args)
		}
	}
}
