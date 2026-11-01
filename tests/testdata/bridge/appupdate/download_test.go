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
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/network"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"
)

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDownloadReservesUpdateUntilCancellationFinishes(t *testing.T) {
	previousClient := newRequestClient
	t.Cleanup(func() { newRequestClient = previousClient })
	paths := storage.NewPaths(t.TempDir())
	service := NewService(platform.NewService(paths, nil, platform.Environment{}), nil, nil, "1", false, logging.LevelInfo, 0)
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	requests := 0
	newRequestClient = func(_ network.Options, _ time.Duration, _ bool) (*http.Client, error) {
		return &http.Client{Transport: updateTransport(func(request *http.Request) (*http.Response, error) {
			requests++
			if requests > 1 {
				return nil, fmt.Errorf("concurrent update reached network")
			}
			close(entered)
			<-request.Context().Done()
			close(canceled)
			<-release
			return nil, request.Context().Err()
		})}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.DownloadAppUpdate(ctx, connect.NewRequest(&appv1.DownloadAppUpdateRequest{ProgressEvent: "first"}))
		done <- err
	}()
	<-entered
	// The reservation must cover release lookup, not just the asset transfer.
	for _, event := range []string{"first", "second"} {
		_, err := service.DownloadAppUpdate(context.Background(), connect.NewRequest(&appv1.DownloadAppUpdateRequest{ProgressEvent: event}))
		if connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("parallel download %q was not rejected: %v", event, err)
		}
	}
	_, err := service.CancelAppUpdate(context.Background(), connect.NewRequest(&appv1.CancelAppUpdateRequest{ProgressEvent: "first"}))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Error("release lookup did not receive cancellation")
		cancel()
		<-canceled
	}
	_, err = service.DownloadAppUpdate(context.Background(), connect.NewRequest(&appv1.DownloadAppUpdateRequest{ProgressEvent: "first"}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("canceled transfer released its reservation before returning: %v", err)
	}
	_, err = service.ApplyAppUpdate(context.Background(), connect.NewRequest(&appv1.ApplyAppUpdateRequest{}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "download") {
		t.Errorf("apply did not reject active download: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	if err = <-done; connect.CodeOf(err) != connect.CodeCanceled {
		t.Errorf("download cancellation lost: %v", err)
	}
	// A completed cancellation releases the reservation for a new attempt.
	_, err = service.DownloadAppUpdate(context.Background(), connect.NewRequest(&appv1.DownloadAppUpdateRequest{ProgressEvent: "first"}))
	if connect.CodeOf(err) == connect.CodeFailedPrecondition {
		t.Fatalf("download remained locked after cancellation: %v", err)
	}
}

type updateEvents func(string, ...any)

func (f updateEvents) Publish(name string, values ...any) { f(name, values...) }

func TestAppDownloadRequiresVerifiedArchiveFromConfiguredRepository(t *testing.T) {
	previousClient := newRequestClient
	t.Cleanup(func() { newRequestClient = previousClient })
	newRequestClient = func(_ network.Options, timeout time.Duration, _ bool) (*http.Client, error) {
		return &http.Client{Transport: http.DefaultTransport, Timeout: timeout}, nil
	}
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
