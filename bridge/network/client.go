// Package network applies the backend's persisted outbound request policy.
package network

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ModeNone        = "none"
	ModeEnvironment = "environment"
	ModeCore        = "core"
	ModeCustom      = "custom"
)

type Options struct {
	Mode                 string
	ProxyURL             string
	UserAgent            string
	GitHubToken          string
	GitHubMirror         string
	GitHubMirrorDisabled bool
	CoreProxy            func() (string, error)
}

func Validate(options Options) error {
	switch options.Mode {
	case "", ModeNone, ModeEnvironment, ModeCore:
	case ModeCustom:
		if _, err := ParseProxy(options.ProxyURL); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported request proxy mode")
	}
	if strings.ContainsAny(options.UserAgent, "\r\n") || len(options.UserAgent) > 1024 {
		return fmt.Errorf("user agent must be at most 1024 bytes without line breaks")
	}
	if strings.ContainsAny(options.GitHubToken, "\r\n") || len(options.GitHubToken) > 1024 {
		return fmt.Errorf("GitHub API token must be at most 1024 bytes without line breaks")
	}
	return ValidateMirror(options.GitHubMirror)
}

func ParseProxy(raw string) (*url.URL, error) {
	if len(raw) > 4096 {
		return nil, fmt.Errorf("proxy URL must be at most 4096 bytes")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Hostname() == "" || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("proxy must be an HTTP(S) or SOCKS5 URL with host and port")
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("proxy scheme must be http, https, socks5 or socks5h")
	}
	if parsed.Port() == "" {
		if parsed.Scheme == "http" {
			parsed.Host = net.JoinHostPort(parsed.Hostname(), "80")
		} else if parsed.Scheme == "https" {
			parsed.Host = net.JoinHostPort(parsed.Hostname(), "443")
		} else {
			return nil, fmt.Errorf("SOCKS5 proxy requires a port")
		}
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("proxy port must be between 1 and 65535")
	}
	parsed.Path = ""
	return parsed, nil
}

func ValidateMirror(template string) error {
	if template == "" {
		return nil
	}
	if len(template) > 2048 || strings.Count(template, "{url}") != 1 {
		return fmt.Errorf("GitHub mirror must be an HTTPS URL containing exactly one {url} placeholder")
	}
	parsed, err := url.Parse(strings.Replace(template, "{url}", "https://github.com/owner/repo/releases/download/version/asset", 1))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("GitHub mirror must use HTTPS without credentials or a fragment")
	}
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("GitHub mirror port must be between 1 and 65535")
		}
	}
	// The placeholder belongs in the path/query, never in the destination host.
	prefix := strings.SplitN(template, "{url}", 2)[0]
	base, err := url.Parse(prefix)
	if err != nil || base.Hostname() != parsed.Hostname() || base.Hostname() == "" {
		return fmt.Errorf("GitHub mirror placeholder must follow its hostname")
	}
	return nil
}

func NewClient(options Options, timeout time.Duration, insecure bool) (*http.Client, error) {
	if err := Validate(options); err != nil {
		return nil, err
	}
	var proxy func(*http.Request) (*url.URL, error)
	switch options.Mode {
	case "", ModeEnvironment:
		proxy = http.ProxyFromEnvironment
	case ModeCustom, ModeCore:
		raw := options.ProxyURL
		if options.Mode == ModeCore {
			if options.CoreProxy == nil {
				return nil, fmt.Errorf("core request proxy is unavailable; start the core with an HTTP or mixed inbound")
			}
			var err error
			raw, err = options.CoreProxy()
			if err != nil {
				return nil, err
			}
		}
		parsed, err := ParseProxy(raw)
		if err != nil {
			return nil, err
		}
		proxy = http.ProxyURL(parsed)
	}
	transport := &http.Transport{
		Proxy: proxy, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: insecure}, // Explicit per-subscription setting.
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 90 * time.Second,
	}
	return &http.Client{Transport: privateTransport{transport}, Timeout: timeout, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if !isGitHubAPI(request.URL) {
			// Go permits sensitive headers to reach subdomains on redirects.
			// A GitHub API token is restricted to the exact API origin instead.
			if len(via) > 0 && isGitHubAPI(via[0].URL) {
				request.Header.Del("Authorization")
			}
		}
		return nil
	}}, nil
}

// Proxy URLs may carry passwords. Preserve cancellation/error identity for the
// caller, but never stringify a transport's nested proxy URL in logs or the UI.
type requestError struct{ cause error }

func (err requestError) Error() string {
	if errors.Is(err.cause, context.DeadlineExceeded) {
		return "backend connection timed out"
	}
	if errors.Is(err.cause, context.Canceled) {
		return "backend request canceled"
	}
	return "backend connection failed; check the proxy, DNS and network settings"
}
func (err requestError) Unwrap() error { return err.cause }

type privateTransport struct{ *http.Transport }

func (transport privateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.Transport.RoundTrip(request)
	if err != nil {
		return nil, requestError{cause: err}
	}
	return response, nil
}

func isGitHubAPI(target *url.URL) bool {
	return target.Scheme == "https" && strings.EqualFold(target.Hostname(), "api.github.com") && (target.Port() == "" || target.Port() == "443")
}

// PrepareRequest mirrors release/archive downloads only, never GitHub API calls.
// Tokens are only attached to the exact HTTPS API origin, never to a mirror.
func PrepareRequest(request *http.Request, options Options, allowMirror bool) error {
	if err := ValidateMirror(options.GitHubMirror); err != nil {
		return err
	}
	if request.Header.Get("User-Agent") == "" && options.UserAgent != "" {
		request.Header.Set("User-Agent", options.UserAgent)
	}
	mirrored := false
	if allowMirror && !options.GitHubMirrorDisabled && options.GitHubMirror != "" && request.URL.Scheme == "https" && request.URL.Host == "github.com" && (strings.Contains(request.URL.Path, "/releases/download/") || strings.Contains(request.URL.Path, "/archive/")) {
		original := request.URL.String()
		if query := strings.IndexByte(options.GitHubMirror, '?'); query >= 0 && strings.Index(options.GitHubMirror, "{url}") > query {
			original = url.QueryEscape(original)
		}
		target, err := url.Parse(strings.Replace(options.GitHubMirror, "{url}", original, 1))
		if err != nil {
			return fmt.Errorf("invalid mirrored download URL")
		}
		request.URL = target
		request.Host = ""
		request.Header.Del("Authorization")
		request.Header.Del("Cookie")
		mirrored = true
	}
	if !mirrored && isGitHubAPI(request.URL) && strings.TrimSpace(options.GitHubToken) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(options.GitHubToken))
	}
	return nil
}
