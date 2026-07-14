package parser

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The input layer follows the normalized object shape used by Sub-Store and
// Clash. Keep helpers here deliberately format-agnostic so URI and application
// line parsers apply the same coercion rules.

func cleanInput(raw string) string {
	raw = strings.TrimPrefix(raw, "\xef\xbb\xbf")
	raw = strings.TrimPrefix(raw, "\ufeff")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	return strings.TrimSpace(raw)
}

func unwrapSubscriptionBase64(raw string) string {
	current := cleanInput(raw)
	for range 2 {
		if looksLikeSubscription(current) {
			break
		}
		decoded, ok := decodeBase64Text(current)
		if !ok || !looksLikeSubscription(decoded) {
			break
		}
		current = cleanInput(decoded)
	}
	return current
}

func looksLikeSubscription(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	for _, prefix := range []string{
		"ss://", "ssr://", "vmess://", "vless://", "trojan://",
		"hysteria://", "hy://", "hysteria2://", "hy2://", "tuic://",
		"wireguard://", "wg://", "anytls://", "socks://", "socks5://",
		"socks5+tls://", "http://", "https://",
	} {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	trimmed := strings.TrimSpace(lower)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") ||
		hasTopLevelYAMLKey(value, "proxies") {
		return true
	}
	for _, prefix := range []string{
		"shadowsocks=", "shadowsocksr=", "vmess=", "vless=", "trojan=",
		"socks5=", "http=", "anytls=",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	// Loon and Surge use "name = type, server, port, ...".
	if strings.Contains(trimmed, "=") && strings.Contains(trimmed, ",") {
		return true
	}
	return false
}

func hasTopLevelYAMLKey(value, wanted string) bool {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		// Top-level YAML keys cannot be indented. Restricting the match avoids
		// mistaking a nested provider's `proxies` property for a Clash document.
		if len(line) != len(strings.TrimLeft(line, " \t")) {
			continue
		}
		key, _, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), wanted) {
			return true
		}
	}
	return false
}

func decodeBase64Text(value string) (string, bool) {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
	if len(compact) < 8 {
		return "", false
	}
	decoded, ok := decodeBase64Bytes(compact)
	if !ok || len(decoded) == 0 || !utf8.Valid(decoded) {
		return "", false
	}
	printable := 0
	for _, r := range string(decoded) {
		if r == '\n' || r == '\r' || r == '\t' || !unicode.IsControl(r) {
			printable++
		}
	}
	if printable*100 < utf8.RuneCount(decoded)*95 {
		return "", false
	}
	return string(decoded), true
}

func decodeBase64String(value string) (string, bool) {
	decoded, ok := decodeBase64Bytes(strings.TrimSpace(value))
	if !ok || !utf8.Valid(decoded) {
		return "", false
	}
	return string(decoded), true
}

func decodeBase64Bytes(value string) ([]byte, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, true
		}
	}
	return nil, false
}

