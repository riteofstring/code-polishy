package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type assetLinkInput struct {
	Link   string          `json:"link"`
	Target string          `json:"target"`
	Files  []assetLinkFile `json:"files"`
}

type assetLinkFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func (repo Repository) IsSymbolicLink(path string) bool {
	info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(path)))
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func (repo Repository) InputDigest(path string) (string, error) {
	data, link, err := repo.AssetLinkIdentity(path)
	if err != nil {
		return "", err
	}
	if !link {
		return repo.ContentDigest(path)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (repo Repository) AssetLinkIdentity(path string) ([]byte, bool, error) {
	normalized, err := repo.NormalizePath(path)
	if err != nil || normalized != path || strings.ContainsAny(path, "\\\x00:") {
		return nil, false, fmt.Errorf("asset input must be a canonical contained path")
	}
	raw := filepath.Join(repo.Root, filepath.FromSlash(path))
	before, err := os.Lstat(raw)
	if err != nil {
		return nil, false, err
	}
	if before.Mode()&os.ModeSymlink == 0 {
		return nil, false, nil
	}
	data, err := repo.captureAssetLink(path, raw, before)
	return data, true, err
}

func (repo Repository) captureAssetLink(path, raw string, before os.FileInfo) ([]byte, error) {
	text, err := os.Readlink(raw)
	if err != nil {
		return nil, err
	}
	target, err := repo.Resolve(path)
	if err != nil {
		return nil, err
	}
	canonicalRoot, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(canonicalRoot, target)
	if err != nil {
		return nil, err
	}
	targetBefore, err := os.Lstat(target)
	if err != nil {
		return nil, err
	}
	files, err := repo.assetLinkFiles(canonicalRoot, target)
	if err != nil {
		return nil, err
	}
	if err := unchangedAssetLink(raw, target, text, before, targetBefore); err != nil {
		return nil, err
	}
	return json.Marshal(assetLinkInput{Link: text, Target: filepath.ToSlash(relative), Files: files})
}

func (repo Repository) assetLinkFiles(root, target string) ([]assetLinkFile, error) {
	files := []assetLinkFile{}
	snapshots := map[string]os.FileInfo{}
	err := filepath.WalkDir(target, func(absolute string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if len(snapshots) >= 10000 {
			return fmt.Errorf("asset link exceeds 10000 entries")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		snapshots[absolute] = info
		if entry.IsDir() {
			return nil
		}
		file, err := repo.assetLinkFile(root, absolute, info)
		if err != nil {
			return err
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, unchangedAssetEntries(snapshots)
}

func (repo Repository) assetLinkFile(root, absolute string, info os.FileInfo) (assetLinkFile, error) {
	name, err := filepath.Rel(root, absolute)
	if err != nil {
		return assetLinkFile{}, err
	}
	name = filepath.ToSlash(name)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 != 0 || !assetLinkExtension(name) || repo.IsControlInput(name) || repo.IsExecutableSource(name) || repo.IsExcluded(name) || len(repo.OwnerModuleNames(name)) != 1 {
		return assetLinkFile{}, fmt.Errorf("asset link target %s is not an owned contained asset", name)
	}
	digest, err := repo.ContentDigest(name)
	return assetLinkFile{Path: name, SHA256: digest}, err
}

func unchangedAssetEntries(snapshots map[string]os.FileInfo) error {
	for path, before := range snapshots {
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return fmt.Errorf("asset entry changed during verification")
		}
	}
	return nil
}

func unchangedAssetLink(path, target, text string, before, targetBefore os.FileInfo) error {
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		return fmt.Errorf("asset link changed during verification")
	}
	current, err := os.Readlink(path)
	if err != nil || current != text {
		return fmt.Errorf("asset link target changed during verification")
	}
	targetAfter, err := os.Lstat(target)
	if err != nil || !os.SameFile(targetBefore, targetAfter) || targetBefore.ModTime() != targetAfter.ModTime() {
		return fmt.Errorf("asset target changed during verification")
	}
	return nil
}

func assetLinkExtension(path string) bool {
	return slices.Contains([]string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".avif", ".ico", ".mp3", ".wav", ".ogg", ".flac", ".mp4", ".webm", ".woff", ".woff2", ".ttf", ".otf", ".eot", ".pdf"}, strings.ToLower(filepath.Ext(path)))
}
