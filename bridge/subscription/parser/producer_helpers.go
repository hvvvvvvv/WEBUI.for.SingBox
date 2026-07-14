package parser

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

func tlsRequested(node Node) bool {
	if enabled, ok := nodeBool(node, "tls", "over-tls"); ok {
		return enabled
	}
	if options := nodeMap(node, "tls"); len(options) > 0 {
		if enabled, ok := mapBool(options, "enabled"); ok {
			return enabled
		}
		return true
	}
	switch strings.ToLower(nodeString(node, "security")) {
	case "tls", "reality":
		return true
	}
	return len(nodeMap(node, "reality-opts", "reality")) > 0
}

func tlsExplicitlyDisabled(node Node) bool {
	if value := nodeValue(node, "tls", "over-tls"); value != nil {
		if enabled, ok := boolValue(value); ok {
			return !enabled
		}
		if text, ok := value.(string); ok {
			switch strings.ToLower(strings.TrimSpace(text)) {
			case "none", "off", "disabled":
				return true
			}
		}
	}
	return strings.EqualFold(nodeString(node, "security"), "none")
}

func applyUDPOverTCP(node Node, out map[string]any) {
	value := nodeValue(node, "udp-over-tcp", "udp_over_tcp", "uot")
	if value == nil {
		return
	}
	options := map[string]any{}
	switch typed := value.(type) {
	case map[string]any:
		if enabled, ok := mapBool(typed, "enabled"); ok && !enabled {
			return
		}
		options["enabled"] = true
		if version, ok := mapInt(typed, "version"); ok && version > 0 {
			options["version"] = version
		}
	default:
		enabled, ok := boolValue(typed)
		if !ok || !enabled {
			return
		}
		options["enabled"] = true
	}
	if version, ok := nodeInt(node, "udp-over-tcp-version", "uot-version"); ok && version > 0 {
		options["version"] = version
	}
	out["udp_over_tcp"] = options
}

func applyShadowsocksPlugin(node Node, out map[string]any) error {
	plugin := strings.ToLower(nodeString(node, "plugin"))
	if plugin == "" {
		return nil
	}
	options := nodeMap(node, "plugin-opts", "plugin_opts", "plugin-options", "plugin_options")
	switch canonicalKey(plugin) {
	case "obfs", "obfslocal", "simpleobfs":
		mode := strings.ToLower(mapString(options, "mode", "obfs"))
		if mode == "websocket" {
			mode = "http"
		}
		if mode != "http" && mode != "tls" {
			return errors.New("unsupported Shadowsocks obfs mode")
		}
		parts := []string{"obfs=" + mode}
		if host := mapString(options, "host", "obfs-host"); host != "" {
			parts = append(parts, "obfs-host="+host)
		}
		out["plugin"] = "obfs-local"
		out["plugin_opts"] = strings.Join(parts, ";")
		return nil
	case "v2rayplugin":
		parts := make([]string, 0, 5)
		mode := strings.ToLower(mapString(options, "mode"))
		if mode == "" || mode == "ws" {
			mode = "websocket"
		}
		if mode != "websocket" && mode != "quic" {
			return errors.New("unsupported v2ray-plugin mode")
		}
		parts = append(parts, "mode="+mode)
		for _, pair := range []struct {
			output  string
			aliases []string
		}{
			{output: "host", aliases: []string{"host", "obfs-host"}},
			{output: "path", aliases: []string{"path"}},
		} {
			if value := mapString(options, pair.aliases...); value != "" {
				parts = append(parts, pair.output+"="+value)
			}
		}
		if tls, ok := mapBool(options, "tls"); ok && tls {
			parts = append(parts, "tls")
		}
		out["plugin"] = "v2ray-plugin"
		out["plugin_opts"] = strings.Join(parts, ";")
		return nil
	case "shadowtls", "shadowtlsplugin":
		// A Shadowsocks-over-ShadowTLS source needs two chained sing-box
		// outbounds. The fallback deliberately emits one independent outbound
		// per source proxy, so retaining only half would create a broken node.
		return errors.New("unsupported Shadowsocks over TLS plugin")
	default:
		return errors.New("unsupported Shadowsocks plugin")
	}
}

