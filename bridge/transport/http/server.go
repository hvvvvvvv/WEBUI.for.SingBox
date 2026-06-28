package httptransport

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

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
	RollingRelease func() bool
}

type Server struct {
	options Options
	server  *http.Server
}

type FlagResult = platform.Result
type IOOptions = platform.IOOptions
type RequestOptions = platform.RequestOptions
type ExecOptions = platform.ExecOptions

var websocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

func NewServer(options Options) (*Server, error) {
	server := &Server{options: options}
	handler, err := server.handler()
	if err != nil {
		return nil, err
	}
	server.server = &http.Server{Addr: options.Address, Handler: handler}
	return server, nil
}

func isPublicPath(r *http.Request, authService *auth.Service) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/ws") {
		return true
	}
	if r.URL.Path == "/api/auth/login" {
		return true
	}
	if r.URL.Path == "/api/auth/setup" && authService.SecretHash() == "" {
		return true
	}
	return false
}

func requestAuthToken(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/ws") {
		return strings.TrimSpace(r.URL.Query().Get("auth"))
	}
	auth := r.Header.Get("Authorization")
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

func authMiddleware(authService *auth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r, authService) {
			next.ServeHTTP(w, r)
			return
		}

		if !authService.ValidateSession(requestAuthToken(r)) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
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

func (s *Server) handler() (http.Handler, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		s.options.Events.ServeWebSocket(w, r, requestAuthToken(r))
	})

	mux.HandleFunc("/api/kernel/", s.handleKernelProxy)
	mux.HandleFunc("/ws/kernel/", s.handleKernelWebSocketProxy)
	s.registerRPCRoutes(mux)
	registerAPIRoutes(mux, s.options.Platform, s.options.Auth)

	distFS, err := fs.Sub(s.options.Assets, "frontend/dist")
	if err != nil {
		return nil, fmt.Errorf("access embedded frontend: %w", err)
	}

	fileServer := http.FileServer(http.FS(distFS))
	rollingHandler := s.rollingReleaseHandler(fileServer)

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			rollingHandler.ServeHTTP(w, r)
			return
		}
		f, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			rollingHandler.ServeHTTP(w, r)
			return
		}
		html := string(f)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))

	return authMiddleware(s.options.Auth, mux), nil
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
}

func (s *Server) rollingReleaseHandler(next http.Handler) http.Handler {
	environment := s.options.Platform.Environment()
	isDevelopment := strings.Contains(environment.AppVersion, "dev")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := r.URL.Path
		isIndex := requestPath == "/"
		if isIndex {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "max-age=31536000, immutable")
		}

		rollingRelease := s.options.RollingRelease != nil && s.options.RollingRelease()
		if isDevelopment || !rollingRelease {
			next.ServeHTTP(w, r)
			return
		}
		if isIndex {
			requestPath = "/index.html"
		}
		filePath := s.options.Platform.ResolvePath("data/rolling-release" + requestPath)
		if _, err := os.Stat(filePath); err != nil {
			next.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filePath)
	})
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

func registerAPIRoutes(mux *http.ServeMux, app *platform.Service, authService *auth.Service) {
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
		token := requestAuthToken(r)
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

	// App
	apiRoute(mux, "/api/app/env", func(args []json.RawMessage) any {
		key, _ := unmarshalArg[string](args, 0)
		return app.GetEnv(key)
	})
	apiRoute(mux, "/api/app/interfaces", func(args []json.RawMessage) any {
		return app.GetInterfaces()
	})

	// IO
	apiRoute(mux, "/api/file/write", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		content, _ := unmarshalArg[string](args, 1)
		options, _ := unmarshalArg[IOOptions](args, 2)
		return app.WriteFile(path, content, options)
	})
	apiRoute(mux, "/api/file/read", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		options, _ := unmarshalArg[IOOptions](args, 1)
		return app.ReadFile(path, options)
	})
	apiRoute(mux, "/api/file/move", func(args []json.RawMessage) any {
		source, _ := unmarshalArg[string](args, 0)
		target, _ := unmarshalArg[string](args, 1)
		return app.MoveFile(source, target)
	})
	apiRoute(mux, "/api/file/remove", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		return app.RemoveFile(path)
	})
	apiRoute(mux, "/api/file/makeDir", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		return app.MakeDir(path)
	})
	apiRoute(mux, "/api/file/readDir", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		return app.ReadDir(path)
	})
	apiRoute(mux, "/api/file/openDir", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		return app.OpenDir(path)
	})
	apiRoute(mux, "/api/file/openURI", func(args []json.RawMessage) any {
		uri, _ := unmarshalArg[string](args, 0)
		return app.OpenURI(uri)
	})
	apiRoute(mux, "/api/file/absolutePath", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		return app.AbsolutePath(path)
	})
	apiRoute(mux, "/api/file/exists", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		return app.FileExists(path)
	})
	apiRoute(mux, "/api/file/unzipZIP", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		output, _ := unmarshalArg[string](args, 1)
		return app.UnzipZIPFile(path, output)
	})
	apiRoute(mux, "/api/file/unzipTarGZ", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		output, _ := unmarshalArg[string](args, 1)
		return app.UnzipTarGZFile(path, output)
	})

	// Net
	apiRoute(mux, "/api/net/requests", func(args []json.RawMessage) any {
		method, _ := unmarshalArg[string](args, 0)
		url, _ := unmarshalArg[string](args, 1)
		headers, _ := unmarshalArg[map[string]string](args, 2)
		body, _ := unmarshalArg[string](args, 3)
		options, _ := unmarshalArg[RequestOptions](args, 4)
		return app.Requests(method, url, headers, body, options)
	})
	apiRoute(mux, "/api/net/download", func(args []json.RawMessage) any {
		method, _ := unmarshalArg[string](args, 0)
		url, _ := unmarshalArg[string](args, 1)
		path, _ := unmarshalArg[string](args, 2)
		headers, _ := unmarshalArg[map[string]string](args, 3)
		event, _ := unmarshalArg[string](args, 4)
		options, _ := unmarshalArg[RequestOptions](args, 5)
		return app.Download(method, url, path, headers, event, options)
	})

	// Exec
	apiRoute(mux, "/api/exec/run", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		execArgs, _ := unmarshalArg[[]string](args, 1)
		options, _ := unmarshalArg[ExecOptions](args, 2)
		return app.Exec(path, execArgs, options)
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
