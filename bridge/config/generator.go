package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/storage"
	configv1 "guiforcores/gen/profile/v1"

	"github.com/dop251/goja"

	"gopkg.in/yaml.v3"
)

const (
	subscribesFilePath = "data/subscribes.yaml"
	rulesetsFilePath   = "data/rulesets.yaml"

	logLevelTrace = "trace"
	logLevelDebug = "debug"
	logLevelInfo  = "info"

	ruleTypeInline         = "inline"
	ruleTypeRuleSet        = "rule_set"
	ruleTypeInbound        = "inbound"
	ruleTypeIpIsPrivate    = "ip_is_private"
	ruleTypeIpAcceptAny    = "ip_accept_any"
	ruleTypeClashMode      = "clash_mode"
	ruleTypePort           = "port"
	ruleTypeSourcePort     = "source_port"
	ruleTypeInsertionPoint = "InsertionPoint"

	ruleActionRoute        = "route"
	ruleActionRouteOptions = "route-options"
	ruleActionReject       = "reject"
	ruleActionSniff        = "sniff"
	ruleActionResolve      = "resolve"
	ruleActionPredefined   = "predefined"
	ruleActionInline       = "inline"

	outboundTypeDirect   = "direct"
	outboundTypeBlock    = "block"
	outboundTypeSelector = "selector"
	outboundTypeURLTest  = "urltest"

	inboundTypeDirect = "direct"
	inboundTypeTun    = "tun"

	inboundNetworkTCP = "tcp"
	inboundNetworkUDP = "udp"

	dnsServerLocal     = "local"
	dnsServerHosts     = "hosts"
	dnsServerTCP       = "tcp"
	dnsServerUDP       = "udp"
	dnsServerTLS       = "tls"
	dnsServerHTTPS     = "https"
	dnsServerQUIC      = "quic"
	dnsServerH3        = "h3"
	dnsServerDHCP      = "dhcp"
	dnsServerFakeIP    = "fakeip"
	dnsServerTailscale = "tailscale"

	strategyDefault = "default"

	mixinPriorityMixin = "mixin"
	mixinPriorityGUI   = "gui"

	coreAPIDefaultMode = "rule"

	CoreAPIController = "127.0.0.1:20123"
)

type invalidArgumentError struct {
	message string
}

func (e invalidArgumentError) Error() string {
	return e.message
}

type configGenerator struct {
	paths               *storage.Paths
	subscriptions       map[string]subscriptionMeta
	rulesets            map[string]rulesetMeta
	subscriptionProxies map[string][]map[string]any
}

type subscriptionMeta struct {
	ID string `yaml:"id"`
}

type rulesetMeta struct {
	ID   string `yaml:"id"`
	Path string `yaml:"path"`
}

func newConfigGenerator(paths *storage.Paths) (*configGenerator, error) {
	generator := &configGenerator{
		paths:               paths,
		subscriptions:       map[string]subscriptionMeta{},
		rulesets:            map[string]rulesetMeta{},
		subscriptionProxies: map[string][]map[string]any{},
	}

	if err := generator.loadSubscriptions(); err != nil {
		return nil, err
	}
	if err := generator.loadRulesets(); err != nil {
		return nil, err
	}

	return generator, nil
}

func (g *configGenerator) GenerateConfig(profile *configv1.Profile) (map[string]any, error) {
	config := map[string]any{
		"log":          generateLog(profile.GetLog()),
		"experimental": generateExperimental(profile.GetExperimental(), profile.GetOutbounds()),
		"inbounds":     generateInbounds(profile.GetInbounds()),
	}

	outbounds, err := g.generateOutbounds(profile.GetOutbounds())
	if err != nil {
		return nil, err
	}
	config["outbounds"] = outbounds

	route, err := g.generateRoute(profile.GetRoute(), profile.GetInbounds(), profile.GetOutbounds(), profile.GetDns())
	if err != nil {
		return nil, err
	}
	config["route"] = route

	dns, err := g.generateDNS(profile.GetDns(), profile.GetRoute().GetRuleSet(), profile.GetInbounds(), profile.GetOutbounds())
	if err != nil {
		return nil, err
	}
	config["dns"] = dns

	return config, nil
}

func (g *configGenerator) loadSubscriptions() error {
	var records []subscriptionMeta
	if err := readYAMLFile(g.paths, subscribesFilePath, &records); err != nil {
		return fmt.Errorf("load subscriptions: %w", err)
	}
	for _, record := range records {
		g.subscriptions[record.ID] = record
	}
	return nil
}

func (g *configGenerator) loadRulesets() error {
	var records []rulesetMeta
	if err := readYAMLFile(g.paths, rulesetsFilePath, &records); err != nil {
		return fmt.Errorf("load rulesets: %w", err)
	}
	for _, record := range records {
		g.rulesets[record.ID] = record
	}
	return nil
}

