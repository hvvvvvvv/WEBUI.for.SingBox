package config

import (
	"testing"

	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestRouterPluginsRunAfterScriptAndKeepNativeAPIProtected(t *testing.T) {
	// A script-created TUN demonstrates that every preview and core start uses
	// the same final pipeline, including references introduced by mixins/scripts.
	profile := &profilev1.Profile{
		Script:        &profilev1.Script{Code: `function onGenerate(config) { config.inbounds = [{type:"tun",tag:"gateway",address:["172.18.0.1/30"]}]; return config; }`},
		RouterPlugins: []*profilev1.RouterPlugin{{Id: "dns", Enabled: true, ManifestJson: `{"apiVersion":"webui.router/v1","id":"test.dns","name":"DNS","description":"","version":"1.0.0","kind":"dns-hijack","config":{"inbound":"gateway"}}`}},
	}
	result, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(profile, &kernelv1.GenerateConfigOptions{EnableScriptProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	rules := result["route"].(map[string]any)["rules"].([]any)
	if rules[0].(map[string]any)["action"] != "hijack-dns" {
		t.Fatalf("plugin did not run: %#v", rules)
	}
	if len(NativeAPISecret(result)) != 64 {
		t.Fatal("plugin bypassed native API protection")
	}
	profile.RouterPlugins[0].ManifestJson = `{"apiVersion":"webui.router/v1","id":"test.dns","name":"DNS","description":"","version":"1.0.0","kind":"dns-hijack","config":{"inbound":"missing"}}`
	if _, err := NewService(storage.NewPaths(t.TempDir()), "test").Generate(profile, &kernelv1.GenerateConfigOptions{EnableScriptProcessing: true}); err == nil {
		t.Fatal("invalid plugin reference accepted")
	}
}
