package plugincatalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"guiforcores/bridge/plugins"
)

func TestLegacyCacheMergesBundledUpgradeWithoutChangingInstalledOrCacheFiles(t *testing.T) {
	bundled, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	legacy := cloneItems(bundled[:4])
	for i := range legacy {
		legacy[i].Version = "1.0.0"
		legacy[i].Conflicts = nil
		legacy[i].Source = "function onSubscribe(p){return p};function onRun(p){return p}"
	}
	directory := t.TempDir()
	cachePath := filepath.Join(directory, "catalog.json")
	cache := jsonBytes(t, legacy)
	if err := os.WriteFile(cachePath, cache, 0600); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(directory, "plugins.json")
	installed := []byte(`{"plugins":[],"revision":17}`)
	if err := os.WriteFile(installedPath, installed, 0600); err != nil {
		t.Fatal(err)
	}
	s := NewService(cachePath)
	// No refresh and no live server: upgrade must be usable offline.
	s.baseURL = "http://127.0.0.1:1/"
	result, err := s.Get(context.Background(), false)
	if err != nil || result.Source != "cache" || result.Warning != "" || len(result.Plugins) != len(bundled) {
		t.Fatalf("legacy cache hid bundled plugins: %+v %v", result, err)
	}
	for i, item := range bundled {
		if result.Plugins[i].ID != item.ID || result.Plugins[i].Version != item.Version || result.Plugins[i].Source != item.Source {
			t.Fatalf("bundled upgrade not applied: %s", item.ID)
		}
	}
	if len(result.Plugins[0].Conflicts) != 2 {
		t.Fatal("upgrade lost new conflict metadata")
	}
	actual, _ := os.ReadFile(cachePath)
	if string(actual) != string(cache) {
		t.Fatal("offline merge rewrote downloaded cache")
	}
	actual, _ = os.ReadFile(installedPath)
	if string(actual) != string(installed) {
		t.Fatal("catalog merge rewrote installed plugins")
	}
}

func TestCachePreservesNewerUnknownAndEqualVersions(t *testing.T) {
	bundled, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	cached := cloneItems(bundled[:4])
	cached[0].Version = "99.0.0"
	cached[1].Version = "custom-nightly"
	cached[2].Version = "v" + bundled[2].Version + "+custom"
	for i := range cached {
		cached[i].Source = fmt.Sprintf("// cached %d\nfunction onSubscribe(p){return p};function onRun(p){return p}", i)
	}
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(cachePath, jsonBytes(t, cached), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := NewService(cachePath).Get(context.Background(), false)
	if err != nil || len(result.Plugins) != len(bundled) {
		t.Fatalf("cache merge failed: %+v %v", result, err)
	}
	for i := range cached {
		if result.Plugins[i].Version != cached[i].Version || result.Plugins[i].Source != cached[i].Source {
			t.Fatalf("cache version/source was downgraded: %+v", result.Plugins[i])
		}
	}
}

func TestSuccessfulRemoteRefreshRemainsAuthoritativeAfterOfflineMerge(t *testing.T) {
	bundled, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(cachePath, jsonBytes(t, bundled[:4]), 0600); err != nil {
		t.Fatal(err)
	}
	_, files := catalogFixture(t, "function onSubscribe(p){return p}")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(files[strings.TrimPrefix(r.URL.Path, "/")])
	}))
	defer server.Close()
	s := NewService(cachePath)
	s.baseURL = server.URL + "/"
	if initial, err := s.Get(context.Background(), false); err != nil || len(initial.Plugins) != len(bundled) {
		t.Fatalf("offline upgrade failed: %v", err)
	}
	remote, err := s.Get(context.Background(), true)
	if err != nil || remote.Source != "remote" || len(remote.Plugins) != 1 {
		t.Fatalf("remote refresh not authoritative: %+v %v", remote, err)
	}
	current, err := s.Get(context.Background(), false)
	if err != nil || len(current.Plugins) != 1 || current.Plugins[0].ID != remote.Plugins[0].ID {
		t.Fatalf("bundled catalog remerged into fresh remote: %+v %v", current, err)
	}
}

