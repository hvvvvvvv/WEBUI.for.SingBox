package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"guiforcores/bridge/storage"
)

func TestUnconfiguredAndExplicitlyDisabledSecretsRemainCompatible(t *testing.T) {
	s := NewService(storage.NewPaths(t.TempDir()))
	if !s.VerifySecret("") {
		t.Fatal("an unconfigured installation must allow the initial login")
	}
	if err := s.SetSecret("correct"); err != nil {
		t.Fatal(err)
	}
	if !s.VerifySecret("correct") || s.VerifySecret("") || s.VerifySecret("wrong") {
		t.Fatal("configured secret verification failed")
	}
	if err := s.SetSecret(""); err != nil {
		t.Fatal(err)
	}
	if !s.VerifySecret("") {
		t.Fatal("an explicitly disabled secret must keep allowing login")
	}
}

func TestInvalidSecretFilesFailClosed(t *testing.T) {
	for _, content := range []string{" ", "\r\n", "not-a-hash", "1234"} {
		t.Run(content, func(t *testing.T) {
			paths := storage.NewPaths(t.TempDir())
			s := NewService(paths)
			if err := s.SetSecret("correct"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.Resolve(secretPath), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SecretHash(); err == nil {
				t.Fatal("invalid key content was accepted")
			}
			if s.VerifySecret("") || s.VerifySecret("correct") {
				t.Fatal("damaged key disabled authentication")
			}
		})
	}
	for _, invalidPath := range []string{secretPath, "data"} {
		t.Run(invalidPath, func(t *testing.T) {
			paths := storage.NewPaths(t.TempDir())
			s := NewService(paths)
			if invalidPath == secretPath {
				if err := os.MkdirAll(paths.Resolve(secretPath), 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(paths.Resolve("data"), []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SecretHash(); err == nil {
				t.Fatal("invalid key path was accepted")
			}
			if s.VerifySecret("") {
				t.Fatal("key read failure disabled authentication")
			}
		})
	}
}

func TestUnreadableSecretFailsClosed(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	s := NewService(paths)
	if err := s.SetSecret("correct"); err != nil {
		t.Fatal(err)
	}
	path := paths.Resolve(secretPath)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); !os.IsPermission(err) {
		t.Skip("host permissions do not deny reading a mode-000 file (Windows or privileged user)")
	}
	if _, err := s.SecretHash(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected preserved permission error, got %v", err)
	}
	if s.VerifySecret("") || s.VerifySecret("correct") {
		t.Fatal("permission error disabled authentication")
	}
}

func TestBrokenSecretSymlinkFailsClosed(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(paths.Resolve(secretPath)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(paths.Resolve("missing.key"), paths.Resolve(secretPath)); err != nil {
		t.Skipf("host cannot create a test symlink: %v", err)
	}
	s := NewService(paths)
	if _, err := s.SecretHash(); err == nil {
		t.Fatal("broken key symlink was accepted as unconfigured")
	}
	if s.VerifySecret("") {
		t.Fatal("broken key symlink disabled authentication")
	}
}
