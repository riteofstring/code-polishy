package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var releasePinPaths = struct {
	version, goVersion, govulncheck, node, osv, pnpm, ruff, shellcheck, staticcheck string
}{
	"VERSION", "scripts/go_version.txt", "tools/govulncheck-version.txt", "tools/node-version.txt",
	"tools/osv-scanner-version.txt", "tools/pnpm-version.txt", "tools/ruff-version.txt",
	"tools/shellcheck-version.txt", "tools/staticcheck-version.txt",
}

// WriteManifest gives a staged native release its complete byte-level record.
// The stage must not already contain a manifest and is never published by this
// function; callers verify it and archive or install it only after success.
func WriteManifest(releaseDir, sourceRevision string) (Manifest, error) {
	root, err := filepath.EvalSymlinks(releaseDir)
	if err != nil {
		return Manifest{}, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Manifest{}, errors.New("release stage must be an existing directory")
	}
	manifestPath := filepath.Join(root, ManifestFilename)
	if _, err := os.Lstat(manifestPath); !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, errors.New("release stage already contains a manifest")
	}
	manifest, err := composeReleaseManifest(root, sourceRevision)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateManifestIdentity(manifest, manifestPath); err != nil {
		return Manifest{}, err
	}
	if err := validateEntries(manifest, manifestPath); err != nil {
		return Manifest{}, err
	}
	if err := writeReleaseManifestFile(root, manifestPath, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func composeReleaseManifest(root, sourceRevision string) (Manifest, error) {
	host, err := Host()
	if err != nil {
		return Manifest{}, err
	}
	version, err := readReleasePin(root, releasePinPaths.version, false)
	if err != nil {
		return Manifest{}, err
	}
	pins := make([]string, 8)
	for index, path := range []string{releasePinPaths.goVersion, releasePinPaths.govulncheck, releasePinPaths.node, releasePinPaths.osv, releasePinPaths.pnpm, releasePinPaths.ruff, releasePinPaths.shellcheck, releasePinPaths.staticcheck} {
		pins[index], err = readReleasePin(root, path, true)
		if err != nil {
			return Manifest{}, err
		}
	}
	entries, err := collectReleaseEntries(root)
	if err != nil {
		return Manifest{}, err
	}
	return newReleaseManifest(version, sourceRevision, host, pins, entries), nil
}

func readReleasePin(root, relative string, trimV bool) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return "", fmt.Errorf("read release pin %s: %w", relative, err)
	}
	value := strings.TrimSpace(string(data))
	if trimV {
		value = strings.TrimPrefix(value, "v")
	}
	if !versionPattern.MatchString(value) {
		return "", fmt.Errorf("release pin %s is invalid", relative)
	}
	return value, nil
}

func newReleaseManifest(version, sourceRevision, host string, pins []string, entries []Entry) Manifest {
	manifest := Manifest{ManifestVersion: ManifestVersion, CodePolishyVersion: version, SourceRevision: sourceRevision,
		Host: host, Features: []string{"javascript-bundle"}, Tools: Tools{Go: pins[0], Govulncheck: pins[1], Node: pins[2], OSVScanner: pins[3], PNPM: pins[4], Ruff: pins[5], Shellcheck: pins[6], Staticcheck: pins[7]}, Entries: entries, EntryCount: len(entries)}
	manifest.ReleaseDigest = manifest.Identity()
	manifest.ContentDigest = releaseEntriesDigest(entries)
	return manifest
}

func writeReleaseManifestFile(root, manifestPath string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(root, ".release-manifest-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	_, writeErr := temporary.Write(append(data, '\n'))
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(temporaryPath, manifestPath)
}

func collectReleaseEntries(root string) ([]Entry, error) {
	entries := []Entry{}
	err := filepath.WalkDir(root, func(installed string, found fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if found.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, installed)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ManifestFilename || strings.HasPrefix(filepath.Base(relative), ".release-manifest-") {
			return nil
		}
		entry := Entry{Path: relative}
		if found.Type()&fs.ModeSymlink != 0 {
			entry.Symlink, err = os.Readlink(installed)
		} else if found.Type().IsRegular() {
			entry.SHA256, err = fileSHA256(installed)
		} else {
			return fmt.Errorf("release entry %s is neither a regular file nor a symlink", relative)
		}
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(first, second Entry) int { return strings.Compare(first.Path, second.Path) })
	if len(entries) == 0 {
		return nil, errors.New("release stage contains no entries")
	}
	return entries, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func releaseEntriesDigest(entries []Entry) string {
	lines := &bytes.Buffer{}
	for _, entry := range entries {
		value := entry.SHA256
		if entry.Symlink != "" {
			value = "symlink " + entry.Symlink
		}
		fmt.Fprintf(lines, "./%s\t%s\n", entry.Path, value)
	}
	digest := sha256.Sum256(lines.Bytes())
	return hex.EncodeToString(digest[:])
}

// EntriesDigest returns the canonical content identity for one ordered entry
// list. It is exported for release tooling that constructs a manifest fixture
// before writing the document through the normal parser.
func EntriesDigest(entries []Entry) string { return releaseEntriesDigest(entries) }
