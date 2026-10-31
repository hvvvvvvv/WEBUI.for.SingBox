package kernel

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
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
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/network"
	appv1 "guiforcores/gen/app/v1"
	kernelv1 "guiforcores/gen/kernel/v1"
)

type coreDownloadTransport func(*http.Request) (*http.Response, error)

func (f coreDownloadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type coreDownloadEvents func(string, ...any)

func (f coreDownloadEvents) Publish(name string, values ...any) { f(name, values...) }

func coreDownloadArchive(t *testing.T, assetName string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	const executable = "verified new core"
	if strings.HasSuffix(assetName, ".zip") {
		writer := zip.NewWriter(&buffer)
		file, err := writer.Create(strings.TrimSuffix(assetName, ".zip") + "/" + getKernelFileName(false))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, executable); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		compressed := gzip.NewWriter(&buffer)
		writer := tar.NewWriter(compressed)
		if err := writer.WriteHeader(&tar.Header{Name: strings.TrimSuffix(assetName, ".tar.gz") + "/" + getKernelFileName(false), Mode: 0755, Size: int64(len(executable))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, executable); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buffer.Bytes()
}

func TestCoreDownloadVerifiesArchiveBeforeReplacingExecutable(t *testing.T) {
	previousClient := newRequestClient
	t.Cleanup(func() { newRequestClient = previousClient })
	newRequestClient = func(_ network.Options, timeout time.Duration, _ bool) (*http.Client, error) {
		return &http.Client{Transport: http.DefaultTransport, Timeout: timeout}, nil
	}
	assetName := getKernelAssetFileName("1.14.2")
	archive := coreDownloadArchive(t, assetName)
	for _, mode := range []string{"success", "corrupted", "invalid_archive", "missing_digest", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			downloadedArchive := archive
			if mode == "invalid_archive" {
				downloadedArchive = []byte("verified digest but invalid archive")
			}
			base := t.TempDir()
			corePath := filepath.Join(base, coreWorkingDirectory, getKernelFileName(false))
			if err := os.MkdirAll(filepath.Dir(corePath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(corePath, []byte("current core"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(corePath+".bak", []byte("previous core"), 0755); err != nil {
				t.Fatal(err)
			}
			service := NewService(fakeProcesses{resolveBase: base}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, nil)
			defer service.lifecycleCancel()
			service.events = coreDownloadEvents(func(name string, _ ...any) {
				if mode == "canceled" && name == "progress" {
					_, err := service.CancelCoreDownload(context.Background(), connect.NewRequest(&kernelv1.CancelCoreDownloadRequest{ProgressEvent: "progress"}))
					if err != nil {
						t.Error(err)
					}
				}
			})
			previousTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previousTransport })
			http.DefaultTransport = coreDownloadTransport(func(request *http.Request) (*http.Response, error) {
				payload := downloadedArchive
				if request.URL.Host == "api.github.com" {
					asset := githubAsset{Name: assetName, BrowserDownloadURL: "https://assets.invalid/" + assetName, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(downloadedArchive))}
					asset.Uploader.Type = "Bot"
					if mode == "missing_digest" {
						asset.Digest = ""
					}
					payload, _ = json.Marshal(githubRelease{TagName: "v1.14.2", Assets: []githubAsset{asset}})
				} else if mode == "corrupted" {
					payload = []byte("corrupted archive")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(payload)), ContentLength: int64(len(payload)), Header: http.Header{}}, nil
			})
			_, err := service.DownloadCore(context.Background(), connect.NewRequest(&kernelv1.DownloadCoreRequest{ProgressEvent: "progress"}))
			if (err == nil) != (mode == "success") {
				t.Fatalf("mode %s returned %v", mode, err)
			}
			if mode == "canceled" && connect.CodeOf(err) != connect.CodeCanceled {
				t.Fatalf("cancellation returned %v", err)
			}
			want, wantBackup := "current core", "previous core"
			if mode == "success" {
				want, wantBackup = "verified new core", "current core"
			}
			current, _ := os.ReadFile(corePath)
			backup, _ := os.ReadFile(corePath + ".bak")
			if string(current) != want || string(backup) != wantBackup {
				t.Fatalf("core=%q backup=%q", current, backup)
			}
			files, err := os.ReadDir(filepath.Join(base, coreDownloadCacheFolder))
			if err != nil || len(files) != 0 {
				t.Fatalf("download leftovers=%v error=%v", files, err)
			}
		})
	}
}

func TestCoreArchiveExtractionFailureCleansPartialFiles(t *testing.T) {
	base := t.TempDir()
	cacheDir := filepath.Join(base, coreDownloadCacheFolder)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(cacheDir, "sing-box-partial.tar.gz")
	var archive bytes.Buffer
	compressed := gzip.NewWriter(&archive)
	writer := tar.NewWriter(compressed)
	for _, name := range []string{"sing-box-partial/" + getKernelFileName(false), "../../outside"} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: 7}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte("partial")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	service := NewService(fakeProcesses{resolveBase: base}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, nil)
	defer service.lifecycleCancel()
	if err := service.installCoreArchive(context.Background(), archivePath, cacheDir, appv1.KernelBranch_KERNEL_BRANCH_MAIN); err == nil {
		t.Fatal("archive with unsafe second entry was accepted")
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(archivePath) {
		t.Fatalf("partial extraction left files behind: %v", entries)
	}
}
