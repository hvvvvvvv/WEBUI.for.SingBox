package parser

import "strings"

// applyDomainResolver only converts resolver information supplied by this
// node. Resolving a missing server or inheriting a global default belongs to
// sing-box, which has the complete configuration.
func applyDomainResolver(node Node, out map[string]any) {
	delete(out, "domain_strategy")
	value := nodeValue(node, "domain_resolver", "domain-resolver")
	if value == nil {
		return
	}
	strategy := normalizedDomainStrategy(node)
	switch resolver := value.(type) {
	case string:
		if strings.TrimSpace(resolver) == "" {
			return
		}
		if strategy == "" {
			out["domain_resolver"] = resolver
		} else {
			out["domain_resolver"] = map[string]any{"server": resolver, "strategy": strategy}
		}
	case map[string]any:
		out["domain_resolver"] = resolverWithStrategy(resolver, strategy)
	case Node:
		out["domain_resolver"] = resolverWithStrategy(map[string]any(resolver), strategy)
	case map[any]any:
		object := make(map[string]any, len(resolver))
		for key, item := range resolver {
			object[stringValue(key)] = item
		}
		out["domain_resolver"] = resolverWithStrategy(object, strategy)
	default:
		// An explicitly configured invalid value is left for core validation.
		out["domain_resolver"] = value
	}
}

func resolverWithStrategy(resolver map[string]any, strategy string) map[string]any {
	result := make(map[string]any, len(resolver)+1)
	for key, value := range resolver {
		result[key] = value
	}
	server, _ := resolver["server"].(string)
	_, hasStrategy := resolver["strategy"]
	if strings.TrimSpace(server) != "" && !hasStrategy && strategy != "" {
		result["strategy"] = strategy
	}
	return result
}

func normalizedDomainStrategy(node Node) string {
	strategy := strings.ToLower(nodeString(node, "domain-strategy", "domain_strategy", "ip-version", "ip_version"))
	strategy = strings.NewReplacer("-", "_", " ", "_").Replace(strategy)
	switch strategy {
	case "4", "ipv4", "ipv4only", "ipv4_only":
		return "ipv4_only"
	case "6", "ipv6", "ipv6only", "ipv6_only":
		return "ipv6_only"
	case "preferipv4", "prefer_ipv4":
		return "prefer_ipv4"
	case "preferipv6", "prefer_ipv6":
		return "prefer_ipv6"
	default:
		return ""
	}
}
