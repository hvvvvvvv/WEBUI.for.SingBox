package plugins

import (
	"errors"
	"testing"
)

func TestValidateMetadataDoesNotRequireOrCompileSource(t *testing.T) {
	for _, source := range []string{"", "function {"} {
		plugin := pluginForTest("metadata-only", source)
		if err := ValidateMetadata(plugin); err != nil {
			t.Fatalf("catalog metadata required installation source: %v", err)
		}
		if err := ValidatePlugin(plugin); !errors.Is(err, ErrInvalid) {
			t.Fatalf("installation skipped source validation: %v", err)
		}
		plugin.ConfigurationSchema = []Setting{{Key: "unsafe", Title: "Unsafe", Component: "Shell", Value: "command"}}
		if err := ValidateMetadata(plugin); !errors.Is(err, ErrInvalid) {
			t.Fatalf("metadata skipped setting validation: %v", err)
		}
	}
}