func applyServerPorts(node Node, out map[string]any) {
	ports := nodeStrings(node, "server-ports", "server_ports", "ports", "mport")
	if len(ports) == 0 {
		return
	}
	cleaned := make([]string, 0, len(ports))
	for _, portRange := range ports {
		portRange = strings.TrimSpace(portRange)
		if start, end, ok := strings.Cut(portRange, "-"); ok && !strings.Contains(end, "-") {
			startPort, startErr := strconv.Atoi(strings.TrimSpace(start))
			endPort, endErr := strconv.Atoi(strings.TrimSpace(end))
			if startErr == nil && endErr == nil && validPort(startPort) && validPort(endPort) && startPort <= endPort {
				portRange = strconv.Itoa(startPort) + ":" + strconv.Itoa(endPort)
			}
		}
		if portRange != "" {
			cleaned = append(cleaned, portRange)
		}
	}
	if len(cleaned) > 0 {
		delete(out, "server_port")
		out["server_ports"] = cleaned
	}
}

func applyBandwidth(node Node, out map[string]any) {
	applyBandwidthDirection(node, out, "up", "up-mbps", "upmbps", "upload-bandwidth", "upload-speed")
	applyBandwidthDirection(node, out, "down", "down-mbps", "downmbps", "download-bandwidth", "download-speed")
}

func applyHysteria2Bandwidth(node Node, out map[string]any) error {
	for _, direction := range []struct {
		output  string
		aliases []string
	}{
		{output: "up_mbps", aliases: []string{"up", "up-mbps", "up_mbps", "upmbps", "upload-bandwidth", "upload-speed"}},
		{output: "down_mbps", aliases: []string{"down", "down-mbps", "down_mbps", "downmbps", "download-bandwidth", "download-speed"}},
	} {
		value := nodeValue(node, direction.aliases...)
		if value == nil || strings.TrimSpace(stringValue(value)) == "" {
			continue
		}
		mbps, ok := bandwidthMbps(value)
		if !ok {
			return errors.New("unsupported Hysteria2 bandwidth value")
		}
		out[direction.output] = mbps
	}
	return nil
}

