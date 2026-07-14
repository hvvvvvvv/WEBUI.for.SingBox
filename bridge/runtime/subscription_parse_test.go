package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"guiforcores/bridge/config"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

func testShadowsocksURI(name string) string {
	credentials := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:password"))
	return "ss://" + credentials + "@127.0.0.1:8388#" + name
}

func TestParseNativeSubscriptionAcceptsSingBoxObjectAndArray(t *testing.T) {
	for _, body := range []string{
		`{"outbounds":[{"type":"direct","tag":"direct"}]}`,
		`[{"type":"direct","tag":"direct"}]`,
	} {
		proxies, err := parseNativeSubscription(body)
		if err != nil {
			t.Fatal(err)
		}
		if len(proxies) != 1 || proxies[0]["tag"] != "direct" {
			t.Fatalf("unexpected native proxies: %#v", proxies)
		}
		parsed, err := parseSubscriptionBody(body, "Http", false)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.usedFallback {
			t.Fatal("valid native sing-box content must not use the fallback parser")
		}
	}
}

func TestParseSubscriptionBodyFallsBackOnlyForHTTP(t *testing.T) {
	body := testShadowsocksURI("fallback")
	parsed, err := parseSubscriptionBody(body, "Http", true)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.usedFallback || len(parsed.proxies) != 1 {
		t.Fatalf("expected one fallback proxy, got %#v", parsed)
	}
	if parsed.proxies[0]["type"] != "shadowsocks" || parsed.proxies[0]["tag"] != "fallback" {
		t.Fatalf("unexpected fallback outbound: %#v", parsed.proxies[0])
	}

	if _, err := parseSubscriptionBody(body, "Manual", true); err == nil {
		t.Fatal("manual subscription should not use the fallback parser")
	}

	if _, err := parseSubscriptionBody(body, "Http", false); err == nil || !strings.Contains(err.Error(), "node conversion is disabled") {
		t.Fatalf("disabled HTTP conversion error = %v, want an explicit disabled error", err)
	}
}

func TestUpdateHTTPSubscriptionFallbackDoesNotOverwriteCacheOnFailure(t *testing.T) {
	withTempBasePath(t)
	previousRequest := subscriptionHTTPRequest
	defer func() { subscriptionHTTPRequest = previousRequest }()

	responseBody := testShadowsocksURI("converted")
	responseStatus := http.StatusOK
	subscriptionHTTPRequest = func(method string, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int) (*http.Response, string, error) {
		return &http.Response{StatusCode: responseStatus, Header: make(http.Header)}, responseBody, nil
	}
	if err := saveSubscriptions([]subscription{{
		ID:                   "fallback-http",
		Name:                 "Fallback HTTP",
		Type:                 "Http",
		URL:                  "https://example.com/subscription",
		EnableNodeConversion: true,
	}}); err != nil {
		t.Fatal(err)
	}
	service := &appRuntimeService{config: staticAppConfig{value: config.AppConfig{}}}

	response, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "fallback-http"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Msg.GetResults()) != 1 || !response.Msg.GetResults()[0].GetOk() {
		t.Fatalf("expected fallback update success, got %#v", response.Msg.GetResults())
	}
	if !strings.Contains(response.Msg.GetResults()[0].GetResult(), "Imported 1 proxies") {
		t.Fatalf("expected import summary, got %q", response.Msg.GetResults()[0].GetResult())
	}
	cachePath := GetPath(subscriptionContentPath("fallback-http"))
	before, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), `"type": "shadowsocks"`) {
		t.Fatalf("expected converted cache, got %s", before)
	}

	responseStatus = http.StatusInternalServerError
	responseBody = testShadowsocksURI("must-not-replace-cache")
	response, err = service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "fallback-http"}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetResults()[0].GetOk() {
		t.Fatal("expected a non-2xx subscription response to fail")
	}
	afterStatusFailure, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterStatusFailure) != string(before) {
		t.Fatal("non-2xx subscription response overwrote the previous cache")
	}

	responseStatus = http.StatusOK
	responseBody = "not a subscription password=must-not-leak"
	response, err = service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "fallback-http"}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetResults()[0].GetOk() {
		t.Fatal("expected invalid fallback update to fail")
	}
	if strings.Contains(response.Msg.GetResults()[0].GetResult(), "must-not-leak") {
		t.Fatal("fallback error leaked source content")
	}
	after, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed fallback update overwrote the previous cache")
	}
}

