package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

func TestRuleSetFailedDownloadsPreserveContentAndMetadata(t *testing.T) {
	for _, format := range []string{"binary", "source"} {
		for _, status := range []int{http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError} {
			t.Run(fmt.Sprintf("%s/HTTP%d", format, status), func(t *testing.T) {
				withTempBasePath(t)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
					// Valid source JSON must not override the unsuccessful HTTP status.
					_, _ = io.WriteString(w, `{"version":2,"rules":[]}`)
				}))
				defer server.Close()
				assertRuleSetDownloadFailurePreservesState(t, server.URL, format)
			})
		}
	}
	for _, failure := range []string{"oversized", "truncated"} {
		t.Run(failure, func(t *testing.T) {
			withTempBasePath(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if failure == "truncated" {
					w.Header().Set("Content-Length", "1024")
					_, _ = io.WriteString(w, "incomplete")
					return
				}
				chunk := bytes.Repeat([]byte("x"), 64*1024)
				for written := 0; written <= maxRuleSetBodyBytes; written += len(chunk) {
					if _, err := w.Write(chunk); err != nil {
						return
					}
				}
			}))
			defer server.Close()
			assertRuleSetDownloadFailurePreservesState(t, server.URL, "binary")
		})
	}
}

func assertRuleSetDownloadFailurePreservesState(t *testing.T, url, format string) {
	t.Helper()
	item := ruleset{ID: "rules", Tag: "Rules", Type: "Http", Format: format, Path: "data/rulesets/rules.cache", URL: url, UpdateTime: 123, Count: 7}
	path := GetPath(item.Path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const previous = "previous valid rule-set"
	if err := os.WriteFile(path, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	items := []ruleset{item}
	result, changed := updateRulesetAt(items, 0)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.GetOk() || changed || string(got) != previous || items[0] != item {
		t.Fatalf("failed download changed rule-set: result=%v changed=%v content=%q metadata=%+v", result, changed, got, items[0])
	}
}

func TestRuntimeHTTPTimeoutInterruptsStalledBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	started := time.Now()
	_, body, err := httpRequestWithLimit(http.MethodGet, server.URL, nil, "", false, 1, maxRuleSetBodyBytes)
	if err == nil || body != "" {
		t.Fatalf("stalled response body was accepted: body=%q error=%v", body, err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("response body did not respect request timeout: %s", elapsed)
	}
}

func TestBatchSubscriptionFailurePreservesPreviousState(t *testing.T) {
	for _, failure := range []string{"script", "cache-write"} {
		t.Run(failure, func(t *testing.T) {
			withTempBasePath(t)
			service := NewService(nil, runtimePaths.Load(), staticAppConfig{}, nil, nil)
			previousRequest := subscriptionHTTPRequest
			subscriptionHTTPRequest = func(string, string, map[string]string, string, bool, int) (*http.Response, string, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, `[{"type":"shadowsocks","tag":"new-node","server":"127.0.0.1","server_port":1234,"method":"aes-128-gcm","password":"test"}]`, nil
			}
			t.Cleanup(func() { subscriptionHTTPRequest = previousRequest })
			items := []subscription{
				{ID: "failing", Name: "Failing", Type: "Http", URL: "https://example.invalid/failing", UpdateTime: 123, Proxies: []proxyRef{{ID: "existing-id", Tag: "old-node", Type: "shadowsocks"}}, Header: subscriptionHeader{Request: map[string]string{"X-Test": "previous"}}},
				{ID: "success", Name: "Success", Type: "Http", URL: "https://example.invalid/success"},
			}
			if failure == "script" {
				items[0].Script = `function onSubscribe(proxies, subscription) { subscription.Header.Request["X-Test"] = "changed"; throw new Error("script failed"); }`
			}
			if err := saveSubscriptions(items); err != nil {
				t.Fatal(err)
			}
			before, err := loadSubscriptions()
			if err != nil {
				t.Fatal(err)
			}
			path := GetPath(subscriptionContentPath("failing"))
			if failure == "cache-write" {
				// A directory cannot be atomically replaced with the downloaded file.
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, "previous.json")
			} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			const original = `[{"type":"shadowsocks","tag":"old-node"}]`
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			response, err := service.UpdateAllSubscriptions(context.Background(), connect.NewRequest(&appv1.UpdateAllSubscriptionsRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			results := response.Msg.GetResults()
			if len(results) != 2 || results[0].GetOk() || !results[1].GetOk() {
				t.Fatalf("unexpected update results: %v", results)
			}
			if failure == "script" && !strings.Contains(results[0].GetFailureReason(), "script failed") {
				t.Fatalf("script did not reach the intended failure: %v", results[0])
			}
			after, err := loadSubscriptions()
			if err != nil {
				t.Fatal(err)
			}
			cached, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after[0], before[0]) || string(cached) != original {
				t.Fatalf("failed subscription changed: before=%+v after=%+v content=%s", before[0], after[0], cached)
			}
			if len(after[1].Proxies) != 1 || after[1].Proxies[0].Tag != "new-node" {
				t.Fatalf("successful subscription was not saved: %+v", after[1])
			}
		})
	}
}
