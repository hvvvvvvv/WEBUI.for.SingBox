package runtime

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestNative114SubscriptionPreservesExplicitFields(t *testing.T) {
	const body = `{"outbounds":[
  {"type":"snell","tag":"native","server":"native.example.com","server_port":443,"version":6,"psk":"native-secret","domain_strategy":"prefer_ipv4","domain_resolver":{"strategy":"prefer_ipv6","timeout":"2s"},"custom_option":{"keep":true}},
  {"type":"hysteria2","tag":"native-hy2","server":"native.example.com","server_port":443,"password":"password","hop_interval":"15s","hop_interval_max":"30s","obfs":{"type":"gecko","password":"secret","min_packet_size":600}}
]}`
	var expected struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(body), &expected); err != nil {
		t.Fatal(err)
	}
	for _, enableConversion := range []bool{false, true} {
		parsed, err := parseSubscriptionBody(body, "Http", enableConversion)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.usedFallback || !reflect.DeepEqual(parsed.proxies, expected.Outbounds) {
			t.Fatalf("native subscription changed (conversion=%v): %#v", enableConversion, parsed)
		}
	}
}

func TestHTTP114SubscriptionPipelineKeepsFieldsAndNodeIDs(t *testing.T) {
	withTempBasePath(t)
	previousRequest := subscriptionHTTPRequest
	t.Cleanup(func() { subscriptionHTTPRequest = previousRequest })
	responseBody := `proxies:
  - {name: keep-snell, type: snell, server: snell.example.com, port: 443, version: 5, psk: password, domain-resolver: local, ip-version: prefer-ipv4}
  - {name: keep-hy2, type: hysteria2, server: hy2.example.com, port: 443, password: password, up: 100, hop-interval: 15-30, ip-version: ipv6, obfs: {type: gecko, password: secret}}
  - {name: keep-http, type: http, server: http.example.com, port: 8080}
  - {name: drop-snell, type: snell, server: snell.example.com, port: 443, version: 4, psk: password}
  - {name: invalid-snell, type: snell, server: snell.example.com, port: 443, version: 5, psk: secret-must-not-leak, quic: true}
`
	subscriptionHTTPRequest = func(method, rawURL string, headers map[string]string, body string, insecure bool, timeoutSeconds int) (*http.Response, string, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, responseBody, nil
	}
	items := []subscription{{
		ID: "protocol-114", Name: "Protocol 1.14", Type: "Http", URL: "https://example.com/subscription",
		EnableNodeConversion: true, Include: `^keep-`, IncludeProtocol: `^(snell|hysteria2)$`, ProxyPrefix: "PRE-",
		Proxies: []proxyRef{{ID: "snell-id", Tag: "PRE-keep-snell", Type: "snell"}},
		Script: `function onSubscribe(proxies, subscription) {
  if (proxies[0].tag !== "PRE-keep-snell" || proxies[1].tag !== "PRE-keep-hy2") throw new Error("pipeline order");
  if (proxies[0].version !== 4 || proxies[0].domain_resolver.server !== "local") throw new Error("missing converted Snell fields");
  if (proxies[1].hop_interval_max !== "30s" || proxies[1].obfs.type !== "gecko") throw new Error("missing converted Hysteria2 fields");
  proxies[0].reuse = true;
  return {proxies, subscription};
}`,
	}}
	cachePath := GetPath(subscriptionContentPath(items[0].ID))
	readCache := func() ([]byte, []map[string]any) {
		t.Helper()
		data, err := os.ReadFile(cachePath)
		if err != nil {
			t.Fatal(err)
		}
		var nodes []map[string]any
		if err := json.Unmarshal(data, &nodes); err != nil {
			t.Fatal(err)
		}
		return data, nodes
	}
	var previousRefs []proxyRef
	for update := 0; update < 2; update++ {
		result, changed := updateSubscriptionAt(items, 0, "")
		if !changed || !result.GetOk() || result.GetSuccessCount() != 2 || result.GetFilteredCount() != 2 || result.GetSkippedCount() != 1 {
			t.Fatalf("update %d: unexpected result %#v, changed=%v", update, result, changed)
		}
		_, nodes := readCache()
		if len(nodes) != 2 || nodes[0]["reuse"] != true || nodes[0]["version"] != float64(4) || nodes[1]["hop_interval_max"] != "30s" {
			t.Fatalf("converted/scripted fields lost from cache: %#v", nodes)
		}
		for _, node := range nodes {
			if _, exists := node["domain_strategy"]; exists {
				t.Fatal("deprecated domain_strategy appeared in converted cache")
			}
		}
		if _, exists := nodes[1]["domain_resolver"]; exists {
			t.Fatal("converter synthesized a resolver for a node without one")
		}
		if len(items[0].Proxies) != 2 || items[0].Proxies[0].ID != "snell-id" || items[0].Proxies[1].ID == "" {
			t.Fatalf("node IDs not assigned/reused: %#v", items[0].Proxies)
		}
		if update == 0 {
			previousRefs = append([]proxyRef(nil), items[0].Proxies...)
			responseBody = strings.Replace(responseBody, "up: 100", "up: 200", 1)
		} else {
			if !reflect.DeepEqual(items[0].Proxies, previousRefs) || nodes[1]["up_mbps"] != float64(200) {
				t.Fatalf("field update changed IDs or failed to update bandwidth: refs=%#v nodes=%#v", items[0].Proxies, nodes)
			}
		}
	}

	before, _ := readCache()
	responseBody = `proxies:
  - {name: invalid-snell, type: snell, server: snell.example.com, port: 443, version: 5, psk: secret-must-not-leak, quic: true}
  - {name: invalid-hy2, type: hysteria2, server: hy2.example.com, port: 443, password: secret-must-not-leak, hop-interval-max: 30}
`
	result, changed := updateSubscriptionAt(items, 0, "")
	if changed || result.GetOk() || strings.Contains(result.GetResult()+result.GetFailureReason(), "secret-must-not-leak") {
		t.Fatalf("invalid protocols should fail with sanitized errors: %#v", result)
	}
	after, _ := readCache()
	if string(after) != string(before) {
		t.Fatal("failed protocol conversion overwrote the cache")
	}
}