// bandwidthMbps losslessly converts the decimal units accepted by Hysteria 1
// into the integer Mbps fields used by Hysteria2 in sing-box 1.13. Values that
// would require rounding are rejected so a fallback never silently changes a
// subscription's bandwidth semantics.
func bandwidthMbps(value any) (int, bool) {
	if number, ok := intValue(value); ok {
		return number, number > 0
	}
	text := strings.TrimSpace(stringValue(value))
	if text == "" {
		return 0, false
	}
	fields := strings.Fields(text)
	if len(fields) == 1 {
		for index, current := range fields[0] {
			if (current >= '0' && current <= '9') || current == '.' || current == '+' || current == '-' {
				continue
			}
			fields = []string{fields[0][:index], fields[0][index:]}
			break
		}
	}
	if len(fields) != 2 {
		return 0, false
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || number <= 0 || math.IsInf(number, 0) || math.IsNaN(number) {
		return 0, false
	}
	factor, ok := map[string]float64{
		"bps":  1.0 / 1_000_000,
		"Bps":  8.0 / 1_000_000,
		"Kbps": 1.0 / 1_000,
		"KBps": 8.0 / 1_000,
		"Mbps": 1,
		"MBps": 8,
		"Gbps": 1_000,
		"GBps": 8_000,
		"Tbps": 1_000_000,
		"TBps": 8_000_000,
	}[fields[1]]
	if !ok {
		return 0, false
	}
	converted := number * factor
	if converted <= 0 || converted > float64(maxInt()) || converted != math.Trunc(converted) {
		return 0, false
	}
	return int(converted), true
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func copyDurationSeconds(node Node, out map[string]any, output string, aliases ...string) error {
	value := nodeValue(node, aliases...)
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(stringValue(value))
	if text == "" {
		return nil
	}
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		if number <= 0 || math.IsInf(number, 0) || math.IsNaN(number) {
			return errors.New("invalid proxy duration")
		}
		text += "s"
	}
	duration, err := time.ParseDuration(text)
	if err != nil || duration <= 0 {
		return errors.New("invalid proxy duration")
	}
	out[output] = text
	return nil
}

func applyBandwidthDirection(node Node, out map[string]any, direction string, aliases ...string) {
	keys := append([]string{direction}, aliases...)
	value := nodeValue(node, keys...)
	if value == nil {
		return
	}
	if number, ok := intValue(value); ok {
		if number > 0 {
			out[direction+"_mbps"] = number
		}
		return
	}
	text := strings.TrimSpace(stringValue(value))
	if text == "" {
		return
	}
	if number, err := strconv.Atoi(text); err == nil {
		if number > 0 {
			out[direction+"_mbps"] = number
		}
		return
	}
	out[direction] = text
}

func hasBandwidthPair(out map[string]any) bool {
	_, hasUp := out["up"]
	if !hasUp {
		_, hasUp = out["up_mbps"]
	}
	_, hasDown := out["down"]
	if !hasDown {
		_, hasDown = out["down_mbps"]
	}
	return hasUp && hasDown
}

func applyHysteria2Obfs(node Node, out map[string]any) error {
	value := nodeValue(node, "obfs")
	options := map[string]any{}
	if mapped, ok := value.(map[string]any); ok {
		options = mapped
	}
	typ := strings.ToLower(strings.TrimSpace(stringValue(value)))
	if len(options) > 0 {
		typ = strings.ToLower(mapString(options, "type", "mode"))
	}
	if typ == "" || typ == "none" {
		return nil
	}
	if typ != "salamander" {
		return errors.New("unsupported Hysteria2 obfuscation")
	}
	password := nodeString(node, "obfs-password", "obfs_password", "obfs-param")
	if password == "" {
		password = mapString(options, "password")
	}
	if password == "" {
		return errors.New("missing required Hysteria2 obfuscation password")
	}
	obfs := map[string]any{"type": typ, "password": password}
	out["obfs"] = obfs
	return nil
}

func outboundTLS(node Node, force bool, protocol string) (map[string]any, error) {
	if !force && !tlsRequested(node) {
		return nil, nil
	}
	tls := map[string]any{"enabled": true}
	options := nodeMap(node, "tls")
	if serverName := firstNonEmpty(mapString(options, "server-name", "server_name", "servername", "sni"), nodeString(node, "sni", "servername", "server-name", "peer", "tls-host")); serverName != "" {
		tls["server_name"] = serverName
	}
	if insecure, ok := firstBool(options, node, []string{"insecure", "skip-cert-verify", "allow-insecure"}); ok {
		tls["insecure"] = insecure
	}
	if disabled, ok := firstBool(options, node, []string{"disable-sni", "disable_sni"}); ok {
		tls["disable_sni"] = disabled
	}
	if alpn := firstStrings(options, node, "alpn"); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	for _, field := range []struct {
		output  string
		aliases []string
	}{
		{output: "min_version", aliases: []string{"min-version", "min_version"}},
		{output: "max_version", aliases: []string{"max-version", "max_version"}},
	} {
		if value := firstNonEmpty(mapString(options, field.aliases...), nodeString(node, field.aliases...)); value != "" {
			tls[field.output] = value
		}
	}
	if fingerprint := firstNonEmpty(mapString(options, "client-fingerprint", "fp"), nodeString(node, "client-fingerprint", "fp")); fingerprint != "" && !strings.EqualFold(fingerprint, "none") {
		normalized, ok := normalizeUTLSFingerprint(fingerprint)
		if !ok {
			return nil, errors.New("unsupported uTLS fingerprint")
		}
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": normalized}
	} else if utls := nodeMap(node, "utls"); len(utls) > 0 {
		utlsOut := map[string]any{"enabled": true}
		if enabled, ok := mapBool(utls, "enabled"); ok {
			utlsOut["enabled"] = enabled
		}
		if fingerprint := mapString(utls, "fingerprint"); fingerprint != "" {
			normalized, ok := normalizeUTLSFingerprint(fingerprint)
			if !ok {
				return nil, errors.New("unsupported uTLS fingerprint")
			}
			utlsOut["fingerprint"] = normalized
		}
		tls["utls"] = utlsOut
	}
	if reality := nodeMap(node, "reality-opts", "reality"); len(reality) > 0 {
		realityOut := map[string]any{"enabled": true}
		if publicKey := mapString(reality, "public-key", "public_key"); publicKey != "" {
			realityOut["public_key"] = publicKey
		}
		if shortID := mapString(reality, "short-id", "short_id"); shortID != "" {
			realityOut["short_id"] = shortID
		}
		if len(realityOut) > 1 {
			tls["reality"] = realityOut
		}
	}
	if ech := nodeMap(node, "ech-opts", "ech"); len(ech) > 0 {
		echOut := map[string]any{"enabled": true}
		if enabled, ok := mapBool(ech, "enabled"); ok {
			echOut["enabled"] = enabled
		}
		if config := mapStrings(ech, "config"); len(config) > 0 {
			echOut["config"] = config
		}
		if configPath := mapString(ech, "config-path", "config_path"); configPath != "" {
			echOut["config_path"] = configPath
		}
		tls["ech"] = echOut
	}
	// NaiveProxy only exposes a subset of TLS knobs. Keep the universally
	// accepted fields and ECH, dropping implementation-specific extensions.
	if protocol == "naive" {
		for _, key := range []string{"insecure", "disable_sni", "alpn", "min_version", "max_version", "utls", "reality"} {
			delete(tls, key)
		}
	}
	return tls, nil
}

func normalizeUTLSFingerprint(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "chrome", "firefox", "edge", "safari", "360", "qq", "ios", "android", "random", "randomized":
		return value, true
	default:
		return "", false
	}
}

