package platform

import (
	"context"
	"crypto/sha256"
	"fmt"
	"guiforcores/bridge/network"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifiedReleaseDownloadUsesRequestProxyWithoutLeakingAPIToken(t *testing.T) {
	const payload = "verified through request proxy"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "http://unresolvable.invalid/release.zip" || r.Header.Get("User-Agent") != "webui/test" {
			t.Errorf("wrong release request: %s %q", r.URL, r.Header.Get("User-Agent"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("API token leaked to asset host")
		}
		w.Write([]byte(payload))
	}))
	defer proxy.Close()
	path := filepath.Join(t.TempDir(), "asset.zip")
	options := network.Options{Mode: network.ModeCustom, ProxyURL: proxy.URL, UserAgent: "webui/test", GitHubToken: "private-token"}
	if err := DownloadReleaseAssetWithOptions(context.Background(), "http://unresolvable.invalid/release.zip", fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(payload))), path, options, nil); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != payload {
		t.Fatalf("saved=%q err=%v", saved, err)
	}
}
