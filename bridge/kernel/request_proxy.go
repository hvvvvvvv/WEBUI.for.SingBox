package kernel

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	kernelv1 "guiforcores/gen/kernel/v1"
)

// CoreRequestProxy reads a snapshot captured only after the managed core starts.
// Preview generation can replace config.json without changing this running
// instance; reading that file or the editable profile would select wrong ports.
func (s *Service) CoreRequestProxy() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || s.nativeAPI == nil || s.nativeAPI.ctx.Err() != nil || s.nativeAPI.pid != s.corePID {
		return "", fmt.Errorf("core proxy requires a running core")
	}
	if s.requestProxyURL == "" {
		return "", fmt.Errorf("running core has no usable HTTP/mixed proxy: require the managed configuration, a fixed listen port, no inbound TLS, and the backend network namespace")
	}
	return s.requestProxyURL, nil
}

func usesManagedProxyConfig(args []string, expected string) bool {
	count := 0
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "-C" || strings.HasPrefix(arg, "--config-directory") || (strings.HasPrefix(arg, "-C") && len(arg) > 2) {
			return false
		}
		path := ""
		if arg == "-c" || arg == "--config" {
			index++
			if index >= len(args) {
				return false
			}
			path = args[index]
		} else if strings.HasPrefix(arg, "--config=") {
			path = strings.TrimPrefix(arg, "--config=")
		} else if strings.HasPrefix(arg, "-c") && len(arg) > 2 {
			return false
		}
		if path == "" {
			continue
		}
		count++
		if !filepath.IsAbs(path) {
			return false
		}
		actual, want := filepath.Clean(path), filepath.Clean(expected)
		if runtime.GOOS == "windows" {
			actual, want = strings.ToLower(actual), strings.ToLower(want)
		}
		if actual != want {
			return false
		}
	}
	return count == 1
}

func requestProxyFromConfig(root map[string]any) string {
	inbounds, _ := root["inbounds"].([]any)
	for _, value := range inbounds {
		inbound, ok := value.(map[string]any)
		if !ok || (inbound["type"] != "http" && inbound["type"] != "mixed") {
			continue
		}
		if ns, _ := inbound["netns"].(string); ns != "" {
			continue
		}
		if tls, _ := inbound["tls"].(map[string]any); tls["enabled"] == true {
			continue
		}
		port, err := strconv.Atoi(fmt.Sprint(inbound["listen_port"]))
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		host, _ := inbound["listen"].(string)
		if host == "" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		if host == "::" {
			host = "::1"
		}
		if net.ParseIP(host) == nil {
			continue
		}
		proxy := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port))}
		if users, ok := inbound["users"].([]any); ok && len(users) > 0 {
			user, ok := users[0].(map[string]any)
			if !ok {
				continue
			}
			username, _ := user["username"].(string)
			password, _ := user["password"].(string)
			proxy.User = url.UserPassword(username, password)
		}
		return proxy.String()
	}
	return ""
}
