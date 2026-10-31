package runtime

import (
	"guiforcores/bridge/network"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubscriptionAndRuleRequestsUseBackendNetworkSettings(t *testing.T) {
	previous := requestOptions.Load()
	t.Cleanup(func() { requestOptions.Store(previous) })
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "unresolvable.invalid" || r.Header.Get("User-Agent") != "configured-agent" {
			t.Errorf("wrong network settings: %s %q", r.URL, r.Header.Get("User-Agent"))
		}
		w.Write([]byte("subscription payload"))
	}))
	defer proxy.Close()
	requestOptions.Store(&runtimeRequestOptions{provider: func() network.Options {
		return network.Options{Mode: network.ModeCustom, ProxyURL: proxy.URL, UserAgent: "configured-agent"}
	}})
	response, body, err := httpRequestWithLimit(http.MethodGet, "http://unresolvable.invalid/subscription", nil, "", false, 2, 1024)
	if err != nil || response.StatusCode != 200 || body != "subscription payload" {
		t.Fatalf("response=%v body=%q err=%v", response, body, err)
	}
}
