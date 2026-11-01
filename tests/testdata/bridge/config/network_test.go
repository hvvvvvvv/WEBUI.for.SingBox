package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/network"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"
)

func TestNetworkSettingsPersistAndRejectInvalidChanges(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	store, err := NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	if store.Current().RequestProxyMode != network.ModeEnvironment {
		t.Fatal("legacy defaults did not retain environment proxy")
	}
	cfg := store.Current()
	cfg.RequestProxyMode = network.ModeCustom
	cfg.CustomProxy = "socks5://user:pass@127.0.0.1:1080"
	cfg.UserAgent = "custom-agent"
	cfg.GitHubAPIToken = "test-token"
	cfg.GitHubDownloadMirror = "https://mirror.invalid/{url}"
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	actual := reloaded.Current()
	if actual.RequestProxyMode != cfg.RequestProxyMode || actual.CustomProxy != cfg.CustomProxy || actual.UserAgent != cfg.UserAgent || actual.GitHubAPIToken != cfg.GitHubAPIToken || actual.GitHubDownloadMirror != cfg.GitHubDownloadMirror {
		t.Fatalf("network settings not persisted: %#v", actual)
	}
	previous, _ := os.ReadFile(paths.Resolve(appConfigPath))
	invalid := cfg
	invalid.CustomProxy = "file:///private"
	if store.Save(invalid) == nil {
		t.Fatal("invalid proxy saved")
	}
	after, _ := os.ReadFile(paths.Resolve(appConfigPath))
	if string(after) != string(previous) || store.Current().CustomProxy != cfg.CustomProxy {
		t.Fatal("failed save changed persisted settings")
	}
}

func TestGitHubMirrorSwitchMigration(t *testing.T) {
	for _, test := range []struct {
		name, saved string
		disabled    bool
	}{
		{"new configuration", "", true},
		{"legacy empty URL", "githubDownloadMirror: ''\n", true},
		{"legacy configured URL", "githubDownloadMirror: https://mirror.invalid/{url}\n", false},
		{"explicit enabled empty URL", "githubDownloadMirror: ''\ngithubDownloadAccelerationDisabled: false\n", false},
		{"explicit disabled configured URL", "githubDownloadMirror: https://mirror.invalid/{url}\ngithubDownloadAccelerationDisabled: true\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := storage.NewPaths(t.TempDir())
			if test.saved != "" {
				if err := os.MkdirAll(filepath.Dir(paths.Resolve(appConfigPath)), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.Resolve(appConfigPath), []byte(test.saved), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Loading persists the migration. Check both the initial load and
			// another process reading that saved switch, including false + "".
			for range 2 {
				store, err := NewStore(paths)
				if err != nil {
					t.Fatal(err)
				}
				if store.Current().GitHubDownloadAccelerationDisabled != test.disabled {
					t.Fatalf("unexpected migrated switch: %#v", store.Current())
				}
				if strings.Contains(test.saved, "https://mirror.invalid") && store.Current().GitHubDownloadMirror != "https://mirror.invalid/{url}" {
					t.Fatal("migration cleared the stored mirror URL")
				}
			}
		})
	}
}

func TestGitHubMirrorSwitchRPCPersistsWithoutClearingURL(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	store, err := NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	service := NewAppService(store)
	cfg := appConfigToProto(store.Current())
	cfg.GithubDownloadMirror = "https://mirror.invalid/{url}"
	const target = "https://github.com/example/project/releases/download/v1/asset.tar.gz"
	for _, disabled := range []bool{false, true, false} {
		cfg.GithubDownloadAccelerationDisabled = disabled
		response, err := service.SaveAppConfig(context.Background(), connect.NewRequest(&appv1.SaveAppConfigRequest{Config: cfg}))
		if err != nil {
			t.Fatal(err)
		}
		if response.Msg.GetConfig().GetGithubDownloadAccelerationDisabled() != disabled || response.Msg.GetConfig().GetGithubDownloadMirror() != cfg.GithubDownloadMirror {
			t.Fatalf("RPC lost the switch or URL: %v", response.Msg)
		}
		reloaded, err := NewStore(paths)
		if err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequest(http.MethodGet, target, nil)
		if err := network.PrepareRequest(request, reloaded.NetworkOptions(), true); err != nil {
			t.Fatal(err)
		}
		want := "https://mirror.invalid/" + target
		if disabled {
			want = target
		}
		if request.URL.String() != want || reloaded.Current().GitHubDownloadMirror != cfg.GithubDownloadMirror {
			t.Fatalf("saved switch not applied or URL lost: target=%s config=%#v", request.URL, reloaded.Current())
		}
	}
}

func TestNetworkProbeUsesPersistedBackendProxyAndHeaders(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "http://unresolvable.invalid/probe" || r.Header.Get("User-Agent") != "router/test" {
			t.Errorf("unexpected request: %s %q", r.URL, r.Header.Get("User-Agent"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("GitHub token leaked to probe target")
		}
		w.WriteHeader(204)
	}))
	defer proxy.Close()
	store, err := NewStore(storage.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Current()
	cfg.RequestProxyMode = network.ModeCustom
	cfg.CustomProxy = proxy.URL
	cfg.UserAgent = "router/test"
	cfg.GitHubAPIToken = "private-token"
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	service := NewAppService(store)
	response, err := service.TestNetworkRequest(context.Background(), connect.NewRequest(&appv1.TestNetworkRequestRequest{Url: "http://unresolvable.invalid/probe"}))
	if err != nil || response.Msg.StatusCode != 204 {
		t.Fatalf("response=%v error=%v", response, err)
	}
	for _, target := range []string{"file:///etc/passwd", "http://user:secret@localhost/"} {
		if _, err := service.TestNetworkRequest(context.Background(), connect.NewRequest(&appv1.TestNetworkRequestRequest{Url: target})); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("invalid target error: %v", err)
		}
	}
	service.SetNetworkOptions(func() network.Options { return network.Options{Mode: network.ModeCore} })
	if _, err := service.TestNetworkRequest(context.Background(), connect.NewRequest(&appv1.TestNetworkRequestRequest{})); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "core") {
		t.Fatalf("missing core error: %v", err)
	}
}
