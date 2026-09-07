package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"
)

func TestCoreLogDaysYAMLValidation(t *testing.T) {
	for _, value := range []string{"0", "7", "2147483647", "-1", "1.5", "2147483648", "\"7\"", "true", "null"} {
		t.Run(value, func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, appConfigPath)
			_ = os.MkdirAll(filepath.Dir(path), 0755)
			original := "coreLogDays: " + value + "\n"
			if err := os.WriteFile(path, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(storage.NewPaths(base))
			valid := value == "0" || value == "7" || value == "2147483647"
			if valid {
				if err != nil {
					t.Fatal(err)
				}
				if store.Current().CoreLogDays < 0 {
					t.Fatal("invalid value")
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "coreLogDays") {
					t.Fatalf("expected validation error, got %v", err)
				}
				data, _ := os.ReadFile(path)
				if string(data) != original {
					t.Fatal("invalid YAML was rewritten")
				}
			}
		})
	}
}

func TestCoreLogDaysSaveValidationAndChangeNotification(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	store, err := NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	if store.Current().CoreLogDays != 0 {
		t.Fatal("default must be zero")
	}
	service := NewAppService(store)
	handler := &recordingAppConfigChanges{}
	service.SetChangeHandler(handler)
	response, err := service.SaveAppConfig(context.Background(), connect.NewRequest(&appv1.SaveAppConfigRequest{Config: &appv1.AppConfig{CoreLogDays: 7}}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Config.CoreLogDays != 7 || handler.current.CoreLogDays != 7 || handler.calls != 1 {
		t.Fatal("config did not round-trip")
	}
	before, _ := os.ReadFile(paths.Resolve(appConfigPath))
	_, err = service.SaveAppConfig(context.Background(), connect.NewRequest(&appv1.SaveAppConfigRequest{Config: &appv1.AppConfig{CoreLogDays: -1}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("error=%v", err)
	}
	after, _ := os.ReadFile(paths.Resolve(appConfigPath))
	if string(before) != string(after) || handler.calls != 1 {
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
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		_ = os.WriteFile(path, []byte(content), 0644)
		if _, err := NewStore(storage.NewPaths(base)); err == nil {
			t.Fatalf("accepted invalid YAML: %s", content)
		}
	}
}
