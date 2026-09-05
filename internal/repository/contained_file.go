package repository

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (repo Repository) containedRegularFileInfo(root *os.Root, path string) (os.FileInfo, error) {
	normalized, err := repo.NormalizePath(path)
	if err != nil || normalized != path || strings.ContainsAny(path, "\\\x00:") {
		return nil, fmt.Errorf("input path must be a canonical contained repository path")
	}
	parts := strings.Split(path, "/")
	var info os.FileInfo
	for index := range parts {
		info, err = root.Lstat(filepath.FromSlash(strings.Join(parts[:index+1], "/")))
		if err != nil {
			return nil, fmt.Errorf("input path is missing or unreadable: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("input path cannot contain a symbolic link")
		}
		if index < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("input path parent is not a directory")
		}
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input path must name a regular file")
	}
	return info, nil
}

func readContainedFile(root *os.Root, path string, expected os.FileInfo, limit int) ([]byte, error) {
	if expected.Size() > int64(limit) {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	file, err := root.Open(filepath.FromSlash(path))
	if err != nil {
		return nil, fmt.Errorf("input is unreadable: %w", err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || !os.SameFile(expected, before) {
		return nil, fmt.Errorf("input changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("input is unreadable: %w", err)
	}
	after, err := file.Stat()
	if err != nil || !sameContainedFileSnapshot(before, after, len(data)) {
		return nil, fmt.Errorf("input changed while reading")
	}
	if len(data) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	return data, nil
}

func sameContainedFileSnapshot(before, after os.FileInfo, size int) bool {
	return before.Size() == int64(size) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}