func readYAMLFile(paths *storage.Paths, path string, target any) error {
	bytes, err := os.ReadFile(paths.Resolve(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(bytes) == 0 {
		return nil
	}
	return yaml.Unmarshal(bytes, target)
}

func subscriptionContentPath(id string) string {
	return "data/subscribes/" + safeFileName(id, "subscription") + ".json"
}

func safeFileName(id string, fallback string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", " ", "_")
	safe := replacer.Replace(id)
	safe = strings.Trim(safe, "._")
	if safe == "" {
		return fallback
	}
	return safe
}

func generateLog(log *configv1.Log) map[string]any {
	if log == nil {
		return map[string]any{}
	}
	return map[string]any{
		"disabled":  log.GetDisabled(),
		"level":     logLevelString(log.GetLevel()),
		"output":    log.GetOutput(),
		"timestamp": log.GetTimestamp(),
	}
}

func generateExperimental(experimental *configv1.Experimental, outbounds []*configv1.Outbound) map[string]any {
	clashAPI := map[string]any{
		"external_controller": CoreAPIController,
		"secret":              generateCoreAPISecret(),
		"default_mode":        coreAPIDefaultMode,
	}

	cacheFile := map[string]any{}
	if experimental != nil && experimental.GetCacheFile() != nil {
		source := experimental.GetCacheFile()
		cacheFile["enabled"] = source.GetEnabled()
		cacheFile["path"] = source.GetPath()
		cacheFile["cache_id"] = source.GetCacheId()
		cacheFile["store_fakeip"] = source.GetStoreFakeip()
		cacheFile["store_rdrc"] = source.GetStoreRdrc()
		cacheFile["rdrc_timeout"] = source.GetRdrcTimeout()
	}

	return map[string]any{
		"clash_api":  clashAPI,
		"cache_file": cacheFile,
	}
}

func generateCoreAPISecret() string {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return auth.HashSecret(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	return hex.EncodeToString(buffer)
}

func generateInbounds(inbounds []*configv1.Inbound) []any {
	result := make([]any, 0, len(inbounds))
	for _, inbound := range inbounds {
		if inbound == nil || !inbound.GetEnable() {
			continue
		}

		inboundType, err := inboundTypeString(inbound.GetType())
		if err != nil {
			continue
		}

		if inboundType == inboundTypeDirect {
			config := inbound.GetDirect()
			if config == nil {
				continue
			}
			listen := config.GetListen()
			result = append(result, map[string]any{
				"type":           inboundType,
				"tag":            inbound.GetTag(),
				"listen":         listen.GetListen(),
				"listen_port":    listen.GetListenPort(),
				"tcp_fast_open":  listen.GetTcpFastOpen(),
				"tcp_multi_path": listen.GetTcpMultiPath(),
				"udp_fragment":   listen.GetUdpFragment(),
				"network":        inboundNetworkString(config.GetNetwork()),
			})
			continue
		}

		if inboundType != inboundTypeTun {
			config := inboundUserConfigForType(inbound, inboundType)
			if config == nil {
				continue
			}
			item := map[string]any{
				"type":           inboundType,
				"tag":            inbound.GetTag(),
				"listen":         config.GetListen().GetListen(),
				"listen_port":    config.GetListen().GetListenPort(),
				"tcp_fast_open":  config.GetListen().GetTcpFastOpen(),
				"tcp_multi_path": config.GetListen().GetTcpMultiPath(),
				"udp_fragment":   config.GetListen().GetUdpFragment(),
			}
			if users := splitUsers(config.GetUsers()); len(users) > 0 {
				item["users"] = users
			}
			result = append(result, item)
			continue
		}

		tun := inbound.GetTun()
		if tun == nil {
			continue
		}
		item := map[string]any{
			"type":                     inboundType,
			"tag":                      inbound.GetTag(),
			"interface_name":           tun.GetInterfaceName(),
			"address":                  stringsToAnySlice(tun.GetAddress()),
			"mtu":                      tun.GetMtu(),
			"auto_route":               tun.GetAutoRoute(),
			"strict_route":             tun.GetStrictRoute(),
			"endpoint_independent_nat": tun.GetEndpointIndependentNat(),
			"stack":                    tunStackString(tun.GetStack()),
		}
		if len(tun.GetRouteAddress()) > 0 {
			item["route_address"] = stringsToAnySlice(tun.GetRouteAddress())
		}
		if len(tun.GetRouteExcludeAddress()) > 0 {
			item["route_exclude_address"] = stringsToAnySlice(tun.GetRouteExcludeAddress())
		}
		result = append(result, item)
	}
	return result
}

func (g *configGenerator) generateOutbounds(outbounds []*configv1.Outbound) ([]any, error) {
	result := make([]any, 0, len(outbounds))
	proxyItems := make([]any, 0)
	proxySeen := map[string]struct{}{}
	builtInTags := make([]string, 0)
	builtInSeen := map[string]struct{}{}

	for _, outbound := range outbounds {
		if outbound == nil {
			continue
		}

		outboundType, err := outboundTypeString(outbound.GetType())
		if err != nil {
			return nil, err
		}

		item := map[string]any{
			"type": outboundType,
			"tag":  outbound.GetTag(),
		}

		if outboundType == outboundTypeURLTest {
			item["url"] = outbound.GetUrl()
			item["interval"] = outbound.GetInterval()
			item["tolerance"] = outbound.GetTolerance()
		}

		if outboundType == outboundTypeSelector || outboundType == outboundTypeURLTest {
			item["interrupt_exist_connections"] = outbound.GetInterruptExistConnections()
			item["outbounds"] = make([]any, 0)
			match := createTextMatcher(outbound.GetInclude(), outbound.GetExclude())

			for _, proxy := range outbound.GetOutbounds() {
				if proxy == nil {
					continue
				}

				if proxy.GetType() == "Built-in" {
					if proxy.GetId() == outboundTypeDirect || proxy.GetId() == outboundTypeBlock {
						if _, ok := builtInSeen[proxy.GetId()]; !ok {
							builtInSeen[proxy.GetId()] = struct{}{}
							builtInTags = append(builtInTags, proxy.GetId())
						}
					}
					item["outbounds"] = append(item["outbounds"].([]any), proxy.GetTag())
					continue
				}

				subID := proxy.GetType()
				if proxy.GetType() == "Subscription" {
					subID = proxy.GetId()
				}

				proxies, err := g.subscriptionEntries(subID)
				if err != nil {
					return nil, err
				}

				if proxy.GetType() == "Subscription" {
					for _, candidate := range proxies {
						tag, _ := candidate["tag"].(string)
						if !match(tag) {
							continue
						}
						item["outbounds"] = append(item["outbounds"].([]any), tag)
						proxyItems = appendUniqueProxy(proxyItems, proxySeen, subID+"\x00"+tag, candidate)
					}
					continue
				}

				for _, candidate := range proxies {
					tag, _ := candidate["tag"].(string)
					if tag != proxy.GetTag() || !match(tag) {
						continue
					}
					item["outbounds"] = append(item["outbounds"].([]any), tag)
					proxyItems = appendUniqueProxy(proxyItems, proxySeen, subID+"\x00"+tag, candidate)
					break
				}
			}
		}

		result = append(result, item)
	}

	result = append(result, proxyItems...)
	for _, tag := range builtInTags {
		result = append(result, map[string]any{"type": tag, "tag": tag})
	}

	return result, nil
}

func appendUniqueProxy(items []any, seen map[string]struct{}, key string, proxy map[string]any) []any {
	if _, ok := seen[key]; ok {
		return items
	}
	seen[key] = struct{}{}
	return append(items, deepCopyMap(proxy))
}

func (g *configGenerator) generateRoute(
	route *configv1.Route,
	inbounds []*configv1.Inbound,
	outbounds []*configv1.Outbound,
	dns *configv1.Dns,
) (map[string]any, error) {
	if route == nil {
		return map[string]any{}, nil
	}

	result := map[string]any{
		"rules":                   make([]any, 0),
		"rule_set":                make([]any, 0, len(route.GetRuleSet())),
		"auto_detect_interface":   route.GetAutoDetectInterface(),
		"default_domain_resolver": map[string]any{},
	}

	if route.GetFindProcess() {
		result["find_process"] = true
	}
	if !route.GetAutoDetectInterface() {
		result["default_interface"] = route.GetDefaultInterface()
	}
	if final := getOutboundTag(outbounds, route.GetFinal()); final != "" {
		result["final"] = final
	}
	if server := getDNSServerTag(dns.GetServers(), route.GetDefaultDomainResolver().GetServer()); server != "" {
		result["default_domain_resolver"].(map[string]any)["server"] = server
	}

	for _, rule := range route.GetRules() {
		if rule == nil || !rule.GetEnable() {
			continue
		}
		ruleType, err := ruleTypeString(rule.GetType())
		if err != nil {
			return nil, err
		}
		if ruleType == ruleTypeInsertionPoint {
			continue
		}
		if ruleType == ruleTypeInbound && !isInboundEnabled(inbounds, rule.GetPayload()) {
			continue
		}

		action, err := ruleActionString(rule.GetAction())
		if err != nil {
			return nil, err
		}

		ruleFields, err := generateRuleFields(ruleType, rule.GetPayload(), route.GetRuleSet(), inbounds)
		if err != nil {
			return nil, err
		}
		item := map[string]any{}
		if rule.GetInvert() {
			item["invert"] = true
		}
		deepAssign(item, ruleFields)

		if action != ruleActionInline {
			item["action"] = action
			switch action {
			case ruleActionRoute:
				if outbound := getOutboundTag(outbounds, rule.GetOutbound()); outbound != "" {
					item["outbound"] = outbound
				}
			case ruleActionRouteOptions:
				parsed, err := parseJSONObject(rule.GetOutbound())
				if err != nil {
					return nil, err
				}
				deepAssign(item, parsed)
			case ruleActionReject:
				item["method"] = rule.GetOutbound()
			case ruleActionSniff:
				if len(rule.GetSniffer()) > 0 {
					item["sniffer"] = stringsToAnySlice(rule.GetSniffer())
				}
			case ruleActionResolve:
				if strategy := strategyString(rule.GetStrategy()); strategy != strategyDefault && strategy != "" {
					item["strategy"] = strategy
				}
				if server := getDNSServerTag(dns.GetServers(), rule.GetServer()); server != "" {
					item["server"] = server
				}
			}
		}
		applyInlineInvert(item, ruleType, ruleFields)
		result["rules"] = append(result["rules"].([]any), item)
	}

	for _, ruleset := range route.GetRuleSet() {
		if ruleset == nil {
			continue
		}
		rulesetType, err := rulesetTypeString(ruleset.GetType())
		if err != nil {
			return nil, err
		}
		item := map[string]any{
			"tag":  ruleset.GetTag(),
			"type": rulesetType,
		}
		switch rulesetType {
		case ruleTypeInline:
			parsed, err := parseJSONArray(ruleset.GetRules())
			if err != nil {
				return nil, err
			}
			item["rules"] = parsed
		case "local":
			if meta, ok := g.rulesets[ruleset.GetPath()]; ok && meta.Path != "" {
				item["path"] = strings.Replace(meta.Path, "data/", "../", 1)
			}
			if format := rulesetFormatString(ruleset.GetFormat()); format != "" {
				item["format"] = format
			}
		case "remote":
			item["url"] = ruleset.GetUrl()
			if format := rulesetFormatString(ruleset.GetFormat()); format != "" {
				item["format"] = format
			}
			if detour := getOutboundTag(outbounds, ruleset.GetDownloadDetour()); detour != "" {
				item["download_detour"] = detour
			}
			if ruleset.GetUpdateInterval() != "" {
				item["update_interval"] = ruleset.GetUpdateInterval()
			}
		}
		result["rule_set"] = append(result["rule_set"].([]any), item)
	}

	return result, nil
}

func (g *configGenerator) generateDNS(
	dns *configv1.Dns,
	ruleSet []*configv1.RuleSet,
	inbounds []*configv1.Inbound,
	outbounds []*configv1.Outbound,
) (map[string]any, error) {
	if dns == nil {
		return map[string]any{}, nil
	}

	result := map[string]any{
		"servers":           make([]any, 0, len(dns.GetServers())),
		"rules":             make([]any, 0, len(dns.GetRules())),
		"disable_cache":     dns.GetDisableCache(),
		"disable_expire":    dns.GetDisableExpire(),
		"independent_cache": dns.GetIndependentCache(),
	}
	if strategy := strategyString(dns.GetStrategy()); strategy != strategyDefault && strategy != "" {
		result["strategy"] = strategy
	}
	if dns.GetClientSubnet() != "" {
		result["client_subnet"] = dns.GetClientSubnet()
	}
	if final := getDNSServerTag(dns.GetServers(), dns.GetFinal()); final != "" {
		result["final"] = final
	}

	for _, server := range dns.GetServers() {
		if server == nil {
			continue
		}
		serverType, err := dnsServerTypeString(server.GetType())
		if err != nil {
			return nil, err
		}
		item := map[string]any{
			"tag":  server.GetTag(),
			"type": serverType,
		}

		switch serverType {
		case dnsServerLocal, dnsServerTCP, dnsServerUDP, dnsServerTLS, dnsServerQUIC, dnsServerHTTPS, dnsServerH3:
			if server.GetDetour() != "" {
				if outbound := getOutbound(outbounds, server.GetDetour()); outbound != nil {
					if outboundType, _ := outboundTypeString(outbound.GetType()); outboundType != outboundTypeDirect {
						item["detour"] = outbound.GetTag()
					}
				}
			}
			if resolver := getDNSServerTag(dns.GetServers(), server.GetDomainResolver()); resolver != "" {
				item["domain_resolver"] = resolver
			}
			if serverType == dnsServerTCP || serverType == dnsServerUDP || serverType == dnsServerTLS || serverType == dnsServerQUIC || serverType == dnsServerHTTPS || serverType == dnsServerH3 {
				if server.GetServerPort() != "" {
					port, err := strconv.Atoi(server.GetServerPort())
					if err != nil {
						return nil, invalidArgumentError{message: fmt.Sprintf("invalid dns server port %q", server.GetServerPort())}
					}
					item["server_port"] = port
				}
				item["server"] = server.GetServer()
				if (serverType == dnsServerHTTPS || serverType == dnsServerH3) && server.GetPath() != "" {
					item["path"] = server.GetPath()
				}
			}
		case dnsServerHosts:
			paths := make([]any, 0)
			for _, part := range server.GetHostsPath() {
				for _, segment := range strings.Split(part, ",") {
					paths = append(paths, segment)
				}
			}
			item["path"] = paths
			predefined := map[string]any{}
			for key, value := range server.GetPredefined() {
				entries := make([]any, 0)
				for _, segment := range strings.Split(value, ",") {
					entries = append(entries, segment)
				}
				predefined[key] = entries
			}
			item["predefined"] = predefined
		case dnsServerDHCP:
			if server.GetDetour() != "" {
				if outbound := getOutbound(outbounds, server.GetDetour()); outbound != nil {
					if outboundType, _ := outboundTypeString(outbound.GetType()); outboundType != outboundTypeDirect {
						item["detour"] = outbound.GetTag()
					}
				}
			}
			if resolver := getDNSServerTag(dns.GetServers(), server.GetDomainResolver()); resolver != "" {
				item["domain_resolver"] = resolver
			}
			if server.GetInterface() != "" {
				item["interface"] = server.GetInterface()
			}
		case dnsServerFakeIP:
			if server.GetInet4Range() != "" {
				item["inet4_range"] = server.GetInet4Range()
			}
			if server.GetInet6Range() != "" {
				item["inet6_range"] = server.GetInet6Range()
			}
		}

		result["servers"] = append(result["servers"].([]any), item)
	}

	hasFakeIP := false
	for _, server := range dns.GetServers() {
		if server != nil {
			if serverType, _ := dnsServerTypeString(server.GetType()); serverType == dnsServerFakeIP {
				hasFakeIP = true
				break
			}
		}
	}

	for _, rule := range dns.GetRules() {
		if rule == nil || !rule.GetEnable() {
			continue
		}
		ruleType, err := ruleTypeString(rule.GetType())
		if err != nil {
			return nil, err
		}
		if ruleType == ruleTypeInsertionPoint {
			continue
		}

		action, err := dnsRuleActionString(rule.GetAction())
		if err != nil {
			return nil, err
		}
		ruleFields, err := generateRuleFields(ruleType, rule.GetPayload(), ruleSet, inbounds)
		if err != nil {
			return nil, err
		}
		item := map[string]any{}
		if rule.GetInvert() {
			item["invert"] = true
		}
		if len(rule.GetQueryType()) > 0 {
			item["query_type"] = stringsToAnySlice(rule.GetQueryType())
		}
		deepAssign(item, ruleFields)

		if ruleType == ruleTypeInline {
			_, isFakeIPRule := ruleFields["__is_fake_ip"]
			if isFakeIPRule && !hasFakeIP {
				continue
			}
		}
		if action != ruleActionInline {
			item["action"] = action
			if action == ruleActionRoute || action == ruleActionRouteOptions {
				if rule.GetDisableCache() {
					item["disable_cache"] = true
				}
				if rule.GetClientSubnet() != "" {
					item["client_subnet"] = rule.GetClientSubnet()
				}
				if action == ruleActionRoute {
					if server := getDNSServerTag(dns.GetServers(), rule.GetServer()); server != "" {
						item["server"] = server
					}
				}
			}

			if action == ruleActionRouteOptions || action == ruleActionPredefined {
				parsed, err := parseJSONObject(rule.GetServer())
				if err != nil {
					return nil, err
				}
				deepAssign(item, parsed)
			}

			if action == ruleActionReject {
				item["method"] = rule.GetServer()
			}
		}
		applyInlineInvert(item, ruleType, ruleFields)
		if ruleType == ruleTypeInline {
			delete(item, "__is_fake_ip")
		}

		result["rules"] = append(result["rules"].([]any), item)
	}

	return result, nil
}

func generateRuleFields(
	ruleType string,
	payload string,
	ruleSets []*configv1.RuleSet,
	inbounds []*configv1.Inbound,
) (map[string]any, error) {
	result := map[string]any{}

	switch ruleType {
	case ruleTypeInline:
		parsed, err := parseJSONObject(payload)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	case ruleTypeRuleSet:
		entries := make([]any, 0)
		for _, id := range strings.Split(payload, ",") {
			entries = append(entries, getRulesetTag(ruleSets, id))
		}
		result[ruleType] = entries
	case ruleTypeInbound:
		result[ruleType] = getInboundTag(inbounds, payload)
	case ruleTypeIpIsPrivate, ruleTypeIpAcceptAny:
		result[ruleType] = payload == "true"
	case ruleTypeClashMode:
		result[ruleType] = payload
	default:
		parts := strings.Split(payload, ",")
		values := make([]any, 0, len(parts))
		for _, part := range parts {
			if ruleType == ruleTypePort || ruleType == ruleTypeSourcePort {
				value, err := strconv.Atoi(part)
				if err != nil {
					return nil, invalidArgumentError{message: fmt.Sprintf("invalid numeric rule payload %q", part)}
				}
				values = append(values, value)
				continue
			}
			values = append(values, part)
		}
		if len(values) == 1 {
			result[ruleType] = values[0]
		} else {
			result[ruleType] = values
		}
	}

	return result, nil
}

func applyInlineInvert(item map[string]any, ruleType string, ruleFields map[string]any) {
	if ruleType != ruleTypeInline {
		return
	}
	if inlineInvert, ok := ruleFields["invert"]; ok {
		item["invert"] = deepCopyValue(inlineInvert)
	}
}

func (g *configGenerator) subscriptionEntries(id string) ([]map[string]any, error) {
	if entries, ok := g.subscriptionProxies[id]; ok {
		return entries, nil
	}

	_, ok := g.subscriptions[id]
	if !ok {
		return nil, invalidArgumentError{message: fmt.Sprintf("subscription %q not found", id)}
	}

	bytes, err := os.ReadFile(g.paths.Resolve(subscriptionContentPath(id)))
	if err != nil {
		return nil, fmt.Errorf("read subscription %s: %w", id, err)
	}

	var entries []map[string]any
	if err := json.Unmarshal(bytes, &entries); err != nil {
		return nil, fmt.Errorf("parse subscription %s: %w", id, err)
	}

	g.subscriptionProxies[id] = entries
	return entries, nil
}

func applyMixin(config map[string]any, mixin *configv1.Mixin) (map[string]any, error) {
	if mixin == nil || mixin.GetConfig() == "" {
		return config, nil
	}

	parsed, err := parseMixinDocument(mixin.GetConfig())
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return config, nil
	}

	priority, err := mixinPriorityString(mixin.GetPriority())
	if err != nil {
		return nil, err
	}

	if priority == mixinPriorityGUI {
		merged := deepCopyMap(parsed)
		deepAssign(merged, config)
		return merged, nil
	}

	deepAssign(config, parsed)
	return config, nil
}

func applyScript(config map[string]any, script *configv1.Script) (map[string]any, error) {
	if script == nil || strings.TrimSpace(script.GetCode()) == "" {
		return config, nil
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal config for script runtime: %w", err)
	}

	runtime := goja.New()
	if err := runtime.Set("__CONFIG_JSON__", string(configJSON)); err != nil {
		return nil, fmt.Errorf("set runtime bootstrap config: %w", err)
	}

	if _, err := runtime.RunString("var config = JSON.parse(__CONFIG_JSON__);"); err != nil {
		return nil, invalidArgumentError{message: fmt.Sprintf("bootstrap script runtime failed: %v", err)}
	}

	if _, err := runtime.RunString(script.GetCode()); err != nil {
		return nil, invalidArgumentError{message: fmt.Sprintf("execute script failed: %v", err)}
	}

	onGenerate := runtime.Get("onGenerate")
	fn, ok := goja.AssertFunction(onGenerate)
	if !ok {
		return nil, invalidArgumentError{message: "script must define function onGenerate(config)"}
	}

	currentConfig := runtime.Get("config")
	resultValue, err := fn(goja.Undefined(), currentConfig)
	if err != nil {
		return nil, invalidArgumentError{message: fmt.Sprintf("onGenerate execution failed: %v", err)}
	}

	// Accept two styles:
	// 1) return a config object from onGenerate(config)
	// 2) mutate config in-place without returning an object
	toObject := func(v goja.Value) (map[string]any, bool) {
		if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
			return nil, false
		}
		normalized := normalizeValue(v.Export())
		obj, ok := normalized.(map[string]any)
		return obj, ok
	}

	if resultMap, ok := toObject(resultValue); ok {
		return resultMap, nil
	}

	if runtimeConfig, ok := toObject(runtime.Get("config")); ok {
		return runtimeConfig, nil
	}

	if currentConfigMap, ok := toObject(currentConfig); ok {
		return currentConfigMap, nil
	}

	return nil, invalidArgumentError{message: "onGenerate must return an object or mutate config object"}
}

func parseMixinDocument(content string) (map[string]any, error) {
	var value any
	if err := yaml.Unmarshal([]byte(content), &value); err != nil {
		return nil, invalidArgumentError{message: fmt.Sprintf("invalid mixin config: %v", err)}
	}
	if value == nil {
		return nil, nil
	}
	normalized := normalizeValue(value)
	if object, ok := normalized.(map[string]any); ok {
		return object, nil
	}
	return nil, invalidArgumentError{message: "mixin config must be an object"}
}

func parseJSONObject(raw string) (map[string]any, error) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, invalidArgumentError{message: fmt.Sprintf("invalid json object: %v", err)}
	}
	normalized := normalizeValue(value)
	object, ok := normalized.(map[string]any)
	if !ok {
		return nil, invalidArgumentError{message: "json value must be an object"}
	}
	return object, nil
}

