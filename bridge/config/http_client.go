package config

import (
	"fmt"
	"strconv"
)

const ruleSetDefaultHTTPClientTag = "webui-rule-set-default"

// These are the JSON dial fields accepted by sing-box 1.14. Do not copy
// outbound-only fields into a client. Preserve an existing domain_strategy as
// well: 1.14 still reads it, and removing it would change DNS behavior.
var httpClientDirectDialFields = []string{
	"bind_interface", "inet4_bind_address", "inet6_bind_address", "bind_address_no_port",
	"protect_path", "routing_mark", "reuse_addr", "netns", "connect_timeout",
	"tcp_fast_open", "tcp_multi_path", "disable_tcp_keep_alive", "tcp_keep_alive",
	"tcp_keep_alive_interval", "udp_fragment", "domain_resolver", "network_strategy",
	"network_type", "fallback_network_type", "fallback_delay", "domain_strategy",
}

type httpClientOutbound struct {
	options  map[string]any
	endpoint bool
}

type ruleSetHTTPClients struct {
	config       map[string]any
	route        map[string]any
	outbounds    []any
	outboundTags map[string]httpClientOutbound
	clients      []any
	clientTags   map[string]int
	final        string
}

// normalizeRuleSetHTTPClients runs after mixin/script processing. The profile
// field download_detour remains an outbound ID; the temporary generated tag is
// consumed here, so no legacy rule-set download fields escape Service.Generate.
func normalizeRuleSetHTTPClients(config map[string]any) error {
	if config["route"] == nil {
		return nil
	}
	route, ok := config["route"].(map[string]any)
	if !ok || route == nil {
		return httpClientFieldError("route", "must be an object")
	}
	ruleSets, err := httpClientArray(route["rule_set"], "route.rule_set")
	if err != nil {
		return err
	}
	var remote []int
	for index, value := range ruleSets {
		item, ok := value.(map[string]any)
		path := fmt.Sprintf("route.rule_set[%d]", index)
		if !ok || item == nil {
			return httpClientFieldError(path, "must be an object")
		}
		kind, err := httpClientString(item["type"], path+".type")
		if err != nil {
			return err
		}
		if kind == "remote" {
			remote = append(remote, index)
		}
	}
	if len(remote) == 0 {
		return nil
	}

	clients, err := newRuleSetHTTPClients(config, route)
	if err != nil {
		return err
	}
	needsDefault := false
	for _, index := range remote {
		item := ruleSets[index].(map[string]any)
		path := fmt.Sprintf("route.rule_set[%d]", index)
		specified, err := clients.validateClient(item["http_client"], path+".http_client")
		if err != nil {
			return err
		}
		if specified {
			// Modern configuration, including {}, takes precedence over the
			// GUI's temporary legacy field and legacy mixin/script input.
			delete(item, "download_detour")
			continue
		}
		detour, err := httpClientString(item["download_detour"], path+".download_detour")
		if err != nil {
			return err
		}
		delete(item, "download_detour")
		delete(item, "http_client")
		if detour != "" {
			client, err := clients.forOutbound(detour, path+".download_detour")
			if err != nil {
				return err
			}
			item["http_client"] = client
		} else {
			needsDefault = true
		}
	}
	if needsDefault {
		return clients.ensureDefault()
	}
	return clients.validateExistingDefault()
}

func newRuleSetHTTPClients(config, route map[string]any) (*ruleSetHTTPClients, error) {
	final, err := httpClientString(route["final"], "route.final")
	if err != nil {
		return nil, err
	}
	result := &ruleSetHTTPClients{
		config: config, route: route, final: final,
		outboundTags: map[string]httpClientOutbound{}, clientTags: map[string]int{},
	}
	// An outbound takes precedence over an endpoint with the same tag. Both
	// namespaces use their array index as the tag when none is configured.
	for _, key := range []string{"endpoints", "outbounds"} {
		items, err := httpClientArray(config[key], key)
		if err != nil {
			return nil, err
		}
		if key == "outbounds" {
			result.outbounds = items
		}
		for index, value := range items {
			path := fmt.Sprintf("%s[%d]", key, index)
			item, ok := value.(map[string]any)
			if !ok || item == nil {
				return nil, httpClientFieldError(path, "must be an object")
			}
			tag, err := httpClientString(item["tag"], path+".tag")
			if err != nil {
				return nil, err
			}
			if _, err := httpClientString(item["type"], path+".type"); err != nil {
				return nil, err
			}
			if tag == "" {
				tag = strconv.Itoa(index)
			}
			result.outboundTags[tag] = httpClientOutbound{options: item, endpoint: key == "endpoints"}
		}
	}
	if len(result.outbounds) == 0 && final == "" {
		// Core creates this direct outbound when no default is configured.
		result.outboundTags["direct"] = httpClientOutbound{options: map[string]any{"type": "direct"}}
	}
	result.clients, err = httpClientArray(config["http_clients"], "http_clients")
	if err != nil {
		return nil, err
	}
	for index, value := range result.clients {
		path := fmt.Sprintf("http_clients[%d]", index)
		client, ok := value.(map[string]any)
		if !ok || client == nil {
			return nil, httpClientFieldError(path, "must be an object")
		}
		tag, err := httpClientString(client["tag"], path+".tag")
		if err != nil {
			return nil, err
		}
		// Match core's shared-client lookup if raw input contains duplicates.
		result.clientTags[tag] = index
	}
	return result, nil
}

