package supplychain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCargoArchiveRejectsAmbiguousOrMismatchedPublishedManifests(t *testing.T) {
	t.Parallel()
	manifest := "[package]\nname='crate'\nversion='1.0.0'\nlicense='MIT'\n"
	valid := cargoTestEntry{Name: "crate-1.0.0/Cargo.toml", Content: manifest}
	tests := map[string][]cargoTestEntry{
		"missing":           {{Name: "crate-1.0.0/Cargo.toml.orig", Content: manifest}},
		"duplicate":         {valid, valid},
		"alias":             {valid, {Name: "crate-1.0.0/./Cargo.toml", Content: manifest}},
		"backslash alias":   {{Name: "crate-1.0.0\\Cargo.toml", Content: manifest}},
		"symbolic link":     {{Name: valid.Name, Kind: tar.TypeSymlink}},
		"hard link":         {{Name: valid.Name, Kind: tar.TypeLink}},
		"wrong package":     {{Name: valid.Name, Content: strings.Replace(manifest, "name='crate'", "name='other'", 1)}},
		"wrong version":     {{Name: valid.Name, Content: strings.Replace(manifest, "1.0.0", "2.0.0", 1)}},
		"malformed TOML":    {{Name: valid.Name, Content: "[package"}},
		"inherited license": {{Name: valid.Name, Content: "[package]\nname='crate'\nversion='1.0.0'\nlicense.workspace=true\n"}},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			data := cargoArchiveFixture(t, entries)
			pkg := repository.CargoLockedPackage{Name: "crate", Version: "1.0.0"}
			if license, err := cargoArchiveLicense(t.Context(), data, pkg); err == nil {
				t.Fatalf("%s archive established license %q", name, license)
			}
		})
	}
}

func TestCargoArchiveRejectsTruncationAndArchiveResourceExhaustion(t *testing.T) {
	t.Parallel()
	pkg := repository.CargoLockedPackage{Name: "crate", Version: "1.0.0"}
	manifest := cargoTestEntry{Name: "crate-1.0.0/Cargo.toml", Content: "[package]\nname='crate'\nversion='1.0.0'\nlicense='MIT'\n"}
	complete := cargoArchiveFixture(t, []cargoTestEntry{manifest})
	entries := make([]cargoTestEntry, 65537)
	entries[0] = manifest
	for index := 1; index < len(entries); index++ {
		entries[index] = cargoTestEntry{Name: "crate-1.0.0/empty"}
	}
	for name, data := range map[string][]byte{
		"not gzip": []byte("not an archive"), "truncated gzip": complete[:len(complete)-4],
		"large manifest": cargoArchiveFixture(t, []cargoTestEntry{{Name: manifest.Name, Content: manifest.Content + strings.Repeat(" ", 8<<20)}}),
		"entry count":    cargoArchiveFixture(t, entries),
	} {
		if _, err := cargoArchiveLicense(t.Context(), data, pkg); err == nil {
			t.Fatalf("%s archive passed bounded license decoding", name)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := cargoArchiveLicense(ctx, complete, pkg); err == nil {
		t.Fatal("canceled archive inspection passed")
	}
}

func TestCargoArchiveChecksTheDecompressedStreamBudget(t *testing.T) {
	t.Parallel()
	buffer := bytes.Buffer{}
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	if err := archive.WriteHeader(&tar.Header{Name: "crate-1.0.0/oversized", Size: 513 << 20, Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	block := make([]byte, 1<<20)
	for index := 0; index < 513; index++ {
		if _, err := archive.Write(block); err != nil {
			t.Fatal(err)
		}
	}
	manifest := "[package]\nname='crate'\nversion='1.0.0'\nlicense='MIT'\n"
	if err := archive.WriteHeader(&tar.Header{Name: "crate-1.0.0/Cargo.toml", Size: int64(len(manifest)), Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cargoArchiveLicense(t.Context(), buffer.Bytes(), repository.CargoLockedPackage{Name: "crate", Version: "1.0.0"}); err == nil || !strings.Contains(err.Error(), "decompressed byte budget") {
		t.Fatalf("compressed archive budget was not enforced: %v", err)
	}
}
