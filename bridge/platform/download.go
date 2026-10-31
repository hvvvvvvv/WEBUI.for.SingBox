package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const releaseDownloadTimeout = 10 * time.Minute

// DownloadReleaseAsset publishes a complete file only after matching the SHA256
// supplied by GitHub's release metadata. Failed/canceled downloads leave no
// partial archive and preserve any previously verified destination.
func DownloadReleaseAsset(ctx context.Context, url, digest, path, token string, progress func(int64, int64)) error {
	algorithm, encoded, ok := strings.Cut(strings.TrimSpace(digest), ":")
	expected, err := hex.DecodeString(encoded)
	if !ok || algorithm != "sha256" || err != nil || len(expected) != sha256.Size {
		return fmt.Errorf("release asset has no valid SHA256 digest; refusing unverified download")
	}
	ctx, cancel := context.WithTimeout(ctx, releaseDownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("download failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".release-download-*")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() {
		_ = file.Close()
		_ = os.Remove(temporaryPath)
	}()
	hash := sha256.New()
	writer := io.MultiWriter(file, hash)
	buffer := make([]byte, 128*1024)
	var downloaded int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if _, err := writer.Write(buffer[:n]); err != nil {
				return err
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, resp.ContentLength)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	actual := hash.Sum(nil)
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("SHA256 mismatch for %s: expected %s, got %x", filepath.Base(path), encoded, actual)
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
