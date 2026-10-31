package httptransport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/logging"
)

const maxAuthBody = 8 * 1024

// Every argument has an explicit type and count; malformed input must never
// become an empty password (which is an explicit request to disable login).
func authRoute(mux *http.ServeMux, path string, minArgs, maxArgs int, handler func(*http.Request, []string) (any, error)) {
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Bearer tokens aren't sent automatically by browsers. Refuse a browser's
		// explicitly cross-site requests as an additional bootstrap/CSRF boundary.
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Cross-site authentication request rejected", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(10 * time.Second))
		defer controller.SetReadDeadline(time.Time{})
		raw, err := io.ReadAll(r.Body)
		_ = controller.SetReadDeadline(time.Time{})
		if err != nil {
			var oversized *http.MaxBytesError
			if errors.As(err, &oversized) {
				http.Error(w, "Authentication request too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "Invalid authentication request", http.StatusBadRequest)
			}
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if err := validatePluginJSON(decoder, 0); err != nil {
			http.Error(w, "Invalid authentication request", http.StatusBadRequest)
			return
		}
		if _, err := decoder.Token(); err != io.EOF {
			http.Error(w, "Invalid authentication request", http.StatusBadRequest)
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || len(fields) != 1 || len(bytes.TrimSpace(fields["args"])) == 0 || bytes.TrimSpace(fields["args"])[0] != '[' {
			http.Error(w, "Expected an args array", http.StatusBadRequest)
			return
		}
		var request apiRequest
		if json.Unmarshal(raw, &request) != nil {
			http.Error(w, "Invalid authentication arguments", http.StatusBadRequest)
			return
		}
		if len(request.Args) < minArgs || len(request.Args) > maxArgs {
			http.Error(w, "Invalid authentication arguments", http.StatusBadRequest)
			return
		}
		args := make([]string, maxArgs)
		for i, raw := range request.Args {
			if strings.TrimSpace(string(raw)) == "null" || json.Unmarshal(raw, &args[i]) != nil || len(args[i]) > 1024 {
				http.Error(w, "Invalid authentication arguments", http.StatusBadRequest)
				return
			}
		}
		value, err := handler(r, args)
		if err != nil {
			// No request values or nested filesystem/crypto errors enter logs.
			logging.FromContext(r.Context()).WarnContext(r.Context(), "authentication operation rejected", "component", "auth", "operation", r.URL.Path, "result", "failure")
			jsonResponse(w, FlagResult{Flag: false, Data: err.Error()})
			return
		}
		data, ok := value.(string)
		if !ok {
			encoded, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				http.Error(w, "Authentication response failed", http.StatusInternalServerError)
				return
			}
			data = string(encoded)
		}
		jsonResponse(w, FlagResult{Flag: true, Data: data})
	})
}

func registerAPIRoutes(mux *http.ServeMux, service *auth.Service) {
	authRoute(mux, "/api/auth/status", 0, 0, func(r *http.Request, args []string) (any, error) { return service.Status() })
	authRoute(mux, "/api/auth/login", 1, 2, func(r *http.Request, args []string) (any, error) {
		return service.Login(r.RemoteAddr, args[0], args[1])
	})
	authRoute(mux, "/api/auth/logout", 0, 0, func(r *http.Request, args []string) (any, error) {
		service.RemoveSession(authTokenFromRequest(r))
		return "Success", nil
	})
	authRoute(mux, "/api/auth/session", 0, 0, func(r *http.Request, args []string) (any, error) { return "Valid", nil })
	authRoute(mux, "/api/auth/setup", 1, 3, func(r *http.Request, args []string) (any, error) {
		return service.ChangePassword(r.RemoteAddr, authTokenFromRequest(r), args[0], args[1], args[2])
	})
	authRoute(mux, "/api/auth/totp/setup", 1, 1, func(r *http.Request, args []string) (any, error) {
		return service.BeginTOTP(r.RemoteAddr, authTokenFromRequest(r), args[0])
	})
	authRoute(mux, "/api/auth/totp/cancel", 0, 0, func(r *http.Request, args []string) (any, error) {
		service.CancelTOTP(authTokenFromRequest(r))
		return "Success", nil
	})
	authRoute(mux, "/api/auth/totp/enable", 1, 1, func(r *http.Request, args []string) (any, error) {
		return service.EnableTOTP(r.RemoteAddr, authTokenFromRequest(r), args[0])
	})
	authRoute(mux, "/api/auth/totp/disable", 2, 2, func(r *http.Request, args []string) (any, error) {
		return service.DisableTOTP(r.RemoteAddr, authTokenFromRequest(r), args[0], args[1])
	})
	authRoute(mux, "/api/auth/totp/recovery", 2, 2, func(r *http.Request, args []string) (any, error) {
		return service.RegenerateRecovery(r.RemoteAddr, authTokenFromRequest(r), args[0], args[1])
	})
}
