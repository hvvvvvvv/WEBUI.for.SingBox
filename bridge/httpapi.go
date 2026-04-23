package bridge

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var nonAuthPaths = []string{}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always allow static assets, index.html and auth endpoints

		if !strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}

		// WebSocket paths are handled separately
		if strings.HasPrefix(r.URL.Path, "/ws") {
			token := r.URL.Query().Get("auth")
			if !ValidateSession(token) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")

		if !ValidateSession(token) {
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
		Hub.ServeWS(w, r)
	})

	// Kernel API proxy (HTTP)
	mux.HandleFunc("/api/kernel/", handleKernelProxy)

	// Kernel WebSocket proxy
	mux.HandleFunc("/ws/kernel/", handleKernelWSProxy)

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
	apiRoute(mux, "/api/auth/login", func(args []json.RawMessage) any {
		plainSecret, _ := unmarshalArg[string](args, 0)
		if plainSecret == "" && Config.AuthSecret != "" {
			return FlagResult{false, "Secret is required"}
		}
		if !VerifySecret(plainSecret) {
			return FlagResult{false, "Invalid secret"}
		}
		token, err := GenerateToken()
		if err != nil {
			return FlagResult{false, "Failed to generate token"}
		}
		AddSession(token)
		return FlagResult{true, token}
	})

	apiRoute(mux, "/api/auth/logout", func(args []json.RawMessage) any {
		token, _ := unmarshalArg[string](args, 0)
		if token != "" {
			RemoveSession(token)
		}
		return FlagResult{true, "Success"}
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
	apiRoute(mux, "/api/app/restart", func(args []json.RawMessage) any {
		return app.RestartApp()
	})
	apiRoute(mux, "/api/app/env", func(args []json.RawMessage) any {
		key, _ := unmarshalArg[string](args, 0)
		return app.GetEnv(key)
	})
	apiRoute(mux, "/api/app/interfaces", func(args []json.RawMessage) any {
		return app.GetInterfaces()
	})
	apiRoute(mux, "/api/app/isStartup", func(args []json.RawMessage) any {
		return app.IsStartup()
	})
	apiRoute(mux, "/api/app/showMainWindow", func(args []json.RawMessage) any {
		app.ShowMainWindow()
		return FlagResult{true, "Success"}
	})

	// Tray
	apiRoute(mux, "/api/tray/update", func(args []json.RawMessage) any {
		tray, _ := unmarshalArg[TrayContent](args, 0)
		app.UpdateTray(tray)
		return FlagResult{true, "Success"}
	})
	apiRoute(mux, "/api/tray/updateMenus", func(args []json.RawMessage) any {
		menus, _ := unmarshalArg[[]MenuItem](args, 0)
		app.UpdateTrayMenus(menus)
		return FlagResult{true, "Success"}
	})
	apiRoute(mux, "/api/tray/updateTrayAndMenus", func(args []json.RawMessage) any {
		tray, _ := unmarshalArg[TrayContent](args, 0)
		menus, _ := unmarshalArg[[]MenuItem](args, 1)
		app.UpdateTrayAndMenus(tray, menus)
		return FlagResult{true, "Success"}
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
	apiRoute(mux, "/api/file/copy", func(args []json.RawMessage) any {
		src, _ := unmarshalArg[string](args, 0)
		dst, _ := unmarshalArg[string](args, 1)
		return app.CopyFile(src, dst)
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
	apiRoute(mux, "/api/file/unzipGZ", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		output, _ := unmarshalArg[string](args, 1)
		return app.UnzipGZFile(path, output)
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
	apiRoute(mux, "/api/net/upload", func(args []json.RawMessage) any {
		method, _ := unmarshalArg[string](args, 0)
		url, _ := unmarshalArg[string](args, 1)
		path, _ := unmarshalArg[string](args, 2)
		headers, _ := unmarshalArg[map[string]string](args, 3)
		event, _ := unmarshalArg[string](args, 4)
		options, _ := unmarshalArg[RequestOptions](args, 5)
		return app.Upload(method, url, path, headers, event, options)
	})

	// Exec
	apiRoute(mux, "/api/exec/run", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		execArgs, _ := unmarshalArg[[]string](args, 1)
		options, _ := unmarshalArg[ExecOptions](args, 2)
		return app.Exec(path, execArgs, options)
	})
	apiRoute(mux, "/api/exec/background", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		execArgs, _ := unmarshalArg[[]string](args, 1)
		outEvent, _ := unmarshalArg[string](args, 2)
		endEvent, _ := unmarshalArg[string](args, 3)
		options, _ := unmarshalArg[ExecOptions](args, 4)
		return app.ExecBackground(path, execArgs, outEvent, endEvent, options)
	})
	apiRoute(mux, "/api/exec/processInfo", func(args []json.RawMessage) any {
		pid, _ := unmarshalArg[int32](args, 0)
		return app.ProcessInfo(pid)
	})
	apiRoute(mux, "/api/exec/processMemory", func(args []json.RawMessage) any {
		pid, _ := unmarshalArg[int32](args, 0)
		return app.ProcessMemory(pid)
	})
	apiRoute(mux, "/api/exec/killProcess", func(args []json.RawMessage) any {
		pid, _ := unmarshalArg[int](args, 0)
		timeout, _ := unmarshalArg[int](args, 1)
		return app.KillProcess(pid, timeout)
	})

	// Server
	apiRoute(mux, "/api/server/start", func(args []json.RawMessage) any {
		address, _ := unmarshalArg[string](args, 0)
		serverID, _ := unmarshalArg[string](args, 1)
		options, _ := unmarshalArg[ServerOptions](args, 2)
		return app.StartServer(address, serverID, options)
	})
	apiRoute(mux, "/api/server/stop", func(args []json.RawMessage) any {
		id, _ := unmarshalArg[string](args, 0)
		return app.StopServer(id)
	})
	apiRoute(mux, "/api/server/list", func(args []json.RawMessage) any {
		return app.ListServer()
	})

	// MMDB
	apiRoute(mux, "/api/mmdb/open", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		id, _ := unmarshalArg[string](args, 1)
		return app.OpenMMDB(path, id)
	})
	apiRoute(mux, "/api/mmdb/close", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		id, _ := unmarshalArg[string](args, 1)
		return app.CloseMMDB(path, id)
	})
	apiRoute(mux, "/api/mmdb/query", func(args []json.RawMessage) any {
		path, _ := unmarshalArg[string](args, 0)
		ip, _ := unmarshalArg[string](args, 1)
		dataType, _ := unmarshalArg[string](args, 2)
		return app.QueryMMDB(path, ip, dataType)
	})
}

func apiRoute(mux *http.ServeMux, path string, handler func(args []json.RawMessage) any) {
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
		result := handler(args)
		jsonResponse(w, result)
	})
}

