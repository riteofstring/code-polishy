package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func acquirePackMigrationLock(repoRoot string) (func(), error) {
	directory := filepath.Join(repoRoot, filepath.FromSlash(PackMigrationDirectory))
	if err := ensureManagedReportDirectory(repoRoot, directory); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, ".write-lock")
	created, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if err := created.Close(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("pack migration write lock must be a regular file")
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, errors.New("pack migration write lock changed during opening")
	}
	if err := lockPackMigrationFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("pack migration is busy or cannot acquire its write lock: %w", err)
	}
	return func() { _ = file.Close() }, nil
}
