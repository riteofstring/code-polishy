package release

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func ReadCapabilityCatalog(releaseRoot string, locked Lock) (AuthenticatedCapabilityCatalog, error) {
	root, err := os.OpenRoot(releaseRoot)
	if err != nil {
		return AuthenticatedCapabilityCatalog{}, fmt.Errorf("open locked release catalog: %w", err)
	}
	defer root.Close()
	manifestData, err := readCapabilityFile(root, ManifestFilename, 16<<20)
	if err != nil {
		return AuthenticatedCapabilityCatalog{}, fmt.Errorf("read catalog release manifest: %w", err)
	}
	manifest, err := parseManifest(manifestData, ManifestFilename)
	if err != nil {
		return AuthenticatedCapabilityCatalog{}, err
	}
	if err := manifest.Satisfies(locked); err != nil {
		return AuthenticatedCapabilityCatalog{}, err
	}
	data, err := readCapabilityFile(root, CapabilityCatalogPath, MaximumCapabilityCatalogBytes)
	if err != nil {
		return AuthenticatedCapabilityCatalog{}, fmt.Errorf("read locked release capability catalog: %w", err)
	}
	digest := capabilityContentSHA256(data)
	if digest != manifest.CapabilityCatalogSHA256 || !manifestRecordsCapabilityFile(manifest, CapabilityCatalogPath, digest) {
		return AuthenticatedCapabilityCatalog{}, fmt.Errorf("capability catalog does not match the locked release manifest")
	}
	catalog, err := ParseCapabilityCatalog(data)
	if err != nil {
		return AuthenticatedCapabilityCatalog{}, err
	}
	if err := validateCapabilityWorkflowFiles(root, manifest, catalog); err != nil {
		return AuthenticatedCapabilityCatalog{}, err
	}
	if err := verifyCapabilityManifestUnchanged(root, manifestData); err != nil {
		return AuthenticatedCapabilityCatalog{}, err
	}
	return AuthenticatedCapabilityCatalog{Release: locked, SHA256: digest, Catalog: catalog, proof: newCapabilityCatalogProof(locked, manifest, data)}, nil
}

func verifyCapabilityManifestUnchanged(root *os.Root, expected []byte) error {
	current, err := readCapabilityFile(root, ManifestFilename, 16<<20)
	if err != nil || !bytes.Equal(current, expected) {
		return fmt.Errorf("release manifest changed during catalog discovery")
	}
	return nil
}

func validateCapabilityWorkflowFiles(root *os.Root, manifest Manifest, catalog CapabilityCatalog) error {
	seen := map[string]bool{}
	for _, capability := range catalog.Capabilities {
		for _, workflow := range capability.Workflows {
			if seen[workflow] {
				continue
			}
			seen[workflow] = true
			if !manifestRecordsCapabilityFile(manifest, workflow, "") {
				return fmt.Errorf("capability %q workflow %q is absent from the release manifest", capability.Name, workflow)
			}
			if _, err := capabilityFileInfo(root, workflow); err != nil {
				return fmt.Errorf("capability %q workflow %q: %w", capability.Name, workflow, err)
			}
		}
	}
	return nil
}

func manifestRecordsCapabilityFile(manifest Manifest, path, digest string) bool {
	for _, entry := range manifest.Entries {
		if entry.Path == path && entry.Symlink == "" && entry.SHA256 != "" {
			return digest == "" || entry.SHA256 == digest
		}
	}
	return false
}

func readCapabilityFile(root *os.Root, relative string, maximum int64) ([]byte, error) {
	info, err := capabilityFileInfo(root, relative)
	if err != nil {
		return nil, err
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("%s exceeds %d bytes", relative, maximum)
	}
	file, err := root.Open(filepath.FromSlash(relative))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !os.SameFile(info, before) || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%s changed while opening", relative)
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !sameCapabilityFileRead(before, after, len(data), maximum) {
		return nil, fmt.Errorf("%s changed while reading or exceeded its byte bound", relative)
	}
	return data, nil
}

func sameCapabilityFileRead(before, after os.FileInfo, length int, maximum int64) bool {
	return int64(length) <= maximum && int64(length) == before.Size() && after.Size() == before.Size() && after.ModTime().Equal(before.ModTime())
}

func capabilityFileInfo(root *os.Root, relative string) (os.FileInfo, error) {
	if !capabilityRelativePath(relative) {
		return nil, fmt.Errorf("catalog input requires an exact contained path")
	}
	var info os.FileInfo
	var err error
	parts := strings.Split(relative, "/")
	for index := range parts {
		info, err = root.Lstat(filepath.FromSlash(strings.Join(parts[:index+1], "/")))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || index < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("%s contains a symbolic link or invalid parent", relative)
		}
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", relative)
	}
	return info, nil
}

func capabilityRelativePath(value string) bool {
	return value != "" && value != "." && value != ".." && path.Clean(value) == value &&
		!strings.HasPrefix(value, "../") && !strings.HasPrefix(value, "/") && !strings.ContainsAny(value, "\\\x00:")
}
