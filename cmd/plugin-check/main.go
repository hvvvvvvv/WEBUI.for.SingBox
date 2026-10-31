// plugin-check validates an independent plugin repository using the app's parser.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"guiforcores/bridge/plugincatalog"
	"guiforcores/bridge/storage"
)

func main() {
	write := flag.Bool("write-index", false, "recompute SHA256 values after editing script files")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: plugin-check [-write-index] <plugin-repository>")
		os.Exit(2)
	}
	if err := check(flag.Arg(0), *write); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(directory string, write bool) error {
	root, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("repository must be a real directory")
	}
	read := func(name string) ([]byte, error) {
		if !filepath.IsLocal(name) || strings.Contains(name, "\\") {
			return nil, fmt.Errorf("invalid path %q", name)
		}
		current := root
		components := strings.Split(name, "/")
		for index, component := range components {
			current = filepath.Join(current, component)
			entry, err := os.Lstat(current)
			if err != nil {
				return nil, err
			}
			if entry.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("symlinks are not allowed: %s", name)
			}
			// Opening a FIFO can block indefinitely before file.Stat can reject it.
			if index == len(components)-1 && !entry.Mode().IsRegular() {
				return nil, fmt.Errorf("not a regular file: %s", name)
			}
		}
		file, err := os.Open(current)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		entry, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if !entry.Mode().IsRegular() {
			return nil, fmt.Errorf("not a regular file: %s", name)
		}
		data, err := io.ReadAll(io.LimitReader(file, (256<<10)+1))
		if err != nil {
			return nil, err
		}
		if len(data) > 256<<10 {
			return nil, fmt.Errorf("file exceeds 256 KiB: %s", name)
		}
		return data, nil
	}
	var updated []byte
	if write {
		data, err := read("index.json")
		if err != nil {
			return err
		}
		// Preserve unknown fields so the strict parser below reports them.
		var index map[string]json.RawMessage
		if err := json.Unmarshal(data, &index); err != nil {
			return err
		}
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal(index["plugins"], &entries); err != nil {
			return err
		}
		if len(entries) == 0 || len(entries) > 32 {
			return fmt.Errorf("invalid plugin count")
		}
		for _, entry := range entries {
			var name string
			if err := json.Unmarshal(entry["path"], &name); err != nil {
				return err
			}
			if !strings.HasPrefix(name, "plugins/") || strings.Contains(name, "..") || strings.Count(name, "/") != 1 || !strings.HasSuffix(name, ".js") {
				return fmt.Errorf("invalid script path %q", name)
			}
			source, err := read(name)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(source)
			entry["sha256"], _ = json.Marshal(hex.EncodeToString(digest[:]))
		}
		index["plugins"], _ = json.Marshal(entries)
		updated, err = json.MarshalIndent(index, "", "  ")
		if err != nil {
			return err
		}
		updated = append(updated, '\n')
	}
	items, err := plugincatalog.Read(func(name string) ([]byte, error) {
		if name == "index.json" && updated != nil {
			return updated, nil
		}
		return read(name)
	})
	if err != nil {
		return err
	}
	if updated != nil {
		if err := storage.AtomicWriteFile(filepath.Join(root, "index.json"), updated, 0644); err != nil {
			return err
		}
	}
	fmt.Printf("Validated %d JavaScript plugins (%s)\n", len(items), plugincatalog.APIVersion)
	return nil
}
