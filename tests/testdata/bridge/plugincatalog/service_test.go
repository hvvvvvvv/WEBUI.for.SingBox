package plugincatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/plugins"
)

func jsonBytes(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func catalogFixture(t *testing.T, sources ...string) (Index, map[string][]byte) {
	t.Helper()
	index := Index{APIVersion: APIVersion}
	files := make(map[string][]byte)
	for number, source := range sources {
		digest := sha256.Sum256([]byte(source))
		entry := Entry{
			ID: fmt.Sprint("plugin-", number), Name: fmt.Sprint("Plugin ", number), Version: "v1.0.0",
			Triggers: []string{plugins.OnSubscribe}, Configuration: map[string]any{"nested": map[string]any{"value": "original"}},
			Path: fmt.Sprintf("plugins/test-%d.js", number), SHA256: hex.EncodeToString(digest[:]),
		}
		index.Plugins = append(index.Plugins, entry)
		files[entry.Path] = []byte(source)
	}
	files["index.json"] = jsonBytes(t, index)
	return index, files
}

func readFixture(files map[string][]byte) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if value, exists := files[path]; exists {
			return value, nil
		}
		return nil, os.ErrNotExist
	}
}

func TestReadVerifiesDigestPathMetadataAndCompilesWithoutExecution(t *testing.T) {
	index, files := catalogFixture(t, "throw Error('catalog must never evaluate installation source'); function onSubscribe(p) { return p }")
	items, err := Read(readFixture(files))
	if err != nil || len(items) != 1 || items[0].Source != string(files[index.Plugins[0].Path]) {
		t.Fatalf("compile-only catalog failed: %#v %v", items, err)
	}
	for name, mutate := range map[string]func(*Index, map[string][]byte){
		"wrong digest":    func(i *Index, f map[string][]byte) { i.Plugins[0].SHA256 = strings.Repeat("0", 64) },
		"invalid digest":  func(i *Index, f map[string][]byte) { i.Plugins[0].SHA256 = "not-a-digest" },
		"absolute path":   func(i *Index, f map[string][]byte) { i.Plugins[0].Path = "https://example.test/script.js" },
		"traversal":       func(i *Index, f map[string][]byte) { i.Plugins[0].Path = "plugins/../outside.js" },
		"backslash":       func(i *Index, f map[string][]byte) { i.Plugins[0].Path = `plugins\outside.js` },
		"encoded path":    func(i *Index, f map[string][]byte) { i.Plugins[0].Path = "plugins/%2e%2e.js" },
		"wrong API":       func(i *Index, f map[string][]byte) { i.APIVersion = "webui.router/v1" },
		"unknown trigger": func(i *Index, f map[string][]byte) { i.Plugins[0].Triggers = []string{"on::startup"} },
		"duplicate ID":    func(i *Index, f map[string][]byte) { i.Plugins[1].ID = i.Plugins[0].ID },
		"duplicate path":  func(i *Index, f map[string][]byte) { i.Plugins[1].Path = i.Plugins[0].Path },
		"missing script":  func(i *Index, f map[string][]byte) { delete(f, i.Plugins[0].Path) },
	} {
		t.Run(name, func(t *testing.T) {
			index, files := catalogFixture(t, "function onSubscribe(p) { return p }", "function onSubscribe(p) { p.reverse(); return p }")
			mutate(&index, files)
			files["index.json"] = jsonBytes(t, index)
			if items, err := Read(readFixture(files)); err == nil || items != nil {
				t.Fatalf("invalid catalog returned partial or accepted data: %#v %v", items, err)
			}
		})
	}
	for _, source := range []string{"function {", "export function onSubscribe(p) {return p}"} {
		_, files := catalogFixture(t, source)
		if _, err := Read(readFixture(files)); !errors.Is(err, plugins.ErrInvalid) {
			t.Fatalf("invalid source accepted: %v", err)
		}
	}
	for _, raw := range []string{`{"apiVersion":"webui.plugins/v1","plugins":[],"extra":true}`, string(files["index.json"]) + " {}"} {
		files["index.json"] = []byte(raw)
		if _, err := Read(readFixture(files)); err == nil {
			t.Fatal("invalid index fields or trailing JSON accepted")
		}
	}
}

func TestReadBoundsIndexScriptsCountAndTotalSize(t *testing.T) {
	_, files := catalogFixture(t, "function onSubscribe(p) { return p }")
	files["index.json"] = []byte(strings.Repeat(" ", (256<<10)+1))
	if _, err := Read(readFixture(files)); err == nil || !strings.Contains(err.Error(), "index exceeds") {
		t.Fatalf("index limit ignored: %v", err)
	}
	_, files = catalogFixture(t, "/*"+strings.Repeat("x", maxScriptBytes)+"*/")
	if _, err := Read(readFixture(files)); err == nil || !strings.Contains(err.Error(), "size limits") {
		t.Fatalf("script limit ignored: %v", err)
	}
	for _, count := range []int{0, 33} {
		sources := make([]string, count)
		for index := range sources {
			sources[index] = "function onSubscribe(p) {return p}"
		}
		_, files = catalogFixture(t, sources...)
		if _, err := Read(readFixture(files)); err == nil {
			t.Fatalf("count %d accepted", count)
		}
	}
	sources := make([]string, 17)
	for index := range sources {
		sources[index] = "/*" + strings.Repeat("x", maxScriptBytes-4) + "*/"
	}
	_, files = catalogFixture(t, sources...)
	if _, err := Read(readFixture(files)); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("aggregate limit ignored: %v", err)
	}
	// Escaping source for the cache can exceed its limit before raw source does.
	sources = []string{"/*" + strings.Repeat("\t", maxScriptBytes-4) + "*/"}
	for len(sources) < 9 {
		sources = append(sources, sources[0])
	}
	_, files = catalogFixture(t, sources...)
	if _, err := Read(readFixture(files)); err == nil || !strings.Contains(err.Error(), "serialized size") {
		t.Fatalf("serialized size limit ignored: %v", err)
	}
}

