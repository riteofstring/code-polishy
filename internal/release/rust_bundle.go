package release

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func rustToolEntry(manifest Manifest, relative string) bool {
	if manifest.ManifestVersion < 7 {
		return false
	}
	return strings.HasPrefix(relative, ".tools/rust/"+manifest.Host+"/") || relative == rustNextestPath(manifest.Host)
}

func rustNextestPath(host string) string {
	name := ".tools/bin/cargo-nextest"
	if host == "windows-x64" {
		name += ".exe"
	}
	return name
}

func rustToolManifest(manifest Manifest) (Manifest, error) {
	entries := []Entry{}
	for _, entry := range manifest.Entries {
		if rustToolEntry(manifest, entry.Path) {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return Manifest{}, errors.New("the selected release has no authenticated Rust tool inventory")
	}
	for _, name := range []string{"rustc", "rustdoc", "cargo", "rustfmt", "cargo-fmt", "clippy-driver", "cargo-clippy"} {
		suffix := ""
		if manifest.Host == "windows-x64" {
			suffix = ".exe"
		}
		path := ".tools/rust/" + manifest.Host + "/bin/" + name + suffix
		if !slices.ContainsFunc(entries, func(entry Entry) bool { return entry.Path == path && entry.SHA256 != "" }) {
			return Manifest{}, fmt.Errorf("the selected release has no authenticated %s", path)
		}
	}
	if !slices.ContainsFunc(entries, func(entry Entry) bool { return entry.Path == rustNextestPath(manifest.Host) && entry.SHA256 != "" }) {
		return Manifest{}, errors.New("the selected release has no authenticated Nextest executable")
	}
	return Manifest{Entries: entries}, nil
}

func VerifyRustTools(root string) error {
	manifest, present, err := ReadManifest(root)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	tools, err := rustToolManifest(manifest)
	if err != nil {
		return err
	}
	parents := map[string]bool{}
	for _, entry := range tools.Entries {
		if err := rustToolParents(root, entry.Path, parents); err != nil {
			return err
		}
		installed := filepath.Join(root, filepath.FromSlash(entry.Path))
		info, err := os.Lstat(installed)
		if err != nil {
			return fmt.Errorf("rust tools are incomplete; run code-polishy install-rust: %w", err)
		}
		if err := verifyEntry(installed, fs.FileInfoToDirEntry(info), entry); err != nil {
			return fmt.Errorf("rust tools do not match the selected release; run code-polishy install-rust: %w", err)
		}
	}
	return verifyRustInventory(root, manifest, tools.Entries)
}

func rustToolParents(root, relative string, checked map[string]bool) error {
	for parent := filepath.Dir(relative); parent != "." && !checked[parent]; parent = filepath.Dir(parent) {
		info, err := os.Lstat(filepath.Join(root, parent))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("rust tool directory %s must be a contained regular directory; run code-polishy install-rust", parent)
		}
		checked[parent] = true
	}
	return nil
}

func verifyRustInventory(root string, manifest Manifest, entries []Entry) error {
	known := map[string]bool{}
	for _, entry := range entries {
		known[entry.Path] = true
	}
	return filepath.WalkDir(filepath.Join(root, ".tools", "rust", manifest.Host), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || !known[filepath.ToSlash(relative)] {
			return fmt.Errorf("rust tool inventory contains an unrecorded file: %s", path)
		}
		return nil
	})
}

func WriteRustArchive(releaseRoot, output string) (string, error) {
	root, manifest, err := verifiedReleaseRoot(releaseRoot)
	if err != nil {
		return "", err
	}
	if err := VerifyRustTools(root); err != nil {
		return "", err
	}
	tools, err := rustToolManifest(manifest)
	if err != nil {
		return "", err
	}
	destination, err := newOutputPath(output, root)
	if err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".code-polishy-rust-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(temporary.Name())
	if err := writeReleaseZip(temporary, root, tools); err != nil {
		return "", err
	}
	digest, err := fileSHA256(temporary.Name())
	if err != nil {
		return "", err
	}
	return digest, os.Rename(temporary.Name(), destination)
}

func coreArchiveManifest(manifest Manifest) Manifest {
	manifest.Entries = slices.DeleteFunc(slices.Clone(manifest.Entries), func(entry Entry) bool {
		return rustToolEntry(manifest, entry.Path)
	})
	return manifest
}

func MaterializeRustNotices(root string) error {
	host, err := Host()
	if err != nil {
		return err
	}
	for _, destination := range rustReleaseNoticePaths(host, "rust") {
		relative := strings.TrimPrefix(destination, "tools/rust-notices/"+host+"/")
		source := filepath.Join(root, ".tools", "rust", host, "share", filepath.FromSlash(relative))
		target := filepath.Join(root, filepath.FromSlash(destination))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyReleaseFile(source, target, 0o644); err != nil {
			return err
		}
	}
	return nil
}
