package config

import (
	"fmt"
	"sort"
	"strconv"
)

type subscriptionOutboundKey struct {
	subscriptionID string
	tag            string
	index          int
}

type subscriptionOutboundReference struct {
	group map[string]any
	index int
	key   subscriptionOutboundKey
}

// Preserve existing names unless they collide. Allocate all colliding names
// together so changing group/subscription order cannot select a different node.
func assignSubscriptionOutboundTags(outbounds []any, builtIns []string, proxies []any, keys []subscriptionOutboundKey, references []subscriptionOutboundReference) error {
	reserved := make(map[string]bool, len(outbounds)+len(builtIns)+len(keys))
	for index, value := range outbounds {
		tag, _ := value.(map[string]any)["tag"].(string)
		if tag == "" {
			tag = strconv.Itoa(index)
		}
		if reserved[tag] {
			return invalidArgumentError{message: fmt.Sprintf("outbounds[%d].tag: duplicate outbound tag %q", index, tag)}
		}
		reserved[tag] = true
	}
	for _, tag := range builtIns {
		if reserved[tag] {
			return invalidArgumentError{message: fmt.Sprintf("outbound tag %q conflicts with a built-in outbound", tag)}
		}
		reserved[tag] = true
	}
	counts := make(map[string]int, len(keys))
	for _, key := range keys {
		counts[key.tag]++
	}
	names := make(map[subscriptionOutboundKey]string, len(keys))
	var collisions []subscriptionOutboundKey
	for _, key := range keys {
		if counts[key.tag] == 1 && !reserved[key.tag] {
			names[key] = key.tag
			reserved[key.tag] = true
		} else {
			collisions = append(collisions, key)
		}
	}
	sort.Slice(collisions, func(i, j int) bool {
		if collisions[i].subscriptionID != collisions[j].subscriptionID {
			return collisions[i].subscriptionID < collisions[j].subscriptionID
		}
		if collisions[i].tag != collisions[j].tag {
			return collisions[i].tag < collisions[j].tag
		}
		return collisions[i].index < collisions[j].index
	})
	for _, key := range collisions {
		base := key.tag + " [" + key.subscriptionID + "]"
		tag := base
		for suffix := 2; reserved[tag]; suffix++ {
			tag = fmt.Sprintf("%s %d", base, suffix)
		}
		names[key] = tag
		reserved[tag] = true
	}
	for _, reference := range references {
		reference.group["outbounds"].([]any)[reference.index] = names[reference.key]
	}
	// Subscription-internal references contain names, not GUI node IDs. When a
	// name is ambiguous, keep the first included node in source order, independent
	// of which profile group happened to request it first.
	firstByTag := make(map[subscriptionOutboundKey]subscriptionOutboundKey, len(keys))
	for _, key := range keys {
		alias := subscriptionOutboundKey{subscriptionID: key.subscriptionID, tag: key.tag}
		if first, exists := firstByTag[alias]; !exists || key.index < first.index {
			firstByTag[alias] = key
		}
	}
	for index, value := range proxies {
		proxy := value.(map[string]any)
		key := keys[index]
		proxy["tag"] = names[key]
		remap := func(tag string) string {
			alias := subscriptionOutboundKey{subscriptionID: key.subscriptionID, tag: tag}
			if first, exists := firstByTag[alias]; exists {
				return names[first]
			}
			return tag
		}
		// Raw sing-box subscriptions may contain chained dialers or groups.
		for _, field := range []string{"detour", "default"} {
			if tag, ok := proxy[field].(string); ok {
				proxy[field] = remap(tag)
			}
		}
		if members, ok := proxy["outbounds"].([]any); ok {
			for i, value := range members {
				if tag, ok := value.(string); ok {
					members[i] = remap(tag)
				}
			}
		}
	}
	return nil
}
