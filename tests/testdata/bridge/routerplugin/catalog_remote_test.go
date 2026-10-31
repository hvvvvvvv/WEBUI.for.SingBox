package routerplugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogRefreshKeepsKnownGoodOnChecksumFailure(t *testing.T) {
	bundled, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	manifest := bundled[0]
	manifest.Version = "1.1.0"
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	index, _ := json.Marshal(map[string]any{
		"apiVersion": catalogAPI,
		"plugins":    []any{map[string]string{"path": "plugins/example.json", "sha256": hex.EncodeToString(digest[:])}},
	})
	var broken atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			_, _ = w.Write(index)
		case "/plugins/example.json":
			if broken.Load() {
				_, _ = w.Write([]byte("{}"))
				return
			}
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cache := filepath.Join(t.TempDir(), "cache", "catalog.json")
	service := NewCatalogService(cache)
	service.baseURL = server.URL + "/"
	first, err := service.Get(context.Background(), true)
	if err != nil || first.Source != "remote" || len(first.Plugins) != 1 || first.Plugins[0].Version != "1.1.0" || first.Warning != "" {
		t.Fatalf("refresh failed: %#v %v", first, err)
	}
	original, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	broken.Store(true)
	service.lastAttempt = time.Time{}
	fallback, err := service.Get(context.Background(), true)
	if err != nil || fallback.Source != "cache" || fallback.Plugins[0].Version != "1.1.0" || !strings.Contains(fallback.Warning, "checksum mismatch") {
		t.Fatalf("bad update replaced known-good catalog: %#v %v", fallback, err)
	}
	after, err := os.ReadFile(cache)
	if err != nil || string(original) != string(after) {
		t.Fatal("failed update changed cache")
	}
	offline := NewCatalogService(cache)
	reloaded, err := offline.Get(context.Background(), false)
	if err != nil || reloaded.Source != "cache" || reloaded.Plugins[0].Version != "1.1.0" {
		t.Fatalf("offline cache failed: %#v %v", reloaded, err)
	}
}

func TestCatalogRejectsUnsafeIndexWithoutFetchingManifest(t *testing.T) {
	for _, path := range []string{"../outside.json", "https://example.com/plugin.json", "plugins/../../secret.json"} {
		t.Run(path, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"apiVersion": catalogAPI,
					"plugins":    []any{map[string]string{"path": path, "sha256": strings.Repeat("0", 64)}},
				})
			}))
			defer server.Close()
			service := NewCatalogService("")
			service.baseURL = server.URL + "/"
			result, err := service.Get(context.Background(), true)
			if err != nil || result.Source != "bundled" || result.Warning == "" || requests.Load() != 1 {
				t.Fatalf("unsafe path accepted: %#v %v requests=%d", result, err, requests.Load())
			}
		})
	}
}

func TestCatalogRejectsRedirectAndOversizedResponse(t *testing.T) {
	for _, mode := range []string{"redirect", "oversize", "http-error"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, "https://example.com/untrusted", http.StatusFound)
				case "oversize":
					_, _ = w.Write([]byte(strings.Repeat(" ", 64*1024+1)))
				default:
					http.Error(w, "untrusted response body", http.StatusBadGateway)
				}
			}))
			defer server.Close()
			service := NewCatalogService("")
			service.baseURL = server.URL + "/"
			result, err := service.Get(context.Background(), true)
			if err != nil || result.Source != "bundled" || result.Warning == "" || strings.Contains(result.Warning, "untrusted response body") {
				t.Fatalf("invalid response accepted: %#v %v", result, err)
			}
		})
	}
}

func TestCatalogWaitCanBeCanceledAndQueuedRefreshIsCoalesced(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		<-release
		http.Error(w, "offline", http.StatusBadGateway)
	}))
	defer server.Close()
	service := NewCatalogService("")
	service.baseURL = server.URL + "/"
	done := make(chan struct{})
	go func() { defer close(done); _, _ = service.Get(context.Background(), true) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Get(ctx, false)
	if !errors.Is(err, context.Canceled) {
		close(release)
		<-done
		t.Fatalf("canceled waiter did not return: %v", err)
	}
	close(release)
	<-done
	_, err = service.Get(context.Background(), true)
	if err != nil || requests.Load() != 1 {
		t.Fatalf("queued refresh repeated fetch: %d %v", requests.Load(), err)
	}
}