func validateTLSOptions(node Node) error {
	if unsupported, _ := nodeBool(node, "_unsupported_certificate_fingerprint"); unsupported {
		return errors.New("unsupported certificate fingerprint pin")
	}
	if nodeString(node, "fingerprint") != "" || mapString(nodeMap(node, "tls"), "fingerprint") != "" {
		return errors.New("unsupported certificate fingerprint pin")
	}
	return validateReality(node)
}

func validateReality(node Node) error {
	reality := nodeMap(node, "reality-opts", "reality")
	requested, _ := nodeBool(node, "_reality")
	requested = requested || strings.EqualFold(nodeString(node, "security"), "reality") || len(reality) > 0
	if !requested {
		return nil
	}
	if mapString(reality, "public-key", "public_key") == "" {
		return errors.New("missing required Reality public key")
	}
	return nil
}

func v2rayTransport(node Node) (map[string]any, bool, error) {
	rawNetwork := nodeValue(node, "network", "net")
	network, _ := rawNetwork.(string)
	network = strings.ToLower(strings.TrimSpace(network))
	needsTLS := network == "wss"
	transportMap := nodeMap(node, "transport")
	if network == "" && len(transportMap) > 0 {
		network = strings.ToLower(mapString(transportMap, "type"))
	}
	switch network {
	case "", "none", "tcp", "raw":
		return nil, false, nil
	case "websocket", "wss":
		network = "ws"
	case "http2":
		network = "h2"
	case "http-upgrade":
		network = "httpupgrade"
	}
	options := transportMap
	if len(options) == 0 {
		switch network {
		case "ws":
			options = nodeMap(node, "ws-opts", "ws_opts")
		case "http", "h2":
			options = nodeMap(node, "h2-opts", "h2_opts", "http-opts", "http_opts")
		case "grpc":
			options = nodeMap(node, "grpc-opts", "grpc_opts")
		case "httpupgrade":
			options = nodeMap(node, "httpupgrade-opts", "httpupgrade_opts")
		}
	}

	switch network {
	case "ws":
		transport := map[string]any{"type": "ws"}
		copyTransportString(transport, "path", options, node, "path", "ws-path")
		headers := mapHeaders(mapValue(options, "headers"))
		if len(headers) == 0 {
			headers = mapHeaders(nodeValue(node, "headers", "ws-headers"))
		}
		if host := firstNonEmpty(mapString(options, "host"), nodeString(node, "host", "ws-host")); host != "" && !hasHeader(headers, "Host") {
			if headers == nil {
				headers = map[string]any{}
			}
			headers["Host"] = host
		}
		if len(headers) > 0 {
			transport["headers"] = headers
		}
		if value, ok := firstInt(options, node, "max-early-data", "max_early_data"); ok {
			transport["max_early_data"] = value
		}
		copyTransportString(transport, "early_data_header_name", options, node, "early-data-header-name", "early_data_header_name")
		return transport, needsTLS, nil
	case "http", "h2":
		transport := map[string]any{"type": "http"}
		copyTransportString(transport, "path", options, node, "path")
		copyTransportString(transport, "method", options, node, "method")
		if hosts := firstStringList(options, node, "host"); len(hosts) > 0 {
			transport["host"] = hosts
		}
		if headers := mapHeaders(mapValue(options, "headers")); len(headers) > 0 {
			transport["headers"] = headers
		}
		return transport, needsTLS, nil
	case "grpc":
		transport := map[string]any{"type": "grpc"}
		copyTransportString(transport, "service_name", options, node, "grpc-service-name", "service-name", "serviceName", "service_name", "path")
		copyTransportString(transport, "idle_timeout", options, node, "idle-timeout", "idle_timeout")
		copyTransportString(transport, "ping_timeout", options, node, "ping-timeout", "ping_timeout")
		if value, ok := firstBool(options, node, []string{"permit-without-stream", "permit_without_stream"}); ok {
			transport["permit_without_stream"] = value
		}
		return transport, needsTLS, nil
	case "httpupgrade":
		transport := map[string]any{"type": "httpupgrade"}
		copyTransportString(transport, "host", options, node, "host")
		copyTransportString(transport, "path", options, node, "path")
		if headers := mapHeaders(mapValue(options, "headers")); len(headers) > 0 {
			transport["headers"] = headers
		}
		return transport, needsTLS, nil
	case "quic":
		return map[string]any{"type": "quic"}, needsTLS, nil
	case "kcp", "mkcp", "domainsocket", "unix":
		return nil, false, errors.New("unsupported V2Ray transport")
	default:
		return nil, false, errors.New("unsupported V2Ray transport")
	}
}

