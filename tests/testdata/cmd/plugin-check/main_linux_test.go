package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCheckRejectsNamedPipeBeforeOpening(t *testing.T) {
	directory, index := pluginRepository(t, "function onSubscribe(p) { return p }")
	manifest := readFixtureFile(t, filepath.Join(directory, "index.json"))
	name := filepath.Join(directory, filepath.FromSlash(index.Plugins[0].Path))
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(name, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- check(directory, true) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("named pipe was not rejected: %v", err)
		}
		if !bytes.Equal(manifest, readFixtureFile(t, filepath.Join(directory, "index.json"))) {
			t.Fatal("rejected named pipe modified the index")
		}
	case <-time.After(2 * time.Second):
		// Release a regressed reader so a failing test does not leave it blocked.
		if fd, err := unix.Open(name, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
			_ = unix.Close(fd)
		}
		t.Fatal("validation tried to open a named pipe and blocked")
	}
}
