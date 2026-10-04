package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
