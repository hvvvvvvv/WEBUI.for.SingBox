//go:build windows

package storage

import "golang.org/x/sys/windows"

func replaceFile(source string, target string) error {
	return windows.Rename(source, target)
}
