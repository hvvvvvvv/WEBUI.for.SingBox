//go:build windows

package auth

import (
	"testing"

	"golang.org/x/sys/windows"
	"guiforcores/bridge/storage"
)

func TestLockedSecretFailsClosedAndFailedReplacementPreservesKey(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	s := NewService(paths)
	if err := s.SetSecret("correct"); err != nil {
		t.Fatal(err)
	}
	path, err := windows.UTF16PtrFromString(paths.Resolve(secretPath))
	if err != nil {
		t.Fatal(err)
	}
	// An exclusive reader reproduces a real OS access failure without changing ACLs.
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if handle != windows.InvalidHandle {
			_ = windows.CloseHandle(handle)
		}
	})
	if _, err := s.SecretHash(); err == nil {
		t.Fatal("exclusive lock should prevent reading the key")
	}
	if s.VerifySecret("") || s.VerifySecret("correct") {
		t.Fatal("OS access failure disabled authentication")
	}
	if err := s.SetSecret("replacement"); err == nil {
		t.Fatal("exclusive lock should prevent replacing the key")
	}
	if err := windows.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
	handle = windows.InvalidHandle
	if !s.VerifySecret("correct") || s.VerifySecret("replacement") {
		t.Fatal("failed secret replacement destroyed the configured key")
	}
}
