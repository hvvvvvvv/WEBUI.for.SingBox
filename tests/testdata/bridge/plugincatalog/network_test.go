package plugincatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"guiforcores/bridge/network"
)

func TestCatalogRefreshUsesBackendRequestProxyAndUserAgent(t *testing.T) {
	_, files := catalogFixture(t, "function onSubscribe(p) { return p }")
	var count atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.URL.Host != "catalog.invalid" || r.UserAgent() != "router-test-agent" || r.Header.Get("Authorization") != "" {
			t.Errorf("catalog request policy mismatch: host=%s UA=%s authorizationPresent=%v", r.URL.Host, r.UserAgent(), r.Header.Get("Authorization") != "")
		}
		data, exists := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !exists {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	defer proxy.Close()
	s := NewService("")
	s.baseURL = "http://catalog.invalid/"
	s.SetNetworkOptions(func() network.Options {
		return network.Options{Mode: network.ModeCustom, ProxyURL: proxy.URL, UserAgent: "router-test-agent", GitHubToken: "must-not-reach-catalog"}
	})
	result, err := s.Get(context.Background(), true)
	if err != nil || result.Source != "remote" || len(result.Plugins) != 1 || count.Load() != 2 {
		t.Fatalf("catalog did not use the configured proxy: %+v requests=%d err=%v", result, count.Load(), err)
	}
}