// handleKernelProxy proxies HTTP requests to the sing-box kernel's Clash API.
// The frontend passes target address and bearer via headers.
func handleKernelProxy(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("X-Kernel-Target")
	if target == "" {
		http.Error(w, "Missing X-Kernel-Target header", http.StatusBadRequest)
		return
	}

	kernelPath := strings.TrimPrefix(r.URL.Path, "/api/kernel")
	if kernelPath == "" {
		kernelPath = "/"
	}

	targetURL := "http://" + target + kernelPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if bearer := r.Header.Get("X-Kernel-Bearer"); bearer != "" {
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
// Query params: target (host:port), auth (session token or api secret).
// The remaining path after /ws/kernel is forwarded to the kernel.
func handleKernelWSProxy(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	target := query.Get("target")
	if target == "" {
		http.Error(w, "Missing target parameter", http.StatusBadRequest)
		return
	}

	// Authenticate: check auth query param
	authToken := query.Get("auth")
	if !ValidateSession(authToken) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	kernelPath := strings.TrimPrefix(r.URL.Path, "/ws/kernel")
	if kernelPath == "" {
		kernelPath = "/"
	}

	// Build upstream query params (exclude proxy-specific params)
	upstreamParams := url.Values{}
	for k, v := range query {
		if k != "target" && k != "auth" {
			upstreamParams[k] = v
		}
	}

	upstreamURL := "ws://" + target + kernelPath
	if qs := upstreamParams.Encode(); qs != "" {
		upstreamURL += "?" + qs
	}

	upstreamConn, _, err := websocket.DefaultDialer.Dial(upstreamURL, nil)
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
			if err := upstreamConn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}()

	<-done
}
