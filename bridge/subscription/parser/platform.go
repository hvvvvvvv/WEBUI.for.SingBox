package parser

import (
	"errors"
	"strings"
)

func parsePlatformLine(line string) (Node, string, bool, error) {
	left, right, ok := splitFirstTopLevel(line, '=')
	if !ok || strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return nil, "", false, nil
	}
	if tokens := splitTopLevel(right, ','); len(tokens) > 0 {
		rawType := strings.ToLower(strings.TrimSpace(unquoteValue(tokens[0])))
		if isLoonSurgeType(rawType, normalizeType(rawType)) {
			node, err := parseLoonSurgeLine(left, right)
			return node, "Loon/Surge", true, err
		}
	}
	if isQXType(left) {
		node, err := parseQXLine(left, right)
		return node, "Quantumult X", true, err
	}
	node, err := parseLoonSurgeLine(left, right)
	if errors.Is(err, errUnknownPlatformType) {
		return nil, "", false, nil
	}
	return node, "Loon/Surge", true, err
}

var errUnknownPlatformType = errors.New("unsupported application proxy type")

func isQXType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "shadowsocks", "shadowsocksr", "vmess", "vless", "trojan", "http", "socks5", "anytls":
		return true
	default:
		return false
	}
}

func parseQXLine(rawType, content string) (Node, error) {
	typ := normalizeType(rawType)
	tokens := splitTopLevel(content, ',')
	if len(tokens) == 0 {
		return nil, errMissingEndpoint
	}
	server, port, ok := parseHostPort(tokens[0], 0)
	if !ok {
		return nil, errMissingEndpoint
	}
	options, _ := optionMap(tokens[1:])
	node := Node{"type": typ, "server": server, "port": port}
	applyCommonOptions(node, options)
	applyTransportOptions(node, options)
	applyPlatformProtocolOptions(node, options)
	applyPlatformShadowsocksObfs(node, options)

	// QX calls the UUID "password" for VMess and VLESS.
	if typ == "vmess" || typ == "vless" {
		uuid := stringValue(firstOption(options, "uuid", "password"))
		if uuid == "" {
			return nil, errMissingCredentials
		}
		node["uuid"] = uuid
		delete(node, "password")
	}
	if typ == "ss" || typ == "ssr" || typ == "trojan" || typ == "anytls" {
		if stringValue(node["password"]) == "" {
			return nil, errMissingCredentials
		}
	}
	if value := firstOption(options, "aead"); value != nil && typ == "vmess" {
		if enabled, ok := boolValue(value); ok && !enabled {
			node["alterId"] = 64
		}
	}
	name := stringValue(firstOption(options, "tag", "name", "remarks"))
	if name == "" {
		name = defaultName(typ, server, port)
	}
	node["name"] = name
	return node, nil
}

func parseLoonSurgeLine(rawName, content string) (Node, error) {
	tokens := splitTopLevel(content, ',')
	if len(tokens) == 0 {
		return nil, errUnknownPlatformType
	}
	rawType := strings.ToLower(strings.TrimSpace(unquoteValue(tokens[0])))
	typ := normalizeType(rawType)
	if !isLoonSurgeType(rawType, typ) {
		return nil, errUnknownPlatformType
	}
	name := strings.TrimSpace(unquoteValue(rawName))
	if typ == "direct" || typ == "reject" {
		return Node{"name": name, "type": typ}, nil
	}

	endpointStart := 1
	server, port, ok := "", 0, false
	if len(tokens) > endpointStart {
		server, port, ok = parseHostPort(tokens[endpointStart], 0)
	}
	if !ok && len(tokens) > endpointStart+1 {
		if parsedPort, portOK := intValue(parseScalar(tokens[endpointStart+1])); portOK && validPort(parsedPort) {
			server = strings.Trim(strings.TrimSpace(unquoteValue(tokens[endpointStart])), "[]")
			port = parsedPort
			ok = server != ""
			endpointStart++
		}
	}
	if !ok {
		// Surge external proxies and WireGuard section references do not
		// necessarily have an endpoint. Keep them recognizable so the
		// producer can report an unsupported protocol instead of an unknown
		// line, but require endpoints for protocols sing-box can produce.
		if typ == "external" || typ == "wireguard" {
			options, positionals := optionMap(tokens[1:])
			node := Node{"name": name, "type": typ}
			applyPlatformProtocolOptions(node, options)
			if len(positionals) > 0 {
				node["section-name"] = positionals[0]
			}
			return node, nil
		}
		return nil, errMissingEndpoint
	}

	remaining := tokens[endpointStart+1:]
	options, positionals := optionMap(remaining)
	node := Node{"name": name, "type": typ, "server": server, "port": port}
	applyCommonOptions(node, options)
	applyTransportOptions(node, options)
	applyPlatformPositionals(node, typ, positionals)
	applyPlatformProtocolOptions(node, options)
	applyPlatformShadowsocksObfs(node, options)

	if rawType == "https" || rawType == "socks5-tls" || rawType == "socks5+tls" || strings.Contains(rawType, "http/2") {
		node["tls"] = true
	}
	if strings.Contains(rawType, "http/2") {
		node["network"] = "h2"
	}
	if err := validatePlatformCredentials(node); err != nil {
		return nil, err
	}
	return node, nil
}

