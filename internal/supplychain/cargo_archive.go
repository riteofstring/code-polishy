package supplychain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func cargoArchiveLicense(ctx context.Context, data []byte, pkg repository.CargoLockedPackage) (string, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("crate archive is not readable gzip data")
	}
	defer compressed.Close()
	limited := &io.LimitedReader{R: compressed, N: 512<<20 + 1}
	archive := tar.NewReader(limited)
	manifest, err := cargoArchiveManifest(ctx, archive, pkg.Name+"-"+pkg.Version+"/Cargo.toml")
	if limited.N == 0 {
		return "", fmt.Errorf("crate archive exceeds its decompressed byte budget")
	}
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(io.Discard, limited); err != nil || limited.N == 0 {
		return "", fmt.Errorf("crate archive is malformed or exceeds its decompressed byte budget")
	}
	return cargoPublishedLicense(manifest, pkg)
}

func cargoArchiveManifest(ctx context.Context, archive *tar.Reader, expected string) ([]byte, error) {
	var manifest []byte
	for count := 0; count < 65536; count++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			if manifest == nil {
				return nil, fmt.Errorf("crate archive omitted its published Cargo.toml")
			}
			return manifest, nil
		}
		if err != nil {
			return nil, fmt.Errorf("crate archive contains unreadable entries")
		}
		if path.Clean(strings.ReplaceAll(header.Name, "\\", "/")) != expected {
			continue
		}
		if err := cargoManifestHeader(header, expected, manifest != nil); err != nil {
			return nil, err
		}
		manifest, err = io.ReadAll(archive)
		if err != nil {
			return nil, fmt.Errorf("crate archive manifest is unreadable")
		}
	}
	return nil, fmt.Errorf("crate archive exceeds 65536 entries")
}

func cargoManifestHeader(header *tar.Header, expected string, repeated bool) error {
	if header.Name != expected || header.Typeflag != tar.TypeReg || repeated || header.Size > 8<<20 {
		return fmt.Errorf("crate archive has an ambiguous, linked or oversized Cargo.toml")
	}
	return nil
}

func cargoPublishedLicense(data []byte, expected repository.CargoLockedPackage) (string, error) {
	var manifest struct {
		Package struct {
			Name    string `toml:"name"`
			Version string `toml:"version"`
			License string `toml:"license"`
		} `toml:"package"`
	}
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("published crate license metadata is malformed")
	}
	if manifest.Package.Name != expected.Name || manifest.Package.Version != expected.Version {
		return "", fmt.Errorf("published crate manifest does not match the locked package identity")
	}
	return manifest.Package.License, nil
}
