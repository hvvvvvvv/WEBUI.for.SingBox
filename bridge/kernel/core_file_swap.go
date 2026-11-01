package kernel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var errCoreFileRestoreUnconfirmed = errors.New("core executable restoration could not be confirmed")

// Caller holds coreTransitionMu and has confirmed this branch is stopped.
// Retain both versions so a failed start can undo the swap before recovering.
func swapCoreFiles(current, backup string) error {
	for _, path := range []string{current, backup} {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("core executable is not a regular file: %s", path)
		}
	}
	directory, err := os.MkdirTemp(filepath.Dir(current), ".core-swap-*")
	if err != nil {
		return err
	}
	// Remove only an empty staging directory. A restoration failure retains the
	// original executable there for manual recovery instead of deleting it.
	defer os.Remove(directory)
	temporary := filepath.Join(directory, "previous")
	if err := os.Rename(current, temporary); err != nil {
		return err
	}
	if err := os.Rename(backup, current); err != nil {
		if restoreErr := os.Rename(temporary, current); restoreErr != nil {
			return errors.Join(errCoreFileRestoreUnconfirmed, err, restoreErr)
		}
		return err
	}
	if err := os.Rename(temporary, backup); err != nil {
		if restoreErr := os.Rename(current, backup); restoreErr != nil {
			return errors.Join(errCoreFileRestoreUnconfirmed, err, restoreErr)
		}
		if restoreErr := os.Rename(temporary, current); restoreErr != nil {
			return errors.Join(errCoreFileRestoreUnconfirmed, err, restoreErr)
		}
		return err
	}
	return nil
}
