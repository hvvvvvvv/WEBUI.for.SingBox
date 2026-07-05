package httptransport

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"guiforcores/bridge/appsystem"
	"guiforcores/bridge/appupdate"
	"guiforcores/bridge/auth"
	"guiforcores/bridge/config"
	"guiforcores/bridge/event"
	"guiforcores/bridge/kernel"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/profile"
	"guiforcores/bridge/ruleset"
	"guiforcores/bridge/scheduler"
	"guiforcores/bridge/settings"
	"guiforcores/bridge/subscription"
	"guiforcores/gen/app/v1/appv1connect"
	"guiforcores/gen/kernel/v1/kernelv1connect"
	"guiforcores/gen/profile/v1/profilev1connect"

	"github.com/gorilla/websocket"
)

type Options struct {
	Address        string
	Assets         embed.FS
	Platform       *platform.Service
	Auth           *auth.Service
	Events         *event.Hub
	Config         *config.Service
	AppConfig      *config.AppService
	Profiles       *profile.Service
	Kernel         *kernel.Service
	Settings       *settings.Service
	Subscriptions  *subscription.Service
	RuleSets       *ruleset.Service
	Scheduler      *scheduler.Service
	Update         *appupdate.Service
	System         *appsystem.Service
	RollingRelease func() bool
}

type Server struct {
	options Options
	server  *http.Server
}

type FlagResult = platform.Result

var websocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

func NewServer(options Options) (*Server, error) {
	server := &Server{options: options}
	handler, err := server.buildHandler()
	if err != nil {
		return nil, err
	}
	server.server = &http.Server{Addr: options.Address, Handler: handler}
	return server, nil
}

func authTokenFromRequest(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/ws") {
		return strings.TrimSpace(r.URL.Query().Get("auth"))
	}
	auth := r.Header.Get("Authorization")
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

type apiRequest struct {
	Args []json.RawMessage `json:"args"`
}

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func readArgs(r *http.Request) ([]json.RawMessage, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	var req apiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return req.Args, nil
}

func unmarshalArg[T any](args []json.RawMessage, index int) (T, bool) {
	var zero T
	if index >= len(args) {
		return zero, false
	}
	if err := json.Unmarshal(args[index], &zero); err != nil {
		return zero, false
	}
	return zero, true
}

func (s *Server) buildHandler() (http.Handler, error) {
	distFS, err := fs.Sub(s.options.Assets, "frontend/dist")
	if err != nil {
		return nil, fmt.Errorf("access embedded frontend: %w", err)
	}

	frontendHandler := s.buildFrontendHandler(distFS)

	return s.buildRootHandler(frontendHandler, s.buildProtectedMux()), nil
}

func (s *Server) buildRootHandler(frontendHandler http.Handler, protectedHandlerMux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, pattern := protectedHandlerMux.Handler(r)
		if pattern != "" {
			if r.URL.Path != "/api/auth/login" {
				token := authTokenFromRequest(r)
				if !s.options.Auth.ValidateSession(token) {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
			handler.ServeHTTP(w, r)
			return
		}
		frontendHandler.ServeHTTP(w, r)
	})
}

func (s *Server) buildProtectedMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		s.options.Events.ServeWebSocket(w, r, authTokenFromRequest(r))
	})

	mux.HandleFunc("/api/kernel/", s.handleKernelProxy)
	mux.HandleFunc("/ws/kernel/", s.handleKernelWebSocketProxy)
	s.registerRPCRoutes(mux)
	registerAPIRoutes(mux, s.options.Auth)

	return mux
}

