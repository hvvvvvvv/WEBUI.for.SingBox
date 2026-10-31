package httptransport

import (
	"net/http"

	"guiforcores/bridge/routerdiag"
	"guiforcores/bridge/routerplugin"
)

// These routes are registered on the authenticated mux; neither executes a plugin.
func (s *Server) registerRouterRoutes(mux *http.ServeMux) {
	catalog := s.routerCatalog
	if catalog == nil {
		catalog = routerplugin.NewCatalogService("")
	}
	mux.HandleFunc("/api/router/plugins", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		result, err := catalog.Get(r.Context(), r.URL.Query().Get("refresh") == "1")
		if err != nil {
			http.Error(w, "Plugin catalog unavailable", http.StatusInternalServerError)
			return
		}
		jsonResponse(w, result)
	})
	mux.HandleFunc("/api/router/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		jsonResponse(w, routerdiag.Collect(r.Context()))
	})
}
