package runtime

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"guiforcores/bridge/network"
)

func TestRuntimeRequestLogsDoNotExposeSubscriptionCredentials(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	previousOptions := requestOptions.Load()
	requestOptions.Store(&runtimeRequestOptions{provider: func() network.Options { return network.Options{Mode: network.ModeNone} }})
	t.Cleanup(func() { requestOptions.Store(previousOptions) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	defer server.Close()
	target := strings.Replace(server.URL, "http://", "http://subscriber:private-password@", 1) + "/private-path-token?token=private-query-token"
	_, _, err := httpRequestWithLimit(http.MethodGet, target, nil, "", false, 2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"subscriber", "private-password", "private-path-token", "private-query-token"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("request log contains %q: %s", secret, output.String())
		}
	}
	if !strings.Contains(output.String(), "127.0.0.1") || !strings.Contains(output.String(), "status_code=200") {
		t.Fatalf("safe network diagnostic fields are missing: %s", output.String())
	}
}

func TestRuntimeRequestFailureDoesNotExposeSubscriptionURL(t *testing.T) {
	previousOptions := requestOptions.Load()
	requestOptions.Store(&runtimeRequestOptions{provider: func() network.Options { return network.Options{Mode: network.ModeNone} }})
	t.Cleanup(func() { requestOptions.Store(previousOptions) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	server.Close()
	target := strings.Replace(server.URL, "http://", "http://subscriber:private-password@", 1) + "/private-path-token?token=private-query-token"
	for _, raw := range []string{target, target + "%zz"} {
		_, _, err := httpRequestWithLimit(http.MethodGet, raw, nil, "", false, 1, 1024)
		if err == nil {
			t.Fatal("invalid/refused request succeeded")
		}
		var cause *url.Error
		if !errors.As(err, &cause) {
			t.Fatalf("original transport cause was lost: %v", err)
		}
		for _, secret := range []string{"subscriber", "private-password", "private-path-token", "private-query-token"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("request failure exposes %q: %s", secret, err)
			}
		}
	}
}
