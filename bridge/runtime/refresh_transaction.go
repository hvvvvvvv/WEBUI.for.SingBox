package runtime

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"guiforcores/bridge/storage"
)

type refreshBackup struct {
	path, backup string
}

// Keep previous cache inodes until their shared metadata file commits. This
// avoids holding every subscription's previous nodes in router RAM or writing
// all metadata once per node list. It handles ordinary write failures, not a
// multi-file crash transaction. The caller holds the resource domain mutex.
type refreshTransaction struct{ files []refreshBackup }

func (transaction *refreshTransaction) write(path string, data []byte, mode fs.FileMode) error {
	if transaction == nil {
		return storage.AtomicWriteFile(path, data, mode)
	}
	for _, saved := range transaction.files {
		if saved.path == path {
			return storage.AtomicWriteFile(path, data, mode)
		}
	}
	backup, err := backupRefreshFile(path)
	if err != nil {
		return fmt.Errorf("preserve previous cache: %w", err)
	}
	if err := storage.AtomicWriteFile(path, data, mode); err != nil {
		if backup.backup != "" {
			_ = os.Remove(backup.backup)
		}
		return err
	}
	transaction.files = append(transaction.files, backup)
	return nil
}

func backupRefreshFile(path string) (saved refreshBackup, err error) {
	saved.path = path
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return saved, nil
	}
	if err != nil {
		return saved, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSubscriptionBodyBytes {
		return saved, fmt.Errorf("previous cache is not a regular file of at most %d bytes", maxSubscriptionBodyBytes)
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".refresh-*")
	if err != nil {
		return saved, err
	}
	saved.backup = file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(saved.backup)
		}
	}()
	if err = file.Close(); err != nil {
		return saved, err
	}
	if err = os.Remove(saved.backup); err != nil {
		return saved, err
	}
	if os.Link(path, saved.backup) == nil {
		return saved, nil
	}
	// FAT and some overlay filesystems cannot hard-link. Copy through a bounded
	// stream instead; buffers do not grow with the number of refreshed resources.
	err = copyRefreshBackup(path, saved.backup, info.Mode().Perm())
	return saved, err
}

func copyRefreshBackup(path, backup string, mode fs.FileMode) (err error) {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	file, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		if err != nil {
			_ = os.Remove(backup)
		}
	}()
	if err = file.Chmod(mode); err != nil {
		return err
	}
	count, err := io.Copy(file, io.LimitReader(source, maxSubscriptionBodyBytes+1))
	if err != nil {
		return err
	}
	if count > maxSubscriptionBodyBytes {
		return fmt.Errorf("previous cache exceeds size limit")
	}
	if err = file.Sync(); err != nil {
		return err
	}
	return file.Close()
}

func (transaction *refreshTransaction) finish(commit bool) error {
	var failures []error
	for _, saved := range transaction.files {
		if !commit {
			var err error
			if saved.backup == "" {
				err = os.Remove(saved.path)
				if os.IsNotExist(err) {
					err = nil
				}
			} else {
				err = storage.AtomicReplaceFile(saved.backup, saved.path)
			}
			if err != nil {
				// Preserve a recovery backup if restoration itself cannot commit.
				failures = append(failures, fmt.Errorf("restore previous cache %s: %w", saved.path, err))
				continue
			}
		}
		if saved.backup != "" {
			if err := os.Remove(saved.backup); err != nil && !os.IsNotExist(err) {
				slog.Warn("cache refresh backup could not be removed", "component", "runtime", "operation", "refresh_cleanup", "path", saved.backup, "error", err)
			}
		}
	}
	transaction.files = nil
	return errors.Join(failures...)
}
