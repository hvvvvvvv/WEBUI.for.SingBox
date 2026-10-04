// Run the centralized backend tests with: go run ./tests [go test arguments].
package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	os.Exit(run())
}

func run() int {
	if err := runTests(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runTests() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("run backend tests from the repository root: %w", err)
	}
	backingDir := filepath.Join(root, "tests", "testdata")
	replace := make(map[string]string)
	err = filepath.WalkDir(backingDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(backingDir, path)
		if err != nil {
			return err
		}
		replace[filepath.Join(root, rel)] = path
		return nil
	})
	if err != nil {
		return err
	}
	if len(replace) == 0 {
		return fmt.Errorf("no backend tests found in %s", backingDir)
	}

	// Overlay keeps package-private access and platform build constraints without
	// copying tests into production directories. Go skips testdata directories.
	overlay, err := os.CreateTemp("", "singbox-tests-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(overlay.Name())
	err = json.NewEncoder(overlay).Encode(struct {
		Replace map[string]string
	}{Replace: replace})
	closeErr := overlay.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"./..."}
	}
	cmd := exec.Command("go", append([]string{"test", "-overlay", overlay.Name()}, args...)...)
	cmd.Dir = root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
