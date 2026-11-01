package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"guiforcores/bridge/scriptvm"
)

func runSubscribeScript(script string, proxies []map[string]any, sub subscription) ([]map[string]any, subscription, error) {
	return runSubscribeScriptContext(context.Background(), script, proxies, sub)
}

func runSubscribeScriptContext(ctx context.Context, script string, proxies []map[string]any, sub subscription) ([]map[string]any, subscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, sub, err
	}
	if strings.TrimSpace(script) == "" {
		return proxies, sub, nil
	}
	// The old Goja Go-struct wrapper exposes exported Go field names, including
	// Header.Request and Proxies[].Tag. Keep that interface, but JSON-clone it so
	// a failed script cannot modify caller-owned maps/slices through Go wrappers.
	data, err := scriptvm.Run(ctx, script, map[string]any{
		"proxies": proxies, "subscription": legacyScriptValue(reflect.ValueOf(sub)),
	}, `(async () => {
		const result = await onSubscribe(proxies, subscription);
		if (result == null) return {proxies, subscription};
		if (typeof result === 'object' && !Array.isArray(result)) {
			if (result.proxies == null) result.proxies = proxies;
			if (result.subscription == null) result.subscription = subscription;
		}
		return result;
	})()`)
	if err != nil {
		return nil, sub, err
	}
	var result struct {
		Proxies      []map[string]any `json:"proxies"`
		Subscription subscription     `json:"subscription"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, sub, err
	}
	if result.Proxies == nil {
		result.Proxies = proxies
	}
	for index, proxy := range result.Proxies {
		typ, _ := proxy["type"].(string)
		tag, _ := proxy["tag"].(string)
		if strings.TrimSpace(typ) == "" || strings.TrimSpace(tag) == "" {
			return nil, sub, fmt.Errorf("subscription script node %d is missing a valid type or tag", index)
		}
	}
	if result.Subscription.ID == "" {
		result.Subscription = sub
	}
	return result.Proxies, result.Subscription, nil
}

func legacyScriptValue(value reflect.Value) any {
	switch value.Kind() {
	case reflect.Struct:
		result := make(map[string]any, value.NumField())
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if field.IsExported() {
				result[field.Name] = legacyScriptValue(value.Field(i))
			}
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return nil
		}
		result := make([]any, value.Len())
		for i := range result {
			result[i] = legacyScriptValue(value.Index(i))
		}
		return result
	default:
		return value.Interface()
	}
}
