package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/storage"
)

func TestLocalAuthRecoveryDoesNotDependOnOtherApplicationConfiguration(t *testing.T) {
	directory := t.TempDir()
	paths := storage.NewPaths(directory)
	if err := os.MkdirAll(paths.Resolve("data"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"secret.key", "plugins.json", "app.json"} {
		if err := os.WriteFile(paths.Resolve("data/"+name), []byte("damaged"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	password := "recovered from local console"
	if err := resetAuthInDirectory(directory, password, &output); err != nil {
		t.Fatal(err)
	}
	authentication := auth.NewService(paths)
	status, err := authentication.Status()
	if err != nil || !status.Enabled || status.TOTPEnabled || !authentication.VerifySecret(password) {
		t.Fatalf("recovery failed: %+v %v", status, err)
	}
	if strings.Contains(output.String(), password) {
		t.Fatal("recovery logged password")
	}
	if err := resetAuthInDirectory(directory, "clear", &output); err != nil {
		t.Fatal(err)
	}
	status, err = authentication.Status()
	if err != nil || status.Enabled || status.TOTPEnabled {
		t.Fatal("clear failed", err)
	}
}

func TestLocalAuthRecoveryRefusesRunningBackend(t *testing.T) {
	directory := t.TempDir()
	paths := storage.NewPaths(directory)
	service := auth.NewService(paths)
	if err := service.SetSecret("original"); err != nil {
		t.Fatal(err)
	}
	lock, err := storage.LockFile(paths.Resolve("data/backend.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	var output bytes.Buffer
	if err := resetAuthInDirectory(directory, "replacement", &output); err == nil {
		t.Fatal("reset raced active backend")
	}
	if !service.VerifySecret("original") {
		t.Fatal("failed recovery changed password")
	}
}

func TestRecoveryStdinIsBoundedAndDoesNotTrimPasswordSpaces(t *testing.T) {
	value, err := readRecoveryPassword(strings.NewReader("  password with spaces  \r\n"))
	if err != nil || value != "  password with spaces  " {
		t.Fatalf("stdin changed password: %q %v", value, err)
	}
	for _, input := range []string{"", "\n", "clear\n", strings.Repeat("x", 1025)} {
		if _, err := readRecoveryPassword(strings.NewReader(input)); err == nil {
			t.Fatal("invalid recovery input accepted")
		}
	}
}
