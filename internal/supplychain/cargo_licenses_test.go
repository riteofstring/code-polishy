package supplychain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCargoLicensesApplySharedSPDXPolicyToEveryLockedVersion(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.AllowedLicenses = []string{"MIT", "Apache-2.0"}
	allowed := cachedCargoFixture(t, repo, "registry-one", "crate", "1.0.0", "MIT OR Apache-2.0")
	refused := cachedCargoFixture(t, repo, "registry-one", "crate", "2.0.0", "AGPL-3.0-only")
	lock := cargoLicenseTestLock(allowed, refused)
	lock += "[[package]]\nname='crate'\nversion='1.0.0'\nsource='git+https://private.example.test/repo#" + strings.Repeat("a", 40) + "'\n"
	writeSupplyFile(t, repo.Root, "Cargo.lock", lock)
	writeSupplyFile(t, repo.Root, "nested/Cargo.lock", cargoLicenseTestLock(refused))
	files := []string{"Cargo.lock", "nested/Cargo.lock"}
	findings := Static(t.Context(), repo, files)
	if len(findings) != 2 || findings[0].Check != "supplyChain.dependencyLicense" || findings[1].Check != "supplyChain.dependencyLicense" || findings[0].Subject != "crate@2.0.0" || findings[1].Subject != "crate@2.0.0" || findings[0].Path == findings[1].Path {
		t.Fatalf("complete locked license policy = %+v", findings)
	}
	if findings := DependencyReviewStatic(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("pre-install review enforced cached license evidence: %+v", findings)
	}
	repo.Config.SupplyChain.AllowedLicenses = nil
	if findings := Static(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("unconfigured license policy enforced a license: %+v", findings)
	}
}

func TestCargoLicensesRequireArchiveIdentityNotCacheDirectoryNames(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
	_ = cachedCargoFixture(t, repo, "aaa-private-index", "crate", "1.0.0", "AGPL-3.0-only")
	public := cachedCargoFixture(t, repo, "zzz-public-index", "crate", "1.0.0", "MIT")
	writeSupplyFile(t, repo.Root, cargoArchiveCache+"/cache-note", "unrelated cache metadata")
	writeSupplyFile(t, repo.Root, "Cargo.lock", cargoLicenseTestLock(public))
	if findings := Static(t.Context(), repo, []string{"Cargo.lock"}); len(findings) != 0 {
		t.Fatalf("matching artifact was selected by guessed registry directory: %+v", findings)
	}
	public.Checksum = strings.Repeat("b", 64)
	writeSupplyFile(t, repo.Root, "other/Cargo.lock", cargoLicenseTestLock(public))
	findings := Static(t.Context(), repo, []string{"other/Cargo.lock", "Cargo.lock"})
	if len(findings) != 1 || findings[0].Path != "other/Cargo.lock" || findings[0].Check != "supplyChain.licenseCoverage" {
		t.Fatalf("one lock's archive identity satisfied a different checksum: %+v", findings)
	}
}

func TestCargoLicensesReportMissingAndCorruptArchivesWithoutNetwork(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{true, false} {
		repo := supplyRepository(t)
		repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
		pkg := cachedCargoFixture(t, repo, "registry", "crate", "1.0.0", "MIT")
		archive := filepath.Join(repo.Root, cargoArchiveCache, "registry", "crate-1.0.0.crate")
		if missing {
			if err := os.Remove(archive); err != nil {
				t.Fatal(err)
			}
		} else {
			writeSupplyFile(t, repo.Root, cargoArchiveCache+"/registry/crate-1.0.0.crate", "corrupt")
		}
		writeSupplyFile(t, repo.Root, "Cargo.lock", cargoLicenseTestLock(pkg))
		findings := Static(t.Context(), repo, []string{"Cargo.lock"})
		if len(findings) != 1 || findings[0].Check != "supplyChain.licenseCoverage" || !strings.Contains(findings[0].Message, "fetch --locked") {
			t.Fatalf("missing or corrupt archive admitted: %+v", findings)
		}
	}
}

