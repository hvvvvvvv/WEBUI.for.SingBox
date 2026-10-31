package plugins

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"guiforcores/bridge/network"

	"github.com/dop251/goja"
)

const (
	DiagnosticTimeout              = 20 * time.Second
	DiagnosticRequestTimeout       = 5 * time.Second
	MaxDiagnosticRequests          = 8
	MaxDiagnosticBodyBytes         = 64 << 10
	MaxDiagnosticTotalBodyBytes    = 256 << 10
	MaxDiagnosticReportBytes       = 256 << 10
	MaxDiagnosticDownloadBytes     = 8 << 20
	DefaultDiagnosticDownloadBytes = 1 << 20
)

// SetNetworkOptions supplies the router's request policy. Credentials remain in
// Go; diagnostic scripts receive neither the provider nor its options.
func (s *Service) SetNetworkOptions(provider func() network.Options) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networkOptions = provider
}

// RunDiagnostic executes an explicit manual diagnostic without subscription or
// configuration persistence. The deadline includes waiting for the shared VM slot.
func (s *Service) RunDiagnostic(ctx context.Context, id string) (any, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, DiagnosticTimeout)
	defer cancel()
	selected, err := s.executions(OnManual, id)
	if err != nil {
		return nil, nil, err
	}
	if selected[0].plugin.ManualTarget != ManualDiagnostic {
		return nil, nil, fmt.Errorf("%w: plugin requires a subscription", ErrInvalid)
	}
	select {
	case s.runSlot <- struct{}{}:
		defer func() { <-s.runSlot }()
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.RLock()
	provider := s.networkOptions
	s.mu.RUnlock()
	options := network.Options{Mode: network.ModeNone}
	if provider != nil {
		options = provider()
	}
	// Diagnostics must not inherit host GitHub credentials or a download mirror.
	options.GitHubToken, options.GitHubMirror = "", ""
	diagnostic := &diagnosticRun{ctx: ctx, options: options}
	logs := &logBuffer{lines: []string{}}
	output, err := executeHook(ctx, selected[0], OnManual, []byte("null"), []byte("null"), logs, diagnostic)
	if err != nil {
		return nil, logs.lines, fmt.Errorf("plugin %q: %w", id, err)
	}
	var report any
	if err := decodeJSON(output, &report, false); err != nil {
		return nil, logs.lines, err
	}
	return report, logs.lines, nil
}

type diagnosticRun struct {
	ctx       context.Context
	options   network.Options
	requests  int
	bodyBytes int
	measured  bool
}

type diagnosticRequest struct {
	url      *url.URL
	method   string
	route    string
	maxBytes int64
}

func (run *diagnosticRun) install(vm *goja.Runtime, api *goja.Object) error {
	bind := func(measure bool) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			request, err := parseDiagnosticRequest(call, measure)
			if err != nil {
				diagnosticThrow(vm, err.Error(), "invalid")
			}
			result, err := run.request(request, measure)
			if err != nil {
				diagnosticThrow(vm, "Diagnostic request failed: "+diagnosticErrorMessage(err), diagnosticErrorCode(err))
			}
			// Return an ordinary JS object, never a reflected Go object.
			value := vm.NewObject()
			for key, item := range result {
				_ = value.Set(key, item)
			}
			return value
		}
	}
	if err := api.Set("fetch", bind(false)); err != nil {
		return err
	}
	return api.Set("measureDownload", bind(true))
}

func diagnosticThrow(vm *goja.Runtime, message, code string) {
	value := vm.NewTypeError(message)
	_ = value.Set("code", code)
	panic(value)
}

func parseDiagnosticRequest(call goja.FunctionCall, measure bool) (diagnosticRequest, error) {
	result := diagnosticRequest{method: http.MethodGet, route: "configured", maxBytes: DefaultDiagnosticDownloadBytes}
	invalid := func() (diagnosticRequest, error) {
		return result, fmt.Errorf("expected an HTTP(S) URL without credentials and supported diagnostic options")
	}
	value := call.Argument(0)
	if value.ExportType() != reflect.TypeOf("") {
		return invalid()
	}
	raw := value.String()
	if len(raw) > 4096 {
		return invalid()
	}
	parsed, err := url.Parse(raw)
	if err != nil || !validDiagnosticURL(parsed) {
		return invalid()
	}
	result.url = parsed
	if len(call.Arguments) > 2 {
		return invalid()
	}
	options := call.Argument(1)
	if !goja.IsUndefined(options) {
		object, ok := options.(*goja.Object)
		if !ok || object.ClassName() != "Object" {
			return invalid()
		}
		for _, key := range object.Keys() {
			value := object.Get(key)
			switch key {
			case "route":
				if value.ExportType() != reflect.TypeOf("") {
					return invalid()
				}
				result.route = value.String()
			case "method":
				if measure || value.ExportType() != reflect.TypeOf("") {
					return invalid()
				}
				result.method = value.String()
			case "max_bytes":
				if !measure {
					return invalid()
				}
				switch value.ExportType() {
				case reflect.TypeOf(int64(0)), reflect.TypeOf(float64(0)):
					bytes := value.ToFloat()
					if math.IsNaN(bytes) || bytes < 1 || bytes > MaxDiagnosticDownloadBytes || bytes != math.Trunc(bytes) {
						return invalid()
					}
					result.maxBytes = int64(bytes)
				default:
					return invalid()
				}
			default:
				return invalid()
			}
		}
	}
	if (result.method != http.MethodGet && result.method != http.MethodHead) || (result.route != "configured" && result.route != "direct" && result.route != "core") {
		return invalid()
	}
	return result, nil
}

