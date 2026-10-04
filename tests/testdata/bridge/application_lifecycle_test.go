package bridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplicationDirectoryLockLivesUntilClose(t *testing.T) {
	options := Options{BaseDir: t.TempDir(), AcquireInstanceLock: true}
	first, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(context.Background())
	if second, err := New(options); err == nil {
		_ = second.Close(context.Background())
		t.Fatal("second backend acquired the same directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
}

func TestApplicationInitializationFailureReleasesLock(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "data/config.yaml")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, []byte("coreLogDays: -1\n"), 0644)
	if app, err := New(Options{BaseDir: base, AcquireInstanceLock: true}); err == nil {
		_ = app.Close(context.Background())
		t.Fatal("invalid configuration accepted")
	}
	_ = os.WriteFile(path, []byte("coreLogDays: 0\n"), 0644)
	app, err := New(Options{BaseDir: base, AcquireInstanceLock: true})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
}