func TestCargoLicensesDoNotInferSPDXFromLicenseFilenames(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
	manifest := "[package]\nname='crate'\nversion='1.0.0'\nlicense-file='LICENSE-MIT'\n"
	data := cargoArchiveFixture(t, []cargoTestEntry{{Name: "crate-1.0.0/Cargo.toml", Content: manifest}})
	pkg := writeCargoTestArchive(t, repo, "registry", "crate", "1.0.0", data)
	writeSupplyFile(t, repo.Root, "Cargo.lock", cargoLicenseTestLock(pkg))
	findings := Static(t.Context(), repo, []string{"Cargo.lock"})
	if len(findings) != 1 || findings[0].Check != "supplyChain.dependencyLicense" {
		t.Fatalf("license filename became a license expression: %+v", findings)
	}
}

func TestCargoLicensesRejectLinkedCacheInputs(t *testing.T) {
	t.Parallel()
	for _, parent := range []bool{true, false} {
		repo := supplyRepository(t)
		repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
		pkg := cachedCargoFixture(t, repo, "registry", "crate", "1.0.0", "MIT")
		path := filepath.Join(repo.Root, cargoArchiveCache, "registry", "crate-1.0.0.crate")
		if parent {
			path = filepath.Dir(path)
		}
		original := path + "-original"
		if parent {
			original = filepath.Join(repo.Root, "relocated-registry")
		}
		if err := os.Rename(path, original); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(original, path); err != nil {
			t.Fatal(err)
		}
		writeSupplyFile(t, repo.Root, "Cargo.lock", cargoLicenseTestLock(pkg))
		findings := Static(t.Context(), repo, []string{"Cargo.lock"})
		if len(findings) != 1 || findings[0].Check != "supplyChain.licenseCoverage" {
			t.Fatalf("linked crate archive established license evidence: %+v", findings)
		}
	}
}

func TestCargoLicensesHonorCancellationAndDoNotReadUnsafeNames(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
	pkg := cachedCargoFixture(t, repo, "registry", "crate", "1.0.0", "MIT")
	writeSupplyFile(t, repo.Root, "Cargo.lock", cargoLicenseTestLock(pkg))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	findings := cargoLicenseFindings(ctx, repo, []string{"Cargo.lock"})
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "canceled") {
		t.Fatalf("canceled license verification passed: %+v", findings)
	}
	pkg.Name = "../private"
	writeSupplyFile(t, repo.Root, "Cargo.lock", cargoLicenseTestLock(pkg))
	findings = cargoLicenseFindings(t.Context(), repo, []string{"Cargo.lock"})
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "safe registry identity") {
		t.Fatalf("unsafe archive name was used as a path: %+v", findings)
	}
}

type cargoTestEntry struct {
	Name    string
	Content string
	Kind    byte
}

func cachedCargoFixture(t *testing.T, repo repository.Repository, registry, name, version, license string) repository.CargoLockedPackage {
	t.Helper()
	manifest := fmt.Sprintf("[package]\nname=%q\nversion=%q\nlicense=%q\n", name, version, license)
	data := cargoArchiveFixture(t, []cargoTestEntry{{Name: name + "-" + version + "/Cargo.toml", Content: manifest}})
	return writeCargoTestArchive(t, repo, registry, name, version, data)
}

func writeCargoTestArchive(t *testing.T, repo repository.Repository, registry, name, version string, data []byte) repository.CargoLockedPackage {
	t.Helper()
	writeSupplyFile(t, repo.Root, cargoArchiveCache+"/"+registry+"/"+name+"-"+version+".crate", string(data))
	checksum := sha256.Sum256(data)
	return repository.CargoLockedPackage{Name: name, Version: version, Source: repository.CargoCratesIOSource, Checksum: hex.EncodeToString(checksum[:])}
}

func cargoArchiveFixture(t *testing.T, entries []cargoTestEntry) []byte {
	t.Helper()
	buffer := bytes.Buffer{}
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.Name, Size: int64(len(entry.Content)), Typeflag: entry.Kind, Mode: 0o600}
		if entry.Kind == tar.TypeSymlink || entry.Kind == tar.TypeLink {
			header.Linkname, header.Size = "foreign.toml", 0
		}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size != 0 {
			if _, err := archive.Write([]byte(entry.Content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func cargoLicenseTestLock(packages ...repository.CargoLockedPackage) string {
	lock := "version=4\n"
	for _, pkg := range packages {
		lock += fmt.Sprintf("[[package]]\nname=%q\nversion=%q\nsource=%q\nchecksum=%q\n", pkg.Name, pkg.Version, pkg.Source, pkg.Checksum)
	}
	return lock
}
