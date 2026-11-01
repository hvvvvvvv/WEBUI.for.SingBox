package config

import (
	"net"
	"strconv"
	"strings"
)

// CoreAPIController is fixed for production and may be replaced before starting
// an isolated integration test. It must not change while a core is running.
var CoreAPIController = "127.0.0.1:20123"

// EnforceNativeAPIConfig reserves the managed service after user mixins and
// scripts. Each generated configuration has its own credential; a preview can
// never retrieve the credential of an already running core.
func EnforceNativeAPIConfig(root map[string]any) error {
	host, portText, err := net.SplitHostPort(CoreAPIController)
	if err != nil {
		return invalidArgumentError{message: "invalid native API controller address"}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || !net.ParseIP(host).IsLoopback() {
		return invalidArgumentError{message: "native API controller must use a loopback IP and a valid port"}
	}
	secret, err := generateCoreAPISecret()
	if err != nil {
		return err
	}
	experimental := ensureChildMap(root, "experimental")
	// Mode selection is shared with the native API. Keep this one setting
	// without allowing mixins/scripts to reopen a Clash controller or dashboard.
	var defaultMode string
	if legacy, ok := experimental["clash_api"].(map[string]any); ok {
		if value, exists := legacy["default_mode"]; exists {
			text, ok := value.(string)
			if !ok {
				return invalidArgumentError{message: "default mode must be global, rule or direct"}
			}
			defaultMode = strings.ToLower(strings.TrimSpace(text))
			if defaultMode != "" && defaultMode != "global" && defaultMode != "rule" && defaultMode != "direct" {
				return invalidArgumentError{message: "default mode must be global, rule or direct"}
			}
		}
	}
	delete(experimental, "clash_api")
	if defaultMode != "" {
		experimental["clash_api"] = map[string]any{"default_mode": defaultMode}
	}
	var services []any
	if value, exists := root["services"]; exists && value != nil {
		var ok bool
		services, ok = value.([]any)
		if !ok {
			return invalidArgumentError{message: "services must be an array"}
		}
	}
	managed := make([]any, 0, len(services)+1)
	for _, value := range services {
		if service, ok := value.(map[string]any); ok && service["tag"] == CoreAPIServiceTag {
			continue
		}
		managed = append(managed, value)
	}
	managed = append(managed, map[string]any{
		"type": "api", "tag": CoreAPIServiceTag, "listen": host,
		"listen_port": port, "secret": secret, "dashboard": false,
	})
	root["services"] = managed
	return nil
}

func NativeAPISecret(root map[string]any) string {
	services, _ := root["services"].([]any)
	for _, value := range services {
		service, ok := value.(map[string]any)
		if !ok || service["tag"] != CoreAPIServiceTag || service["type"] != "api" {
			continue
		}
		secret, _ := service["secret"].(string)
		return secret
	}
	return ""
}

// RedactGeneratedConfig returns a copy, preserving the credential in the file
// that will be consumed by the core.
func RedactGeneratedConfig(root map[string]any) map[string]any {
	copy := make(map[string]any, len(root))
	for key, value := range root {
		copy[key] = value
	}
	services, _ := root["services"].([]any)
	redacted := make([]any, len(services))
	for index, value := range services {
		service, ok := value.(map[string]any)
		if !ok || service["tag"] != CoreAPIServiceTag {
			redacted[index] = value
			continue
		}
		item := make(map[string]any, len(service))
		for key, field := range service {
			item[key] = field
		}
		item["secret"] = "<redacted>"
		redacted[index] = item
	}
	copy["services"] = redacted
	return copy
}
