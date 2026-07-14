package parser

import (
	"errors"
	"fmt"
	"strings"
)

// produceNode converts the normalized Sub-Store/Clash shaped node into one
// sing-box outbound.  It deliberately rejects source protocols and plugin
// combinations that cannot be represented by a single valid outbound.
func produceNode(node Node) (map[string]any, error) {
	if node == nil {
		return nil, errors.New("invalid empty proxy")
	}
	rawType := strings.ToLower(nodeString(node, "type"))
	typ := producerType(rawType)
	if typ == "" {
		return nil, errors.New("unsupported proxy protocol")
	}
	tag := nodeString(node, "name", "tag", "remark", "remarks")
	if tag == "" {
		return nil, errors.New("missing required proxy name")
	}

	out := map[string]any{"type": typ, "tag": tag}
	if typ == "direct" {
		applyDialFields(node, out)
		return out, nil
	}
	if typ == "block" {
		return out, nil
	}

	server := nodeString(node, "server", "address", "host")
	port, ok := nodeInt(node, "port", "server-port", "server_port")
	if server == "" {
		return nil, errors.New("missing required server")
	}
	if !ok || !validPort(port) {
		return nil, errors.New("invalid or missing server port")
	}
	out["server"] = strings.Trim(server, "[]")
	out["server_port"] = port

	forceTLS := false
	supportsTLS := false
	supportsTransport := false
	supportsMultiplex := false
	supportsNetwork := false

	switch typ {
	case "http":
		copyOptionalString(node, out, "username", "username", "user")
		copyOptionalString(node, out, "password", "password", "pass")
		copyOptionalString(node, out, "path", "path")
		if headers := stringMap(nodeValue(node, "headers", "http-headers")); len(headers) > 0 {
			out["headers"] = headers
		}
		forceTLS = rawType == "https" || rawType == "http2" || rawType == "http/2"
		supportsTLS = true
	case "socks":
		if tlsRequested(node) {
			return nil, errors.New("unsupported SOCKS TLS transport")
		}
		version := "5"
		switch rawType {
		case "socks4":
			version = "4"
		case "socks4a":
			version = "4a"
		}
		out["version"] = version
		copyOptionalString(node, out, "username", "username", "user")
		copyOptionalString(node, out, "password", "password", "pass")
		applyUDPOverTCP(node, out)
		supportsNetwork = true
	case "shadowsocks":
		rawMethod := nodeString(node, "cipher", "method", "encryption")
		password := nodeString(node, "password", "pass")
		if rawMethod == "" || password == "" {
			return nil, errors.New("missing required Shadowsocks credentials")
		}
		method, methodOK := normalizeShadowsocksMethod(rawMethod)
		if !methodOK {
			return nil, errors.New("unsupported Shadowsocks method")
		}
		out["method"] = method
		out["password"] = password
		if err := applyShadowsocksPlugin(node, out); err != nil {
			return nil, err
		}
		applyUDPOverTCP(node, out)
		supportsNetwork = true
		supportsMultiplex = true
	case "vmess":
		uuid := nodeString(node, "uuid", "id", "username")
		if uuid == "" {
			return nil, errors.New("missing required VMess user id")
		}
		out["uuid"] = uuid
		security := nodeString(node, "cipher", "security")
		if security == "" {
			security = "auto"
		}
		out["security"] = security
		if alterID, exists := nodeInt(node, "alterId", "alter-id", "alter_id", "aid"); exists {
			out["alter_id"] = alterID
		}
		copyOptionalBool(node, out, "global_padding", "global-padding", "global_padding")
		copyOptionalBool(node, out, "authenticated_length", "authenticated-length", "authenticated_length")
		copyOptionalString(node, out, "packet_encoding", "packet-encoding", "packet_encoding")
		supportsTLS, supportsTransport, supportsMultiplex, supportsNetwork = true, true, true, true
	case "vless":
		uuid := nodeString(node, "uuid", "id", "username")
		if uuid == "" {
			return nil, errors.New("missing required VLESS user id")
		}
		out["uuid"] = uuid
		copyOptionalString(node, out, "flow", "flow")
		copyOptionalString(node, out, "packet_encoding", "packet-encoding", "packet_encoding")
		supportsTLS, supportsTransport, supportsMultiplex, supportsNetwork = true, true, true, true
	case "trojan":
		password := nodeString(node, "password", "pass")
		if password == "" {
			return nil, errors.New("missing required Trojan credentials")
		}
		out["password"] = password
		forceTLS = !tlsExplicitlyDisabled(node)
		supportsTLS, supportsTransport, supportsMultiplex, supportsNetwork = true, true, true, true
	case "hysteria":
		applyServerPorts(node, out)
		applyBandwidth(node, out)
		if !hasBandwidthPair(out) {
			return nil, errors.New("missing required Hysteria bandwidth")
		}
		copyOptionalString(node, out, "obfs", "obfs")
		if auth := nodeString(node, "auth-str", "auth_str", "auth", "password"); auth != "" {
			out["auth_str"] = auth
		}
		if err := copyDurationSeconds(node, out, "hop_interval", "hop-interval", "hop_interval"); err != nil {
			return nil, err
		}
		forceTLS, supportsTLS, supportsNetwork = true, true, true
	case "hysteria2":
		password := nodeString(node, "password", "auth", "auth-str")
		if password == "" {
			return nil, errors.New("missing required Hysteria2 credentials")
		}
		out["password"] = password
		applyServerPorts(node, out)
		if err := applyHysteria2Bandwidth(node, out); err != nil {
			return nil, err
		}
		if err := applyHysteria2Obfs(node, out); err != nil {
			return nil, err
		}
		if err := copyDurationSeconds(node, out, "hop_interval", "hop-interval", "hop_interval"); err != nil {
			return nil, err
		}
		// hop_interval_max and Gecko obfuscation were added in sing-box 1.14.
		// This application currently targets the stable 1.13 schema, so the
		// optional randomization upper bound is intentionally not emitted.
		forceTLS, supportsTLS, supportsNetwork = true, true, true
	case "tuic":
		uuid := nodeString(node, "uuid", "token", "username")
		password := nodeString(node, "password", "pass")
		if uuid == "" || password == "" {
			return nil, errors.New("missing required TUIC credentials")
		}
		out["uuid"], out["password"] = uuid, password
		copyOptionalString(node, out, "congestion_control", "congestion-controller", "congestion-control", "congestion_control")
		udpRelayMode := strings.ToLower(nodeString(node, "udp-relay-mode", "udp_relay_mode"))
		if udpRelayMode != "" && udpRelayMode != "native" && udpRelayMode != "quic" {
			return nil, errors.New("unsupported TUIC UDP relay mode")
		}
		udpOverStream, hasUDPOverStream := nodeBool(node, "udp-over-stream", "udp_over_stream")
		if udpRelayMode != "" && hasUDPOverStream && udpOverStream {
			return nil, errors.New("conflicting TUIC UDP relay options")
		}
		if udpRelayMode != "" {
			out["udp_relay_mode"] = udpRelayMode
		} else if hasUDPOverStream {
			out["udp_over_stream"] = udpOverStream
		}
		copyOptionalBool(node, out, "zero_rtt_handshake", "reduce-rtt", "zero-rtt", "zero_rtt_handshake")
		copyOptionalString(node, out, "heartbeat", "heartbeat")
		forceTLS, supportsTLS, supportsNetwork = true, true, true
	case "ssh":
		user := nodeString(node, "username", "user")
		password := nodeString(node, "password")
		privateKey := nodeExactString(node, "private-key-content", "private_key_content", "privateKeyContent")
		privateKeyPath := nodeExactString(node, "private-key-path", "private_key_path", "privateKeyPath")
		if privateKey != "" && privateKeyPath != "" {
			return nil, errors.New("conflicting SSH private key options")
		}
		if privateKey == "" && privateKeyPath == "" {
			genericPrivateKey := nodeExactString(node, "private-key", "private_key", "privateKey")
			if looksLikePrivateKeyContent(genericPrivateKey) {
				privateKey = genericPrivateKey
			} else {
				privateKeyPath = genericPrivateKey
			}
		}
		if user == "" || (password == "" && privateKey == "" && privateKeyPath == "") {
			return nil, errors.New("missing required SSH credentials")
		}
		out["user"] = user
		setString(out, "password", password)
		setString(out, "private_key", privateKey)
		setString(out, "private_key_path", privateKeyPath)
		copyOptionalString(node, out, "private_key_passphrase", "private-key-passphrase", "private_key_passphrase")
		if hostKeys := nodeStrings(node, "host-key", "host_key", "server-fingerprint"); len(hostKeys) > 0 {
			out["host_key"] = hostKeys
		}
	case "anytls":
		password := nodeString(node, "password", "pass")
		if password == "" {
			return nil, errors.New("missing required AnyTLS credentials")
		}
		out["password"] = password
		if err := copyDurationSeconds(node, out, "idle_session_check_interval", "idle-session-check-interval", "idle_session_check_interval"); err != nil {
			return nil, err
		}
		if err := copyDurationSeconds(node, out, "idle_session_timeout", "idle-session-timeout", "idle_session_timeout"); err != nil {
			return nil, err
		}
		copyOptionalInt(node, out, "min_idle_session", "min-idle-session", "min_idle_session")
		forceTLS, supportsTLS = true, true
	case "naive":
		copyOptionalString(node, out, "username", "username", "user")
		copyOptionalString(node, out, "password", "password", "pass")
		insecureConcurrency := 0
		if value, ok := nodeInt(node, "insecure-concurrency", "insecure_concurrency"); ok {
			if value < 0 {
				return nil, errors.New("invalid Naive insecure concurrency")
			}
			insecureConcurrency = value
			if value > 0 {
				out["insecure_concurrency"] = value
			}
		}
		if headers := stringMap(nodeValue(node, "extra-headers", "extra_headers", "headers")); len(headers) > 0 {
			out["extra_headers"] = headers
		}
		applyUDPOverTCP(node, out)
		if quic, ok := nodeBool(node, "quic"); ok {
			if quic && insecureConcurrency > 0 {
				return nil, errors.New("conflicting Naive QUIC and insecure concurrency options")
			}
			out["quic"] = quic
		}
		copyOptionalString(node, out, "quic_congestion_control", "quic-congestion-control", "quic_congestion_control")
		forceTLS, supportsTLS = true, true
	default:
		return nil, errors.New("unsupported proxy protocol")
	}

	if supportsTransport {
		transport, needsTLS, err := v2rayTransport(node)
		if err != nil {
			return nil, err
		}
		if transport != nil {
			out["transport"] = transport
		}
		forceTLS = forceTLS || needsTLS
	}
	if supportsTLS {
		if err := validateTLSOptions(node); err != nil {
			return nil, err
		}
		tls, err := outboundTLS(node, forceTLS, typ)
		if err != nil {
			return nil, err
		}
		if tls != nil {
			out["tls"] = tls
		}
	}
	if supportsNetwork {
		applyNetwork(node, out)
	}
	if supportsMultiplex {
		if err := applyMultiplex(node, out); err != nil {
			return nil, err
		}
	}
	if typ == "shadowsocks" {
		_, hasUDPOverTCP := out["udp_over_tcp"]
		_, hasMultiplex := out["multiplex"]
		if hasUDPOverTCP && hasMultiplex {
			return nil, errors.New("conflicting Shadowsocks UDP-over-TCP and multiplex options")
		}
	}
	applyDialFields(node, out)
	return out, nil
}

