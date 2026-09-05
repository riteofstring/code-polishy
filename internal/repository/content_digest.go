package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const MaximumContentDigestBytes = 64 << 20

func (repo Repository) ContentDigest(path string) (string, error) {
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return "", err
	}
	defer root.Close()
	expected, err := repo.containedRegularFileInfo(root, path)
	if err != nil {
		return "", err
	}
	if expected.Size() > MaximumContentDigestBytes {
		return "", fmt.Errorf("content identity exceeds %d bytes", MaximumContentDigestBytes)
	}
	file, err := root.Open(filepath.FromSlash(path))
	if err != nil {
		return "", err
	}
	defer file.Close()
	return boundedContentDigest(file, expected)
}

func boundedContentDigest(file *os.File, expected os.FileInfo) (string, error) {
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || !os.SameFile(expected, before) {
		return "", fmt.Errorf("content changed while opening")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, MaximumContentDigestBytes+1))
	if err != nil {
		return "", err
	}
	after, err := file.Stat()
	if err != nil || size > MaximumContentDigestBytes || before.Size() != size || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", fmt.Errorf("content changed or exceeded its byte limit while reading")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