func parseJSONArray(raw string) ([]any, error) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, invalidArgumentError{message: fmt.Sprintf("invalid json array: %v", err)}
	}
	normalized := normalizeValue(value)
	array, ok := normalized.([]any)
	if !ok {
		return nil, invalidArgumentError{message: "json value must be an array"}
	}
	return array, nil
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = normalizeValue(item)
		}
		return result
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[fmt.Sprint(key)] = normalizeValue(item)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, normalizeValue(item))
		}
		return result
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, normalizeValue(item))
		}
		return result
	default:
		return value
	}
}

func deepAssign(dst, src map[string]any) {
	for key, value := range src {
		nextValue := normalizeValue(value)
		if dstMap, ok := dst[key].(map[string]any); ok {
			if srcMap, ok := nextValue.(map[string]any); ok {
				deepAssign(dstMap, srcMap)
				continue
			}
		}
		dst[key] = deepCopyValue(nextValue)
	}
}

func deepCopyMap(src map[string]any) map[string]any {
	result := make(map[string]any, len(src))
	for key, value := range src {
		result[key] = deepCopyValue(value)
	}
	return result
}

func deepCopyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return deepCopyMap(typed)
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, deepCopyValue(item))
		}
		return result
	default:
		return typed
	}
}

func splitUsers(users []string) []any {
	result := make([]any, 0, len(users))
	for _, user := range users {
		parts := strings.SplitN(user, ":", 2)
		username := ""
		password := ""
		if len(parts) > 0 {
			username = parts[0]
		}
		if len(parts) > 1 {
			password = parts[1]
		}
		result = append(result, map[string]any{
			"username": username,
			"password": password,
		})
	}
	return result
}