func decodeURLComponent(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

// splitTopLevel splits on separators only outside quotes and nested [], {},
// and (). It is intentionally tolerant: an unterminated quote/bracket keeps
// the remaining text in one token and lets the protocol parser reject it.
func splitTopLevel(value string, separator rune) []string {
	var parts []string
	start := 0
	quote := rune(0)
	escaped := false
	depthRound, depthSquare, depthCurly := 0, 0, 0
	for index, current := range value {
		if escaped {
			escaped = false
			continue
		}
		if current == '\\' && quote != 0 {
			escaped = true
			continue
		}
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		switch current {
		case '\'', '"':
			quote = current
		case '(':
			depthRound++
		case ')':
			if depthRound > 0 {
				depthRound--
			}
		case '[':
			depthSquare++
		case ']':
			if depthSquare > 0 {
				depthSquare--
			}
		case '{':
			depthCurly++
		case '}':
			if depthCurly > 0 {
				depthCurly--
			}
		default:
			if current == separator && depthRound == 0 && depthSquare == 0 && depthCurly == 0 {
				parts = append(parts, strings.TrimSpace(value[start:index]))
				start = index + len(string(current))
			}
		}
	}
	parts = append(parts, strings.TrimSpace(value[start:]))
	return parts
}

func splitFirstTopLevel(value string, separators ...rune) (string, string, bool) {
	quote := rune(0)
	escaped := false
	depthRound, depthSquare, depthCurly := 0, 0, 0
	for index, current := range value {
		if escaped {
			escaped = false
			continue
		}
		if current == '\\' && quote != 0 {
			escaped = true
			continue
		}
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		switch current {
		case '\'', '"':
			quote = current
		case '(':
			depthRound++
		case ')':
			if depthRound > 0 {
				depthRound--
			}
		case '[':
			depthSquare++
		case ']':
			if depthSquare > 0 {
				depthSquare--
			}
		case '{':
			depthCurly++
		case '}':
			if depthCurly > 0 {
				depthCurly--
			}
		default:
			if depthRound != 0 || depthSquare != 0 || depthCurly != 0 {
				continue
			}
			for _, separator := range separators {
				if current == separator {
					return strings.TrimSpace(value[:index]), strings.TrimSpace(value[index+len(string(current)):]), true
				}
			}
		}
	}
	return "", "", false
}

func unquoteValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
		(value[0] == '\'' && value[len(value)-1] == '\'')) {
		quote := value[0]
		body := value[1 : len(value)-1]
		if quote == '"' {
			if decoded, err := strconv.Unquote(value); err == nil {
				return decoded
			}
		}
		body = strings.ReplaceAll(body, `\`+string(quote), string(quote))
		body = strings.ReplaceAll(body, `\\`, `\`)
		return body
	}
	return value
}

func parseScalar(value string) any {
	raw := strings.TrimSpace(value)
	if len(raw) >= 2 && ((raw[0] == '"' && raw[len(raw)-1] == '"') ||
		(raw[0] == '\'' && raw[len(raw)-1] == '\'')) {
		return unquoteValue(raw)
	}
	value = unquoteValue(raw)
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "on":
		return true
	case "false", "no", "off":
		return false
	case "null", "nil", "~":
		return nil
	}
	if integer, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return integer
	}
	return value
}

func boolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case int:
		return typed != 0, true
	case int64:
		return typed != 0, true
	case float64:
		return typed != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "on", "1", "enable", "enabled":
			return true, true
		case "false", "no", "off", "0", "disable", "disabled":
			return false, true
		}
	}
	return false, false
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case uint64:
		return int(typed), true
	case float64:
		return int(typed), typed == float64(int(typed))
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func parseHostPort(value string, defaultPort int) (string, int, bool) {
	value = strings.TrimSpace(unquoteValue(value))
	if value == "" {
		return "", 0, false
	}
	if host, portText, err := net.SplitHostPort(value); err == nil {
		port, err := strconv.Atoi(portText)
		return strings.Trim(host, "[]"), port, err == nil && validPort(port)
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") && defaultPort > 0 {
		return strings.Trim(value, "[]"), defaultPort, true
	}
	lastColon := strings.LastIndexByte(value, ':')
	if lastColon >= 0 && strings.Count(value, ":") == 1 {
		port, err := strconv.Atoi(value[lastColon+1:])
		if err == nil && validPort(port) {
			return value[:lastColon], port, true
		}
	}
	if defaultPort > 0 {
		return strings.Trim(value, "[]"), defaultPort, true
	}
	return "", 0, false
}

func validPort(port int) bool { return port > 0 && port <= 65535 }

func defaultName(typ, server string, port int) string {
	return strings.ToUpper(typ) + " " + net.JoinHostPort(server, strconv.Itoa(port))
}

func normalizeType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "shadowsocks", "ss-local":
		return "ss"
	case "shadowsocksr":
		return "ssr"
	case "socks", "socks5-tls", "socks5+tls":
		return "socks5"
	case "hy", "hysteria1":
		return "hysteria"
	case "hy2", "hysteria-2", "hysteria 2":
		return "hysteria2"
	case "wg":
		return "wireguard"
	case "http2", "http/2", "http2-connect", "http/2-connect", "http2 connect", "http/2 connect":
		return "http"
	case "any-tls":
		return "anytls"
	case "trusttunnel":
		return "trust-tunnel"
	case "external proxy program":
		return "external"
	}
	return normalized
}

func normalizeOptionKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func setIfNotEmpty(node Node, key string, value any) {
	switch typed := value.(type) {
	case nil:
		return
	case string:
		if strings.TrimSpace(typed) == "" {
			return
		}
	}
	node[key] = value
}

func parseStringList(value string) []string {
	value = strings.TrimSpace(unquoteValue(value))
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return nil
	}
	parts := splitTopLevel(value, ',')
	if len(parts) == 1 && strings.Contains(value, ";") {
		parts = strings.Split(value, ";")
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(unquoteValue(part)); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func parseReserved(value string) []int {
	parts := parseStringList(value)
	if len(parts) == 1 && strings.Contains(parts[0], "-") {
		parts = strings.Split(parts[0], "-")
	}
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		if parsed, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && parsed >= 0 && parsed <= 255 {
			result = append(result, parsed)
		}
	}
	return result
}

func optionMap(tokens []string) (map[string]any, []string) {
	options := make(map[string]any)
	positionals := make([]string, 0, len(tokens))
	for _, token := range tokens {
		key, value, ok := splitFirstTopLevel(token, '=')
		if !ok {
			colonKey, colonValue, colonOK := splitFirstTopLevel(token, ':')
			if colonOK && isOptionKey(colonKey) {
				key, value, ok = colonKey, colonValue, true
			}
		}
		if !ok {
			positionals = append(positionals, unquoteValue(token))
			continue
		}
		options[normalizeOptionKey(key)] = parseScalar(value)
	}
	return options, positionals
}

func isOptionKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for index, current := range value {
		if (current >= 'a' && current <= 'z') || (current >= 'A' && current <= 'Z') ||
			current == '_' || current == '-' || (index > 0 && current >= '0' && current <= '9') {
			continue
		}
		return false
	}
	return true
}

func firstOption(options map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := options[normalizeOptionKey(key)]; ok && stringValue(value) != "" {
			return value
		}
	}
	return nil
}

func applyCommonOptions(node Node, options map[string]any) {
	if value := firstOption(options, "fingerprint"); value != nil && stringValue(value) != "" {
		node["_unsupported_certificate_fingerprint"] = true
	}
	copyString := map[string][]string{
		"username":           {"username", "user"},
		"password":           {"password", "pass", "passwd"},
		"uuid":               {"uuid", "id"},
		"cipher":             {"cipher", "method", "encrypt-method", "encryption"},
		"sni":                {"sni", "servername", "server-name", "tls-host", "peer"},
		"client-fingerprint": {"client-fingerprint", "fp"},
		"flow":               {"flow"},
	}
	for target, aliases := range copyString {
		setIfNotEmpty(node, target, stringValue(firstOption(options, aliases...)))
	}
	copyBool := map[string][]string{
		"udp":              {"udp", "udp-relay"},
		"tfo":              {"tfo", "fast-open", "tcp-fast-open"},
		"skip-cert-verify": {"skip-cert-verify", "allow-insecure", "insecure"},
		"tls":              {"tls", "over-tls"},
	}
	for target, aliases := range copyBool {
		if value := firstOption(options, aliases...); value != nil {
			if parsed, ok := boolValue(value); ok {
				node[target] = parsed
			}
		}
	}
	if value := firstOption(options, "tls-verification", "verify-cert"); value != nil {
		if parsed, ok := boolValue(value); ok {
			node["skip-cert-verify"] = !parsed
		}
	}
	if value := firstOption(options, "alpn"); value != nil {
		node["alpn"] = parseStringList(stringValue(value))
	}
}

func applyTransportOptions(node Node, options map[string]any) {
	network := strings.ToLower(stringValue(firstOption(options, "network", "net", "transport", "obfs")))
	if network == "" {
		candidate := strings.ToLower(stringValue(firstOption(options, "type")))
		if candidate == "ws" || candidate == "websocket" || candidate == "grpc" || candidate == "http" ||
			candidate == "h2" || candidate == "httpupgrade" || candidate == "http-upgrade" || candidate == "quic" || candidate == "kcp" {
			network = candidate
		}
	}
	wss := network == "wss"
	if network == "websocket" || network == "wss" {
		network = "ws"
	}
	if network == "http-upgrade" {
		network = "httpupgrade"
	}
	if network == "http2" {
		network = "h2"
	}
	if network == "none" || network == "tcp" {
		network = ""
	}
	wsEnabled, _ := boolValue(firstOption(options, "ws", "websocket"))
	if wsEnabled {
		network = "ws"
	}
	if wss {
		node["tls"] = true
	}
	if network == "" {
		return
	}
	node["network"] = network
	path := stringValue(firstOption(options, "path", "ws-path", "obfs-path", "obfs-uri", "uri"))
	host := stringValue(firstOption(options, "host", "ws-host", "obfs-host"))
	if host == "" {
		for key, value := range parseHeaders(stringValue(firstOption(options, "obfs-header"))) {
			if strings.EqualFold(key, "Host") {
				host = stringValue(value)
				break
			}
		}
	}
	switch network {
	case "ws":
		opts := map[string]any{}
		setMapIfNotEmpty(opts, "path", path)
		if host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		if headers := firstOption(options, "ws-headers", "headers"); headers != nil {
			for key, value := range parseHeaders(stringValue(headers)) {
				headerMap, _ := opts["headers"].(map[string]any)
				if headerMap == nil {
					headerMap = map[string]any{}
					opts["headers"] = headerMap
				}
				headerMap[key] = value
			}
		}
		node["ws-opts"] = opts
	case "http", "h2":
		opts := map[string]any{}
		setMapIfNotEmpty(opts, "path", path)
		if host != "" {
			opts["host"] = parseStringList(host)
		}
		if network == "h2" {
			node["h2-opts"] = opts
		} else {
			node["http-opts"] = opts
		}
	case "grpc":
		opts := map[string]any{}
		setMapIfNotEmpty(opts, "grpc-service-name", stringValue(firstOption(options, "grpc-service-name", "service-name", "serviceName", "path")))
		node["grpc-opts"] = opts
	case "httpupgrade":
		opts := map[string]any{}
		setMapIfNotEmpty(opts, "path", path)
		setMapIfNotEmpty(opts, "host", host)
		if headers := firstOption(options, "headers"); headers != nil {
			opts["headers"] = parseHeaders(stringValue(headers))
		}
		node["httpupgrade-opts"] = opts
	}
}

func setMapIfNotEmpty(target map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		target[key] = value
	}
}

func parseHeaders(value string) map[string]any {
	value = unquoteValue(value)
	result := map[string]any{}
	parts := splitTopLevel(value, ',')
	if len(parts) == 1 && strings.Contains(value, "|") {
		parts = strings.Split(value, "|")
	}
	for _, part := range parts {
		key, item, ok := splitFirstTopLevel(part, ':', '=')
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		result[strings.TrimSpace(unquoteValue(key))] = strings.TrimSpace(unquoteValue(item))
	}
	return result
}
