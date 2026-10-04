package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"
)

func TestCoreLogDaysInvalidSavePreservesPersistedConfig(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	store, err := NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	service := NewAppService(store)
	_, err = service.SaveAppConfig(context.Background(), connect.NewRequest(&appv1.SaveAppConfigRequest{Config: &appv1.AppConfig{CoreLogDays: 7}}))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.Resolve(appConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SaveAppConfig(context.Background(), connect.NewRequest(&appv1.SaveAppConfigRequest{Config: &appv1.AppConfig{CoreLogDays: -1}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("error=%v", err)
	}
	after, err := os.ReadFile(paths.Resolve(appConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("invalid save changed state")
	}
	reloaded, err := NewStore(paths)
	if err != nil || reloaded.Current().CoreLogDays != 7 {
		t.Fatalf("reload failed: %v", err)
	}
}

func TestCoreLogDaysYAMLAliasesAndMergesAreValidated(t *testing.T) {
	for _, content := range []string{
		"value: &v null\ncoreLogDays: *v\n",
		"value: &v 1.5\ncoreLogDays: *v\n",
		"defaults: &defaults {coreLogDays: null}\n<<: *defaults\n",
		"defaults: &defaults {coreLogDays: -1}\n<<: [*defaults]\n",
	} {
		base := t.TempDir()
		path := filepath.Join(base, appConfigPath)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(storage.NewPaths(base)); err == nil {
			t.Fatalf("accepted invalid YAML: %s", content)
		}
	}
}