func createTextMatcher(include, exclude string) func(string) bool {
	includeRegexp := buildSmartRegexp(include)
	excludeRegexp := buildSmartRegexp(exclude)
	return func(text string) bool {
		flag1 := true
		flag2 := false
		if includeRegexp != nil {
			flag1 = includeRegexp.MatchString(text)
		}
		if excludeRegexp != nil {
			flag2 = excludeRegexp.MatchString(text)
		}
		return flag1 && !flag2
	}
}

func buildSmartRegexp(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	compiled, err := regexp.Compile(pattern)
	if err == nil {
		return compiled
	}
	return regexp.MustCompile(regexp.QuoteMeta(pattern))
}

func inboundUserConfigForType(inbound *configv1.Inbound, inboundType string) *configv1.InboundUserConfig {
	switch inboundType {
	case "mixed":
		return inbound.GetMixed()
	case "socks":
		return inbound.GetSocks()
	case "http":
		return inbound.GetHttp()
	default:
		return nil
	}
}

func getOutbound(outbounds []*configv1.Outbound, id string) *configv1.Outbound {
	for _, outbound := range outbounds {
		if outbound != nil && outbound.GetId() == id {
			return outbound
		}
	}
	return nil
}

func getOutboundTag(outbounds []*configv1.Outbound, id string) string {
	if outbound := getOutbound(outbounds, id); outbound != nil {
		return outbound.GetTag()
	}
	return ""
}

