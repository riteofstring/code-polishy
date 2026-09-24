package supplychain

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCargoLockPolicySeparatesRegistryAndGitCoordinates(t *testing.T) {
	t.Parallel()
	lock := repository.CargoLock{Path: "Cargo.lock", Version: 4, Packages: []repository.CargoLockedPackage{
		{Name: "app", Version: "1.0.0"},
		{Name: "same-name", Version: "1.0.0", Source: repository.CargoCratesIOSource},
		{Name: "same-name", Version: "1.0.0", Source: "git+https://github.com/example/repo?branch=main#" + strings.Repeat("a", 40)},
	}}
	packages, findings := resolvedCargoPackages(lock)
	if len(findings) != 0 || len(packages) != 2 {
		t.Fatalf("resolved source inventory: %+v, %+v", packages, findings)
	}
	if packages[0].Ecosystem != "crates.io" || packages[0].Source.Kind != "registry" || packages[1].Ecosystem != "git" || packages[1].Source.Git.Commit != strings.Repeat("a", 40) {
		t.Fatalf("Git source received a registry identity: %+v", packages)
	}
}

func TestCargoLockPolicyRejectsUnknownSourcesWithoutDroppingValidPackages(t *testing.T) {
	t.Parallel()
	lock := repository.CargoLock{Path: "Cargo.lock", Packages: []repository.CargoLockedPackage{
		{Name: "public", Version: "1.0.0", Source: repository.CargoCratesIOSource},
		{Name: "private", Version: "1.0.0", Source: "registry+https://private.example/index"},
		{Name: "mutable-git", Version: "1.0.0", Source: "git+https://example.test/repo?branch=main#short"},
	}}
	packages, findings := resolvedCargoPackages(lock)
	if len(packages) != 1 || packages[0].Name != "public" || len(findings) != 2 {
		t.Fatalf("partial source inventory: %+v, %+v", packages, findings)
	}
}

func TestCargoStaticChecksLockChecksumsAndSourceCoverage(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	for name, source := range map[string]string{
		"missing checksum": repository.CargoCratesIOSource,
		"unknown source":   "unrecognized+https://example.test/source",
	} {
		t.Run(name, func(t *testing.T) {
			writeSupplyFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='crate'\nversion='1.0.0'\nsource='"+source+"'\n")
			findings := Static(t.Context(), repo, []string{"Cargo.lock"})
			if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
				return finding.Path == "Cargo.lock" && (finding.Check == "supplyChain.cargoLock" || finding.Check == "supplyChain.cargoSource")
			}) {
				t.Fatalf("invalid locked source passed static checks: %+v", findings)
			}
		})
	}
}
