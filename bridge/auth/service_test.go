package auth

import (
	"testing"

	"guiforcores/bridge/storage"
)

func TestLegacySettingsSecretIsIgnored(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	if err := storage.WriteYAML(paths, "data/user.yaml", map[string]any{"authSecret": HashSecret("legacy")}); err != nil {
		t.Fatal(err)
	}
	service := NewService(paths)
	if secret := service.SecretHash(); secret != "" {
		t.Fatalf("expected legacy settings secret to be ignored, got %q", secret)
	}
}
