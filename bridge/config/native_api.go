package config

// EnforceNativeAPIConfig reserves the managed service after user mixins and
// scripts. Each generated configuration has its own credential; a preview can
// never retrieve the credential of an already running core.
func EnforceNativeAPIConfig(root map[string]any) error {
	secret, err := generateCoreAPISecret()
	if err != nil {
		return err
	}
	experimental := ensureChildMap(root, "experimental")
	delete(experimental, "clash_api")
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
		"type": "api", "tag": CoreAPIServiceTag, "listen": "127.0.0.1",
		"listen_port": 20123, "secret": secret, "dashboard": false,
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