func validDiagnosticURL(value *url.URL) bool {
	return value != nil && (value.Scheme == "http" || value.Scheme == "https") && value.Hostname() != "" && value.User == nil && value.Opaque == ""
}

func diagnosticOrigin(value *url.URL) string {
	port := value.Port()
	if port == "" {
		port = "80"
		if value.Scheme == "https" {
			port = "443"
		}
	}
	return value.Scheme + "://" + net.JoinHostPort(strings.ToLower(value.Hostname()), port)
}

type diagnosticPolicyError string

func (err diagnosticPolicyError) Error() string { return string(err) }

type diagnosticTransport struct {
	run  *diagnosticRun
	next http.RoundTripper
}

func (transport diagnosticTransport) CloseIdleConnections() {
	if closer, ok := transport.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
func (transport diagnosticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport.run.requests >= MaxDiagnosticRequests {
		return nil, diagnosticPolicyError("request budget exhausted")
	}
	transport.run.requests++
	return transport.next.RoundTrip(request)
}

func (run *diagnosticRun) request(request diagnosticRequest, measure bool) (map[string]any, error) {
	if err := run.ctx.Err(); err != nil {
		return nil, err
	}
	if measure {
		if run.measured {
			return nil, diagnosticPolicyError("only one download measurement is allowed per run")
		}
		run.measured = true
	} else if request.method != http.MethodHead && run.bodyBytes >= MaxDiagnosticTotalBodyBytes {
		return nil, diagnosticPolicyError("response body budget exhausted")
	}
	options := run.options
	switch request.route {
	case "direct":
		options.Mode = network.ModeNone
	case "core":
		options.Mode = network.ModeCore
	}
	client, err := network.NewClient(options, DiagnosticRequestTimeout, false)
	if err != nil {
		return nil, diagnosticPolicyError("requested route is unavailable; check the router request settings and core HTTP inbound")
	}
	defer client.CloseIdleConnections()
	client.Transport = diagnosticTransport{run: run, next: client.Transport}
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return diagnosticPolicyError("redirect limit exceeded")
		}
		if !validDiagnosticURL(next.URL) || diagnosticOrigin(next.URL) != diagnosticOrigin(request.url) {
			return diagnosticPolicyError("redirects must stay on the same HTTP(S) origin")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(run.ctx, request.method, request.url.String(), nil)
	if err != nil {
		return nil, diagnosticPolicyError("invalid diagnostic URL")
	}
	if options.UserAgent != "" {
		req.Header.Set("User-Agent", options.UserAgent)
	}
	if measure {
		// Count transferred payload bytes, not automatically decompressed text.
		req.Header.Set("Accept-Encoding", "identity")
	}
	started := time.Now()
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if measure {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, diagnosticPolicyError(fmt.Sprintf("download measurement returned HTTP %d", response.StatusCode))
		}
		count, readErr := io.CopyN(io.Discard, response.Body, request.maxBytes)
		elapsed := max(time.Since(started), time.Nanosecond)
		if err := run.ctx.Err(); err != nil {
			return nil, err
		}
		if readErr != nil && readErr != io.EOF && count == 0 {
			return nil, readErr
		}
		truncated := count == request.maxBytes && (response.ContentLength < 0 || response.ContentLength > count)
		return map[string]any{"status": response.StatusCode, "bytes": count, "elapsed_ms": elapsed.Milliseconds(), "bytes_per_second": float64(count) / elapsed.Seconds(), "truncated": truncated, "incomplete": readErr != nil && readErr != io.EOF, "route": request.route}, nil
	}
	limit := min(MaxDiagnosticBodyBytes, MaxDiagnosticTotalBodyBytes-run.bodyBytes)
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
	}
	// Failed or truncated responses consume the same shared body budget, even
	// when JavaScript catches the error and attempts another request.
	run.bodyBytes += len(data)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": response.StatusCode, "body": string(data), "elapsed_ms": time.Since(started).Milliseconds(), "route": request.route, "truncated": truncated}, nil
}

func diagnosticErrorCode(err error) string {
	var policy diagnosticPolicyError
	if errors.As(err, &policy) {
		return "policy"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return "tls"
	}
	return "network"
}

func diagnosticErrorMessage(err error) string {
	var policy diagnosticPolicyError
	if errors.As(err, &policy) {
		return string(policy)
	}
	switch diagnosticErrorCode(err) {
	case "timeout":
		return "request timed out"
	case "tls":
		return "TLS certificate verification failed"
	default:
		return "network connection or response failed"
	}
}
