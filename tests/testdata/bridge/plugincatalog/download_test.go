package plugincatalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/plugins"
)

func TestReadIndexValidatesOnlyMetadataWithoutReadingOrCompilingSource(t *testing.T) {
	index, files := catalogFixture(t, "function {", "throw Error('must not execute')")
	var paths []string
	actual, err := ReadIndex(func(path string) ([]byte, error) { paths = append(paths, path); return files[path], nil })
	if err != nil || len(actual.Plugins) != 2 || !reflect.DeepEqual(paths, []string{"index.json"}) {
		t.Fatalf("metadata fetch accessed scripts: %+v %v paths=%v", actual, err, paths)
	}
	if _, err := Read(readFixture(files)); !errors.Is(err, plugins.ErrInvalid) {
		t.Fatalf("full CLI validation no longer checks syntax: %v", err)
	}
	index.Plugins[0].ConfigurationSchema = []plugins.Setting{{Key: "mode", Title: "Mode", Component: "Shell", Value: "x"}}
	files["index.json"] = jsonBytes(t, index)
	if _, err := ReadIndex(readFixture(files)); !errors.Is(err, plugins.ErrInvalid) {
		t.Fatalf("metadata accepted unsafe schema: %v", err)
	}
}

func TestCatalogGetOnlyFetchesIndexAndExplicitDownloadFetchesChosenScript(t *testing.T) {
	index, files := catalogFixture(t, "throw Error('compile only');function onSubscribe(p){return p}", "function {")
	var indexRequests, chosenRequests, otherRequests atomic.Int32
	var corrupt atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		switch path {
		case "index.json":
			indexRequests.Add(1)
			_, _ = w.Write(files[path])
		case index.Plugins[0].Path:
			chosenRequests.Add(1)
			if corrupt.Load() {
				_, _ = w.Write([]byte("wrong hash"))
			} else {
				_, _ = w.Write(files[path])
			}
		default:
			otherRequests.Add(1)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cache := filepath.Join(t.TempDir(), "catalog.json")
	s := NewService(cache)
	s.baseURL = server.URL + "/"
	result, err := s.Get(context.Background(), false)
	if err != nil || len(result.Plugins) != 2 || indexRequests.Load() != 1 || chosenRequests.Load() != 0 || otherRequests.Load() != 0 {
		t.Fatalf("first visit fetched source: %+v %v counts=%d/%d/%d", result, err, indexRequests.Load(), chosenRequests.Load(), otherRequests.Load())
	}
	s.lastAttempt = time.Now().Add(-time.Minute)
	if _, err := s.Get(context.Background(), true); err != nil || indexRequests.Load() != 2 || chosenRequests.Load() != 0 || otherRequests.Load() != 0 {
		t.Fatalf("refresh fetched source: %v counts=%d/%d/%d", err, indexRequests.Load(), chosenRequests.Load(), otherRequests.Load())
	}
	raw, _ := os.ReadFile(cache)
	var stored Index
	if err := json.Unmarshal(raw, &stored); err != nil || stored.APIVersion != APIVersion || strings.Contains(string(raw), `"source"`) {
		t.Fatalf("cache is not metadata-only: %s %v", raw, err)
	}
	item, err := s.Download(context.Background(), index.Plugins[0].ID)
	if err != nil || item.Source != string(files[index.Plugins[0].Path]) || chosenRequests.Load() != 1 || otherRequests.Load() != 0 || indexRequests.Load() != 2 {
		t.Fatalf("chosen installation fetched wrong scripts or executed code: %+v %v counts=%d/%d/%d", item, err, indexRequests.Load(), chosenRequests.Load(), otherRequests.Load())
	}
	corrupt.Store(true)
	if _, err := s.Download(context.Background(), index.Plugins[0].ID); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("SHA mismatch accepted: %v", err)
	}
	if _, err := s.Download(context.Background(), "unknown"); !errors.Is(err, plugins.ErrNotFound) {
		t.Fatalf("unknown plugin download: %v", err)
	}
	if chosenRequests.Load() != 2 || otherRequests.Load() != 0 {
		t.Fatal("unknown plugin fetched unselected source")
	}
	after, _ := os.ReadFile(cache)
	if string(after) != string(raw) {
		t.Fatal("script download changed metadata cache")
	}
}

