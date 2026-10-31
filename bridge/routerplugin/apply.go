package routerplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"

	profilev1 "guiforcores/gen/profile/v1"
)

const MaxConfigBytes = 8 << 20

// Apply works on a private JSON copy. Neither validation failures nor a later
// missing reference can leave an earlier plugin's changes in the caller's map.
func Apply(config map[string]any, plugins []*profilev1.RouterPlugin) (map[string]any, error) {
	instances, err := parseInstances(plugins)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return config, nil
	}
	if config == nil {
		return nil, fmt.Errorf("router plugins require a generated configuration")
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("copy generated configuration: %w", err)
	}
	if len(payload) > MaxConfigBytes {
		return nil, fmt.Errorf("generated configuration exceeds router plugin limit of %d bytes", MaxConfigBytes)
	}
	var next map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&next); err != nil {
		return nil, err
	}
	inbounds, err := configObjects(next["inbounds"], "inbounds")
	if err != nil {
		return nil, err
	}
	var route map[string]any
	if value, exists := next["route"]; exists {
		var ok bool
		route, ok = value.(map[string]any)
		if !ok || route == nil {
			return nil, fmt.Errorf("route must be an object")
		}
	} else {
		route = make(map[string]any)
		next["route"] = route
	}
	originalRules, err := configObjects(route["rules"], "route.rules")
	if err != nil {
		return nil, err
	}
	var dnsRules, sourceRules []any
	configuredTUNs := make(map[string]string)
	for _, plugin := range instances {
		inbound := ""
		switch value := plugin.config.(type) {
		case tunRoutingConfig:
			inbound = value.Inbound
		case sourceRoutingConfig:
			inbound = value.Inbound
		case dnsHijackConfig:
			inbound = value.Inbound
		}
		tun, err := findTUN(inbounds, inbound)
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", plugin.id, err)
		}
		switch value := plugin.config.(type) {
		case tunRoutingConfig:
			if previous, exists := configuredTUNs[inbound]; exists {
				return nil, fmt.Errorf("plugins %s and %s both configure TUN %q", previous, plugin.id, inbound)
			}
			configuredTUNs[inbound] = plugin.id
			name, _ := tun["interface_name"].(string)
			defaultInterface, _ := route["default_interface"].(string)
			if name != "" && defaultInterface == name {
				return nil, fmt.Errorf("plugin %s: route.default_interface points back to TUN %q", plugin.id, name)
			}
			for _, included := range value.IncludeInterface {
				if name != "" && included == name {
					return nil, fmt.Errorf("plugin %s: TUN cannot include its own interface %q", plugin.id, name)
				}
			}
			tun["auto_route"] = true
			tun["auto_redirect"] = value.AutoRedirect
			tun["strict_route"] = value.StrictRoute
			if len(value.IncludeInterface) > 0 {
				tun["include_interface"] = stringValues(value.IncludeInterface)
			}
			if len(value.ExcludeAddress) > 0 {
				merged, err := mergeExcludedAddresses(tun["route_exclude_address"], value.ExcludeAddress)
				if err != nil {
					return nil, fmt.Errorf("plugin %s: %w", plugin.id, err)
				}
				tun["route_exclude_address"] = stringValues(merged)
			}
			route["auto_detect_interface"] = defaultInterface == ""
			route["find_process"] = false
		case sourceRoutingConfig:
			if err := requireOutbound(next["outbounds"], value.Outbound); err != nil {
				return nil, fmt.Errorf("plugin %s: %w", plugin.id, err)
			}
			sourceRules = append(sourceRules, map[string]any{
				"inbound": []any{inbound}, "source_ip_cidr": stringValues(canonicalCIDRs(value.SourceIPCIDR)),
				"action": "route", "outbound": value.Outbound,
			})
		case dnsHijackConfig:
			dnsRules = append(dnsRules, map[string]any{"inbound": []any{inbound}, "port": []any{53}, "action": "hijack-dns"})
		}
	}
	if len(dnsRules)+len(sourceRules) > 0 {
		rules := make([]any, 0, len(dnsRules)+len(sourceRules)+len(originalRules))
		rules = append(rules, dnsRules...)
		rules = append(rules, sourceRules...)
		for _, rule := range originalRules {
			rules = append(rules, rule)
		}
		route["rules"] = rules
	}
	if err := validateAutoRedirect(next, inbounds, route); err != nil {
		return nil, err
	}
	payload, err = json.Marshal(next)
	if err != nil {
		return nil, err
	}
	if len(payload) > MaxConfigBytes {
		return nil, fmt.Errorf("plugin output exceeds %d bytes", MaxConfigBytes)
	}
	return next, nil
}

