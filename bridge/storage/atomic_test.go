package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFileReplacesTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AtomicWriteFile(target, []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("target content = %q, want new", data)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("target permissions = %o, want 640", info.Mode().Perm())
	}
}

func TestAtomicWriteFilePreservesTargetAndCleansTemporaryFileOnReplaceFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	previousReplace := atomicReplaceFile
	atomicReplaceFile = func(string, string) error { return errors.New("replace failed") }
	t.Cleanup(func() { atomicReplaceFile = previousReplace })

	if err := AtomicWriteFile(target, []byte("new"), 0o644); err == nil {
		t.Fatal("expected replace failure")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("target content = %q, want old", data)
	}
	temporaryFiles, err := filepath.Glob(filepath.Join(dir, ".settings.yaml.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("temporary files were not cleaned: %v", temporaryFiles)
	}
}
