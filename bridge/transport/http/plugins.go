package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"guiforcores/bridge/plugincatalog"
	"guiforcores/bridge/plugins"
	appruntime "guiforcores/bridge/runtime"
)

type SubscriptionPluginRunner interface {
	RunPluginOnSubscription(context.Context, string, string) (appruntime.PluginSubscriptionResult, error)
}

func pluginError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var bodyLimit *http.MaxBytesError
	switch {
	case errors.As(err, &bodyLimit):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, plugins.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, plugins.ErrNotFound), connect.CodeOf(err) == connect.CodeNotFound:
		status = http.StatusNotFound
	case errors.Is(err, context.Canceled):
		status = http.StatusRequestTimeout
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusRequestTimeout
	}
	http.Error(w, err.Error(), status)
}

func (s *Server) registerPluginRoutes(mux *http.ServeMux) {
	catalog := s.pluginCatalog
	if catalog == nil {
		catalog = plugincatalog.NewService("")
	}
	mux.HandleFunc("/api/plugins", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.options.Plugins == nil {
			http.Error(w, "Plugins unavailable", http.StatusServiceUnavailable)
			return
		}
		jsonResponse(w, s.options.Plugins.List())
	})
	mux.HandleFunc("/api/plugins/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		result, err := catalog.Get(r.Context(), r.URL.Query().Get("refresh") == "1")
		if err != nil {
			pluginError(w, err)
			return
		}
		jsonResponse(w, result)
	})
	for _, operation := range []string{"save", "delete", "reorder", "install", "update", "run"} {
		mux.HandleFunc("/api/plugins/"+operation, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if s.options.Plugins == nil {
				http.Error(w, "Plugins unavailable", http.StatusServiceUnavailable)
				return
			}
			request, err := readPluginRequest(w, r, operation)
			if err != nil {
				pluginError(w, fmt.Errorf("invalid plugin request: %w", err))
				return
			}
			if operation == "run" {
				if s.options.PluginRunner == nil {
					http.Error(w, "Subscription runner unavailable", http.StatusServiceUnavailable)
					return
				}
				result, err := s.options.PluginRunner.RunPluginOnSubscription(r.Context(), request.ID, request.SubscriptionID)
				if err != nil {
					pluginError(w, err)
					return
				}
				jsonResponse(w, result)
				return
			}
			if request.ExpectedRevision == nil {
				pluginError(w, fmt.Errorf("expected_revision is required"))
				return
			}
			revision := *request.ExpectedRevision
			var snapshot plugins.Snapshot
			err = nil
			switch operation {
			case "save":
				if request.Plugin == nil {
					pluginError(w, fmt.Errorf("plugin is required"))
					return
				}
				snapshot, err = s.options.Plugins.Save(*request.Plugin, revision)
			case "delete":
				snapshot, err = s.options.Plugins.Delete(request.ID, revision)
			case "reorder":
				snapshot, err = s.options.Plugins.Reorder(request.IDs, revision)
			case "install", "update":
				current := s.options.Plugins.List()
				if current.Revision != revision {
					pluginError(w, plugins.ErrConflict)
					return
				}
				var previous *plugins.Plugin
				for _, item := range current.Plugins {
					if item.ID == request.ID {
						copy := item
						previous = &copy
						break
					}
				}
				if operation == "install" && previous != nil {
					pluginError(w, plugins.ErrConflict)
					return
				}
				if operation == "update" && previous == nil {
					pluginError(w, plugins.ErrNotFound)
					return
				}
				available, fetchErr := catalog.Get(r.Context(), false)
				if fetchErr != nil {
					pluginError(w, fetchErr)
					return
				}
				var candidate *plugins.Plugin
				for _, item := range available.Plugins {
					if item.ID == request.ID {
						copy := item
						candidate = &copy
						break
					}
				}
				if candidate == nil {
					pluginError(w, fmt.Errorf("plugin is absent from the current catalog"))
					return
				}
				if previous != nil {
					candidate.Disabled = previous.Disabled
					candidate.Configuration = previous.Configuration
					candidate.Triggers = append([]string(nil), previous.Triggers...)
				}
				snapshot, err = s.options.Plugins.Save(*candidate, revision)
			}
			if err != nil {
				pluginError(w, err)
				return
			}
			jsonResponse(w, snapshot)
		})
	}
}

type pluginRequest struct {
	ID               string          `json:"id"`
	Plugin           *plugins.Plugin `json:"plugin"`
	IDs              []string        `json:"ids"`
	ExpectedRevision *uint64         `json:"expected_revision"`
	SubscriptionID   string          `json:"subscription_id"`
}

func readPluginRequest(w http.ResponseWriter, r *http.Request, operation string) (pluginRequest, error) {
	var request pluginRequest
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		return request, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return request, fmt.Errorf("expected a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := validatePluginJSON(decoder, 0); err != nil {
		return request, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return request, fmt.Errorf("unexpected request content")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return request, err
	}
	allowed := map[string][]string{
		"save": {"plugin", "expected_revision"}, "delete": {"id", "expected_revision"},
		"reorder": {"ids", "expected_revision"}, "install": {"id", "expected_revision"},
		"update": {"id", "expected_revision"}, "run": {"id", "subscription_id"},
	}[operation]
	if len(fields) != len(allowed) {
		return request, fmt.Errorf("request fields do not match %s", operation)
	}
	for _, field := range allowed {
		value, exists := fields[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return request, fmt.Errorf("%s is required and cannot be null", field)
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	if operation != "save" && operation != "reorder" && strings.TrimSpace(request.ID) == "" {
		return request, fmt.Errorf("id must not be empty")
	}
	if operation == "run" && strings.TrimSpace(request.SubscriptionID) == "" {
		return request, fmt.Errorf("subscription_id must not be empty")
	}
	return request, nil
}

// encoding/json accepts duplicate keys; reject them before typed decoding so a
// request has one unambiguous revision and payload. Configuration values remain
// ordinary JSON and may contain null, but nesting is bounded.
func validatePluginJSON(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("request JSON nesting exceeds 64 levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate or invalid JSON field")
			}
			seen[name] = true
			if err := validatePluginJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := validatePluginJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
