package storage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var atomicReplaceFile = replaceFile

// AtomicReplaceFile moves an already flushed file onto its target atomically.
// Both paths must be on the same filesystem. On success source no longer exists;
// on failure it remains available for recovery. This allows a saved cache inode
// to be restored without requiring enough free space to write another copy.
func AtomicReplaceFile(source, target string) error {
	return atomicReplaceFile(source, target)
}

// AtomicWriteFile writes data to a temporary file in the target directory and
// atomically replaces the target after the data has been flushed.
func AtomicWriteFile(path string, data []byte, perm fs.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := file.Name()
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		_ = os.Remove(temporaryPath)
	}()

	if err := file.Chmod(perm); err != nil {
		return fmt.Errorf("set temporary file permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	closed = true

	if err := atomicReplaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace target file: %w", err)
	}
	return nil
}
