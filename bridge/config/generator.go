package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
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
	ruleActionBypass       = "bypass"
	ruleActionRouteOptions = "route-options"
	ruleActionReject       = "reject"
	ruleActionSniff        = "sniff"
	ruleActionResolve      = "resolve"
	ruleActionPredefined   = "predefined"
	ruleActionEvaluate     = "evaluate"
	ruleActionRespond      = "respond"
	ruleActionInline       = "inline"

	outboundTypeDirect   = "direct"
	outboundTypeBridge   = "bridge"
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
	ID      string                  `yaml:"id"`
	Proxies []subscriptionProxyMeta `yaml:"proxies"`
}

type subscriptionProxyMeta struct {
	ID  string `yaml:"id"`
	Tag string `yaml:"tag"`
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
		"inbounds":     generateInbounds(profile.GetInbounds(), runtime.GOOS),
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

func generateInbounds(inbounds []*configv1.Inbound, platformOS string) []any {
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
		if platformOS == "linux" && tun.GetAutoRoute() {
			item["auto_redirect"] = tun.GetAutoRedirect()
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

	for outboundIndex, outbound := range outbounds {
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
		if outboundType == outboundTypeBridge {
			if outbound.GetInterface() != "" {
				item["interface"] = outbound.GetInterface()
			}
			if outbound.GetBridgeName() != "" {
				item["bridge_name"] = outbound.GetBridgeName()
			}
		}

		if outboundType == outboundTypeSelector || outboundType == outboundTypeURLTest {
			item["interrupt_exist_connections"] = outbound.GetInterruptExistConnections()
			item["outbounds"] = make([]any, 0)
			match := createTextMatcher(outbound.GetInclude(), outbound.GetExclude())

			for proxyIndex, proxy := range outbound.GetOutbounds() {
				if proxy == nil {
					continue
				}
				path := fmt.Sprintf("outbounds[%d].outbounds[%d]", outboundIndex, proxyIndex)

				if proxy.GetType() == "Built-in" {
					if proxy.GetId() == outboundTypeDirect || proxy.GetId() == outboundTypeBlock {
						if _, ok := builtInSeen[proxy.GetId()]; !ok {
							builtInSeen[proxy.GetId()] = struct{}{}
							builtInTags = append(builtInTags, proxy.GetId())
						}
						item["outbounds"] = append(item["outbounds"].([]any), proxy.GetId())
						continue
					}
					tag, err := resolveOutboundTag(outbounds, proxy.GetId(), path+".id")
					if err != nil {
						return nil, err
					}
					item["outbounds"] = append(item["outbounds"].([]any), tag)
					continue
				}

				subID := proxy.GetType()
				if proxy.GetType() == "Subscription" {
					subID = proxy.GetId()
				}

				proxies, err := g.subscriptionEntries(subID)
				if err != nil {
					return nil, invalidArgumentError{message: fmt.Sprintf("%s.id: %v", path, err)}
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

				nodeTag, err := g.resolveSubscriptionProxyTag(subID, proxy.GetId(), path+".id")
				if err != nil {
					return nil, err
				}
				matched := false
				for _, candidate := range proxies {
					tag, _ := candidate["tag"].(string)
					if tag != nodeTag || !match(tag) {
						continue
					}
					item["outbounds"] = append(item["outbounds"].([]any), tag)
					proxyItems = appendUniqueProxy(proxyItems, proxySeen, subID+"\x00"+tag, candidate)
					matched = true
					break
				}
				if !matched {
					return nil, invalidArgumentError{message: fmt.Sprintf("%s references subscription node %q whose generated outbound is unavailable", path+".id", proxy.GetId())}
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
	final, err := resolveOutboundTag(outbounds, route.GetFinal(), "route.final")
	if err != nil {
		return nil, err
	}
	if final != "" {
		result["final"] = final
	}
	server, err := resolveDNSServerTag(dns.GetServers(), route.GetDefaultDomainResolver().GetServer(), "route.default_domain_resolver.server")
	if err != nil {
		return nil, err
	}
	if server != "" {
		result["default_domain_resolver"].(map[string]any)["server"] = server
	}

	for ruleIndex, rule := range route.GetRules() {
		if rule == nil || !rule.GetEnable() {
			continue
		}
		if rule.GetId() == ruleTypeInsertionPoint {
			continue
		}

		action, err := ruleActionString(rule.GetAction())
		if err != nil {
			return nil, err
		}

		path := fmt.Sprintf("route.rules[%d]", ruleIndex)
		item, err := generateRouteRuleMatchFields(rule, route.GetRuleSet(), inbounds, path)
		if err != nil {
			return nil, err
		}
		if rule.GetInvert() {
			item["invert"] = true
		}

		if action != ruleActionInline {
			item["action"] = action
			if err := applyRouteRuleActionOptions(item, action, rule.GetActionOptions(), outbounds, dns.GetServers(), path); err != nil {
				return nil, err
			}
		}

		if raw := strings.TrimSpace(rule.GetRaw()); raw != "" {
			parsed, err := parseJSONObject(raw)
			if err != nil {
				return nil, invalidArgumentError{message: fmt.Sprintf("%s.raw: %v", path, err)}
			}
			deepAssign(item, parsed)
		} else if action == ruleActionInline {
			return nil, invalidArgumentError{message: path + ".raw is required for inline action"}
		}

		if err := validateGeneratedRouteRule(item, path, action == ruleActionInline); err != nil {
			return nil, err
		}
		result["rules"] = append(result["rules"].([]any), item)
	}

	for rulesetIndex, ruleset := range route.GetRuleSet() {
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
			detour, err := resolveOutboundTag(outbounds, ruleset.GetDownloadDetour(), fmt.Sprintf("route.rule_set[%d].download_detour", rulesetIndex))
			if err != nil {
				return nil, err
			}
			if detour != "" {
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

func generateRouteRuleMatchFields(
	rule *configv1.RouteRule,
	ruleSets []*configv1.RuleSet,
	inbounds []*configv1.Inbound,
	path string,
) (map[string]any, error) {
	item := map[string]any{}

	if len(rule.GetInbound()) > 0 {
		tags := make([]any, 0, len(rule.GetInbound()))
		for index, id := range rule.GetInbound() {
			tag, err := resolveEnabledInboundTag(inbounds, id, fmt.Sprintf("%s.inbound[%d]", path, index))
			if err != nil {
				return nil, err
			}
			tags = append(tags, tag)
		}
		item["inbound"] = tags
	}

	if ipVersion := rule.GetIpVersion(); ipVersion != 0 {
		if ipVersion != 4 && ipVersion != 6 {
			return nil, invalidArgumentError{message: fmt.Sprintf("%s.ip_version must be 4 or 6", path)}
		}
		item["ip_version"] = ipVersion
	}

	assignStrings := func(key string, values []string) {
		if len(values) > 0 {
			item[key] = stringsToAnySlice(values)
		}
	}
	assignStrings("network", rule.GetNetwork())
	assignStrings("preferred_by", rule.GetPreferredBy())
	assignStrings("protocol", rule.GetProtocol())
	assignStrings("domain", rule.GetDomain())
	assignStrings("domain_suffix", rule.GetDomainSuffix())
	assignStrings("domain_keyword", rule.GetDomainKeyword())
	assignStrings("domain_regex", rule.GetDomainRegex())
	assignStrings("ip_cidr", rule.GetIpCidr())
	assignStrings("source_ip_cidr", rule.GetSourceIpCidr())
	assignStrings("source_port_range", rule.GetSourcePortRange())
	assignStrings("port_range", rule.GetPortRange())
	assignStrings("process_name", rule.GetProcessName())
	assignStrings("process_path", rule.GetProcessPath())
	assignStrings("process_path_regex", rule.GetProcessPathRegex())

	if rule.GetSourceIpIsPrivate() {
		item["source_ip_is_private"] = true
	}
	if rule.GetIpIsPrivate() {
		item["ip_is_private"] = true
	}
	if len(rule.GetSourcePort()) > 0 {
		ports, err := routePortsToAnySlice(rule.GetSourcePort(), path+".source_port")
		if err != nil {
			return nil, err
		}
		item["source_port"] = ports
	}
	if len(rule.GetPort()) > 0 {
		ports, err := routePortsToAnySlice(rule.GetPort(), path+".port")
		if err != nil {
			return nil, err
		}
		item["port"] = ports
	}
	if clashMode := rule.GetClashMode(); clashMode != "" {
		item["clash_mode"] = clashMode
	}
	if len(rule.GetRuleSet()) > 0 {
		tags := make([]any, 0, len(rule.GetRuleSet()))
		for index, id := range rule.GetRuleSet() {
			tag, err := resolveRuleSetTag(ruleSets, id, fmt.Sprintf("%s.rule_set[%d]", path, index))
			if err != nil {
				return nil, err
			}
			tags = append(tags, tag)
		}
		item["rule_set"] = tags
	}

	return item, nil
}

func routePortsToAnySlice(values []uint32, path string) ([]any, error) {
	result := make([]any, 0, len(values))
	for index, value := range values {
		if value > 65535 {
			return nil, invalidArgumentError{message: fmt.Sprintf("%s[%d] must be between 0 and 65535", path, index)}
		}
		result = append(result, value)
	}
	return result, nil
}

func applyRouteRuleActionOptions(
	item map[string]any,
	action string,
	options *configv1.ActionOptions,
	outbounds []*configv1.Outbound,
	dnsServers []*configv1.DnsServer,
	path string,
) error {
	if options == nil {
		options = &configv1.ActionOptions{}
	}

	switch action {
	case ruleActionRoute, ruleActionBypass:
		outbound, err := resolveOutboundTag(outbounds, options.GetOutbound(), path+".action_options.outbound")
		if err != nil {
			return err
		}
		if outbound != "" {
			item["outbound"] = outbound
		}
		return applyRouteOptions(item, options, path)
	case ruleActionRouteOptions:
		return applyRouteOptions(item, options, path)
	case ruleActionReject:
		method := options.GetMethod()
		if method == "" {
			method = "default"
		}
		if method != "default" && method != "drop" && method != "reply" {
			return invalidArgumentError{message: fmt.Sprintf("%s.action_options.method has unsupported value %q", path, method)}
		}
		if method == "drop" && options.GetNoDrop() {
			return invalidArgumentError{message: path + ".action_options.no_drop is unavailable when method is drop"}
		}
		item["method"] = method
		if options.GetNoDrop() {
			item["no_drop"] = true
		}
	case "hijack-dns":
	case ruleActionSniff:
		if len(options.GetSniffer()) > 0 {
			item["sniffer"] = stringsToAnySlice(options.GetSniffer())
		}
		if options.GetTimeout() != "" {
			item["timeout"] = options.GetTimeout()
		}
	case ruleActionResolve:
		server, err := resolveDNSServerTag(dnsServers, options.GetServer(), path+".action_options.server")
		if err != nil {
			return err
		}
		if server != "" {
			item["server"] = server
		}
		if options.GetStrategy() != "" {
			item["strategy"] = options.GetStrategy()
		}
		if options.GetDisableCache() {
			item["disable_cache"] = true
		}
		if options.GetDisableOptimisticCache() {
			item["disable_optimistic_cache"] = true
		}
		if options.RewriteTtl != nil {
			item["rewrite_ttl"] = options.GetRewriteTtl()
		}
		if options.GetTimeout() != "" {
			item["timeout"] = options.GetTimeout()
		}
		if options.GetClientSubnet() != "" {
			item["client_subnet"] = options.GetClientSubnet()
		}
	}
	return nil
}

func applyRouteOptions(item map[string]any, options *configv1.ActionOptions, path string) error {
	if options.GetOverridePort() > 65535 {
		return invalidArgumentError{message: path + ".action_options.override_port must be between 0 and 65535"}
	}
	if options.GetTlsFragment() && options.GetTlsRecordFragment() {
		return invalidArgumentError{message: path + ".action_options.tls_fragment and tls_record_fragment are mutually exclusive"}
	}
	assignString := func(key, value string) {
		if value != "" {
			item[key] = value
		}
	}
	assignString("override_address", options.GetOverrideAddress())
	if options.GetOverridePort() != 0 {
		item["override_port"] = options.GetOverridePort()
	}
	assignString("network_strategy", options.GetNetworkStrategy())
	if len(options.GetNetworkType()) > 0 {
		item["network_type"] = stringsToAnySlice(options.GetNetworkType())
	}
	if len(options.GetFallbackNetworkType()) > 0 {
		item["fallback_network_type"] = stringsToAnySlice(options.GetFallbackNetworkType())
	}
	assignString("fallback_delay", options.GetFallbackDelay())
	if options.GetUdpDisableDomainUnmapping() {
		item["udp_disable_domain_unmapping"] = true
	}
	if options.GetUdpConnect() {
		item["udp_connect"] = true
	}
	assignString("udp_timeout", options.GetUdpTimeout())
	if options.GetTlsFragment() {
		item["tls_fragment"] = true
	}
	assignString("tls_fragment_fallback_delay", options.GetTlsFragmentFallbackDelay())
	if options.GetTlsRecordFragment() {
		item["tls_record_fragment"] = true
	}
	assignString("tls_spoof", options.GetTlsSpoof())
	assignString("tls_spoof_method", options.GetTlsSpoofMethod())
	return nil
}

func validateGeneratedRouteRule(item map[string]any, path string, inline bool) error {
	action, _ := item["action"].(string)
	if inline && strings.TrimSpace(action) == "" {
		return invalidArgumentError{message: path + ".raw.action is required for inline action"}
	}
	if action == "" {
		action = ruleActionRoute
	}
	if action == ruleActionRoute {
		outbound, _ := item["outbound"].(string)
		if outbound == "" {
			return invalidArgumentError{message: path + ".outbound is required for route action"}
		}
	}
	return nil
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
	final, err := resolveDNSServerTag(dns.GetServers(), dns.GetFinal(), "dns.final")
	if err != nil {
		return nil, err
	}
	if final != "" {
		result["final"] = final
	}

	for serverIndex, server := range dns.GetServers() {
		if server == nil {
			continue
		}
		path := fmt.Sprintf("dns.servers[%d]", serverIndex)
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
			detour, err := resolveOutboundTag(outbounds, server.GetDetour(), path+".detour")
			if err != nil {
				return nil, err
			}
			if detour != "" {
				item["detour"] = detour
			}
			resolver, err := resolveDNSServerTag(dns.GetServers(), server.GetDomainResolver(), path+".domain_resolver")
			if err != nil {
				return nil, err
			}
			if resolver != "" {
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
			detour, err := resolveOutboundTag(outbounds, server.GetDetour(), path+".detour")
			if err != nil {
				return nil, err
			}
			if detour != "" {
				item["detour"] = detour
			}
			resolver, err := resolveDNSServerTag(dns.GetServers(), server.GetDomainResolver(), path+".domain_resolver")
			if err != nil {
				return nil, err
			}
			if resolver != "" {
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

	for ruleIndex, rule := range dns.GetRules() {
		if rule == nil || !rule.GetEnable() {
			continue
		}
		if rule.GetId() == ruleTypeInsertionPoint {
			continue
		}
		path := fmt.Sprintf("dns.rules[%d]", ruleIndex)
		action, err := dnsRuleActionString(rule.GetAction())
		if err != nil {
			return nil, err
		}
		var rawFields map[string]any
		if raw := strings.TrimSpace(rule.GetRaw()); raw != "" {
			rawFields, err = parseJSONObject(raw)
			if err != nil {
				return nil, invalidArgumentError{message: fmt.Sprintf("%s.raw: %v", path, err)}
			}
			if isFakeIPRule, _ := rawFields["__is_fake_ip"].(bool); isFakeIPRule && !hasFakeIP {
				continue
			}
		} else if action == ruleActionInline {
			return nil, invalidArgumentError{message: path + ".raw is required for inline action"}
		}
		item, err := generateDNSRuleMatchFields(rule, ruleSet, inbounds, path)
		if err != nil {
			return nil, err
		}
		if rule.GetInvert() {
			item["invert"] = true
		}
		if action != ruleActionInline {
			item["action"] = action
			if err := applyDNSRuleActionOptions(item, action, rule.GetActionOptions(), dns.GetServers(), path); err != nil {
				return nil, err
			}
		}

		if rawFields != nil {
			deepAssign(item, rawFields)
		}

		if isFakeIPRule, _ := item["__is_fake_ip"].(bool); isFakeIPRule && !hasFakeIP {
			continue
		}
		delete(item, "__is_fake_ip")

		if err := validateGeneratedDNSRule(item, path, action == ruleActionInline); err != nil {
			return nil, err
		}

		result["rules"] = append(result["rules"].([]any), item)
	}

	return result, nil
}

func generateDNSRuleMatchFields(
	rule *configv1.DnsRule,
	ruleSets []*configv1.RuleSet,
	inbounds []*configv1.Inbound,
	path string,
) (map[string]any, error) {
	item := map[string]any{}

	if len(rule.GetInbound()) > 0 {
		tags := make([]any, 0, len(rule.GetInbound()))
		for index, id := range rule.GetInbound() {
			tag, err := resolveEnabledInboundTag(inbounds, id, fmt.Sprintf("%s.inbound[%d]", path, index))
			if err != nil {
				return nil, err
			}
			tags = append(tags, tag)
		}
		item["inbound"] = tags
	}

	if clashMode := rule.GetClashMode(); clashMode != "" {
		item["clash_mode"] = clashMode
	}
	if ipVersion := rule.GetIpVersion(); ipVersion != 0 {
		if ipVersion != 4 && ipVersion != 6 {
			return nil, invalidArgumentError{message: fmt.Sprintf("%s.ip_version must be 4 or 6", path)}
		}
		item["ip_version"] = ipVersion
	}
	if len(rule.GetQueryType()) > 0 {
		queryTypes, err := dnsQueryTypesToAnySlice(rule.GetQueryType(), path+".query_type")
		if err != nil {
			return nil, err
		}
		if len(queryTypes) > 0 {
			item["query_type"] = queryTypes
		}
	}

	assignStrings := func(key string, values []string) {
		if len(values) > 0 {
			item[key] = stringsToAnySlice(values)
		}
	}
	assignStrings("network", rule.GetNetwork())
	assignStrings("protocol", rule.GetProtocol())
	assignStrings("preferred_by", rule.GetPreferredBy())
	assignStrings("domain", rule.GetDomain())
	assignStrings("domain_suffix", rule.GetDomainSuffix())
	assignStrings("domain_keyword", rule.GetDomainKeyword())
	assignStrings("domain_regex", rule.GetDomainRegex())
	assignStrings("ip_cidr", rule.GetIpCidr())
	assignStrings("response_answer", rule.GetResponseAnswer())
	assignStrings("response_ns", rule.GetResponseNs())
	assignStrings("response_extra", rule.GetResponseExtra())
	assignStrings("process_name", rule.GetProcessName())
	assignStrings("process_path", rule.GetProcessPath())
	assignStrings("process_path_regex", rule.GetProcessPathRegex())

	if len(rule.GetRuleSet()) > 0 {
		tags := make([]any, 0, len(rule.GetRuleSet()))
		for index, id := range rule.GetRuleSet() {
			tag, err := resolveRuleSetTag(ruleSets, id, fmt.Sprintf("%s.rule_set[%d]", path, index))
			if err != nil {
				return nil, err
			}
			tags = append(tags, tag)
		}
		item["rule_set"] = tags
	}
	if rule.GetRuleSetIpCidrMatchSource() {
		item["rule_set_ip_cidr_match_source"] = true
	}
	if rule.GetMatchResponse() {
		item["match_response"] = true
	}
	if rule.GetIpAcceptAny() {
		item["ip_accept_any"] = true
	}
	if rule.GetIpIsPrivate() {
		item["ip_is_private"] = true
	}
	if responseRcode := rule.GetResponseRcode(); responseRcode != "" {
		item["response_rcode"] = responseRcode
	}

	return item, nil
}

func dnsQueryTypesToAnySlice(values []string, path string) ([]any, error) {
	result := make([]any, 0, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if isDecimalString(value) {
			number, err := strconv.ParseUint(value, 10, 16)
			if err != nil {
				return nil, invalidArgumentError{message: fmt.Sprintf("%s[%d] must be between 0 and 65535", path, index)}
			}
			result = append(result, number)
			continue
		}
		result = append(result, value)
	}
	return result, nil
}

func isDecimalString(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func applyDNSRuleActionOptions(
	item map[string]any,
	action string,
	options *configv1.DnsActionOptions,
	dnsServers []*configv1.DnsServer,
	path string,
) error {
	if options == nil {
		options = &configv1.DnsActionOptions{}
	}

	applyQueryOptions := func() {
		if options.GetDisableCache() {
			item["disable_cache"] = true
		}
		if options.GetDisableOptimisticCache() {
			item["disable_optimistic_cache"] = true
		}
		if options.RewriteTtl != nil {
			item["rewrite_ttl"] = options.GetRewriteTtl()
		}
		if options.GetTimeout() != "" {
			item["timeout"] = options.GetTimeout()
		}
		if options.GetClientSubnet() != "" {
			item["client_subnet"] = options.GetClientSubnet()
		}
	}

	switch action {
	case ruleActionRoute, ruleActionEvaluate:
		server, err := resolveDNSServerTag(dnsServers, options.GetServer(), path+".action_options.server")
		if err != nil {
			return err
		}
		if server != "" {
			item["server"] = server
		}
		applyQueryOptions()
	case ruleActionRouteOptions:
		applyQueryOptions()
	case ruleActionRespond:
	case ruleActionReject:
		method := options.GetMethod()
		if method == "" {
			method = "default"
		}
		if method != "default" && method != "drop" {
			return invalidArgumentError{message: fmt.Sprintf("%s.action_options.method has unsupported value %q", path, method)}
		}
		if method == "drop" && options.GetNoDrop() {
			return invalidArgumentError{message: path + ".action_options.no_drop is unavailable when method is drop"}
		}
		item["method"] = method
		if options.GetNoDrop() {
			item["no_drop"] = true
		}
	case ruleActionPredefined:
		rcode := options.GetRcode()
		if rcode == "" {
			rcode = "NOERROR"
		}
		item["rcode"] = rcode
		if len(options.GetAnswer()) > 0 {
			item["answer"] = stringsToAnySlice(options.GetAnswer())
		}
		if len(options.GetNs()) > 0 {
			item["ns"] = stringsToAnySlice(options.GetNs())
		}
		if len(options.GetExtra()) > 0 {
			item["extra"] = stringsToAnySlice(options.GetExtra())
		}
	}
	return nil
}

func validateGeneratedDNSRule(item map[string]any, path string, inline bool) error {
	action, _ := item["action"].(string)
	if inline && strings.TrimSpace(action) == "" {
		return invalidArgumentError{message: path + ".raw.action is required for inline action"}
	}
	if action == ruleActionRoute || action == ruleActionEvaluate {
		server, _ := item["server"].(string)
		if server == "" {
			return invalidArgumentError{message: fmt.Sprintf("%s.server is required for %s action", path, action)}
		}
	}
	if action == ruleActionReject {
		method, _ := item["method"].(string)
		if method == "" {
			method = "default"
		}
		if method != "default" && method != "drop" {
			return invalidArgumentError{message: fmt.Sprintf("%s.method has unsupported value %q", path, method)}
		}
		noDrop, _ := item["no_drop"].(bool)
		if method == "drop" && noDrop {
			return invalidArgumentError{message: path + ".no_drop is unavailable when method is drop"}
		}
	}
	if hasDNSResponseMatchFields(item) {
		matchResponse, _ := item["match_response"].(bool)
		if !matchResponse {
			return invalidArgumentError{message: path + ".match_response must be true when response match fields are configured"}
		}
	}
	return nil
}

func hasDNSResponseMatchFields(item map[string]any) bool {
	for _, key := range []string{"ip_accept_any", "ip_cidr", "ip_is_private", "response_rcode", "response_answer", "response_ns", "response_extra"} {
		value, exists := item[key]
		if !exists {
			continue
		}
		switch typed := value.(type) {
		case bool:
			if typed {
				return true
			}
		case string:
			if strings.TrimSpace(typed) != "" {
				return true
			}
		case []any:
			if len(typed) > 0 {
				return true
			}
		case []string:
			if len(typed) > 0 {
				return true
			}
		}
	}
	return false
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

func (g *configGenerator) resolveSubscriptionProxyTag(subscriptionID string, proxyID string, path string) (string, error) {
	if proxyID == "" {
		return "", invalidArgumentError{message: path + " is required"}
	}
	meta, ok := g.subscriptions[subscriptionID]
	if !ok {
		return "", invalidArgumentError{message: fmt.Sprintf("%s references missing subscription %q", path, subscriptionID)}
	}
	for _, proxy := range meta.Proxies {
		if proxy.ID == proxyID {
			return proxy.Tag, nil
		}
	}
	return "", invalidArgumentError{message: fmt.Sprintf("%s references missing subscription node %q", path, proxyID)}
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

func resolveOutboundTag(outbounds []*configv1.Outbound, id string, path string) (string, error) {
	if id == "" {
		return "", nil
	}
	for _, outbound := range outbounds {
		if outbound != nil && outbound.GetId() == id {
			return outbound.GetTag(), nil
		}
	}
	return "", invalidArgumentError{message: fmt.Sprintf("%s references missing outbound ID %q", path, id)}
}

func resolveDNSServerTag(servers []*configv1.DnsServer, id string, path string) (string, error) {
	if id == "" {
		return "", nil
	}
	for _, server := range servers {
		if server != nil && server.GetId() == id {
			return server.GetTag(), nil
		}
	}
	return "", invalidArgumentError{message: fmt.Sprintf("%s references missing DNS server ID %q", path, id)}
}

func resolveEnabledInboundTag(inbounds []*configv1.Inbound, id string, path string) (string, error) {
	if id == "" {
		return "", invalidArgumentError{message: path + " is required"}
	}
	for _, inbound := range inbounds {
		if inbound != nil && inbound.GetId() == id {
			if !inbound.GetEnable() {
				return "", invalidArgumentError{message: fmt.Sprintf("%s references disabled inbound ID %q", path, id)}
			}
			return inbound.GetTag(), nil
		}
	}
	return "", invalidArgumentError{message: fmt.Sprintf("%s references missing inbound ID %q", path, id)}
}

func resolveRuleSetTag(ruleSets []*configv1.RuleSet, id string, path string) (string, error) {
	if id == "" {
		return "", invalidArgumentError{message: path + " is required"}
	}
	for _, ruleset := range ruleSets {
		if ruleset != nil && ruleset.GetId() == id {
			return ruleset.GetTag(), nil
		}
	}
	return "", invalidArgumentError{message: fmt.Sprintf("%s references missing rule set ID %q", path, id)}
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
	case configv1.OutboundType_OUTBOUND_TYPE_BRIDGE:
		return outboundTypeBridge, nil
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
	case configv1.RuleAction_RULE_ACTION_BYPASS:
		return ruleActionBypass, nil
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
	case configv1.DnsRuleAction_DNS_RULE_ACTION_EVALUATE:
		return ruleActionEvaluate, nil
	case configv1.DnsRuleAction_DNS_RULE_ACTION_RESPOND:
		return ruleActionRespond, nil
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
