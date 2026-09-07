package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestFileLockExcludesOtherInstancesAndCanBeReacquired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backend.lock")
	first, err := LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := LockFile(path); !errors.Is(err, ErrLocked) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("expected conflict: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := WaitFileLock(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
}