func (s *Server) buildFrontendHandler(distFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		requestPath := cleanFrontendRequestPath(r.URL.Path)
		if s.serveRollingReleaseAsset(w, r, requestPath) {
			return
		}

		assetPath := frontendAssetPath(requestPath)
		info, err := fs.Stat(distFS, assetPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if info.IsDir() {
			indexPath := path.Join(assetPath, "index.html")
			if assetPath == "." {
				indexPath = "index.html"
			}
			info, err = fs.Stat(distFS, indexPath)
			if err != nil || info.IsDir() {
				http.NotFound(w, r)
				return
			}
			assetPath = indexPath
		}

		setFrontendCacheHeader(w, assetPath)
		data, err := fs.ReadFile(distFS, assetPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if contentType := mime.TypeByExtension(filepath.Ext(assetPath)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		http.ServeContent(w, r, assetPath, time.Time{}, bytes.NewReader(data))
	})
}

func (s *Server) registerRPCRoutes(mux *http.ServeMux) {
	register := func(path string, handler http.Handler) {
		mux.Handle("/api/rpc"+path, http.StripPrefix("/api/rpc", handler))
	}

	path, handler := kernelv1connect.NewKernelConfigServiceHandler(s.options.Config)
	register(path, handler)
	path, handler = profilev1connect.NewProfileServiceHandler(s.options.Profiles)
	register(path, handler)
	path, handler = kernelv1connect.NewKernelRuntimeServiceHandler(s.options.Kernel)
	register(path, handler)
	path, handler = appv1connect.NewAppSettingsServiceHandler(s.options.Settings)
	register(path, handler)
	path, handler = appv1connect.NewAppConfigServiceHandler(s.options.AppConfig)
	register(path, handler)
	path, handler = appv1connect.NewSubscriptionServiceHandler(s.options.Subscriptions)
	register(path, handler)
	path, handler = appv1connect.NewRuleSetServiceHandler(s.options.RuleSets)
	register(path, handler)
	path, handler = appv1connect.NewScheduledTaskServiceHandler(s.options.Scheduler)
	register(path, handler)
	path, handler = appv1connect.NewAppUpdateServiceHandler(s.options.Update)
	register(path, handler)
	path, handler = appv1connect.NewAppSystemServiceHandler(s.options.System)
	register(path, handler)
}

func cleanFrontendRequestPath(requestPath string) string {
	cleaned := path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

func frontendAssetPath(requestPath string) string {
	assetPath := strings.TrimPrefix(cleanFrontendRequestPath(requestPath), "/")
	if assetPath == "" {
		return "."
	}
	return assetPath
}

func indexFileName(filePath string) bool {
	return filepath.Base(filePath) == "index.html"
}

func setFrontendCacheHeader(w http.ResponseWriter, filePath string) {
	if indexFileName(filePath) {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "max-age=31536000, immutable")
	}
}

func (s *Server) serveRollingReleaseAsset(w http.ResponseWriter, r *http.Request, requestPath string) bool {
	rollingRelease := s.options.RollingRelease != nil && s.options.RollingRelease()
	if !rollingRelease {
		return false
	}

	filePath := s.rollingReleaseFilePath(requestPath)
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		setFrontendCacheHeader(w, filePath)
		http.ServeFile(w, r, filePath)
		return true
	}

	indexPath := filepath.Join(filePath, "index.html")
	if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
		setFrontendCacheHeader(w, indexPath)
		http.ServeFile(w, r, indexPath)
		return true
	}

	return false
}

func (s *Server) rollingReleaseFilePath(requestPath string) string {
	root := s.options.Platform.ResolvePath("data/rolling-release")
	assetPath := frontendAssetPath(requestPath)
	if assetPath == "." {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(assetPath))
}

