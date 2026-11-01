package plugincatalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"guiforcores/bridge/plugins"
	"guiforcores/bridge/storage"
)

func TestRetiredCatalogPackagesCannotReturnFromCacheOrRemote(t *testing.T) {
	ids := []string{"plugin-index-the-proxy-name", "plugin-modify-proxy-name", "plugin-beautiful-node-name", "plugin-duplicate-proxies-removal", "plugin-network-info", "plugin-geoip-search"}
	index, _ := catalogFixture(t, "function onSubscribe(p){return p}")
	installed, err := plugins.NewService(storage.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		entry := index.Plugins[0]
		entry.ID, entry.Path = id, "plugins/"+id+".js"
		// Older cached catalogs did not yet mark these IDs deprecated.
		index.Plugins = append(index.Plugins, entry)
		if _, err := installed.Save(entry.plugin("function onSubscribe(p){return p}"), installed.List().Revision); err != nil {
			t.Fatal(err)
		}
	}
	deprecated := index.Plugins[0]
	deprecated.ID, deprecated.Path, deprecated.Deprecated = "another-retired", "plugins/another-retired.js", true
	index.Plugins = append(index.Plugins, deprecated)
	before := installed.List()
	var indexes, sources atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			indexes.Add(1)
			_, _ = w.Write(jsonBytes(t, index))
			return
		}
		sources.Add(1)
		http.Error(w, "retired source must not be requested", 500)
	}))
	defer server.Close()
	cache := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(cache, jsonBytes(t, index), 0600); err != nil {
		t.Fatal(err)
	}
	service := NewService(cache)
	service.baseURL = server.URL + "/"
	for _, refresh := range []bool{false, true} {
		result, err := service.Get(context.Background(), refresh)
		if err != nil || len(result.Plugins) != 1 || result.Plugins[0].ID != index.Plugins[0].ID {
			t.Fatalf("retired packages returned: %+v %v", result, err)
		}
		for _, id := range append(ids, deprecated.ID) {
			if _, err := service.Download(context.Background(), id); !errors.Is(err, plugins.ErrNotFound) {
				t.Fatalf("retired %s remained downloadable: %v", id, err)
			}
		}
	}
	if indexes.Load() != 1 || sources.Load() != 0 {
		t.Fatalf("browse/removal downloaded scripts: indexes=%d sources=%d", indexes.Load(), sources.Load())
	}
	if !reflect.DeepEqual(before, installed.List()) {
		t.Fatal("catalog filtering changed installed user scripts")
	}
}
