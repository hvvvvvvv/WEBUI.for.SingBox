package bridge

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

func isPublicPath(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/ws") {
		return true
	}
	if r.URL.Path == "/api/auth/login" {
		return true
	}
	if r.URL.Path == "/api/auth/setup" && GetSecretKey() == "" {
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

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r) {
			next.ServeHTTP(w, r)
			return
		}

		if !ValidateSession(requestAuthToken(r)) {
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

func StartHTTPServer(addr string, assets embed.FS) {
	app := AppInstance

	mux := http.NewServeMux()

	// WebSocket
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		Hub.ServeWS(w, r, requestAuthToken(r))
	})

	// Kernel API proxy (HTTP)
	mux.HandleFunc("/api/kernel/", handleKernelProxy)

	// Kernel WebSocket proxy
	mux.HandleFunc("/ws/kernel/", handleKernelWSProxy)

	// Connect RPC routes
	kernelSvc := registerConfigRPCRoutes(mux, app)

	// API routes
	registerAPIRoutes(mux, app)

	// Serve embedded frontend
	distFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal("Failed to access embedded frontend:", err)
	}

	fileServer := http.FileServer(http.FS(distFS))
	rollingHandler := RollingRelease(fileServer)

	// Wrap to inject API secret or auth state into index.html
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// For non-root paths that look like static assets, serve directly
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			rollingHandler.ServeHTTP(w, r)
			return
		}
		// Read index.html and inject the secret
		f, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			rollingHandler.ServeHTTP(w, r)
			return
		}
		html := string(f)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	}))

	handler := authMiddleware(mux)

	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	go kernelSvc.autoStartCoreOnLaunch(context.Background())

	// Graceful shutdown on SIGINT / SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	log.Printf("Server starting at http://%s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("Server error:", err)
	}
	log.Println("Server stopped")
}

func registerAPIRoutes(mux *http.ServeMux, app *App) {
	apiRouteWithRequest(mux, "/api/auth/login", func(r *http.Request, args []json.RawMessage) any {
		if IsLoginRateLimited(r.RemoteAddr) {
			return FlagResult{false, "Too many failed attempts, try again later"}
		}

		plainSecret, _ := unmarshalArg[string](args, 0)
		if !VerifySecret(plainSecret) {
			RecordLoginFailure(r.RemoteAddr)
			return FlagResult{false, "Invalid secret"}
		}
		token, err := GenerateToken()
		if err != nil {
			return FlagResult{false, "Failed to generate token"}
		}
		ClearLoginFailures(r.RemoteAddr)
		AddSession(token)
		return FlagResult{true, token}
	})

	apiRouteWithRequest(mux, "/api/auth/logout", func(r *http.Request, args []json.RawMessage) any {
		token := requestAuthToken(r)
		if token != "" {
			RemoveSession(token)
		}
		return FlagResult{true, "Success"}
	})

	apiRoute(mux, "/api/auth/session", func(args []json.RawMessage) any {
		return FlagResult{true, "Valid"}
	})

	apiRoute(mux, "/api/auth/setup", func(args []json.RawMessage) any {
		secret, _ := unmarshalArg[string](args, 0)
		needClear := !(secret == "" || HashSecret(secret) == GetSecretKey())

		err := SetSecretKey(secret)
		if err != nil {
			return FlagResult{false, err.Error()}
		}
		if needClear {
			token, err := GenerateToken()
			if err != nil {
				return FlagResult{false, "Failed to generate token"}
			}
			ClearSessions()
			AddSession(token)
			return FlagResult{true, token}
		}

		return FlagResult{true, ""}
	})

	// App
	apiRoute(mux, "/api/app/exit", func(args []json.RawMessage) any {
		app.ExitApp()
		return FlagResult{true, "Success"}
	})
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
	apiRoute(mux, "/api/exec/processInfo", func(args []json.RawMessage) any {
		pid, _ := unmarshalArg[int32](args, 0)
		return app.ProcessInfo(pid)
	})
	apiRoute(mux, "/api/exec/processMemory", func(args []json.RawMessage) any {
		pid, _ := unmarshalArg[int32](args, 0)
		return app.ProcessMemory(pid)
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
func handleKernelProxy(w http.ResponseWriter, r *http.Request) {
	kernelPath := strings.TrimPrefix(r.URL.Path, "/api/kernel")
	if kernelPath == "" {
		kernelPath = "/"
	}

	targetURL := "http://" + coreAPIController + kernelPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if bearer := readKernelBearerFromGeneratedConfig(); bearer != "" {
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
func handleKernelWSProxy(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Authenticate: check auth query param
	authToken := strings.TrimSpace(query.Get("auth"))
	if !ValidateSession(authToken) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	kernelPath := strings.TrimPrefix(r.URL.Path, "/ws/kernel")
	if kernelPath == "" {
		kernelPath = "/"
	}

	upstreamParams := query
	upstreamParams.Del("auth")

	upstreamURL := "ws://" + coreAPIController + kernelPath
	if qs := upstreamParams.Encode(); qs != "" {
		upstreamURL += "?" + qs
	}

	upstreamHeaders := http.Header{}
	if bearer := readKernelBearerFromGeneratedConfig(); bearer != "" {
		upstreamHeaders.Set("Authorization", "Bearer "+bearer)
	}

	upstreamConn, _, err := websocket.DefaultDialer.Dial(upstreamURL, upstreamHeaders)
	if err != nil {
		http.Error(w, "Failed to connect to kernel: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	clientConn, err := upgrader.Upgrade(w, r, nil)
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
			if !ValidateSessionNonTouch(authToken) {
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
			if !ValidateSessionNonTouch(authToken) {
				return
			}
			if err := upstreamConn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}()

	<-done
}

func readKernelBearerFromGeneratedConfig() string {
	bytes, err := os.ReadFile(GetPath(coreConfigFilePath))
	if err != nil || len(bytes) == 0 {
		return ""
	}

	var root map[string]any
	if err := json.Unmarshal(bytes, &root); err != nil {
		return ""
	}

	experimental, _ := root["experimental"].(map[string]any)
	if experimental == nil {
		return ""
	}

	clashAPI, _ := experimental["clash_api"].(map[string]any)
	if clashAPI == nil {
		return ""
	}

	secret, _ := clashAPI["secret"].(string)
	return strings.TrimSpace(secret)
}