func (s *Server) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownContext)
	}()
	log.Printf("Server starting at http://%s", s.options.Address)
	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) Close(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func registerAPIRoutes(mux *http.ServeMux, authService *auth.Service) {
	apiRouteWithRequest(mux, "/api/auth/login", func(r *http.Request, args []json.RawMessage) any {
		if authService.IsLoginRateLimited(r.RemoteAddr) {
			return FlagResult{Flag: false, Data: "Too many failed attempts, try again later"}
		}

		plainSecret, _ := unmarshalArg[string](args, 0)
		if !authService.VerifySecret(plainSecret) {
			authService.RecordLoginFailure(r.RemoteAddr)
			return FlagResult{Flag: false, Data: "Invalid secret"}
		}
		token, err := authService.GenerateToken()
		if err != nil {
			return FlagResult{Flag: false, Data: "Failed to generate token"}
		}
		authService.ClearLoginFailures(r.RemoteAddr)
		authService.AddSession(token)
		return FlagResult{Flag: true, Data: token}
	})

	apiRouteWithRequest(mux, "/api/auth/logout", func(r *http.Request, args []json.RawMessage) any {
		token := authTokenFromRequest(r)
		if token != "" {
			authService.RemoveSession(token)
		}
		return FlagResult{Flag: true, Data: "Success"}
	})

	apiRoute(mux, "/api/auth/session", func(args []json.RawMessage) any {
		return FlagResult{Flag: true, Data: "Valid"}
	})

	apiRoute(mux, "/api/auth/setup", func(args []json.RawMessage) any {
		secret, _ := unmarshalArg[string](args, 0)
		needClear := !(secret == "" || auth.HashSecret(secret) == authService.SecretHash())

		err := authService.SetSecret(secret)
		if err != nil {
			return FlagResult{Flag: false, Data: err.Error()}
		}
		if needClear {
			token, err := authService.GenerateToken()
			if err != nil {
				return FlagResult{Flag: false, Data: "Failed to generate token"}
			}
			authService.ClearSessions()
			authService.AddSession(token)
			return FlagResult{Flag: true, Data: token}
		}

		return FlagResult{Flag: true, Data: ""}
	})

}

func apiRoute(mux *http.ServeMux, path string, handler func(args []json.RawMessage) any) {
	apiRouteWithRequest(mux, path, func(_ *http.Request, args []json.RawMessage) any {
		return handler(args)
	})
}

func apiRouteWithRequest(mux *http.ServeMux, path string, handler func(r *http.Request, args []json.RawMessage) any) {
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		args, err := readArgs(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result := handler(r, args)
		jsonResponse(w, result)
	})
}

// handleKernelProxy proxies HTTP requests to the sing-box kernel's Clash API.
func (s *Server) handleKernelProxy(w http.ResponseWriter, r *http.Request) {
	kernelPath := strings.TrimPrefix(r.URL.Path, "/api/kernel")
	if kernelPath == "" {
		kernelPath = "/"
	}

	targetURL := "http://" + config.CoreAPIController + kernelPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if bearer := s.options.Config.ReadGeneratedSecret(); bearer != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+bearer)
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		proxyReq.Header.Set("Content-Type", ct)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleKernelWSProxy proxies WebSocket connections to the sing-box kernel.
// Query params: auth (session token).
// The remaining path after /ws/kernel is forwarded to the kernel.
func (s *Server) handleKernelWebSocketProxy(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Authenticate: check auth query param
	authToken := strings.TrimSpace(query.Get("auth"))
	if !s.options.Auth.ValidateSession(authToken) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	kernelPath := strings.TrimPrefix(r.URL.Path, "/ws/kernel")
	if kernelPath == "" {
		kernelPath = "/"
	}

	upstreamParams := query
	upstreamParams.Del("auth")

	upstreamURL := "ws://" + config.CoreAPIController + kernelPath
	if qs := upstreamParams.Encode(); qs != "" {
		upstreamURL += "?" + qs
	}

	upstreamHeaders := http.Header{}
	if bearer := s.options.Config.ReadGeneratedSecret(); bearer != "" {
		upstreamHeaders.Set("Authorization", "Bearer "+bearer)
	}

	upstreamConn, _, err := websocket.DefaultDialer.Dial(upstreamURL, upstreamHeaders)
	if err != nil {
		http.Error(w, "Failed to connect to kernel: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	clientConn, err := websocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer clientConn.Close()

	var once sync.Once
	done := make(chan struct{})
	closeBoth := func() { once.Do(func() { close(done) }) }

	// upstream -> client
	go func() {
		defer closeBoth()
		for {
			msgType, msg, err := upstreamConn.ReadMessage()
			if err != nil {
				return
			}
			if !s.options.Auth.ValidateSessionWithoutTouch(authToken) {
				return
			}
			if err := clientConn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}()

	// client -> upstream
	go func() {
		defer closeBoth()
		for {
			msgType, msg, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			if !s.options.Auth.ValidateSessionWithoutTouch(authToken) {
				return
			}
			if err := upstreamConn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}()

	<-done
}
