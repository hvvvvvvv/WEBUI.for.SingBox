package appupdate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"
)

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type updateEvents func(string, ...any)

func (f updateEvents) Publish(name string, values ...any) { f(name, values...) }

func TestAppDownloadRequiresVerifiedArchiveFromConfiguredRepository(t *testing.T) {
	const archive = "new release zip"
	for _, mode := range []string{"success", "corrupted", "missing_digest", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			paths := storage.NewPaths(t.TempDir())
			cache := paths.Resolve(appUpdateCacheFilePath)
			if err := os.MkdirAll(filepath.Dir(cache), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cache, []byte("previous verified update"), 0600); err != nil {
				t.Fatal(err)
			}
			service := NewService(platform.NewService(paths, nil, platform.Environment{}), nil, nil, "1.0", false, logging.LevelInfo, 7)
			service.updatedVersion = "1.1"
			service.events = updateEvents(func(name string, _ ...any) {
				if mode == "canceled" && name == "progress" {
					_, err := service.CancelAppUpdate(context.Background(), connect.NewRequest(&appv1.CancelAppUpdateRequest{ProgressEvent: "progress"}))
					if err != nil {
						t.Error(err)
					}
				}
			})
			previousTransport, previousRepository := http.DefaultTransport, Repository
			t.Cleanup(func() { http.DefaultTransport, Repository = previousTransport, previousRepository })
			Repository = "test-owner/test-fork"
			assetRequests := 0
			http.DefaultTransport = updateTransport(func(request *http.Request) (*http.Response, error) {
				payload := archive
				if request.URL.Host == "api.github.com" {
					if request.URL.Path != "/repos/test-owner/test-fork/releases/latest" {
						t.Fatalf("queried a different update repository: %s", request.URL)
					}
					digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(archive)))
					if mode == "missing_digest" {
						digest = ""
					}
					metadata, _ := json.Marshal(githubRelease{TagName: "v2.0", Assets: []githubAsset{{Name: appUpdateAssetName(), BrowserDownloadURL: "https://assets.invalid/update.zip", Digest: digest}}})
					payload = string(metadata)
				} else {
					assetRequests++
					if mode == "corrupted" {
						payload += "corruption"
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), ContentLength: int64(len(payload)), Header: http.Header{}}, nil
			})
			_, err := service.DownloadAppUpdate(context.Background(), connect.NewRequest(&appv1.DownloadAppUpdateRequest{ProgressEvent: "progress"}))
			if (err == nil) != (mode == "success") {
				t.Fatalf("mode %s: %v", mode, err)
			}
			if mode == "canceled" && connect.CodeOf(err) != connect.CodeCanceled {
				t.Fatalf("cancellation returned %v", err)
			}
			if mode == "missing_digest" && assetRequests != 0 {
				t.Fatal("downloaded an asset with no digest")
			}
			wantArchive, wantVersion := "previous verified update", "1.1"
			if mode == "success" {
				wantArchive, wantVersion = archive, "2.0"
			}
			data, err := os.ReadFile(cache)
			_, version := service.versions()
			if err != nil || string(data) != wantArchive || version != wantVersion {
				t.Fatalf("archive=%q error=%v version=%q", data, err, version)
			}
			files, err := os.ReadDir(filepath.Dir(cache))
			if err != nil || len(files) != 1 {
				t.Fatalf("partial downloads remain: %v (%v)", files, err)
			}
		})
	}
}
