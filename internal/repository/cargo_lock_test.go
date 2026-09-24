package repository

import (
	"strings"
	"testing"
)

func TestCargoLockPreservesPackageSourcesVersionsAndOpaqueEdges(t *testing.T) {
	t.Parallel()
	data := "version = 4\n[[package]]\nname='app'\nversion='1.0.0'\ndependencies=['renamed 2.1.0 (" + CargoCratesIOSource + ")']\n[[package]]\nname='renamed'\nversion='2.1.0'\nsource='" + CargoCratesIOSource + "'\nchecksum='" + strings.Repeat("a", 64) + "'\n"
	lock, err := ParseCargoLock("Cargo.lock", []byte(data))
	if err != nil || lock.Version != 4 || len(lock.Packages) != 2 {
		t.Fatalf("lockfile inventory: %+v, %v", lock, err)
	}
	if lock.Packages[1].Source != CargoCratesIOSource || lock.Packages[1].Version != "2.1.0" || lock.Packages[0].Dependencies[0] != "renamed 2.1.0 ("+CargoCratesIOSource+")" {
		t.Fatalf("source or dependency identity was rewritten: %+v", lock.Packages)
	}
	if _, err := ParseCargoLock("Cargo.lock", []byte(strings.Replace(data, "version = 4", "version = 3", 1))); err != nil {
		t.Fatalf("supported version-3 Cargo lockfile rejected: %v", err)
	}
}

func TestCargoLockRejectsIncompleteAndAmbiguousInventories(t *testing.T) {
	t.Parallel()
	root := "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n"
	for name, data := range map[string]string{
		"empty": "version=4\n", "unsupported version": strings.Replace(root, "version=4", "version=5", 1),
		"missing name": strings.Replace(root, "name='app'", "", 1), "short version": strings.Replace(root, "1.0.0", "1.0", 1),
		"duplicate":        root + "[[package]]\nname='app'\nversion='1.0.0'\n",
		"missing checksum": root + "source='" + CargoCratesIOSource + "'\n",
		"short checksum":   root + "source='" + CargoCratesIOSource + "'\nchecksum='abc123'\n",
		"local checksum":   root + "checksum='" + strings.Repeat("a", 64) + "'\n",
		"empty edge":       root + "dependencies=['']\n", "malformed edge": root + "dependencies=[1]\n",
		"oversized": strings.Repeat(" ", 8<<20+1),
	} {
		t.Run(name, func(t *testing.T) {
			if lock, err := ParseCargoLock("Cargo.lock", []byte(data)); err == nil {
				t.Fatalf("invalid lockfile passed: %+v", lock)
			}
		})
	}
}

func TestCargoLockAllowsDifferentVersionsAndSourcesOfOnePackage(t *testing.T) {
	t.Parallel()
	data := "version=4\n"
	for _, version := range []string{"1.0.0", "2.0.0", "2.0.0+vendor"} {
		data += "[[package]]\nname='same'\nversion='" + version + "'\n"
	}
	data += "[[package]]\nname='same'\nversion='2.0.0'\nsource='git+https://example.test/repo?branch=main#" + strings.Repeat("a", 40) + "'\n"
	lock, err := ParseCargoLock("Cargo.lock", []byte(data))
	if err != nil || len(lock.Packages) != 4 {
		t.Fatalf("distinct immutable package identities were collapsed: %+v, %v", lock, err)
	}
}
