package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"guiforcores/bridge/scriptvm"
	profilev1 "guiforcores/gen/profile/v1"
)

func applyScript(ctx context.Context, config map[string]any, script *profilev1.Script) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if script == nil || strings.TrimSpace(script.GetCode()) == "" {
		return config, nil
	}
	// Preserve the original return-or-mutate convention. Both the hook and the
	// JSON serialization of its result stay inside the interruptible VM; getters,
	// toJSON and exception toString must never run through Value.Export in Go.
	output, err := scriptvm.Run(ctx, script.GetCode(), map[string]any{"config": config}, `(function () {
  if (typeof onGenerate !== "function") throw new Error("script must define function onGenerate(config)");
  var result = onGenerate(config);
  var normalize = function(value) {
    return value && typeof value === "object" && !Array.isArray(value) ? value : config;
  };
  return result && typeof result.then === "function" ? result.then(normalize) : normalize(result);
})()`)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, invalidArgumentError{message: fmt.Sprintf("onGenerate execution failed: %v", err)}
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil || result == nil {
		return nil, invalidArgumentError{message: "onGenerate must return an object or mutate config object"}
	}
	return result, nil
}