func producerType(value string) string {
	switch canonicalKey(value) {
	case "direct":
		return "direct"
	case "reject", "block":
		return "block"
	case "http", "https", "http2", "http/2":
		return "http"
	case "socks", "socks4", "socks4a", "socks5", "socks5tls":
		return "socks"
	case "ss", "shadowsocks":
		return "shadowsocks"
	case "vmess":
		return "vmess"
	case "vless":
		return "vless"
	case "trojan":
		return "trojan"
	case "hysteria", "hy":
		return "hysteria"
	case "hysteria2", "hy2":
		return "hysteria2"
	case "tuic":
		return "tuic"
	case "ssh":
		return "ssh"
	case "anytls":
		return "anytls"
	case "naive", "naiveproxy":
		return "naive"
	case "wireguard", "wg":
		// sing-box 1.13 removed the WireGuard outbound in favor of a top-level
		// endpoint. Subscription entries are injected into `outbounds`, so an
		// endpoint cannot be represented safely by this fallback pipeline.
		return ""
	case "ssr", "shadowsocksr", "snell", "external", "openvpn", "trusttunnel", "trust-tunnel":
		// Snell outbounds are only available in sing-box 1.14 and cannot be
		// represented by the stable 1.13 schema targeted by this application.
		return ""
	default:
		return ""
	}
}

