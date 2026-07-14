package parser

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	errMalformedURI       = errors.New("malformed proxy URI")
	errMissingEndpoint    = errors.New("proxy endpoint is missing or invalid")
	errMissingCredentials = errors.New("proxy credentials are missing or invalid")
	errInvalidPayload     = errors.New("encoded proxy payload is invalid")
)

func parseURI(line string) (Node, string, bool, error) {
	separator := strings.Index(line, "://")
	if separator <= 0 {
		return nil, "", false, nil
	}
	scheme := strings.ToLower(strings.TrimSpace(line[:separator]))
	parserName := "proxy URI"
	var node Node
	var err error
	switch scheme {
	case "http", "https", "socks", "socks5", "socks5+tls":
		parserName = "HTTP/SOCKS URI"
		node, err = parseProxyURI(line, scheme)
	case "ss":
		parserName = "SS URI"
		node, err = parseSSURI(line)
	case "ssr":
		parserName = "SSR URI"
		node, err = parseSSRURI(line)
	case "vmess":
		parserName = "VMess URI"
		node, err = parseVMessURI(line)
	case "vless":
		parserName = "VLESS URI"
		node, err = parseVLESSURI(line)
	case "trojan":
		parserName = "Trojan URI"
		node, err = parseTrojanURI(line)
	case "hysteria", "hy":
		parserName = "Hysteria URI"
		node, err = parseHysteriaURI(line)
	case "hysteria2", "hy2":
		parserName = "Hysteria2 URI"
		node, err = parseHysteria2URI(line)
	case "tuic":
		parserName = "TUIC URI"
		node, err = parseTUICURI(line)
	case "wireguard", "wg":
		parserName = "WireGuard URI"
		node, err = parseWireGuardURI(line)
	case "anytls":
		parserName = "AnyTLS URI"
		node, err = parseAnyTLSURI(line)
	default:
		return nil, parserName, true, errors.New("unsupported proxy URI scheme")
	}
	return node, parserName, true, err
}

func parseURLWithName(line string) (*url.URL, string, error) {
	name := ""
	withoutFragment := line
	if index := strings.IndexByte(line, '#'); index >= 0 {
		withoutFragment = line[:index]
		name = decodeURLComponent(line[index+1:])
	}
	parsed, err := url.Parse(withoutFragment)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, "", errMalformedURI
	}
	return parsed, name, nil
}

func queryOptions(query url.Values) map[string]any {
	options := make(map[string]any, len(query))
	for key, values := range query {
		if len(values) == 0 {
			continue
		}
		options[normalizeOptionKey(key)] = values[0]
	}
	return options
}

func endpointFromURL(parsed *url.URL, defaultPort int) (string, int, error) {
	server := strings.TrimSpace(parsed.Hostname())
	port := defaultPort
	if parsed.Port() != "" {
		parsedPort, err := strconv.Atoi(parsed.Port())
		if err != nil {
			return "", 0, errMissingEndpoint
		}
		port = parsedPort
	}
	if server == "" || !validPort(port) {
		return "", 0, errMissingEndpoint
	}
	return server, port, nil
}

func parseProxyURI(line, scheme string) (Node, error) {
	parsed, name, err := parseURLWithName(line)
	if err != nil {
		return nil, err
	}
	typ := "http"
	tlsEnabled := scheme == "https" || scheme == "socks5+tls"
	defaultPort := 80
	if scheme == "socks" || scheme == "socks5" || scheme == "socks5+tls" {
		typ = "socks5"
		defaultPort = 1080
	}
	if tlsEnabled {
		defaultPort = 443
	}
	server, port, err := endpointFromURL(parsed, defaultPort)
	if err != nil {
		return nil, err
	}
	node := Node{"type": typ, "server": server, "port": port}
	if parsed.User != nil {
		username := parsed.User.Username()
		password, _ := parsed.User.Password()
		if scheme == "socks" && password == "" {
			if decoded, ok := decodeBase64String(username); ok {
				username, password, _ = strings.Cut(decoded, ":")
			}
		}
		setIfNotEmpty(node, "username", username)
		setIfNotEmpty(node, "password", password)
	}
	if tlsEnabled {
		node["tls"] = true
	}
	options := queryOptions(parsed.Query())
	applyCommonOptions(node, options)
	if name == "" {
		name = defaultName(typ, server, port)
	}
	node["name"] = name
	return node, nil
}