func applyNetwork(node Node, out map[string]any) {
	if udp, ok := nodeBool(node, "udp", "udp-relay"); ok && !udp {
		out["network"] = "tcp"
		return
	}
	value := nodeValue(node, "_network")
	if value == nil {
		candidate := nodeValue(node, "network")
		if text, ok := candidate.(string); ok {
			switch strings.ToLower(strings.TrimSpace(text)) {
			case "tcp", "udp":
				value = text
			}
		}
	}
	if network := strings.ToLower(strings.TrimSpace(stringValue(value))); network == "tcp" || network == "udp" {
		out["network"] = network
	}
}

func applyMultiplex(node Node, out map[string]any) error {
	value := nodeValue(node, "multiplex", "mux", "smux")
	if value == nil {
		return nil
	}
	options := map[string]any{}
	enabled := false
	switch typed := value.(type) {
	case map[string]any:
		options = typed
		enabled = true
		if configured, ok := mapBool(options, "enabled"); ok {
			enabled = configured
		}
	default:
		enabled, _ = boolValue(typed)
	}
	if !enabled {
		return nil
	}
	multiplex := map[string]any{"enabled": true}
	if protocol := strings.ToLower(mapString(options, "protocol")); protocol != "" {
		multiplex["protocol"] = protocol
	}
	for _, field := range []struct {
		output  string
		aliases []string
	}{
		{output: "max_connections", aliases: []string{"max-connections", "max_connections"}},
		{output: "min_streams", aliases: []string{"min-streams", "min_streams"}},
		{output: "max_streams", aliases: []string{"max-streams", "max_streams"}},
	} {
		if value, ok := mapInt(options, field.aliases...); ok {
			multiplex[field.output] = value
		}
	}
	_, hasMaxConnections := multiplex["max_connections"]
	_, hasMinStreams := multiplex["min_streams"]
	_, hasMaxStreams := multiplex["max_streams"]
	if hasMaxStreams && (hasMaxConnections || hasMinStreams) {
		return errors.New("conflicting multiplex stream and connection limits")
	}
	if padding, ok := mapBool(options, "padding"); ok {
		multiplex["padding"] = padding
	}
	if brutalOptions := mapMap(options, "brutal"); len(brutalOptions) > 0 {
		brutal := map[string]any{"enabled": true}
		if enabled, ok := mapBool(brutalOptions, "enabled"); ok {
			brutal["enabled"] = enabled
		}
		if up, ok := mapInt(brutalOptions, "up-mbps", "up_mbps"); ok {
			brutal["up_mbps"] = up
		}
		if down, ok := mapInt(brutalOptions, "down-mbps", "down_mbps"); ok {
			brutal["down_mbps"] = down
		}
		multiplex["brutal"] = brutal
	}
	out["multiplex"] = multiplex
	return nil
}

