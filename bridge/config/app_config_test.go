package config

import (
	"context"
	"os"
	"testing"

	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
	"gopkg.in/yaml.v3"
)

func TestStoreWritesDefaultsWhenMissing(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	store, err := NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Current()
	if cfg.AutoStartKernel || cfg.AutoRestartKernel || !cfg.RollingRelease || cfg.Branch != "main" {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	if len(cfg.Main.Args) == 0 || len(cfg.Alpha.Args) == 0 {
		t.Fatalf("expected default runtime args: %#v", cfg)
	}
	if _, err := os.Stat(paths.Resolve(appConfigPath)); err != nil {
		t.Fatalf("expected config file: %v", err)
	}
}

func TestStoreBackfillsMissingFields(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	if err := storage.WriteYAML(paths, appConfigPath, map[string]any{"branch": "alpha"}); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Current()
	if cfg.Branch != "alpha" || !cfg.RollingRelease || len(cfg.Main.Args) == 0 {
		t.Fatalf("expected normalized config: %#v", cfg)
	}
}

func TestAppServiceSavesConfig(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	store, err := NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	service := NewAppService(store)
	_, err = service.SaveAppConfig(context.Background(), connect.NewRequest(&appv1.SaveAppConfigRequest{
		Config: &appv1.AppConfig{
			AutoStartKernel:   true,
			AutoRestartKernel: true,
			UserAgent:         "Agent/2.0",
			GithubApiToken:    "token-2",
			Branch:            appv1.KernelBranch_KERNEL_BRANCH_ALPHA,
			Profile:           "profile-2",
			Main:              &appv1.CoreRuntimeConfig{Args: []string{"main"}},
			Alpha:             &appv1.CoreRuntimeConfig{Env: map[string]string{"C": "D"}, Args: []string{"alpha"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Current()
	if !cfg.AutoStartKernel || !cfg.AutoRestartKernel || cfg.Branch != "alpha" || cfg.GitHubAPIToken != "token-2" {
		t.Fatalf("unexpected saved config: %#v", cfg)
	}

	data, err := os.ReadFile(paths.Resolve(appConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	var stored AppConfig
	if err := yaml.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Profile != "profile-2" || stored.Alpha.Env["C"] != "D" {
		t.Fatalf("unexpected persisted config: %#v", stored)
	}
}

type recordingAppConfigChanges struct {
	previous AppConfig
	current  AppConfig
	calls    int
}

func (r *recordingAppConfigChanges) AppConfigChanged(previous AppConfig, current AppConfig) {
	r.previous = previous
	r.current = current
	r.calls++
}

func TestAppServiceReportsPersistedConfigChange(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	store, err := NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	service := NewAppService(store)
	handler := &recordingAppConfigChanges{}
	service.SetChangeHandler(handler)

	_, err = service.SaveAppConfig(context.Background(), connect.NewRequest(&appv1.SaveAppConfigRequest{
		Config: &appv1.AppConfig{
			Branch:  appv1.KernelBranch_KERNEL_BRANCH_ALPHA,
			Profile: "selected",
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if handler.calls != 1 || handler.previous.Branch != "main" || handler.current.Branch != "alpha" || handler.current.Profile != "selected" {
		t.Fatalf("config change notification = %#v", handler)
	}
}