func parseSSURI(line string) (Node, error) {
	body := strings.TrimPrefix(line, line[:strings.Index(line, "://")+3])
	name := ""
	if index := strings.IndexByte(body, '#'); index >= 0 {
		name = decodeURLComponent(body[index+1:])
		body = body[:index]
	}
	queryText := ""
	if index := strings.IndexByte(body, '?'); index >= 0 {
		queryText = body[index+1:]
		body = body[:index]
	}
	userinfo, endpoint := "", ""
	if at := strings.LastIndexByte(body, '@'); at >= 0 {
		userinfo, endpoint = body[:at], body[at+1:]
		endpoint = strings.TrimSuffix(endpoint, "/")
		userinfo = decodeURLComponent(userinfo)
		if !strings.Contains(userinfo, ":") {
			decoded, ok := decodeBase64String(userinfo)
			if !ok {
				return nil, errMissingCredentials
			}
			userinfo = decoded
		}
	} else {
		decoded, ok := decodeBase64String(body)
		if !ok {
			return nil, errInvalidPayload
		}
		at := strings.LastIndexByte(decoded, '@')
		if at < 1 {
			return nil, errInvalidPayload
		}
		userinfo, endpoint = decoded[:at], decoded[at+1:]
	}
	cipher, password, ok := strings.Cut(userinfo, ":")
	if !ok || strings.TrimSpace(cipher) == "" || password == "" {
		return nil, errMissingCredentials
	}
	server, port, ok := parseHostPort(endpoint, 0)
	if !ok {
		return nil, errMissingEndpoint
	}
	node := Node{
		"type":     "ss",
		"server":   server,
		"port":     port,
		"cipher":   decodeURLComponent(cipher),
		"password": decodeURLComponent(password),
	}
	query, _ := url.ParseQuery(queryText)
	options := queryOptions(query)
	applyCommonOptions(node, options)
	applyTransportOptions(node, options)
	if plugin := query.Get("plugin"); plugin != "" {
		applySSPlugin(node, plugin)
	}
	if name == "" {
		name = defaultName("ss", server, port)
	}
	node["name"] = name
	return node, nil
}

func applySSPlugin(node Node, raw string) {
	parts := strings.Split(decodeURLComponent(raw), ";")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return
	}
	plugin := strings.ToLower(strings.TrimSpace(parts[0]))
	opts := map[string]any{}
	for _, part := range parts[1:] {
		if key, value, ok := strings.Cut(part, "="); ok {
			opts[normalizeOptionKey(key)] = unquoteValue(value)
		} else if strings.TrimSpace(part) != "" {
			opts[normalizeOptionKey(part)] = true
		}
	}
	switch plugin {
	case "obfs-local", "simple-obfs":
		node["plugin"] = "obfs"
		node["plugin-opts"] = map[string]any{
			"mode": stringValue(firstOption(opts, "obfs", "mode")),
			"host": stringValue(firstOption(opts, "obfs-host", "host")),
		}
	case "v2ray-plugin":
		node["plugin"] = plugin
		pluginOpts := map[string]any{
			"mode": stringValue(firstOption(opts, "mode", "obfs")),
			"host": stringValue(firstOption(opts, "host", "obfs-host")),
			"path": stringValue(firstOption(opts, "path")),
		}
		if enabled, ok := boolValue(firstOption(opts, "tls")); ok {
			pluginOpts["tls"] = enabled
		}
		node["plugin-opts"] = pluginOpts
	default:
		node["plugin"] = plugin
		node["plugin-opts"] = opts
	}
}