func TestLegacySourceCacheIsIgnoredAndCannotSupplyAnInstallation(t *testing.T) {
	index, files := catalogFixture(t, "function onSubscribe(p){return p}")
	legacy := []plugins.Plugin{index.Plugins[0].plugin("throw Error('old cached source must never install')")}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cache := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(cache, jsonBytes(t, legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewService(cache)
	s.baseURL = server.URL + "/"
	result, err := s.Get(context.Background(), false)
	if err != nil || result.Source != "empty" || len(result.Plugins) != 0 || result.Warning == "" || requests.Load() != 1 {
		t.Fatalf("legacy cache remained usable: %+v %v requests=%d", result, err, requests.Load())
	}
	if _, err := s.Download(context.Background(), legacy[0].ID); err == nil || requests.Load() != 1 {
		t.Fatalf("old source cache installed without download: %v requests=%d", err, requests.Load())
	}
	// A valid metadata index may be cached, but source remains unavailable offline.
	if err := os.WriteFile(cache, files["index.json"], 0600); err != nil {
		t.Fatal(err)
	}
	s = NewService(cache)
	s.baseURL = server.URL + "/"
	if result, err := s.Get(context.Background(), false); err != nil || result.Source != "cache" || requests.Load() != 1 {
		t.Fatalf("metadata-only cache should be browsable offline: %+v %v", result, err)
	}
	if _, err := s.Download(context.Background(), legacy[0].ID); err == nil || requests.Load() != 2 {
		t.Fatalf("installation failed to request selected source: %v requests=%d", err, requests.Load())
	}
}

func TestDownloadHonorsCancellation(t *testing.T) {
	index, files := catalogFixture(t, "function onSubscribe(p){return p}")
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			_, _ = w.Write(files["index.json"])
			return
		}
		close(entered)
		select {
		case <-release:
			_, _ = w.Write(files[index.Plugins[0].Path])
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	s := NewService("")
	s.baseURL = server.URL + "/"
	if _, err := s.Get(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.Download(ctx, index.Plugins[0].ID); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("download ignored cancellation: %v", err)
	}
	close(release)
}

func TestDownloadKeepsSelectedMetadataSnapshot(t *testing.T) {
	index, files := catalogFixture(t, "function onSubscribe(p){return p}")
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			_, _ = w.Write(files["index.json"])
			return
		}
		close(entered)
		<-release
		_, _ = w.Write(files[index.Plugins[0].Path])
	}))
	defer server.Close()
	s := NewService("")
	s.baseURL = server.URL + "/"
	if _, err := s.Get(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	type downloaded struct {
		plugin plugins.Plugin
		err    error
	}
	done := make(chan downloaded, 1)
	go func() {
		plugin, err := s.Download(context.Background(), index.Plugins[0].ID)
		done <- downloaded{plugin, err}
	}()
	<-entered
	s.gate <- struct{}{}
	s.index.Plugins[0].SHA256 = strings.Repeat("0", 64)
	s.index.Plugins[0].Version = "future-catalog"
	s.index.Plugins[0].Configuration["nested"].(map[string]any)["value"] = "future-setting"
	<-s.gate
	close(release)
	result := <-done
	if result.err != nil || result.plugin.Version != index.Plugins[0].Version || result.plugin.Configuration["nested"].(map[string]any)["value"] != "original" || result.plugin.Source != string(files[index.Plugins[0].Path]) {
		t.Fatalf("concurrent catalog change corrupted selected download: %+v %v", result.plugin, result.err)
	}
}