func cachePlugin(id, version, source string) plugins.Plugin {
	return plugins.Plugin{ID: id, Name: id, Version: version, Triggers: []string{plugins.OnSubscribe}, Source: source}
}

func TestCacheMergeCapacityDoesNotEvictOrDowngradeCachedEntries(t *testing.T) {
	cached := make([]plugins.Plugin, plugins.MaxPlugins)
	for i := range cached {
		cached[i] = cachePlugin(fmt.Sprint("cached-", i), "1.0.0", "function onSubscribe(p){return p}")
	}
	updated := cached[0]
	updated.Version = "1.1.0"
	merged, warning := mergeBundledCatalog(cached, []plugins.Plugin{updated, cachePlugin("new", "1.0.0", updated.Source)})
	if len(merged) != plugins.MaxPlugins || warning == "" || merged[0].Version != "1.1.0" || merged[len(merged)-1].ID != cached[len(cached)-1].ID {
		t.Fatalf("count merge dropped cache or missed warning: count=%d warning=%q", len(merged), warning)
	}
	if cached[0].Version != "1.0.0" {
		t.Fatal("merge mutated caller cache")
	}
	bigSource := func(size int) string { return "/*" + strings.Repeat("x", size-4) + "*/" }
	cached = make([]plugins.Plugin, 20)
	for i := range cached {
		cached[i] = cachePlugin(fmt.Sprint("large-", i), "1.0.0", bigSource(209000))
	}
	if err := validateItems(cached); err != nil {
		t.Fatal(err)
	}
	updated = cached[0]
	updated.Version, updated.Source = "2.0.0", bigSource(262000)
	merged, warning = mergeBundledCatalog(cached, []plugins.Plugin{updated, cachePlugin("small-new", "1.0.0", "function onSubscribe(p){return p}")})
	if warning == "" || merged[0].Version != "1.0.0" || len(merged) != 21 || len(jsonBytes(t, merged)) > maxCatalogBytes {
		t.Fatalf("size merge exceeded limit or evicted cache: count=%d warning=%q bytes=%d", len(merged), warning, len(jsonBytes(t, merged)))
	}
}

func TestCatalogSemverUpgradePrecedence(t *testing.T) {
	for _, tc := range []struct {
		candidate, cached string
		newer             bool
	}{
		{"1.2.0", "1.1.0", true}, {"v1.2.0", "1.1.0", true}, {"1.2.0", "v1.2.0", false}, {"1.2.0+one", "1.2.0+two", false},
		{"1.0.0", "1.0.0-rc.9", true}, {"1.0.0-rc.9", "1.0.0", false}, {"1.0.0-rc.10", "1.0.0-rc.9", true},
		{"1.0.0-beta", "1.0.0-alpha.9", true}, {"1.0.0-1", "1.0.0-alpha", false}, {"1.0.0-alpha", "1.0.0-1", true},
		{"1.0.0-alpha.1", "1.0.0-alpha", true}, {"1.0.0-alpha", "1.0.0-alpha.1", false},
		{"1.10.0", "1.9.99", true}, {"10.0.0", "9.99.99", true}, {"1.2.0", "99.0.0", false},
		{"100000000000000000000.0.0", "99999999999999999999.0.0", true},
		{"2.0.0", "nightly", false}, {"next", "1.0.0", false}, {"2.0.0", "1.0", false}, {"2.0.0", "1", false},
		{"02.0.0", "1.0.0", false}, {"2.0.0", "01.0.0", false}, {"2.0.0-01", "1.0.0", false}, {"2.0.0", "1.0.0-01", false},
		{"2.0.0-rc..1", "1.0.0", false}, {"2.0.0+", "1.0.0", false}, {"2.0.0+01", "1.0.0", true},
	} {
		if got := strictlyNewerVersion(tc.candidate, tc.cached); got != tc.newer {
			t.Errorf("newer(%q,%q)=%v, want %v", tc.candidate, tc.cached, got, tc.newer)
		}
	}
}