func isLoonSurgeType(rawType, normalized string) bool {
	switch normalized {
	case "direct", "reject", "ss", "ssr", "vmess", "vless", "trojan", "http", "https", "socks5",
		"anytls", "wireguard", "hysteria2", "tuic", "snell", "ssh", "external", "trust-tunnel":
		return true
	}
	switch rawType {
	case "http/2 connect", "http2 connect", "external proxy program":
		return true
	default:
		return false
	}
}

func applyPlatformShadowsocksObfs(node Node, options map[string]any) {
	if normalizeType(nodeString(node, "type")) != "ss" {
		return
	}
	mode := strings.ToLower(stringValue(firstOption(options, "obfs", "obfs-mode")))
	if mode != "http" && mode != "tls" {
		return
	}
	pluginOptions := map[string]any{"mode": mode}
	setMapIfNotEmpty(pluginOptions, "host", stringValue(firstOption(options, "obfs-host", "host")))
	node["plugin"] = "obfs"
	node["plugin-opts"] = pluginOptions
}

func applyPlatformPositionals(node Node, typ string, values []string) {
	value := func(index int) string {
		if index < 0 || index >= len(values) {
			return ""
		}
		return strings.TrimSpace(unquoteValue(values[index]))
	}
	switch typ {
	case "ss":
		setIfNotEmpty(node, "cipher", value(0))
		setIfNotEmpty(node, "password", value(1))
	case "ssr":
		setIfNotEmpty(node, "cipher", value(0))
		setIfNotEmpty(node, "password", value(1))
		setIfNotEmpty(node, "protocol", value(2))
		setIfNotEmpty(node, "protocol-param", value(3))
		setIfNotEmpty(node, "obfs", value(4))
		setIfNotEmpty(node, "obfs-param", value(5))
	case "vmess":
		setIfNotEmpty(node, "cipher", value(0))
		setIfNotEmpty(node, "uuid", value(1))
	case "vless":
		setIfNotEmpty(node, "uuid", value(0))
	case "trojan", "anytls", "hysteria2":
		setIfNotEmpty(node, "password", value(0))
	case "http", "socks5":
		setIfNotEmpty(node, "username", value(0))
		setIfNotEmpty(node, "password", value(1))
	case "tuic":
		setIfNotEmpty(node, "uuid", value(0))
		setIfNotEmpty(node, "password", value(1))
	case "snell":
		setIfNotEmpty(node, "password", value(0))
	case "ssh":
		setIfNotEmpty(node, "username", value(0))
		setIfNotEmpty(node, "password", value(1))
	}
}

