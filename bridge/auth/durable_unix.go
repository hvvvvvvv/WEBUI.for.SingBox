//go:build !windows

package auth

import "os"

func replaceCredentialFile(source, target string) error { return os.Rename(source, target) }

func syncCredentialDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
