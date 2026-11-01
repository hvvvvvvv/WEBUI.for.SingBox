package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"
)

func TestSubscriptionMetadataFailureRestoresAllChangedCaches(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "batch"}[batch], func(t *testing.T) {
			withTempBasePath(t)
			service := newTestRuntimeService(nil)
			items := []subscription{{ID: "existing", Type: "Http", URL: "https://fixture.invalid/existing", Proxies: []proxyRef{{ID: "old-id", Tag: "old", Type: "direct"}}}}
			if batch {
				items = append(items, subscription{ID: "new", Type: "Http", URL: "https://fixture.invalid/new"})
			}
			if err := saveSubscriptions(items); err != nil {
				t.Fatal(err)
			}
			const oldCache = `[{"type":"direct","tag":"old"}]`
			cachePath := GetPath(subscriptionContentPath("existing"))
			if err := storage.AtomicWriteFile(cachePath, []byte(oldCache), 0600); err != nil {
				t.Fatal(err)
			}
			metadataPath := GetPath(subscriptionsFilePath)
			previous := subscriptionHTTPRequest
			t.Cleanup(func() { subscriptionHTTPRequest = previous })
			calls := 0
			subscriptionHTTPRequest = func(string, string, map[string]string, string, bool, int) (*http.Response, string, error) {
				calls++
				if calls == len(items) {
					// Metadata was readable at operation start but replacement now
					// fails. This is a portable real-filesystem failure on Windows
					// and privileged Linux, without relying on chmod enforcement.
					if err := os.Rename(metadataPath, metadataPath+".original"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(metadataPath, 0700); err != nil {
						t.Fatal(err)
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header)}, `[{"type":"direct","tag":"new"}]`, nil
			}
			var err error
			if batch {
				_, err = service.UpdateAllSubscriptions(context.Background(), connect.NewRequest(&appv1.UpdateAllSubscriptionsRequest{}))
			} else {
				_, err = service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "existing"}))
			}
			if err == nil {
				t.Fatal("metadata replacement unexpectedly succeeded")
			}
			cached, readErr := os.ReadFile(cachePath)
			if readErr != nil || string(cached) != oldCache {
				t.Fatalf("failed refresh left new cache with old references: %s %v", cached, readErr)
			}
			if batch {
				if _, err := os.Stat(GetPath(subscriptionContentPath("new"))); !os.IsNotExist(err) {
					t.Fatalf("failed first refresh left a new cache: %v", err)
				}
			}
		})
	}
}

func TestRulesetMetadataFailureRestoresAllChangedCaches(t *testing.T) {
	for _, format := range []string{"source", "binary"} {
		for _, batch := range []bool{false, true} {
			t.Run(format+map[bool]string{false: "/single", true: "/batch"}[batch], func(t *testing.T) {
				withTempBasePath(t)
				metadataPath := GetPath(runtimeRulesetsFilePath)
				calls := 0
				count := 1
				if batch {
					count = 2
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls++
					if calls == count {
						if err := os.Rename(metadataPath, metadataPath+".original"); err != nil {
							t.Error(err)
							w.WriteHeader(500)
							return
						}
						if err := os.Mkdir(metadataPath, 0700); err != nil {
							t.Error(err)
							w.WriteHeader(500)
							return
						}
					}
					_, _ = w.Write([]byte(`{"version":2,"rules":[{"domain":["new.example"]}]}`))
				}))
				defer server.Close()
				items := []ruleset{{ID: "existing", Tag: "existing", Type: "Http", Format: format, URL: server.URL, Path: "data/rulesets/existing.cache"}}
				if batch {
					items = append(items, ruleset{ID: "new", Tag: "new", Type: "Http", Format: format, URL: server.URL, Path: "data/rulesets/new.cache"})
				}
				if err := saveRulesets(items); err != nil {
					t.Fatal(err)
				}
				const oldCache = `{"version":2,"rules":[{"domain":["old.example"]}]}`
				cachePath := GetPath(items[0].Path)
				if err := storage.AtomicWriteFile(cachePath, []byte(oldCache), 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				if batch {
					_, _, err = updateAllRulesetsLocked()
				} else {
					_, _, err = updateRulesetLocked("existing")
				}
				if err == nil {
					t.Fatal("metadata replacement unexpectedly succeeded")
				}
				cached, readErr := os.ReadFile(cachePath)
				if readErr != nil || string(cached) != oldCache {
					t.Fatalf("failed refresh changed ruleset cache: %s %v", cached, readErr)
				}
				if batch {
					if _, err := os.Stat(GetPath(items[1].Path)); !os.IsNotExist(err) {
						t.Fatalf("failed first refresh left a new ruleset: %v", err)
					}
				}
				assertNoRefreshBackups(t, filepath.Dir(cachePath))
			})
		}
	}
}

func assertNoRefreshBackups(t *testing.T, directory string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(directory, ".*.refresh-*"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("cache refresh leaked backups: %v %v", paths, err)
	}
}

func TestRefreshTransactionCommitPreservesNewDataAndRemovesBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	transaction := &refreshTransaction{}
	if err := transaction.write(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := transaction.finish(true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("commit did not retain refreshed cache: %q %v", data, err)
	}
	assertNoRefreshBackups(t, filepath.Dir(path))
}

func TestRefreshCopyFallbackCleansFailures(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	backup := filepath.Join(dir, "backup")
	if err := os.WriteFile(source, []byte("previous cache"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyRefreshBackup(source, backup, 0600); err == nil {
		t.Fatal("backup destination open failure was ignored")
	}
	if data, err := os.ReadFile(backup); err != nil || string(data) != "occupied" {
		t.Fatalf("open failure changed existing file: %q %v", data, err)
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	if err := copyRefreshBackup(dir, backup, 0600); err == nil {
		t.Fatal("directory read failure was ignored")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("failed copy leaked backup: %v", err)
	}
	if err := copyRefreshBackup(source, backup, 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(backup); err != nil || string(data) != "previous cache" {
		t.Fatalf("copy fallback lost original content: %q %v", data, err)
	}
}
