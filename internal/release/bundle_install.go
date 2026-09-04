package release

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maximumReleaseBundleBytes int64 = 4 << 30

func InstallLocalBundle(source, expectedSHA256, prefix string) (Manifest, error) {
	if !digestPattern.MatchString(expectedSHA256) {
		return Manifest{}, errors.New("release bundle requires an exact SHA-256 digest")
	}
	canonicalPrefix, err := prepareInstallPrefix(prefix)
	if err != nil {
		return Manifest{}, err
	}
	archive, cleanup, err := acquireBundle(source, expectedSHA256, canonicalPrefix)
	if err != nil {
		return Manifest{}, err
	}
	defer cleanup()
	staging, err := os.MkdirTemp(canonicalPrefix, ".code-polishy-release-")
	if err != nil {
		return Manifest{}, err
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := extractReleaseZip(archive, staging); err != nil {
		return Manifest{}, err
	}
	manifest, launcherEntry, err := verifyStagedBundle(staging)
	if err != nil {
		return Manifest{}, err
	}
	if err := installStagedRelease(staging, canonicalPrefix, manifest, launcherEntry, &keepStaging); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func verifyStagedBundle(staging string) (Manifest, Entry, error) {
	manifest, installed, err := ReadManifest(staging)
	if err != nil || !installed {
		if err == nil {
			err = errors.New("release bundle has no manifest")
		}
		return Manifest{}, Entry{}, err
	}
	host, err := Host()
	if err != nil {
		return Manifest{}, Entry{}, err
	}
	if manifest.Host != host {
		return Manifest{}, Entry{}, fmt.Errorf("release bundle is for %s, not %s", manifest.Host, host)
	}
	if err := manifest.Verify(staging); err != nil {
		return Manifest{}, Entry{}, err
	}
	launcherEntry, ok := manifestEntry(manifest, LauncherBinaryPath)
	if !ok || launcherEntry.SHA256 == "" {
		return Manifest{}, Entry{}, fmt.Errorf("release bundle records no %s launcher", LauncherBinaryPath)
	}
	return manifest, launcherEntry, nil
}

func installStagedRelease(staging, canonicalPrefix string, manifest Manifest, launcherEntry Entry, keepStaging *bool) error {
	target := Directory(canonicalPrefix, LockFor(manifest))
	created, err := publishStagedRelease(staging, target, manifest, keepStaging)
	if err != nil {
		return err
	}
	if err := publishLauncher(target, canonicalPrefix, launcherEntry); err != nil {
		if created {
			_ = os.RemoveAll(target)
		}
		return err
	}
	return nil
}

func publishStagedRelease(staging, target string, manifest Manifest, keepStaging *bool) (bool, error) {
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return false, err
		}
		if err := os.Rename(staging, target); err != nil {
			return false, err
		}
		*keepStaging = true
		return true, nil
	} else if err != nil {
		return false, err
	}
	existing, present, readErr := ReadManifest(target)
	if readErr != nil || !present || existing.ReleaseDigest != manifest.ReleaseDigest || existing.Host != manifest.Host {
		return false, errors.New("installed release target is occupied by different bytes")
	}
	return false, existing.Verify(target)
}

