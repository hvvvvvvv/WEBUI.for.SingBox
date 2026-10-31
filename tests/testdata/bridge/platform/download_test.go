package platform

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type releaseDownloadTransport func(*http.Request) (*http.Response, error)

func (f releaseDownloadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestReleaseDownloadVerifiesBeforePublishingAndCleansFailures(t *testing.T) {
	const content = "verified release archive"
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
	for _, mode := range []string{"success", "corrupted", "missing_digest", "malformed_digest", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, "release.zip")
			if err := os.WriteFile(path, []byte("previous verified archive"), 0600); err != nil {
				t.Fatal(err)
			}
			previous := http.DefaultClient
			t.Cleanup(func() { http.DefaultClient = previous })
			requests := 0
			http.DefaultClient = &http.Client{Transport: releaseDownloadTransport(func(request *http.Request) (*http.Response, error) {
				requests++
				if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > 10*time.Minute {
					t.Error("release download has no bounded deadline")
				}
				if request.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("release authorization lost")
				}
				payload := content
				if mode == "corrupted" {
					payload += "corruption"
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload)), ContentLength: int64(len(payload)), Header: http.Header{}}, nil
			})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			expected := digest
			if mode == "missing_digest" {
				expected = ""
			} else if mode == "malformed_digest" {
				expected = "sha256:invalid"
			}
			var progress int64
			err := DownloadReleaseAsset(ctx, "https://example.invalid/release.zip", expected, path, "test-token", func(downloaded, _ int64) {
				progress = downloaded
				if mode == "canceled" {
					cancel()
				}
			})
			if (err == nil) != (mode == "success") {
				t.Fatalf("download returned %v in %s mode", err, mode)
			}
			if mode == "corrupted" && !strings.Contains(err.Error(), "SHA256 mismatch") {
				t.Fatalf("missing checksum failure: %v", err)
			}
			if (mode == "missing_digest" || mode == "malformed_digest") && requests != 0 {
				t.Fatal("download started without a usable digest")
			}
			want := "previous verified archive"
			if mode == "success" {
				want = content
				if progress != int64(len(content)) {
					t.Fatalf("progress=%d", progress)
				}
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != want {
				t.Fatalf("published archive=%q error=%v, want %q", data, readErr, want)
			}
			files, err := os.ReadDir(base)
			if err != nil || len(files) != 1 {
				t.Fatalf("left partial download files: %v error=%v", files, err)
			}
		})
	}
}