func TestRemoteRefreshCacheCloneAndFailedUpdateFallback(t *testing.T) {
	_, files := catalogFixture(t, "throw Error('download only'); function onSubscribe(p) { return p }")
	var requestCount atomic.Int64
	var corrupt atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if r.Header.Get("User-Agent") != "WEBUI.for.SingBox-Plugin-Hub" {
			t.Error("missing catalog User-Agent")
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if corrupt.Load() && path != "index.json" {
			_, _ = w.Write([]byte("modified source"))
			return
		}
		data, exists := files[path]
		if !exists {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	service := NewService(cachePath)
	service.baseURL = server.URL + "/"
	initial, err := service.Get(context.Background(), false)
	if err != nil || initial.Source != "bundled" || len(initial.Plugins) != bundledCount(t) || requestCount.Load() != 0 {
		t.Fatalf("offline catalog made a request or failed: %#v %v", initial, err)
	}
	remote, err := service.Get(context.Background(), true)
	if err != nil || remote.Source != "remote" || remote.Warning != "" || remote.Repository != Repository || len(remote.Plugins) != 1 {
		t.Fatalf("refresh failed: %#v %v", remote, err)
	}
	disk, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	remote.Plugins[0].Source = "changed caller source"
	remote.Plugins[0].Triggers[0] = plugins.OnManual
	remote.Plugins[0].Configuration["nested"].(map[string]any)["value"] = "changed"
	throttled, err := service.Get(context.Background(), true)
	if err != nil || throttled.Source != "cache" || requestCount.Load() != 2 || throttled.Plugins[0].Source == remote.Plugins[0].Source || throttled.Plugins[0].Configuration["nested"].(map[string]any)["value"] != "original" {
		t.Fatalf("throttling or clone isolation failed: %#v %v", throttled, err)
	}
	corrupt.Store(true)
	service.lastAttempt = time.Now().Add(-time.Minute)
	fallback, err := service.Get(context.Background(), true)
	if err != nil || fallback.Source != "cache" || !strings.Contains(fallback.Warning, "checksum mismatch") || fallback.Plugins[0].Source != throttled.Plugins[0].Source {
		t.Fatalf("failed refresh replaced previous catalog: %#v %v", fallback, err)
	}
	after, err := os.ReadFile(cachePath)
	if err != nil || string(after) != string(disk) {
		t.Fatal("failed refresh overwrote offline cache")
	}
	restarted := NewService(cachePath)
	cached, err := restarted.Get(context.Background(), false)
	if err != nil || cached.Source != "cache" || cached.Plugins[0].Source != throttled.Plugins[0].Source {
		t.Fatalf("restart did not load validated cache: %#v %v", cached, err)
	}
}

func TestRemoteFallbackStatusRedirectOversizeAndInvalidCache(t *testing.T) {
	for _, raw := range []string{"null", `[{"id":"bad","source":"function {"}]`, strings.Repeat("x", maxCatalogBytes+1)} {
		cachePath := filepath.Join(t.TempDir(), "catalog.json")
		if err := os.WriteFile(cachePath, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := NewService(cachePath).Get(context.Background(), false)
		if err != nil || result.Source != "bundled" || len(result.Plugins) != bundledCount(t) {
			t.Fatalf("corrupt cache blocked bundled fallback: %#v %v", result, err)
		}
	}
	var followed atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Add(1) }))
	defer target.Close()
	for _, mode := range []string{"HTTP", "redirect", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "HTTP":
					http.Error(w, "failure", http.StatusServiceUnavailable)
				case "redirect":
					http.Redirect(w, r, target.URL, http.StatusFound)
				case "oversize":
					_, _ = w.Write([]byte(strings.Repeat("x", maxScriptBytes+1)))
				}
			}))
			defer server.Close()
			service := NewService("")
			service.baseURL = server.URL + "/"
			result, err := service.Get(context.Background(), true)
			if err != nil || result.Source != "bundled" || result.Warning == "" || len(result.Plugins) != bundledCount(t) {
				t.Fatalf("remote failure did not keep bundled catalog: %#v %v", result, err)
			}
		})
	}
	if followed.Load() != 0 {
		t.Fatal("catalog followed an external redirect")
	}
	service := NewService("")
	service.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := service.Get(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("catalog lock ignored cancellation: %v", err)
	}
	<-service.gate
}

func TestRefreshKeepsRemoteResultWhenCacheWriteFailsAndSerializesConcurrentCalls(t *testing.T) {
	_, files := catalogFixture(t, "function onSubscribe(p) { return p }")
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(files[strings.TrimPrefix(r.URL.Path, "/")])
	}))
	defer server.Close()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(filepath.Join(blocker, "cache.json"))
	service.baseURL = server.URL + "/"
	type response struct {
		result Result
		err    error
	}
	results := make(chan response, 8)
	var group sync.WaitGroup
	for index := 0; index < cap(results); index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := service.Get(context.Background(), true)
			results <- response{result: result, err: err}
		}()
	}
	group.Wait()
	close(results)
	remote := 0
	for response := range results {
		if response.err != nil || len(response.result.Plugins) != 1 || !strings.Contains(response.result.Warning, "could not be saved") {
			t.Fatalf("cache failure discarded downloaded catalog: %#v %v", response.result, response.err)
		}
		if response.result.Source == "remote" {
			remote++
		}
	}
	if requests.Load() != 2 || remote != 1 {
		t.Fatalf("concurrent refresh bypassed gate/throttle: %d requests, %d remote results", requests.Load(), remote)
	}
}