func parseSSRURI(line string) (Node, error) {
	encoded := line[strings.Index(line, "://")+3:]
	decoded, ok := decodeBase64String(encoded)
	if !ok {
		return nil, errInvalidPayload
	}
	mainPart, queryPart, _ := strings.Cut(decoded, "/?")
	pattern := regexp.MustCompile(`^(.*):(\d+):([^:]+):([^:]+):([^:]+):([^:]+)$`)
	match := pattern.FindStringSubmatch(mainPart)
	if len(match) != 7 {
		return nil, errInvalidPayload
	}
	port, _ := strconv.Atoi(match[2])
	if strings.TrimSpace(match[1]) == "" || !validPort(port) {
		return nil, errMissingEndpoint
	}
	password, ok := decodeBase64String(match[6])
	if !ok {
		return nil, errMissingCredentials
	}
	node := Node{
		"type":     "ssr",
		"server":   strings.Trim(match[1], "[]"),
		"port":     port,
		"protocol": match[3],
		"cipher":   match[4],
		"obfs":     match[5],
		"password": password,
	}
	query, _ := url.ParseQuery(queryPart)
	if value := query.Get("protoparam"); value != "" {
		if decoded, ok := decodeBase64String(value); ok {
			node["protocol-param"] = decoded
		}
	}
	if value := query.Get("obfsparam"); value != "" {
		if decoded, ok := decodeBase64String(value); ok {
			node["obfs-param"] = decoded
		}
	}
	name := ""
	if value := query.Get("remarks"); value != "" {
		name, _ = decodeBase64String(value)
	}
	if name == "" {
		name = defaultName("ssr", stringValue(node["server"]), port)
	}
	node["name"] = name
	return node, nil
}

func parseVMessURI(line string) (Node, error) {
	body := line[strings.Index(line, "://")+3:]
	name := ""
	if index := strings.IndexByte(body, '#'); index >= 0 {
		name = decodeURLComponent(body[index+1:])
		body = body[:index]
	}
	encoded, queryText, hasQuery := strings.Cut(body, "?")
	decoded, ok := decodeBase64String(encoded)
	if !ok && hasQuery && strings.HasSuffix(encoded, "/") {
		decoded, ok = decodeBase64String(strings.TrimSuffix(encoded, "/"))
	}
	if !ok {
		return nil, errInvalidPayload
	}

	// Quantumult exports a complete application line inside the Base64 VMess
	// payload. Reuse the line parser so quoting and comma handling stay
	// identical to an unwrapped subscription.
	if node, _, matched, err := parsePlatformLine(strings.TrimSpace(decoded)); matched && normalizeType(nodeString(node, "type")) == "vmess" {
		if err != nil {
			return nil, err
		}
		if name != "" {
			node["name"] = name
		}
		return node, nil
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(decoded), &payload); err == nil {
		return parseVMessPayload(payload, name)
	}
	if !hasQuery {
		return nil, errInvalidPayload
	}
	return parseShadowrocketVMess(decoded, queryText, name)
}

func parseVMessPayload(payload map[string]any, name string) (Node, error) {
	server := stringValue(firstMapValue(payload, "add", "server", "address"))
	port, ok := intValue(firstMapValue(payload, "port"))
	if server == "" || !ok || !validPort(port) {
		return nil, errMissingEndpoint
	}
	uuid := stringValue(firstMapValue(payload, "id", "uuid", "password"))
	if uuid == "" {
		return nil, errMissingCredentials
	}
	node := Node{
		"type":   "vmess",
		"server": server,
		"port":   port,
		"uuid":   uuid,
	}
	setIfNotEmpty(node, "cipher", stringValue(firstMapValue(payload, "scy", "cipher", "security")))
	if alterID, ok := intValue(firstMapValue(payload, "aid", "alterId")); ok {
		node["alterId"] = alterID
	}
	options := normalizeMapOptions(payload)
	tlsValue := firstMapValue(payload, "tls")
	tlsEnabled, tlsIsBool := boolValue(tlsValue)
	security := strings.ToLower(stringValue(firstMapValue(payload, "security")))
	if (tlsIsBool && tlsEnabled) || strings.EqualFold(stringValue(tlsValue), "tls") ||
		security == "tls" || security == "reality" {
		node["tls"] = true
	}
	applyCommonOptions(node, options)
	applyTransportOptions(node, options)
	applyRealityOptions(node, options)
	if name == "" {
		name = stringValue(firstMapValue(payload, "ps", "remarks", "remark", "name"))
	}
	if name == "" {
		name = defaultName("vmess", server, port)
	}
	node["name"] = name
	return node, nil
}

