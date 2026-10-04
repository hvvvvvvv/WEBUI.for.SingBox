package config

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

type failedNativeEntropy struct{}

func (failedNativeEntropy) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestNativeAPICredentialGenerationFailurePropagates(t *testing.T) {
	previous := rand.Reader
	rand.Reader = failedNativeEntropy{}
	t.Cleanup(func() { rand.Reader = previous })
	root := map[string]any{}
	if err := EnforceNativeAPIConfig(root); err == nil {
		t.Fatal("entropy failure produced a fallback credential")
	}
	if _, exists := root["services"]; exists {
		t.Fatal("entropy failure installed a service")
	}
	service := NewService(storage.NewPaths(t.TempDir()), "test")
	if _, err := service.Generate(&profilev1.Profile{}, nil); err == nil {
		t.Fatal("generation swallowed entropy failure")
	}
}

func TestNativeAPIOverridesMixinAndScript(t *testing.T) {
	service := NewService(storage.NewPaths(t.TempDir()), "test")
	profile := &profilev1.Profile{
		Mixin:  &profilev1.Mixin{Priority: profilev1.MixinPriority_MIXIN_PRIORITY_MIXIN, Config: `{"experimental":{"clash_api":{"external_controller":"0.0.0.0:9090"}},"services":[{"type":"resolved","tag":"other"},{"type":"api","tag":"webui-api","listen":"0.0.0.0","listen_port":9090,"secret":"user-secret","dashboard":true}]}`},
		Script: &profilev1.Script{Code: `function onGenerate(config) { config.experimental.clash_api = {secret: 'script-secret'}; config.services.push({type:'api',tag:'webui-api',listen:'::',secret:'script-secret'}); return config; }`},
	}
	generated, err := service.Generate(profile, &kernelv1.GenerateConfigOptions{EnableMixinProcessing: true, EnableScriptProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := generated["experimental"].(map[string]any)["clash_api"]; exists {
		t.Fatal("Clash API survived finalization")
	}
	services := generated["services"].([]any)
	if len(services) != 2 || services[0].(map[string]any)["tag"] != "other" {
		t.Fatalf("other services were not preserved: %#v", services)
	}
	managed := services[1].(map[string]any)
	if managed["type"] != "api" || managed["listen"] != "127.0.0.1" || managed["listen_port"] != 20123 || managed["dashboard"] != false {
		t.Fatalf("unsafe managed parameters: %#v", managed)
	}
	secret := NativeAPISecret(generated)
	if len(secret) != 64 || secret == "script-secret" || secret == "user-secret" {
		t.Fatalf("invalid credential: %q", secret)
	}
	second, err := service.Generate(profile, &kernelv1.GenerateConfigOptions{EnableMixinProcessing: true, EnableScriptProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	if NativeAPISecret(second) == secret {
		t.Fatal("preview reused prior credential")
	}
}

func TestGeneratedFileResponseRedactsOnlyCopy(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	service := NewService(paths, "test")
	response, err := service.GenerateConfigFile(context.Background(), connect.NewRequest(&kernelv1.GenerateConfigFileRequest{Profile: &profilev1.Profile{Id: "profile"}}))
	if err != nil {
		t.Fatal(err)
	}
	if got := NativeAPISecret(response.Msg.GetConfig().AsMap()); got != "<redacted>" {
		t.Fatalf("response credential=%q", got)
	}
	data, err := os.ReadFile(paths.Resolve(CoreConfigFilePath))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if secret := NativeAPISecret(config); len(secret) != 64 {
		t.Fatalf("stored credential=%q", secret)
	}
	preview, err := service.Generate(&profilev1.Profile{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if NativeAPISecret(preview) == NativeAPISecret(config) {
		t.Fatal("preview exposed stored credential")
	}
}