// sing-box checks these conflicts when opening sockets, after `check` has
// succeeded. Validate the final configuration, including pre-existing TUNs and
// nested dialer options, before a plugin can produce an unusable router config.
func validateAutoRedirect(config map[string]any, inbounds []map[string]any, route map[string]any) error {
	namespaces := make(map[string]string)
	for index, inbound := range inbounds {
		if inbound["type"] != "tun" || inbound["auto_redirect"] != true {
			continue
		}
		path := fmt.Sprintf("inbounds[%d]", index)
		namespace := ""
		if raw, exists := inbound["netns"]; exists {
			var ok bool
			namespace, ok = raw.(string)
			if !ok {
				return fmt.Errorf("%s.netns must be a string when TUN auto_redirect is enabled", path)
			}
		}
		if previous, exists := namespaces[namespace]; exists {
			return fmt.Errorf("%s and %s both enable TUN auto_redirect in the same network namespace %q", previous, path, namespace)
		}
		namespaces[namespace] = path
	}
	if len(namespaces) == 0 {
		return nil
	}
	if mark, exists := route["default_mark"]; exists && !zeroRoutingMark(mark) {
		return fmt.Errorf("route.default_mark must be zero or omitted when TUN auto_redirect is enabled")
	}
	for _, field := range []string{"outbounds", "dns", "http_clients", "endpoints"} {
		if err := validateDialerMarks(config[field], field); err != nil {
			return err
		}
	}
	return nil
}

func validateDialerMarks(value any, path string) error {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := path + "." + key
			if key == "routing_mark" && !zeroRoutingMark(value[key]) {
				return fmt.Errorf("%s must be zero or omitted when TUN auto_redirect is enabled", childPath)
			}
			if err := validateDialerMarks(value[key], childPath); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range value {
			if err := validateDialerMarks(child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

// FwMark accepts uint32 JSON numbers or base-prefixed strings. Unknown values
// cannot safely be treated as zero, and marks must never be silently removed.
func zeroRoutingMark(value any) bool {
	switch value := value.(type) {
	case json.Number:
		mark, err := strconv.ParseUint(value.String(), 10, 32)
		return err == nil && mark == 0
	case string:
		mark, err := strconv.ParseUint(value, 0, 32)
		return err == nil && mark == 0
	case float64:
		return value == 0
	default:
		return false
	}
}

func configObjects(value any, field string) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", field)
	}
	result := make([]map[string]any, 0, len(values))
	for index, value := range values {
		object, ok := value.(map[string]any)
		if !ok || object == nil {
			return nil, fmt.Errorf("%s[%d] must be an object", field, index)
		}
		result = append(result, object)
	}
	return result, nil
}

func findTUN(inbounds []map[string]any, tag string) (map[string]any, error) {
	var matched map[string]any
	for _, inbound := range inbounds {
		if inbound["tag"] != tag {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("inbound tag %q is ambiguous", tag)
		}
		matched = inbound
	}
	if matched == nil {
		return nil, fmt.Errorf("TUN inbound %q does not exist in generated configuration", tag)
	}
	if matched["type"] != "tun" {
		return nil, fmt.Errorf("inbound %q is not a TUN", tag)
	}
	return matched, nil
}

func requireOutbound(raw any, tag string) error {
	outbounds, err := configObjects(raw, "outbounds")
	if err != nil {
		return err
	}
	matches := 0
	for index, outbound := range outbounds {
		name, _ := outbound["tag"].(string)
		if name == "" {
			name = strconv.Itoa(index)
		}
		if name == tag {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("outbound tag %q must identify exactly one generated outbound", tag)
	}
	return nil
}

func stringValues(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func canonicalCIDRs(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		prefix, _ := netip.ParsePrefix(value)
		result[index] = prefix.Masked().String()
	}
	return result
}

// Existing exclusions may protect the management LAN. A plugin can extend
// these exclusions, but an empty/default template must never remove them.
func mergeExcludedAddresses(existing any, added []string) ([]string, error) {
	var previous []string
	switch value := existing.(type) {
	case nil:
	case string:
		previous = []string{value}
	case []any:
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("existing route_exclude_address must contain CIDR strings")
			}
			previous = append(previous, text)
		}
	default:
		return nil, fmt.Errorf("existing route_exclude_address must contain CIDR strings")
	}
	result := make([]string, 0, len(previous)+len(added))
	seen := make(map[netip.Prefix]bool)
	for _, value := range append(previous, added...) {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("existing route_exclude_address contains invalid CIDR %q", value)
		}
		prefix = prefix.Masked()
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		result = append(result, prefix.String())
	}
	return result, nil
}