func parseShadowrocketVMess(credentials, queryText, name string) (Node, error) {
	at := strings.LastIndexByte(credentials, '@')
	if at <= 0 {
		return nil, errInvalidPayload
	}
	cipher, uuid, ok := strings.Cut(credentials[:at], ":")
	if !ok || strings.TrimSpace(cipher) == "" || strings.TrimSpace(uuid) == "" {
		return nil, errMissingCredentials
	}
	server, port, ok := parseHostPort(credentials[at+1:], 0)
	if !ok {
		return nil, errMissingEndpoint
	}
	query, _ := url.ParseQuery(queryText)
	options := queryOptions(query)
	node := Node{
		"type":   "vmess",
		"server": server,
		"port":   port,
		"uuid":   strings.TrimSpace(uuid),
		"cipher": strings.TrimSpace(cipher),
	}
	if alterID, ok := intValue(firstOption(options, "aid", "alterid", "alter-id")); ok {
		node["alterId"] = alterID
	}
	if value := firstOption(options, "tls", "security"); value != nil {
		text := strings.ToLower(stringValue(value))
		if enabled, ok := boolValue(value); (ok && enabled) || text == "tls" || text == "reality" {
			node["tls"] = true
		}
	}
	applyCommonOptions(node, options)
	if insecure, ok := boolValue(firstOption(options, "allowinsecure")); ok {
		node["skip-cert-verify"] = insecure
	}
	if verify, ok := boolValue(firstOption(options, "verify-cert", "verify_cert")); ok {
		node["skip-cert-verify"] = !verify
	}
	applyTransportOptions(node, options)
	applyRealityOptions(node, options)
	if name == "" {
		name = stringValue(firstOption(options, "ps", "remarks", "remark", "name"))
	}
	if name == "" {
		name = defaultName("vmess", server, port)
	}
	node["name"] = name
	return node, nil
}

func parseVLESSURI(line string) (Node, error) {
	parsed, name, err := parseURLWithName(line)
	if err != nil {
		return nil, err
	}
	server, port, err := endpointFromURL(parsed, 443)
	if err != nil {
		return nil, err
	}
	uuid := ""
	if parsed.User != nil {
		uuid = parsed.User.Username()
	}
	if uuid == "" {
		return nil, errMissingCredentials
	}
	node := Node{"type": "vless", "server": server, "port": port, "uuid": uuid}
	options := queryOptions(parsed.Query())
	security := strings.ToLower(stringValue(firstOption(options, "security")))
	if security == "tls" || security == "reality" {
		node["tls"] = true
	}
	applyCommonOptions(node, options)
	applyTransportOptions(node, options)
	applyRealityOptions(node, options)
	setIfNotEmpty(node, "flow", stringValue(firstOption(options, "flow")))
	if name == "" {
		name = defaultName("vless", server, port)
	}
	node["name"] = name
	return node, nil
}

func parseTrojanURI(line string) (Node, error) {
	parsed, name, err := parseURLWithName(line)
	if err != nil {
		return nil, err
	}
	server, port, err := endpointFromURL(parsed, 443)
	if err != nil {
		return nil, err
	}
	password := ""
	if parsed.User != nil {
		password = parsed.User.Username()
	}
	if password == "" {
		return nil, errMissingCredentials
	}
	node := Node{"type": "trojan", "server": server, "port": port, "password": password, "tls": true}
	options := queryOptions(parsed.Query())
	if strings.EqualFold(stringValue(firstOption(options, "security")), "none") {
		node["tls"] = false
	}
	applyCommonOptions(node, options)
	applyTransportOptions(node, options)
	applyRealityOptions(node, options)
	if name == "" {
		name = defaultName("trojan", server, port)
	}
	node["name"] = name
	return node, nil
}

