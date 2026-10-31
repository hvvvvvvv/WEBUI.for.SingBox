package httptransport

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestPluginHTTPBrowseDoesNotDownloadAndSelectedFailuresDoNotPersist(t *testing.T) {
	service := pluginHTTPService(t)
	good := httpPlugin("good")
	broken := httpPlugin("broken")
	broken.Source = "function {"
	missing := httpPlugin("missing")
	catalog, sources, calls := pluginDownloadCatalog(t, good, broken, missing)
	delete(sources, "/plugins/missing.js")
	handler := pluginHTTPHandler(t, service, catalog, nil)
	result := pluginHTTPRequest(t, handler, http.MethodGet, "/api/plugins/catalog", "", true)
	if result.Code != http.StatusOK || calls.Load() != 0 || strings.Contains(result.Body.String(), `"source":"function`) {
		t.Fatalf("browsing downloaded source: %d %s calls=%d", result.Code, result.Body.String(), calls.Load())
	}
	for _, id := range []string{"missing", "broken"} {
		before := service.List()
		response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/install", pluginBody(t, map[string]any{"id": id, "expected_revision": before.Revision}), true)
		if response.Code != http.StatusBadRequest || !reflect.DeepEqual(service.List(), before) {
			t.Fatalf("failed download installed a plugin: %s %d %s", id, response.Code, response.Body.String())
		}
	}
	if response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/install", `{"id":"good","expected_revision":0}`, true); response.Code != http.StatusOK {
		t.Fatalf("unrelated missing/broken script prevented chosen install: %d %s", response.Code, response.Body.String())
	}
	before := service.List()
	sources["/plugins/good.js"] = "function onRun(p){return []}"
	if response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/update", `{"id":"good","expected_revision":1}`, true); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "checksum mismatch") || !reflect.DeepEqual(service.List(), before) {
		t.Fatalf("SHA failure replaced installed source: %d %s", response.Code, response.Body.String())
	}
}

func TestPluginHTTPDownloadCannotOverwriteConcurrentEdit(t *testing.T) {
	service := pluginHTTPService(t)
	old := httpPlugin("update-target")
	if _, err := service.Save(old, 0); err != nil {
		t.Fatal(err)
	}
	updated := old
	updated.Source = "function onRun(p){return p.slice(1)}"
	updated.Version = "2"
	catalog, _, calls := pluginDownloadCatalog(t, updated)
	transport := http.DefaultTransport
	entered, release := make(chan struct{}), make(chan struct{})
	http.DefaultTransport = pluginHTTPTransport(func(request *http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return transport.RoundTrip(request)
	})
	handler := pluginHTTPHandler(t, service, catalog, nil)
	done := make(chan int, 1)
	go func() {
		response := pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/update", `{"id":"update-target","expected_revision":1}`, true)
		done <- response.Code
	}()
	<-entered
	local := old
	local.Name = "Concurrent local edit"
	if _, err := service.Save(local, 1); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if status := <-done; status != http.StatusConflict || calls.Load() != 1 {
		t.Fatalf("stale download was accepted: status=%d calls=%d", status, calls.Load())
	}
	current := service.List()
	if current.Revision != 2 || len(current.Plugins) != 1 || current.Plugins[0].Name != local.Name || current.Plugins[0].Source != old.Source {
		t.Fatalf("download overwrote concurrent edit: %+v", current)
	}
}