func applyPlatformProtocolOptions(node Node, options map[string]any) {
	typ := normalizeType(stringValue(node["type"]))
	if typ == "vmess" || typ == "vless" {
		setIfNotEmpty(node, "uuid", stringValue(firstOption(options, "uuid", "username", "password", "id")))
		if typ == "vmess" {
			if alterID, ok := intValue(firstOption(options, "alterid", "alter-id")); ok {
				node["alterId"] = alterID
			}
		}
		delete(node, "username")
		delete(node, "password")
	}
	if typ == "ss" || typ == "ssr" {
		setIfNotEmpty(node, "cipher", stringValue(firstOption(options, "cipher", "method", "encrypt-method")))
		setIfNotEmpty(node, "password", stringValue(firstOption(options, "password")))
	}
	if typ == "trojan" || typ == "anytls" || typ == "hysteria2" || typ == "snell" {
		setIfNotEmpty(node, "password", stringValue(firstOption(options, "password", "psk")))
	}
	if typ == "http" || typ == "socks5" {
		setIfNotEmpty(node, "username", stringValue(firstOption(options, "username", "user")))
		setIfNotEmpty(node, "password", stringValue(firstOption(options, "password")))
	}
	if typ == "ssr" {
		for target, keys := range map[string][]string{
			"protocol":       {"protocol"},
			"protocol-param": {"protocol-param"},
			"obfs":           {"obfs"},
			"obfs-param":     {"obfs-param", "obfs-host"},
		} {
			setIfNotEmpty(node, target, stringValue(firstOption(options, keys...)))
		}
	}
	if typ == "snell" {
		if version, ok := intValue(firstOption(options, "version")); ok {
			node["version"] = version
		}
		setIfNotEmpty(node, "obfs", stringValue(firstOption(options, "obfs")))
	}
	if typ == "http" || typ == "https" {
		if value := firstOption(options, "headers", "http-headers"); value != nil {
			if headers := parseHeaders(stringValue(value)); len(headers) > 0 {
				node["headers"] = headers
			}
		}
	}
	if typ == "hysteria2" {
		setIfNotEmpty(node, "obfs", stringValue(firstOption(options, "obfs")))
		setIfNotEmpty(node, "obfs-password", stringValue(firstOption(options, "obfs-password", "obfs-password")))
		setIfNotEmpty(node, "ports", stringValue(firstOption(options, "ports", "server-ports", "port-hopping")))
		setIfNotEmpty(node, "up", stringValue(firstOption(options, "up", "upmbps", "upload-bandwidth")))
		setIfNotEmpty(node, "down", stringValue(firstOption(options, "down", "downmbps", "download-bandwidth")))
	}
	if typ == "tuic" {
		setIfNotEmpty(node, "uuid", stringValue(firstOption(options, "uuid", "token")))
		setIfNotEmpty(node, "password", stringValue(firstOption(options, "password")))
		setIfNotEmpty(node, "congestion-controller", stringValue(firstOption(options, "congestion-controller", "congestion-control")))
		setIfNotEmpty(node, "udp-relay-mode", stringValue(firstOption(options, "udp-relay-mode")))
		for target, aliases := range map[string][]string{
			"disable-sni": {"disable-sni"},
			"reduce-rtt":  {"reduce-rtt", "zero-rtt", "0-rtt"},
		} {
			if parsed, ok := boolValue(firstOption(options, aliases...)); ok {
				node[target] = parsed
			}
		}
	}
	if typ == "wireguard" {
		setIfNotEmpty(node, "private-key", stringValue(firstOption(options, "private-key", "privatekey")))
		setIfNotEmpty(node, "public-key", stringValue(firstOption(options, "public-key", "publickey")))
		setIfNotEmpty(node, "pre-shared-key", stringValue(firstOption(options, "pre-shared-key", "preshared-key", "psk")))
		applyWireGuardAddress(node, stringValue(firstOption(options, "address", "self-ip", "ip")))
		setIfNotEmpty(node, "ipv6", stringValue(firstOption(options, "ipv6", "self-ip-v6")))
		if mtu, ok := intValue(firstOption(options, "mtu")); ok {
			node["mtu"] = mtu
		}
		if reserved := parseReserved(stringValue(firstOption(options, "reserved"))); len(reserved) > 0 {
			node["reserved"] = reserved
		}
	}
	if value := firstOption(options, "reality-public-key", "public-key"); value != nil && (typ == "vless" || typ == "trojan") {
		reality := map[string]any{"public-key": stringValue(value)}
		setMapIfNotEmpty(reality, "short-id", stringValue(firstOption(options, "reality-short-id", "short-id")))
		node["reality-opts"] = reality
		node["tls"] = true
	}
}

func validatePlatformCredentials(node Node) error {
	switch normalizeType(stringValue(node["type"])) {
	case "ss":
		if stringValue(node["cipher"]) == "" || stringValue(node["password"]) == "" {
			return errMissingCredentials
		}
	case "ssr", "trojan", "anytls", "hysteria2", "snell":
		if stringValue(node["password"]) == "" {
			return errMissingCredentials
		}
	case "vmess", "vless":
		if stringValue(node["uuid"]) == "" {
			return errMissingCredentials
		}
	case "tuic":
		if stringValue(node["uuid"]) == "" || stringValue(node["password"]) == "" {
			return errMissingCredentials
		}
	}
	return nil
}