func (c *ruleSetHTTPClients) validateClient(value any, path string) (bool, error) {
	if value == nil {
		return false, nil
	}
	switch client := value.(type) {
	case string:
		if client == "" {
			return false, nil
		}
		return true, c.validateShared(client, path)
	case map[string]any:
		if client == nil {
			return false, nil
		}
		return true, c.validateDetour(client, path)
	default:
		return false, httpClientFieldError(path, "must be a client tag or an object")
	}
}

func (c *ruleSetHTTPClients) validateDetour(client map[string]any, path string) error {
	detour, err := httpClientString(client["detour"], path+".detour")
	if err != nil {
		return err
	}
	if detour != "" {
		_, err = c.outbound(detour, path+".detour")
	}
	return err
}

func (c *ruleSetHTTPClients) validateShared(tag, path string) error {
	index, ok := c.clientTags[tag]
	if !ok {
		return httpClientFieldError(path, fmt.Sprintf("references missing HTTP client %q", tag))
	}
	return c.validateDetour(c.clients[index].(map[string]any), fmt.Sprintf("http_clients[%d]", index))
}

// Core starts a configured default client even when every remote rule set has
// its own client. Validate that active default without adding a new one.
func (c *ruleSetHTTPClients) validateExistingDefault() error {
	tag, err := httpClientString(c.route["default_http_client"], "route.default_http_client")
	if err != nil {
		return err
	}
	if tag != "" {
		return c.validateShared(tag, "route.default_http_client")
	}
	if len(c.clients) > 0 {
		first := c.clients[0].(map[string]any)
		if tag, _ := first["tag"].(string); tag != "" {
			return c.validateShared(tag, "http_clients[0].tag")
		}
	}
	return nil
}

func (c *ruleSetHTTPClients) outbound(tag, path string) (httpClientOutbound, error) {
	item, ok := c.outboundTags[tag]
	if !ok {
		return httpClientOutbound{}, httpClientFieldError(path, fmt.Sprintf("references missing outbound or endpoint %q", tag))
	}
	return item, nil
}

func (c *ruleSetHTTPClients) forOutbound(tag, path string) (map[string]any, error) {
	outbound, err := c.outbound(tag, path)
	if err != nil {
		return nil, err
	}
	if outbound.endpoint || outbound.options["type"] != "direct" {
		return map[string]any{"detour": tag}, nil
	}
	// A detour to an empty direct is rejected by core's modern HTTP dialer.
	// Dial directly instead, keeping the selected direct's network settings.
	client := map[string]any{"engine": "go"}
	for _, field := range httpClientDirectDialFields {
		if value, ok := outbound.options[field]; ok {
			client[field] = deepCopyValue(value)
		}
	}
	return client, nil
}

func (c *ruleSetHTTPClients) ensureDefault() error {
	tag, err := httpClientString(c.route["default_http_client"], "route.default_http_client")
	if err != nil {
		return err
	}
	if tag != "" {
		return c.validateShared(tag, "route.default_http_client")
	}
	if len(c.clients) > 0 {
		first := c.clients[0].(map[string]any)
		tag, _ = first["tag"].(string)
		if tag == "" {
			tag = c.nextTag()
			first["tag"] = tag
			c.clientTags[tag] = 0
		}
		if err := c.validateShared(tag, "route.default_http_client"); err != nil {
			return err
		}
	} else {
		var client map[string]any
		switch {
		case c.final != "":
			client, err = c.forOutbound(c.final, "route.final")
		case len(c.outbounds) > 0:
			first := c.outbounds[0].(map[string]any)
			outboundTag, _ := first["tag"].(string)
			if outboundTag == "" {
				outboundTag = "0"
			}
			client, err = c.forOutbound(outboundTag, "outbounds[0].tag")
		default:
			client = map[string]any{"engine": "go"}
		}
		if err != nil {
			return err
		}
		tag = c.nextTag()
		client["tag"] = tag
		c.clientTags[tag] = len(c.clients)
		c.clients = append(c.clients, client)
		c.config["http_clients"] = c.clients
	}
	c.route["default_http_client"] = tag
	return nil
}

func (c *ruleSetHTTPClients) nextTag() string {
	tag := ruleSetDefaultHTTPClientTag
	for suffix := 2; ; suffix++ {
		if _, exists := c.clientTags[tag]; !exists {
			return tag
		}
		tag = fmt.Sprintf("%s-%d", ruleSetDefaultHTTPClientTag, suffix)
	}
}

func httpClientString(value any, path string) (string, error) {
	if value == nil {
		return "", nil
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	return "", httpClientFieldError(path, "must be a string")
}

func httpClientArray(value any, path string) ([]any, error) {
	switch items := value.(type) {
	case nil:
		return nil, nil
	case []any:
		return items, nil
	case []map[string]any:
		result := make([]any, len(items))
		for index, item := range items {
			result[index] = item
		}
		return result, nil
	default:
		return nil, httpClientFieldError(path, "must be an array")
	}
}

func httpClientFieldError(path, message string) error {
	return invalidArgumentError{message: path + ": " + message}
}
