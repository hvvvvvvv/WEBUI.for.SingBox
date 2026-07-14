package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	subscriptionparser "guiforcores/bridge/subscription/parser"
)

type subscriptionParseResult struct {
	proxies      []map[string]any
	usedFallback bool
	total        int
	skipped      int
}

func parseNativeSubscription(body string) ([]map[string]any, error) {
	var value any
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}

	var entries []any
	switch root := value.(type) {
	case map[string]any:
		var ok bool
		entries, ok = root["outbounds"].([]any)
		if !ok {
			return nil, fmt.Errorf("missing outbounds array")
		}
	case []any:
		entries = root
	default:
		return nil, fmt.Errorf("expected an outbound array or object")
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("outbounds array is empty")
	}

	proxies := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		proxy, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("outbound is not an object")
		}
		typ, _ := proxy["type"].(string)
		tag, _ := proxy["tag"].(string)
		if strings.TrimSpace(typ) == "" || strings.TrimSpace(tag) == "" {
			return nil, fmt.Errorf("outbound is missing type or tag")
		}
		proxies = append(proxies, proxy)
	}
	return proxies, nil
}

func parseSubscriptionBody(body string, subscriptionType string, enableNodeConversion bool) (subscriptionParseResult, error) {
	proxies, nativeErr := parseNativeSubscription(body)
	if nativeErr == nil {
		return subscriptionParseResult{
			proxies: proxies,
			total:   len(proxies),
		}, nil
	}
	if subscriptionType != "Http" {
		return subscriptionParseResult{}, fmt.Errorf("native parser: %w", nativeErr)
	}
	if !enableNodeConversion {
		return subscriptionParseResult{}, fmt.Errorf("native parser: %v; node conversion is disabled", nativeErr)
	}

	fallback, fallbackErr := subscriptionparser.Parse(body)
	if fallbackErr != nil {
		return subscriptionParseResult{}, fmt.Errorf("native parser: %v; fallback parser: %w", nativeErr, fallbackErr)
	}
	return subscriptionParseResult{
		proxies:      fallback.Outbounds,
		usedFallback: true,
		total:        fallback.Total,
		skipped:      fallback.Skipped,
	}, nil
}