func TestDisabledHTTPNodeConversionDoesNotOverwriteCache(t *testing.T) {
	withTempBasePath(t)
	previousRequest := subscriptionHTTPRequest
	defer func() { subscriptionHTTPRequest = previousRequest }()

	subscriptionHTTPRequest = func(method string, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int) (*http.Response, string, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, testShadowsocksURI("must-not-convert"), nil
	}
	if err := saveSubscriptions([]subscription{{
		ID:                   "conversion-disabled",
		Name:                 "Conversion Disabled",
		Type:                 "Http",
		URL:                  "https://example.com/subscription",
		EnableNodeConversion: false,
	}}); err != nil {
		t.Fatal(err)
	}

	cachePath := GetPath(subscriptionContentPath("conversion-disabled"))
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	const previousCache = `[{"type":"direct","tag":"previous"}]`
	if err := os.WriteFile(cachePath, []byte(previousCache), 0644); err != nil {
		t.Fatal(err)
	}

	service := &appRuntimeService{config: staticAppConfig{value: config.AppConfig{}}}
	response, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "conversion-disabled"}))
	if err != nil {
		t.Fatal(err)
	}
	result := response.Msg.GetResults()[0]
	if result.GetOk() || !strings.Contains(result.GetResult(), "node conversion is disabled") {
		t.Fatalf("disabled conversion result = %#v, want an explicit failure", result)
	}
	after, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != previousCache {
		t.Fatalf("disabled conversion overwrote cache: got %s, want %s", after, previousCache)
	}
}

func TestSubscriptionHTTPCallRejectsOversizedBody(t *testing.T) {
	body, err := readHTTPBody(strings.NewReader("123456789"), 8)
	if !errors.Is(err, errHTTPResponseTooLarge) {
		t.Fatalf("readHTTPBody() error = %v, want errHTTPResponseTooLarge", err)
	}
	if body != "" {
		t.Fatalf("readHTTPBody() body = %q, want empty", body)
	}
}

func TestHTTPFallbackRunsFilterPrefixAndScriptBeforeCaching(t *testing.T) {
	withTempBasePath(t)
	previousRequest := subscriptionHTTPRequest
	defer func() { subscriptionHTTPRequest = previousRequest }()

	responseBody := strings.Join([]string{
		testShadowsocksURI("keep"),
		"not-a-supported-proxy-line",
		testShadowsocksURI("drop"),
	}, "\n")
	subscriptionHTTPRequest = func(method string, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int) (*http.Response, string, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, responseBody, nil
	}
	if err := saveSubscriptions([]subscription{{
		ID:                   "fallback-pipeline",
		Name:                 "Fallback Pipeline",
		Type:                 "Http",
		URL:                  "https://example.com/subscription",
		Include:              `^keep$`,
		ProxyPrefix:          "PRE-",
		EnableNodeConversion: true,
		Script: `function onSubscribe(proxies, subscription) {
  proxies[0].tag = proxies[0].tag + "-script";
  return { proxies: proxies, subscription: subscription };
}`,
	}}); err != nil {
		t.Fatal(err)
	}

	service := &appRuntimeService{config: staticAppConfig{value: config.AppConfig{}}}
	response, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "fallback-pipeline"}))
	if err != nil {
		t.Fatal(err)
	}
	result := response.Msg.GetResults()[0]
	if !result.GetOk() {
		t.Fatalf("fallback update failed: %s", result.GetResult())
	}
	if !strings.Contains(result.GetResult(), "Imported 1 proxies") || !strings.Contains(result.GetResult(), "skipped 1") {
		t.Fatalf("unexpected fallback summary: %q", result.GetResult())
	}

	data, err := os.ReadFile(GetPath(subscriptionContentPath("fallback-pipeline")))
	if err != nil {
		t.Fatal(err)
	}
	var cached []map[string]any
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatal(err)
	}
	if len(cached) != 1 || cached[0]["tag"] != "PRE-keep-script" {
		t.Fatalf("cached fallback proxies = %#v, want filtered, prefixed, then scripted result", cached)
	}
}

func TestSubscriptionRequestErrorDoesNotLeakURLCredentials(t *testing.T) {
	withTempBasePath(t)
	previousRequest := subscriptionHTTPRequest
	defer func() { subscriptionHTTPRequest = previousRequest }()

	const secret = "URL-TOKEN-MUST-NOT-LEAK"
	subscriptionHTTPRequest = func(method string, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int) (*http.Response, string, error) {
		return nil, "", errors.New(`Get "https://user:` + secret + `@example.com/sub?token=` + secret + `": request failed`)
	}
	if err := saveSubscriptions([]subscription{{
		ID:   "request-error-redaction",
		Name: "Request Error Redaction",
		Type: "Http",
		URL:  "https://user:" + secret + "@example.com/sub?token=" + secret,
	}}); err != nil {
		t.Fatal(err)
	}

	service := &appRuntimeService{config: staticAppConfig{value: config.AppConfig{}}}
	response, err := service.UpdateSubscription(context.Background(), connect.NewRequest(&appv1.UpdateSubscriptionRequest{Id: "request-error-redaction"}))
	if err != nil {
		t.Fatal(err)
	}
	message := response.Msg.GetResults()[0].GetResult()
	if strings.Contains(message, secret) {
		t.Fatalf("subscription request error leaked URL credentials: %q", message)
	}
}