func getDNSServerTag(servers []*configv1.DnsServer, id string) string {
	for _, server := range servers {
		if server != nil && server.GetId() == id {
			return server.GetTag()
		}
	}
	return ""
}

func getInboundTag(inbounds []*configv1.Inbound, id string) any {
	for _, inbound := range inbounds {
		if inbound != nil && inbound.GetId() == id {
			return inbound.GetTag()
		}
	}
	return nil
}

func isInboundEnabled(inbounds []*configv1.Inbound, id string) bool {
	for _, inbound := range inbounds {
		if inbound != nil && inbound.GetId() == id {
			return inbound.GetEnable()
		}
	}
	return false
}

func getRulesetTag(ruleSets []*configv1.RuleSet, id string) any {
	for _, ruleset := range ruleSets {
		if ruleset != nil && ruleset.GetId() == id {
			return ruleset.GetTag()
		}
	}
	return nil
}

func stringsToAnySlice(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func generateDNSServiceURL(dnsServer *configv1.DnsServer) (string, error) {
	if dnsServer == nil {
		return "", invalidArgumentError{message: "dns_server is required"}
	}

	serverType, err := dnsServerTypeString(dnsServer.GetType())
	if err != nil {
		return "", err
	}

	suffix := ""
	if dnsServer.GetServerPort() != "" {
		suffix = ":" + dnsServer.GetServerPort()
	}

	switch serverType {
	case dnsServerHTTPS:
		return "https://" + dnsServer.GetServer() + suffix + dnsServer.GetPath(), nil
	case dnsServerH3:
		return "h3://" + dnsServer.GetServer() + suffix + dnsServer.GetPath(), nil
	case dnsServerDHCP:
		return "dhcp://" + dnsServer.GetInterface(), nil
	case dnsServerFakeIP:
		value := "fake-ip://"
		if dnsServer.GetInet4Range() != "" {
			value += dnsServer.GetInet4Range()
		}
		if dnsServer.GetInet6Range() != "" {
			if dnsServer.GetInet4Range() != "" {
				value += ","
			}
			value += dnsServer.GetInet6Range()
		}
		return value, nil
	case dnsServerHosts:
		return dnsServerHosts, nil
	case dnsServerLocal:
		return dnsServerLocal, nil
	default:
		return serverType + "://" + dnsServer.GetServer() + suffix, nil
	}
}

func logLevelString(level configv1.LogLevel) string {
	switch level {
	case configv1.LogLevel_LOG_LEVEL_TRACE:
		return logLevelTrace
	case configv1.LogLevel_LOG_LEVEL_DEBUG:
		return logLevelDebug
	case configv1.LogLevel_LOG_LEVEL_INFO:
		return logLevelInfo
	case configv1.LogLevel_LOG_LEVEL_WARN:
		return "warn"
	case configv1.LogLevel_LOG_LEVEL_ERROR:
		return "error"
	case configv1.LogLevel_LOG_LEVEL_FATAL:
		return "fatal"
	case configv1.LogLevel_LOG_LEVEL_PANIC:
		return "panic"
	default:
		return ""
	}
}

func inboundTypeString(inboundType configv1.InboundType) (string, error) {
	switch inboundType {
	case configv1.InboundType_INBOUND_TYPE_MIXED:
		return "mixed", nil
	case configv1.InboundType_INBOUND_TYPE_SOCKS:
		return "socks", nil
	case configv1.InboundType_INBOUND_TYPE_HTTP:
		return "http", nil
	case configv1.InboundType_INBOUND_TYPE_TUN:
		return inboundTypeTun, nil
	case configv1.InboundType_INBOUND_TYPE_DIRECT:
		return inboundTypeDirect, nil
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported inbound type: %v", inboundType)}
	}
}

func inboundNetworkString(network configv1.InboundNetwork) string {
	switch network {
	case configv1.InboundNetwork_INBOUND_NETWORK_TCP:
		return inboundNetworkTCP
	case configv1.InboundNetwork_INBOUND_NETWORK_UDP:
		return inboundNetworkUDP
	default:
		return inboundNetworkUDP
	}
}

func outboundTypeString(outboundType configv1.OutboundType) (string, error) {
	switch outboundType {
	case configv1.OutboundType_OUTBOUND_TYPE_DIRECT:
		return outboundTypeDirect, nil
	case configv1.OutboundType_OUTBOUND_TYPE_BLOCK:
		return outboundTypeBlock, nil
	case configv1.OutboundType_OUTBOUND_TYPE_SELECTOR:
		return outboundTypeSelector, nil
	case configv1.OutboundType_OUTBOUND_TYPE_URLTEST:
		return outboundTypeURLTest, nil
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported outbound type: %v", outboundType)}
	}
}

func tunStackString(stack configv1.TunStack) string {
	switch stack {
	case configv1.TunStack_TUN_STACK_SYSTEM:
		return "system"
	case configv1.TunStack_TUN_STACK_GVISOR:
		return "gvisor"
	case configv1.TunStack_TUN_STACK_MIXED:
		return "mixed"
	default:
		return ""
	}
}

func rulesetTypeString(rulesetType configv1.RulesetType) (string, error) {
	switch rulesetType {
	case configv1.RulesetType_RULESET_TYPE_INLINE:
		return ruleTypeInline, nil
	case configv1.RulesetType_RULESET_TYPE_LOCAL:
		return "local", nil
	case configv1.RulesetType_RULESET_TYPE_REMOTE:
		return "remote", nil
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported ruleset type: %v", rulesetType)}
	}
}

func rulesetFormatString(format configv1.RulesetFormat) string {
	switch format {
	case configv1.RulesetFormat_RULESET_FORMAT_SOURCE:
		return "source"
	case configv1.RulesetFormat_RULESET_FORMAT_BINARY:
		return "binary"
	default:
		return ""
	}
}

func ruleTypeString(ruleType configv1.RuleType) (string, error) {
	switch ruleType {
	case configv1.RuleType_RULE_TYPE_INBOUND:
		return ruleTypeInbound, nil
	case configv1.RuleType_RULE_TYPE_NETWORK:
		return "network", nil
	case configv1.RuleType_RULE_TYPE_PROTOCOL:
		return "protocol", nil
	case configv1.RuleType_RULE_TYPE_DOMAIN:
		return "domain", nil
	case configv1.RuleType_RULE_TYPE_DOMAIN_SUFFIX:
		return "domain_suffix", nil
	case configv1.RuleType_RULE_TYPE_DOMAIN_KEYWORD:
		return "domain_keyword", nil
	case configv1.RuleType_RULE_TYPE_DOMAIN_REGEX:
		return "domain_regex", nil
	case configv1.RuleType_RULE_TYPE_SOURCE_IP_CIDR:
		return "source_ip_cidr", nil
	case configv1.RuleType_RULE_TYPE_IP_CIDR:
		return "ip_cidr", nil
	case configv1.RuleType_RULE_TYPE_IP_IS_PRIVATE:
		return ruleTypeIpIsPrivate, nil
	case configv1.RuleType_RULE_TYPE_SOURCE_PORT:
		return ruleTypeSourcePort, nil
	case configv1.RuleType_RULE_TYPE_SOURCE_PORT_RANGE:
		return "source_port_range", nil
	case configv1.RuleType_RULE_TYPE_PORT:
		return ruleTypePort, nil
	case configv1.RuleType_RULE_TYPE_PORT_RANGE:
		return "port_range", nil
	case configv1.RuleType_RULE_TYPE_PROCESS_NAME:
		return "process_name", nil
	case configv1.RuleType_RULE_TYPE_PROCESS_PATH:
		return "process_path", nil
	case configv1.RuleType_RULE_TYPE_PROCESS_PATH_REGEX:
		return "process_path_regex", nil
	case configv1.RuleType_RULE_TYPE_CLASH_MODE:
		return ruleTypeClashMode, nil
	case configv1.RuleType_RULE_TYPE_RULE_SET:
		return ruleTypeRuleSet, nil
	case configv1.RuleType_RULE_TYPE_IP_ACCEPT_ANY:
		return ruleTypeIpAcceptAny, nil
	case configv1.RuleType_RULE_TYPE_INLINE:
		return ruleTypeInline, nil
	case configv1.RuleType_RULE_TYPE_INSERTION_POINT:
		return ruleTypeInsertionPoint, nil
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported rule type: %v", ruleType)}
	}
}

func strategyString(strategy configv1.Strategy) string {
	switch strategy {
	case configv1.Strategy_STRATEGY_DEFAULT:
		return strategyDefault
	case configv1.Strategy_STRATEGY_PREFER_IPV4:
		return "prefer_ipv4"
	case configv1.Strategy_STRATEGY_PREFER_IPV6:
		return "prefer_ipv6"
	case configv1.Strategy_STRATEGY_IPV4_ONLY:
		return "ipv4_only"
	case configv1.Strategy_STRATEGY_IPV6_ONLY:
		return "ipv6_only"
	default:
		return ""
	}
}

func dnsServerTypeString(serverType configv1.DnsServerType) (string, error) {
	switch serverType {
	case configv1.DnsServerType_DNS_SERVER_TYPE_LOCAL:
		return dnsServerLocal, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_HOSTS:
		return dnsServerHosts, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_TCP:
		return dnsServerTCP, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_UDP:
		return dnsServerUDP, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_TLS:
		return dnsServerTLS, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_HTTPS:
		return dnsServerHTTPS, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_QUIC:
		return dnsServerQUIC, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_H3:
		return dnsServerH3, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_DHCP:
		return dnsServerDHCP, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_FAKEIP:
		return dnsServerFakeIP, nil
	case configv1.DnsServerType_DNS_SERVER_TYPE_TAILSCALE:
		return dnsServerTailscale, nil
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported dns server type: %v", serverType)}
	}
}

func ruleActionString(action configv1.RuleAction) (string, error) {
	switch action {
	case configv1.RuleAction_RULE_ACTION_ROUTE:
		return ruleActionRoute, nil
	case configv1.RuleAction_RULE_ACTION_ROUTE_OPTIONS:
		return ruleActionRouteOptions, nil
	case configv1.RuleAction_RULE_ACTION_REJECT:
		return ruleActionReject, nil
	case configv1.RuleAction_RULE_ACTION_HIJACK_DNS:
		return "hijack-dns", nil
	case configv1.RuleAction_RULE_ACTION_SNIFF:
		return ruleActionSniff, nil
	case configv1.RuleAction_RULE_ACTION_RESOLVE:
		return ruleActionResolve, nil
	case configv1.RuleAction_RULE_ACTION_INLINE:
		return ruleActionInline, nil
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported rule action: %v", action)}
	}
}

func dnsRuleActionString(action configv1.DnsRuleAction) (string, error) {
	switch action {
	case configv1.DnsRuleAction_DNS_RULE_ACTION_ROUTE:
		return ruleActionRoute, nil
	case configv1.DnsRuleAction_DNS_RULE_ACTION_ROUTE_OPTIONS:
		return ruleActionRouteOptions, nil
	case configv1.DnsRuleAction_DNS_RULE_ACTION_REJECT:
		return ruleActionReject, nil
	case configv1.DnsRuleAction_DNS_RULE_ACTION_PREDEFINED:
		return ruleActionPredefined, nil
	case configv1.DnsRuleAction_DNS_RULE_ACTION_INLINE:
		return ruleActionInline, nil
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported dns rule action: %v", action)}
	}
}

func mixinPriorityString(priority configv1.MixinPriority) (string, error) {
	switch priority {
	case configv1.MixinPriority_MIXIN_PRIORITY_MIXIN:
		return mixinPriorityMixin, nil
	case configv1.MixinPriority_MIXIN_PRIORITY_GUI:
		return mixinPriorityGUI, nil
	default:
		return "", invalidArgumentError{message: fmt.Sprintf("unsupported mixin priority: %v", priority)}
	}
}
