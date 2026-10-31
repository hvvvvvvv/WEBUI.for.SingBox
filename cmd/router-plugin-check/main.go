// router-plugin-check validates an independent plugin repository and its index.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"guiforcores/bridge/routerplugin"
	"guiforcores/bridge/storage"
)

func main() {
	write := flag.Bool("write-index", false, "regenerate index.json after validating manifests")
	flag.Parse()
	root := "."
	if flag.NArg() == 1 {
		root = flag.Arg(0)
	} else if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "expected one plugin repository directory")
		os.Exit(1)
	}
	if err := run(root, *write); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(root string, write bool) error {
	pluginDir := filepath.Join(root, "plugins")
	info, err := os.Lstat(pluginDir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("plugins must be a real directory, not a symlink")
	}
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return err
	}
	if len(entries) == 0 || len(entries) > 32 {
		return fmt.Errorf("plugins must contain 1 to 32 files")
	}
	type entry struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	index := struct {
		APIVersion string  `json:"apiVersion"`
		Plugins    []entry `json:"plugins"`
	}{APIVersion: "webui.router.catalog/v1"}
	var manifests []routerplugin.Manifest
	namePattern := regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,80}[.]json$`)
	for _, file := range entries {
		if file.IsDir() {
			return fmt.Errorf("plugin directory contains a nested directory: %s", file.Name())
		}
		if !namePattern.MatchString(file.Name()) || bytes.Contains([]byte(file.Name()), []byte("..")) || file.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid plugin filename: %s", file.Name())
		}
		handle, err := os.Open(filepath.Join(root, "plugins", file.Name()))
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(handle, 32*1024+1))
		_ = handle.Close()
		if err != nil || len(data) > 32*1024 {
			return fmt.Errorf("plugin too large or unreadable: %s", file.Name())
		}
		manifest, err := routerplugin.ParseManifest(string(data))
		if err != nil {
			return fmt.Errorf("%s: %w", file.Name(), err)
		}
		manifests = append(manifests, manifest)
		digest := sha256.Sum256(data)
		index.Plugins = append(index.Plugins, entry{Path: "plugins/" + file.Name(), SHA256: hex.EncodeToString(digest[:])})
	}
	if err := routerplugin.ValidateCatalog(manifests); err != nil {
		return err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	indexPath := filepath.Join(root, "index.json")
	if write {
		if err := storage.AtomicWriteFile(indexPath, data, 0644); err != nil {
			return err
		}
	} else {
		file, err := os.Open(indexPath)
		if err != nil {
			return err
		}
		existing, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
		_ = file.Close()
		if err != nil || len(existing) > 64*1024 {
			return fmt.Errorf("index.json is unreadable or too large")
		}
		var compact bytes.Buffer
		var expected bytes.Buffer
		if json.Compact(&compact, existing) != nil || json.Compact(&expected, data) != nil || !bytes.Equal(compact.Bytes(), expected.Bytes()) {
			return fmt.Errorf("index.json is stale; run router-plugin-check -write-index")
		}
	}
	fmt.Printf("Validated %d router plugins and SHA256 index\n", len(manifests))
	return nil
}