func normalizeShadowsocksMethod(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305",
		"none", "aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305",
		"aes-128-ctr", "aes-192-ctr", "aes-256-ctr", "aes-128-cfb", "aes-192-cfb", "aes-256-cfb",
		"rc4-md5", "chacha20-ietf", "xchacha20":
		return value, true
	default:
		return "", false
	}
}

func nodeExactString(node Node, keys ...string) string {
	for _, wanted := range keys {
		for key, value := range node {
			if strings.EqualFold(strings.TrimSpace(key), wanted) {
				return strings.TrimSpace(stringValue(value))
			}
		}
	}
	return ""
}

func looksLikePrivateKeyContent(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, "\n") ||
		(strings.Contains(value, "-----BEGIN ") && strings.Contains(value, "PRIVATE KEY-----"))
}

func nodeValue(node Node, keys ...string) any {
	for _, wanted := range keys {
		canonicalWanted := canonicalKey(wanted)
		for key, value := range node {
			if value != nil && canonicalKey(key) == canonicalWanted {
				return value
			}
		}
	}
	return nil
}

func canonicalKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("-", "", "_", "", "+", "", " ", "").Replace(value)
}

func nodeString(node Node, keys ...string) string {
	return strings.TrimSpace(stringValue(nodeValue(node, keys...)))
}