func parseHysteriaURI(line string) (Node, error) {
	parsed, name, err := parseURLWithName(line)
	if err != nil {
		return nil, err
	}
	server, port, err := endpointFromURL(parsed, 443)
	if err != nil {
		return nil, err
	}
	options := queryOptions(parsed.Query())
	node := Node{"type": "hysteria", "server": server, "port": port, "tls": true}
	auth := stringValue(firstOption(options, "auth", "auth-str", "authstring"))
	if auth == "" && parsed.User != nil {
		auth = parsed.User.Username()
		if password, ok := parsed.User.Password(); ok {
			auth += ":" + password
		}
	}
	setIfNotEmpty(node, "auth-str", auth)
	setIfNotEmpty(node, "protocol", stringValue(firstOption(options, "protocol")))
	setIfNotEmpty(node, "obfs", stringValue(firstOption(options, "obfs", "obfsParam")))
	setIfNotEmpty(node, "up", stringValue(firstOption(options, "up", "upmbps", "up-speed")))
	setIfNotEmpty(node, "down", stringValue(firstOption(options, "down", "downmbps", "down-speed")))
	setIfNotEmpty(node, "ports", stringValue(firstOption(options, "ports", "mport", "port-hopping")))
	applyCommonOptions(node, options)
	if name == "" {
		name = defaultName("hysteria", server, port)
	}
	node["name"] = name
	return node, nil
}

func parseHysteria2URI(line string) (Node, error) {
	parsed, name, err := parseURLWithName(line)
	if err != nil {
		return nil, err
	}
	server, port, err := endpointFromURL(parsed, 443)
	if err != nil {
		return nil, err
	}
	options := queryOptions(parsed.Query())
	password := stringValue(firstOption(options, "password", "auth"))
	if password == "" && parsed.User != nil {
		password = parsed.User.Username()
		if userPassword, ok := parsed.User.Password(); ok && userPassword != "" {
			password += ":" + userPassword
		}
	}
	if password == "" {
		return nil, errMissingCredentials
	}
	node := Node{"type": "hysteria2", "server": server, "port": port, "password": password, "tls": true}
	setIfNotEmpty(node, "obfs", stringValue(firstOption(options, "obfs")))
	setIfNotEmpty(node, "obfs-password", stringValue(firstOption(options, "obfs-password", "obfs-password", "obfsParam")))
	setIfNotEmpty(node, "ports", stringValue(firstOption(options, "ports", "mport", "port-hopping")))
	setIfNotEmpty(node, "up", stringValue(firstOption(options, "up", "upmbps")))
	setIfNotEmpty(node, "down", stringValue(firstOption(options, "down", "downmbps")))
	applyCommonOptions(node, options)
	if name == "" {
		name = defaultName("hysteria2", server, port)
	}
	node["name"] = name
	return node, nil
}