func prepareInstallPrefix(prefix string) (string, error) {
	if prefix == "" {
		return "", errors.New("release install prefix is empty")
	}
	absolute, err := filepath.Abs(prefix)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("release install prefix is not a regular directory")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func acquireBundle(source, expected, directory string) (string, func(), error) {
	temporary, err := os.CreateTemp(directory, ".code-polishy-bundle-*.zip")
	if err != nil {
		return "", nil, err
	}
	path := temporary.Name()
	cleanup := func() { _ = os.Remove(path) }
	reader, err := openLocalBundle(source)
	if err != nil {
		temporary.Close()
		cleanup()
		return "", nil, err
	}
	defer reader.Close()
	if err := copyAndVerifyBundle(temporary, reader, expected); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

func openLocalBundle(source string) (io.ReadCloser, error) {
	if !filepath.IsAbs(source) {
		return nil, errors.New("release bundle source must be an absolute local path")
	}
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("release bundle source is not a regular file")
	}
	return os.Open(source)
}

func copyAndVerifyBundle(temporary *os.File, reader io.Reader, expected string) error {
	digest := sha256.New()
	limited := &io.LimitedReader{R: reader, N: maximumReleaseBundleBytes + 1}
	written, copyErr := io.Copy(io.MultiWriter(temporary, digest), limited)
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if written > maximumReleaseBundleBytes {
		return errors.New("release bundle exceeds the size limit")
	}
	if hex.EncodeToString(digest.Sum(nil)) != expected {
		return errors.New("release bundle checksum mismatch")
	}
	return nil
}

func extractReleaseZip(archive, destination string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	links, err := validateReleaseZip(reader.File)
	if err != nil {
		return err
	}
	return extractReleaseZipEntries(reader.File, destination, links)
}

func validateReleaseZip(entries []*zip.File) (map[string]string, error) {
	if len(entries) == 0 || len(entries) > 100000 {
		return nil, errors.New("release bundle has an invalid entry count")
	}
	var total uint64
	paths := make(map[string]struct{}, len(entries))
	links := make(map[string]string)
	for _, entry := range entries {
		relative, err := validateReleaseZipEntry(entry)
		if err != nil {
			return nil, err
		}
		if _, exists := paths[relative]; exists {
			return nil, fmt.Errorf("release bundle repeats path %q", relative)
		}
		paths[relative] = struct{}{}
		target, err := validatedArchiveLink(entry, relative)
		if err != nil {
			return nil, err
		}
		if target != "" {
			links[relative] = target
		}
		total += entry.UncompressedSize64
		if total > uint64(maximumReleaseBundleBytes) {
			return nil, errors.New("expanded release bundle exceeds the size limit")
		}
	}
	if err := validateArchiveLinkAncestors(paths, links); err != nil {
		return nil, err
	}
	return links, nil
}

func validatedArchiveLink(entry *zip.File, relative string) (string, error) {
	if entry.FileInfo().Mode()&os.ModeSymlink == 0 {
		return "", nil
	}
	target, err := readArchiveLink(entry)
	if err != nil {
		return "", err
	}
	if err := validateArchiveLink(relative, target); err != nil {
		return "", err
	}
	return target, nil
}

func validateArchiveLinkAncestors(paths map[string]struct{}, links map[string]string) error {
	for link := range links {
		prefix := link + "/"
		for candidate := range paths {
			if strings.HasPrefix(candidate, prefix) {
				return fmt.Errorf("release bundle link %q is an ancestor of another entry", link)
			}
		}
	}
	return nil
}

func extractReleaseZipEntries(entries []*zip.File, destination string, links map[string]string) error {
	for _, entry := range entries {
		relative, err := validateReleaseZipEntry(entry)
		if err != nil {
			return err
		}
		if err := extractReleaseZipEntry(entry, filepath.Join(destination, relative), links[relative]); err != nil {
			return err
		}
	}
	return nil
}

func validateReleaseZipEntry(entry *zip.File) (string, error) {
	if strings.Contains(entry.Name, "\\") {
		return "", fmt.Errorf("release bundle contains unsupported path %q", entry.Name)
	}
	relative := filepath.Clean(filepath.FromSlash(entry.Name))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("release bundle path escapes its root: %q", entry.Name)
	}
	return relative, nil
}

func extractReleaseZipEntry(entry *zip.File, installed, linkTarget string) error {
	if entry.FileInfo().IsDir() {
		return os.MkdirAll(installed, 0o755)
	}
	if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
		if linkTarget == "" {
			return fmt.Errorf("release bundle link has no validated target: %q", entry.Name)
		}
		if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
			return err
		}
		return os.Symlink(linkTarget, installed)
	}
	if !entry.Mode().IsRegular() {
		return fmt.Errorf("release bundle entry is not a regular file: %q", entry.Name)
	}
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		return err
	}
	input, err := entry.Open()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(installed, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode().Perm()|0o600)
	if err != nil {
		input.Close()
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, input.Close(), output.Close())
}

func manifestEntry(manifest Manifest, path string) (Entry, bool) {
	for _, entry := range manifest.Entries {
		if entry.Path == path {
			return entry, true
		}
	}
	return Entry{}, false
}

func publishLauncher(releaseDir, prefix string, entry Entry) error {
	source := filepath.Join(releaseDir, filepath.FromSlash(entry.Path))
	destinationDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(destinationDir, ".code-polishy-launcher-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	input, err := os.Open(source)
	if err != nil {
		temporary.Close()
		return err
	}
	_, copyErr := io.Copy(temporary, input)
	result := errors.Join(copyErr, input.Close(), temporary.Chmod(0o755), temporary.Close())
	if result != nil {
		return result
	}
	destination := filepath.Join(destinationDir, filepath.Base(filepath.FromSlash(BinaryPath)))
	return os.Rename(temporaryPath, destination)
}
