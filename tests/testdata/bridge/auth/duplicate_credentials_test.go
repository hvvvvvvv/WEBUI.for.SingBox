package auth

import (
	"os"
	"strings"
	"testing"

	"guiforcores/bridge/storage"
)

func TestDuplicateCredentialFieldsFailClosed(t *testing.T) {
	for _, field := range []string{"password", "Password", "totp", "TOTP"} {
		t.Run(field, func(t *testing.T) {
			paths := storage.NewPaths(t.TempDir())
			service := NewService(paths)
			if err := service.SetSecret("a long test password"); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(paths.Resolve(secretPath))
			if err != nil {
				t.Fatal(err)
			}
			// encoding/json normally accepts duplicates (including case aliases)
			// and silently lets the final null erase an earlier credential.
			corrupt := strings.TrimSuffix(string(raw), "}") + `,"` + field + `":null}`
			if err := os.WriteFile(paths.Resolve(secretPath), []byte(corrupt), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Status(); err == nil {
				t.Fatal("ambiguous credential document was accepted")
			}
			if _, err := service.Login("test", "", ""); err == nil {
				t.Fatal("duplicate field disabled authentication")
			}
		})
	}
}