func applyDialFields(node Node, out map[string]any) {
	for _, field := range []struct {
		output  string
		aliases []string
	}{
		{output: "bind_interface", aliases: []string{"interface-name", "bind-interface", "bind_interface"}},
		{output: "connect_timeout", aliases: []string{"connect-timeout", "connect_timeout"}},
	} {
		if value := nodeString(node, field.aliases...); value != "" {
			out[field.output] = value
		}
	}
	if mark, ok := nodeInt(node, "routing-mark", "routing_mark"); ok {
		out["routing_mark"] = mark
	}
	for _, field := range []struct {
		output  string
		aliases []string
	}{
		{output: "tcp_fast_open", aliases: []string{"tfo", "fast-open", "tcp-fast-open"}},
		{output: "tcp_multi_path", aliases: []string{"mptcp", "tcp-multi-path"}},
		{output: "udp_fragment", aliases: []string{"udp-fragment", "udp_fragment"}},
	} {
		if value, ok := nodeBool(node, field.aliases...); ok {
			out[field.output] = value
		}
	}
	strategy := strings.ToLower(nodeString(node, "domain-strategy", "domain_strategy", "ip-version", "ip_version"))
	strategy = strings.NewReplacer("-", "_", " ", "_").Replace(strategy)
	switch strategy {
	case "4", "ipv4", "ipv4only", "ipv4_only":
		strategy = "ipv4_only"
	case "6", "ipv6", "ipv6only", "ipv6_only":
		strategy = "ipv6_only"
	case "preferipv4", "prefer_ipv4":
		strategy = "prefer_ipv4"
	case "preferipv6", "prefer_ipv6":
		strategy = "prefer_ipv6"
	default:
		strategy = ""
	}
	if strategy != "" {
		out["domain_strategy"] = strategy
	}
}

func nodeMap(node Node, keys ...string) map[string]any {
	value := nodeValue(node, keys...)
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[stringValue(key)] = item
		}
		return result
	default:
		return nil
	}
}

func mapValue(source map[string]any, keys ...string) any {
	for _, wanted := range keys {
		canonicalWanted := canonicalKey(wanted)
		for key, value := range source {
			if value != nil && canonicalKey(key) == canonicalWanted {
				return value
			}
		}
	}
	return nil
}

func mapString(source map[string]any, keys ...string) string {
	return strings.TrimSpace(stringValue(mapValue(source, keys...)))
}

func mapBool(source map[string]any, keys ...string) (bool, bool) {
	return boolValue(mapValue(source, keys...))
}

func mapInt(source map[string]any, keys ...string) (int, bool) {
	return intValue(mapValue(source, keys...))
}

func mapMap(source map[string]any, keys ...string) map[string]any {
	value := mapValue(source, keys...)
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	return nil
}

func mapStrings(source map[string]any, keys ...string) []string {
	return cleanStringValueList(mapValue(source, keys...))
}

func cleanStringValueList(value any) []string {
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
		return nil
	}
}

func mapHeaders(value any) map[string]any {
	if text, ok := value.(string); ok {
		return parseHeaders(text)
	}
	return stringMap(value)
}

func hasHeader(headers map[string]any, wanted string) bool {
	for key := range headers {
		if strings.EqualFold(key, wanted) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func firstBool(options map[string]any, node Node, aliases []string) (bool, bool) {
	if value, ok := mapBool(options, aliases...); ok {
		return value, true
	}
	return nodeBool(node, aliases...)
}

func firstInt(options map[string]any, node Node, aliases ...string) (int, bool) {
	if value, ok := mapInt(options, aliases...); ok {
		return value, true
	}
	return nodeInt(node, aliases...)
}

func firstStrings(options map[string]any, node Node, aliases ...string) []string {
	if values := mapStrings(options, aliases...); len(values) > 0 {
		return values
	}
	return nodeStrings(node, aliases...)
}

func firstStringList(options map[string]any, node Node, aliases ...string) []string {
	return firstStrings(options, node, aliases...)
}

func copyTransportString(target map[string]any, output string, options map[string]any, node Node, aliases ...string) {
	if value := firstNonEmpty(mapString(options, aliases...), nodeString(node, aliases...)); value != "" {
		target[output] = value
	}
}
