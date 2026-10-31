package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"guiforcores/bridge/plugincatalog"
	"guiforcores/bridge/plugins"
)

func writeFixtureFile(t *testing.T, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(name, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFixtureFile(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func writeFixtureIndex(t *testing.T, directory string, index any) []byte {
	t.Helper()
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(directory, "index.json"), data)
	return data
}

func pluginRepository(t *testing.T, sources ...string) (string, plugincatalog.Index) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	index := plugincatalog.Index{APIVersion: plugincatalog.APIVersion}
	for number, source := range sources {
		digest := sha256.Sum256([]byte(source))
		entry := plugincatalog.Entry{
			ID: fmt.Sprintf("fixture-%d", number), Name: fmt.Sprintf("Fixture %d", number),
			Description: "Preserve plugin metadata", Version: "v1.2.3",
			Triggers: []string{plugins.OnSubscribe}, Configuration: map[string]any{"prefix": "test"},
			Path: fmt.Sprintf("plugins/fixture-%d.js", number), SHA256: hex.EncodeToString(digest[:]),
		}
		writeFixtureFile(t, filepath.Join(directory, filepath.FromSlash(entry.Path)), []byte(source))
		index.Plugins = append(index.Plugins, entry)
	}
	writeFixtureIndex(t, directory, index)
	return directory, index
}

func requireRejectedUnchanged(t *testing.T, directory string, write bool, expected string) {
	t.Helper()
	name := filepath.Join(directory, "index.json")
	before := readFixtureFile(t, name)
	err := check(directory, write)
	if err == nil || (expected != "" && !strings.Contains(err.Error(), expected)) {
		t.Fatalf("expected rejection containing %q, got %v", expected, err)
	}
	if after := readFixtureFile(t, name); !bytes.Equal(after, before) {
		t.Fatal("failed check modified the index")
	}
}

func TestWriteIndexUpdatesDigestsOnlyAfterFullValidation(t *testing.T) {
	directory, original := pluginRepository(t, "function onSubscribe(p) { return p }", "function onSubscribe(p) { p.reverse(); return p }")
	changed := "throw Error('validation must not execute scripts'); function onSubscribe(p) { return p.slice(1) }"
	writeFixtureFile(t, filepath.Join(directory, filepath.FromSlash(original.Plugins[0].Path)), []byte(changed))
	requireRejectedUnchanged(t, directory, false, "checksum mismatch")
	if err := check(directory, true); err != nil {
		t.Fatalf("write-index failed: %v", err)
	}
	var updated plugincatalog.Index
	if err := json.Unmarshal(readFixtureFile(t, filepath.Join(directory, "index.json")), &updated); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(changed))
	if updated.Plugins[0].SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("updated digest does not match edited script")
	}
	updated.Plugins[0].SHA256 = original.Plugins[0].SHA256
	want, _ := json.Marshal(original)
	got, _ := json.Marshal(updated)
	if !bytes.Equal(want, got) {
		t.Fatal("write-index changed metadata or an unedited script digest")
	}
	if err := check(directory, false); err != nil {
		t.Fatalf("written index cannot pass a normal check: %v", err)
	}
}

func TestWriteIndexValidationFailureNeverChangesManifest(t *testing.T) {
	for _, failure := range []string{"invalid script", "unknown field", "invalid metadata", "missing script"} {
		t.Run(failure, func(t *testing.T) {
			directory, index := pluginRepository(t, "function onSubscribe(p) { return p }", "function onSubscribe(p) { return p }")
			writeFixtureFile(t, filepath.Join(directory, filepath.FromSlash(index.Plugins[0].Path)), []byte("function onSubscribe(p) { p.reverse(); return p }"))
			switch failure {
			case "invalid script":
				writeFixtureFile(t, filepath.Join(directory, filepath.FromSlash(index.Plugins[1].Path)), []byte("function {"))
			case "unknown field":
				writeFixtureIndex(t, directory, map[string]any{"apiVersion": index.APIVersion, "plugins": index.Plugins, "unexpected": true})
			case "invalid metadata":
				index.Plugins[1].Triggers = []string{"on::startup"}
				writeFixtureIndex(t, directory, index)
			case "missing script":
				if err := os.Remove(filepath.Join(directory, filepath.FromSlash(index.Plugins[1].Path))); err != nil {
					t.Fatal(err)
				}
			}
			requireRejectedUnchanged(t, directory, true, "")
		})
	}
}

func TestCheckRejectsScriptPathTraversalWithoutRewriting(t *testing.T) {
	for _, path := range []string{"../outside.js", "plugins/../outside.js", "plugins/../../outside.js", "plugins/./fixture-0.js", `plugins\..\outside.js`, "/tmp/outside.js", `C:\outside.js`, "plugins/%2e%2e.js"} {
		for _, write := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/write=%v", path, write), func(t *testing.T) {
				directory, index := pluginRepository(t, "function onSubscribe(p) { return p }")
				index.Plugins[0].Path = path
				writeFixtureIndex(t, directory, index)
				requireRejectedUnchanged(t, directory, write, "")
			})
		}
	}
}

func createSymlink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("Windows symlink privilege is unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func TestCheckRejectsRepositoryDirectoryIndexAndScriptSymlinks(t *testing.T) {
	for _, target := range []string{"repository", "plugins", "index.json", "plugins/fixture-0.js"} {
		for _, write := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/write=%v", target, write), func(t *testing.T) {
				directory, _ := pluginRepository(t, "function onSubscribe(p) { return p }")
				if target == "repository" {
					link := filepath.Join(t.TempDir(), "repository-link")
					createSymlink(t, directory, link)
					requireRejectedUnchanged(t, link, write, "real directory")
					return
				}
				name := filepath.Join(directory, filepath.FromSlash(target))
				real := name + ".original"
				if err := os.Rename(name, real); err != nil {
					t.Fatal(err)
				}
				createSymlink(t, real, name)
				requireRejectedUnchanged(t, directory, write, "symlinks")
			})
		}
	}
}

func TestCheckRejectsOversizedInputWithoutRewriting(t *testing.T) {
	for _, kind := range []string{"index", "script", "count", "total"} {
		for _, write := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/write=%v", kind, write), func(t *testing.T) {
				sources := []string{"function onSubscribe(p) { return p }"}
				switch kind {
				case "script":
					sources[0] = "/*" + strings.Repeat("x", 256<<10) + "*/"
				case "count":
					for len(sources) < 33 {
						sources = append(sources, sources[0])
					}
				case "total":
					sources[0] = "/*" + strings.Repeat("x", (256<<10)-4) + "*/"
					for len(sources) < 17 {
						sources = append(sources, sources[0])
					}
				}
				directory, _ := pluginRepository(t, sources...)
				if kind == "index" {
					writeFixtureFile(t, filepath.Join(directory, "index.json"), []byte(strings.Repeat(" ", (256<<10)+1)))
				}
				requireRejectedUnchanged(t, directory, write, "")
			})
		}
	}
}
