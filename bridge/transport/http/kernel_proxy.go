package httptransport

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"guiforcores/bridge/logging"
)

// A subscription has no overall timeout. Only establishing the connection and
// waiting for its response headers are bounded.
var nativeAPITransport = &http.Transport{
	Proxy:                 nil,
	DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ResponseHeaderTimeout: 15 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	DisableCompression:    true,
}

func (s *Server) handleKernelProxy(w http.ResponseWriter, r *http.Request) {
	kernelPath := strings.TrimPrefix(r.URL.Path, "/api/kernel")
	method := strings.TrimPrefix(kernelPath, "/daemon.StartedService/")
	if method == kernelPath || method == "" || strings.Contains(method, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.options.Kernel == nil {
		http.Error(w, "Kernel is not running", http.StatusServiceUnavailable)
		return
	}
	ctx, target, secret, cancel, err := s.options.Kernel.NativeAPIContext(r.Context())
	if err != nil {
		http.Error(w, "Kernel is not running", http.StatusServiceUnavailable)
		return
	}
	defer cancel()
	if s.lifecycleContext != nil {
		stop := context.AfterFunc(s.lifecycleContext, cancel)
		defer stop()
	}
	upstream, err := url.Parse(target)
	if err != nil {
		http.Error(w, "Invalid kernel API address", http.StatusBadGateway)
		return
	}
	token := authTokenFromRequest(r)
	proxyNativeAPI(w, r.WithContext(ctx), upstream, secret, func() bool {
		return s.options.Auth.ValidateSessionWithoutTouch(token)
	})
}

func proxyNativeAPI(w http.ResponseWriter, r *http.Request, upstream *url.URL, secret string, validSession func() bool) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !validSession() {
					cancel()
					return
				}
			}
		}
	}()
	proxy := &httputil.ReverseProxy{
		Transport:     nativeAPITransport,
		FlushInterval: -1,
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(upstream)
			req.Out.URL.Path = strings.TrimPrefix(req.In.URL.Path, "/api/kernel")
			req.Out.URL.RawPath = ""
			req.Out.Header.Set("Authorization", "Bearer "+secret)
		},
		ModifyResponse: func(response *http.Response) error {
			// Only the application can issue a login challenge. Native gRPC
			// authentication errors (including trailer frames) pass unchanged.
			if response.StatusCode == http.StatusUnauthorized {
				response.StatusCode = http.StatusBadGateway
				response.Header.Del("WWW-Authenticate")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			if req.Context().Err() != nil {
				return
			}
			logging.FromContext(req.Context()).WarnContext(req.Context(), "kernel API request failed",
				"component", "http", "operation", "kernel_proxy", "result", "failure", "error", err)
			http.Error(w, "Kernel API unavailable", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r.WithContext(ctx))
}
