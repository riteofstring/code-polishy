package supplychain

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func prepareOSVInput(repo repository.Repository, scan osvScan) error {
	if scan.InputPath == "" {
		return nil
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return fmt.Errorf("open OSV input repository: %w", err)
	}
	defer root.Close()
	if err := ensureOSVInputDirectory(root); err != nil {
		return err
	}
	file, err := root.OpenFile(filepath.FromSlash(scan.InputPath), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return verifyOSVInput(root, scan)
	}
	if err != nil {
		return fmt.Errorf("create bounded OSV input: %w", err)
	}
	_, writeErr := file.Write(scan.Projection)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = root.Remove(filepath.FromSlash(scan.InputPath))
		return fmt.Errorf("write bounded OSV input: %w", err)
	}
	return verifyOSVInput(root, scan)
}

func ensureOSVInputDirectory(root *os.Root) error {
	segments := strings.Split(osvInputDirectory, "/")
	for index := range segments {
		path := filepath.Join(segments[:index+1]...)
		if err := root.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create OSV input directory: %w", err)
		}
		info, err := root.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("OSV input directory must contain only regular directories")
		}
	}
	return nil
}

func verifyOSVInput(root *os.Root, scan osvScan) error {
	info, err := root.Lstat(filepath.FromSlash(scan.InputPath))
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(scan.Projection)) {
		return fmt.Errorf("OSV input does not match its planned regular file")
	}
	file, err := root.Open(filepath.FromSlash(scan.InputPath))
	if err != nil {
		return fmt.Errorf("open planned OSV input: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return fmt.Errorf("OSV input changed during opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(len(scan.Projection))+1))
	if err != nil || !bytes.Equal(data, scan.Projection) {
		return fmt.Errorf("OSV input content differs from its public package projection")
	}
	return nil
}