func nodeInt(node Node, keys ...string) (int, bool) {
	return intValue(nodeValue(node, keys...))
}

func nodeBool(node Node, keys ...string) (bool, bool) {
	return boolValue(nodeValue(node, keys...))
}

func copyOptionalString(node Node, target map[string]any, outputKey string, inputKeys ...string) {
	setString(target, outputKey, nodeString(node, inputKeys...))
}

func setString(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}

func copyOptionalInt(node Node, target map[string]any, outputKey string, inputKeys ...string) {
	if value, ok := nodeInt(node, inputKeys...); ok {
		target[outputKey] = value
	}
}

func copyOptionalBool(node Node, target map[string]any, outputKey string, inputKeys ...string) {
	if value, ok := nodeBool(node, inputKeys...); ok {
		target[outputKey] = value
	}
}

func stringMap(value any) map[string]any {
	source, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]any, len(source))
	for key, item := range source {
		key = strings.TrimSpace(key)
		if key == "" || item == nil {
			continue
		}
		switch typed := item.(type) {
		case string:
			if typed = strings.TrimSpace(typed); typed != "" {
				result[key] = typed
			}
		case []string:
			values := cleanStrings(typed)
			if len(values) == 1 {
				result[key] = values[0]
			} else if len(values) > 1 {
				result[key] = values
			}
		case []any:
			values := make([]string, 0, len(typed))
			for _, entry := range typed {
				if text, ok := headerScalar(entry); ok {
					values = append(values, text)
				}
			}
			if len(values) == 1 {
				result[key] = values[0]
			} else if len(values) > 1 {
				result[key] = values
			}
		default:
			if text, ok := headerScalar(item); ok {
				result[key] = text
			}
		}
	}
	return result
}

func headerScalar(value any) (string, bool) {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		text = fmt.Sprint(typed)
	default:
		return "", false
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

func nodeStrings(node Node, keys ...string) []string {
	value := nodeValue(node, keys...)
	switch typed := value.(type) {
	case []string:
		return cleanStrings(typed)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(stringValue(item)); text != "" {
				result = append(result, text)
			}
		}
		return result
	case string:
		return cleanStrings(parseStringList(typed))
	default:
		if text := strings.TrimSpace(stringValue(value)); text != "" {
			return []string{text}
		}
	}
	return nil
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
