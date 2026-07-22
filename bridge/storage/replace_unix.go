//go:build !windows

package storage

import "os"

func replaceFile(source string, target string) error {
	return os.Rename(source, target)
}