func parseTUICURI(line string) (Node, error) {
	parsed, name, err := parseURLWithName(line)
	if err != nil {
		return nil, err
	}
	server, port, err := endpointFromURL(parsed, 443)
	if err != nil {
		return nil, err
	}
	uuid, password := "", ""
	if parsed.User != nil {
		uuid = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	options := queryOptions(parsed.Query())
	if uuid == "" {
		uuid = stringValue(firstOption(options, "uuid", "token"))
	}
	if password == "" {
		password = stringValue(firstOption(options, "password"))
	}
	if uuid == "" || password == "" {
		return nil, errMissingCredentials
	}
	node := Node{"type": "tuic", "server": server, "port": port, "uuid": uuid, "password": password, "tls": true}
	for target, aliases := range map[string][]string{
		"congestion-controller": {"congestion-controller", "congestion-control"},
		"udp-relay-mode":        {"udp-relay-mode"},
	} {
		setIfNotEmpty(node, target, stringValue(firstOption(options, aliases...)))
	}
	for target, aliases := range map[string][]string{
		"disable-sni": {"disable-sni"},
		"reduce-rtt":  {"reduce-rtt", "zero-rtt", "0-rtt"},
	} {
		if parsedBool, ok := boolValue(firstOption(options, aliases...)); ok {
			node[target] = parsedBool
		}
	}
	applyCommonOptions(node, options)
	if name == "" {
		name = defaultName("tuic", server, port)
	}
	node["name"] = name
	return node, nil
}

func parseWireGuardURI(line string) (Node, error) {
	parsed, name, err := parseURLWithName(line)
	if err != nil {
		return nil, err
	}
	server, port, err := endpointFromURL(parsed, 51820)
	if err != nil {
		return nil, err
	}
	options := queryOptions(parsed.Query())
	privateKey := stringValue(firstOption(options, "private-key", "privatekey", "secret-key"))
	if privateKey == "" && parsed.User != nil {
		privateKey = parsed.User.Username()
		if password, ok := parsed.User.Password(); ok {
			privateKey += ":" + password
		}
	}
	publicKey := stringValue(firstOption(options, "public-key", "publickey", "peer-public-key"))
	if privateKey == "" || publicKey == "" {
		return nil, errMissingCredentials
	}
	node := Node{
		"type":        "wireguard",
		"server":      server,
		"port":        port,
		"private-key": privateKey,
		"public-key":  publicKey,
	}
	setIfNotEmpty(node, "pre-shared-key", stringValue(firstOption(options, "pre-shared-key", "presharedkey", "psk")))
	applyWireGuardAddress(node, stringValue(firstOption(options, "address", "addresses", "ip", "self-ip")))
	setIfNotEmpty(node, "ipv6", stringValue(firstOption(options, "ipv6", "self-ip-v6")))
	if mtu, ok := intValue(firstOption(options, "mtu")); ok {
		node["mtu"] = mtu
	}
	if reserved := parseReserved(stringValue(firstOption(options, "reserved"))); len(reserved) > 0 {
		node["reserved"] = reserved
	}
	if name == "" {
		name = defaultName("wireguard", server, port)
	}
	node["name"] = name
	return node, nil
}

func parseAnyTLSURI(line string) (Node, error) {
	parsed, name, err := parseURLWithName(line)
	if err != nil {
		return nil, err
	}
	server, port, err := endpointFromURL(parsed, 443)
	if err != nil {
		return nil, err
	}
	password := ""
	if parsed.User != nil {
		password = parsed.User.Username()
		if userPassword, ok := parsed.User.Password(); ok && userPassword != "" {
			password += ":" + userPassword
		}
	}
	if password == "" {
		password = parsed.Query().Get("password")
	}
	if password == "" {
		return nil, errMissingCredentials
	}
	node := Node{"type": "anytls", "server": server, "port": port, "password": password, "tls": true}
	options := queryOptions(parsed.Query())
	applyCommonOptions(node, options)
	if name == "" {
		name = defaultName("anytls", server, port)
	}
	node["name"] = name
	return node, nil
}

func applyRealityOptions(node Node, options map[string]any) {
	security := strings.ToLower(stringValue(firstOption(options, "security")))
	publicKey := stringValue(firstOption(options, "pbk", "public-key", "reality-public-key"))
	shortID := stringValue(firstOption(options, "sid", "short-id", "reality-short-id"))
	if security != "reality" && publicKey == "" && shortID == "" {
		return
	}
	node["tls"] = true
	node["_reality"] = true
	reality := map[string]any{}
	setMapIfNotEmpty(reality, "public-key", publicKey)
	setMapIfNotEmpty(reality, "short-id", shortID)
	node["reality-opts"] = reality
}

func normalizeMapOptions(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[normalizeOptionKey(key)] = value
	}
	return result
}

func firstMapValue(source map[string]any, keys ...string) any {
	for _, wanted := range keys {
		for key, value := range source {
			if strings.EqualFold(key, wanted) && value != nil {
				return value
			}
		}
	}
	return nil
}

func applyWireGuardAddress(node Node, value string) {
	for _, address := range parseStringList(value) {
		host := address
		cidr := ""
		if slash := strings.LastIndexByte(address, '/'); slash >= 0 {
			host, cidr = address[:slash], address[slash+1:]
		}
		host = strings.Trim(host, "[]")
		if strings.Contains(host, ":") {
			setIfNotEmpty(node, "ipv6", host)
			if parsed, err := strconv.Atoi(cidr); err == nil {
				node["ipv6-cidr"] = parsed
			}
		} else if host != "" {
			setIfNotEmpty(node, "ip", host)
			if parsed, err := strconv.Atoi(cidr); err == nil {
				node["ip-cidr"] = parsed
			}
		}
	}
}
