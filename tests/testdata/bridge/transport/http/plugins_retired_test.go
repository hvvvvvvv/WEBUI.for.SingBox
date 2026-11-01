package httptransport

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestRetiredPluginHTTPCatalogInstallAndUpdatePreserveUserInstallation(t *testing.T) {
	for _, id := range []string{"plugin-index-the-proxy-name", "plugin-modify-proxy-name", "plugin-beautiful-node-name", "plugin-duplicate-proxies-removal", "plugin-network-info", "plugin-geoip-search"} {
		t.Run(id, func(t *testing.T) {
			old := httpPlugin(id)
			old.Configuration = map[string]any{"my-setting": "preserve"}
			service := pluginHTTPService(t)
			if _, err := service.Save(old, 0); err != nil {
				t.Fatal(err)
			}
			before := service.List()
			candidate := old
			candidate.Version = "2"
			catalog, _, downloads := pluginDownloadCatalog(t, candidate, httpPlugin("current"))
			handler := pluginHTTPHandler(t, service, catalog, nil)
			response := pluginHTTPRequest(t, handler, http.MethodGet, "/api/plugins/catalog", "", true)
			if response.Code != http.StatusOK || strings.Contains(response.Body.String(), id) {
				t.Fatalf("retired package still in cached directory: %d %s", response.Code, response.Body.String())
			}
			response = pluginHTTPRequest(t, handler, http.MethodPost, "/api/plugins/update", pluginBody(t, map[string]any{"id": id, "expected_revision": before.Revision}), true)
			if response.Code != http.StatusNotFound || !reflect.DeepEqual(before, service.List()) {
				t.Fatalf("retired update changed installation: %d %s", response.Code, response.Body.String())
			}
			newService := pluginHTTPService(t)
			newHandler := pluginHTTPHandler(t, newService, catalog, nil)
			response = pluginHTTPRequest(t, newHandler, http.MethodPost, "/api/plugins/install", pluginBody(t, map[string]any{"id": id, "expected_revision": 0}), true)
			if response.Code != http.StatusNotFound || newService.List().Revision != 0 || downloads.Load() != 0 {
				t.Fatalf("retired install downloaded or saved source: %d %s downloads=%d", response.Code, response.Body.String(), downloads.Load())
			}
		})
	}
}
