package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/plugins"
	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

func configWithPlugin(t *testing.T, source string) (*Service, *storage.Paths) {
	t.Helper()
	paths := storage.NewPaths(t.TempDir())
	hooks, err := plugins.NewService(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.Save(plugins.Plugin{ID: "config-hook", Name: "Config hook", Version: "1", Triggers: []string{plugins.OnGenerate}, Source: source}, 0); err != nil {
		t.Fatal(err)
	}
	service := NewService(paths, "test")
	service.SetPlugins(hooks)
	return service, paths
}

func TestConfigPluginOrderAndManagedAPIProtection(t *testing.T) {
	service, paths := configWithPlugin(t, `function onGenerate(config) {
  if(config.order !== 'mixin-script') throw Error('wrong hook order');
  config.order += '-plugin';
  config.experimental = {clash_api:{secret:'plugin-secret'}, cache_file:{path:'/tmp/plugin-cache.db'}};
  config.services = [{type:'resolved',tag:'other'}, {type:'api',tag:'webui-api',listen:'0.0.0.0',listen_port:9090,secret:'plugin-secret',dashboard:true}];
  config.log = {disabled:true,output:'/tmp/plugin.log',level:'debug'};
  return config;
}`)
	profile := &profilev1.Profile{
		Id: "test", Mixin: &profilev1.Mixin{Priority: profilev1.MixinPriority_MIXIN_PRIORITY_MIXIN, Config: `{"order":"mixin"}`},
		Script: &profilev1.Script{Code: `function onGenerate(c) { c.order += '-script'; return c }`},
	}
	options := &kernelv1.GenerateConfigOptions{EnableMixinProcessing: true, EnableScriptProcessing: true}
	response, err := service.GenerateConfig(context.Background(), connect.NewRequest(&kernelv1.GenerateConfigRequest{Profile: profile, Options: options}))
	if err != nil {
		t.Fatal(err)
	}
	config := response.Msg.Config.AsMap()
	if config["order"] != "mixin-script-plugin" {
		t.Fatalf("wrong pipeline order: %v", config["order"])
	}
	if _, exists := config["experimental"].(map[string]any)["clash_api"]; exists {
		t.Fatal("plugin re-enabled legacy Clash API")
	}
	services := config["services"].([]any)
	if len(services) != 2 || services[0].(map[string]any)["tag"] != "other" {
		t.Fatalf("unmanaged service lost: %v", services)
	}
	managed := services[1].(map[string]any)
	if managed["listen"] != "127.0.0.1" || managed["listen_port"] != float64(20123) || managed["dashboard"] != false || len(NativeAPISecret(config)) != 64 || NativeAPISecret(config) == "plugin-secret" {
		t.Fatalf("plugin replaced managed service: %v", managed)
	}
	written, err := service.GenerateConfigFile(context.Background(), connect.NewRequest(&kernelv1.GenerateConfigFileRequest{Profile: profile, Options: options}))
	if err != nil {
		t.Fatal(err)
	}
	if NativeAPISecret(written.Msg.Config.AsMap()) != "<redacted>" {
		t.Fatal("file response exposed credential")
	}
	data, err := os.ReadFile(paths.Resolve(CoreConfigFilePath))
	if err != nil || json.Unmarshal(data, &config) != nil {
		t.Fatalf("generated file unreadable: %v", err)
	}
	if len(NativeAPISecret(config)) != 64 || config["experimental"].(map[string]any)["cache_file"].(map[string]any)["path"] != coreGeneratedCacheFilePath {
		t.Fatalf("plugin bypassed finalization: %s", data)
	}
	if config["log"].(map[string]any)["disabled"] != true || config["log"].(map[string]any)["output"] != "/tmp/plugin.log" || config["log"].(map[string]any)["level"] != "debug" {
		t.Fatalf("plugin log settings were overwritten: %s", data)
	}
}

func TestConfigPluginFailureAndCancellationPreserveExistingFile(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		deadline     bool
		want         connect.Code
	}{
		{"script error", `function onGenerate(c){ c.partial=true; throw Error('plugin failed') }`, false, connect.CodeInvalidArgument},
		{"request deadline", `function onGenerate(c){ while(true) {} }`, true, connect.CodeDeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, paths := configWithPlugin(t, tc.source)
			path := paths.Resolve(CoreConfigFilePath)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("previous configuration"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if tc.deadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 25*time.Millisecond)
				defer cancel()
			}
			started := time.Now()
			_, err := service.GenerateConfigFile(ctx, connect.NewRequest(&kernelv1.GenerateConfigFileRequest{Profile: &profilev1.Profile{}}))
			if connect.CodeOf(err) != tc.want {
				t.Fatalf("wrong plugin error: %v want %v", err, tc.want)
			}
			if tc.deadline && time.Since(started) > time.Second {
				t.Fatal("generation ignored caller deadline")
			}
			data, _ := os.ReadFile(path)
			if string(data) != "previous configuration" {
				t.Fatalf("failed plugin replaced generated file: %s", data)
			}
		})
	}
}

func TestCanceledConfigRequestDoesNotEvaluatePlugin(t *testing.T) {
	service, _ := configWithPlugin(t, `throw Error('should not run'); function onGenerate(c){return c}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.GenerateConfig(ctx, connect.NewRequest(&kernelv1.GenerateConfigRequest{Profile: &profilev1.Profile{}}))
	if connect.CodeOf(err) != connect.CodeCanceled || strings.Contains(err.Error(), "should not run") {
		t.Fatalf("canceled request evaluated plugin: %v", err)
	}
}
