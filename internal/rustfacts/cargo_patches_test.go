package rustfacts

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestCargoReplacementUsesConsumerLockWithoutCompilingOrStandaloneLocks(t *testing.T) {
	t.Parallel()
	for name, selected := range map[string][]string{
		"whole project":   {"Cargo.toml", "vendor/patch-source/Cargo.toml", "vendor/leaf/Cargo.toml"},
		"dependency only": {"vendor/patch-source/Cargo.toml"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := cargoPatchRepository(t)
			plan := PlanResolution(repo, selected, true)
			result := Resolve(t.Context(), repo, plan, runner.OSRunner{})
			if len(result.Findings) != 0 || len(result.Resolutions) != 1 || result.Resolutions[0].Lock.Path != "Cargo.lock" {
				t.Fatalf("consumer resolution = %+v", result)
			}
			if len(result.Resolutions[0].Packages) != 3 {
				t.Fatalf("optional replacement graph was not examined: %+v", result.Resolutions[0].Packages)
			}
			for _, path := range []string{"vendor/patch-source/Cargo.lock", "vendor/leaf/Cargo.lock", "executed", ".tools/rust-target"} {
				if _, err := os.Stat(filepath.Join(repo.Root, filepath.FromSlash(path))); !os.IsNotExist(err) {
					t.Errorf("resolution created dependency build input or executed code at %s: %v", path, err)
				}
			}
		})
	}
}

func TestCargoReplacementRejectsStaleConsumerAndUnusedSource(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"1.0.1", "2.0.0"} {
		t.Run(version, func(t *testing.T) {
			repo := cargoPatchRepository(t)
			write(t, repo.Root, "vendor/leaf/Cargo.toml", "[package]\nname='patch-leaf'\nversion='"+version+"'\nedition='2021'\n")
			result := Resolve(t.Context(), repo, PlanResolution(repo, []string{"Cargo.toml", "vendor/leaf/Cargo.toml"}, true), runner.OSRunner{})
			if len(result.Findings) == 0 || len(result.Resolutions) != 0 {
				t.Fatalf("changed replacement passed old lock: %+v", result)
			}
		})
	}
}

func TestCargoReplacementDoesNotClaimIndependentPackageOrWorkspaceMember(t *testing.T) {
	t.Parallel()
	repo := cargoPatchRepository(t)
	write(t, repo.Root, "other/Cargo.toml", "[package]\nname='other'\nversion='1.0.0'\nedition='2021'\n[workspace]\n")
	write(t, repo.Root, "other/src/lib.rs", "pub fn value() {}\n")
	result := Resolve(t.Context(), repo, PlanResolution(repo, []string{"Cargo.toml", "other/Cargo.toml"}, true), runner.OSRunner{})
	if len(result.Resolutions) != 1 || !slices.ContainsFunc(result.Findings, func(f policy.Finding) bool { return f.Path == "other/Cargo.lock" }) {
		t.Fatalf("independent missing lock was hidden: %+v", result)
	}
	owner := repo.GovernedCargoPatchManifests().Manifests[0]
	resolved := result.Resolutions[0]
	for _, pkg := range resolved.Packages {
		if pkg.Name == "patch-leaf" {
			resolved.Workspace.Members = append(resolved.Workspace.Members, pkg.ID)
		}
	}
	if findings := cargoResolvedPatchFindings(owner, resolved); len(findings) != 1 || !strings.Contains(findings[0].Subject, "patch-leaf") {
		t.Fatalf("workspace member received replacement dependency treatment: %+v", findings)
	}
}

func TestCargoReplacementRequiresActualConsumerUse(t *testing.T) {
	t.Parallel()
	repo := cargoPatchRepository(t)
	manifest, err := repo.Read("Cargo.toml")
	if err != nil {
		t.Fatal(err)
	}
	write(t, repo.Root, "Cargo.toml", string(manifest)+"unused={path='unused'}\n")
	write(t, repo.Root, "unused/Cargo.toml", "[package]\nname='unused'\nversion='1.0.0'\nedition='2021'\n[workspace]\n")
	write(t, repo.Root, "unused/src/lib.rs", "pub fn unused() {}\n")
	lock, err := repo.Read("Cargo.lock")
	if err != nil {
		t.Fatal(err)
	}
	write(t, repo.Root, "Cargo.lock", string(lock)+"[[patch.unused]]\nname='unused'\nversion='1.0.0'\n")
	result := Resolve(t.Context(), repo, PlanResolution(repo, []string{"Cargo.toml", "unused/Cargo.toml"}, true), runner.OSRunner{})
	if !slices.ContainsFunc(result.Findings, func(f policy.Finding) bool {
		return f.Check == "supplyChain.cargoSource" && f.Subject == "patch.crates-io:unused"
	}) {
		t.Fatalf("unused substitution claimed source evidence: %+v", result.Findings)
	}
}

func cargoPatchRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := rustRepository(t)
	repo.Config.SupplyChain.DependencyOverridePolicies = []policy.DependencyOverridePolicy{{Ecosystem: "cargo", Path: "Cargo.toml", Field: "patch.crates-io"}}
	write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2021'\n[workspace]\nexclude=['vendor/patch-source','vendor/leaf']\n[features]\ndefault=[]\ndesktop=['dep:patch-source']\n[dependencies]\npatch-source={version='=1.0.0',optional=true}\n[patch.crates-io]\npatch-source={path='vendor/patch-source'}\npatch-leaf={path='vendor/leaf'}\n")
	write(t, repo.Root, "src/lib.rs", "compile_error!(\"must not compile\");\n")
	write(t, repo.Root, "vendor/patch-source/Cargo.toml", "[package]\nname='patch-source'\nversion='1.0.0'\nedition='2021'\n[dependencies]\npatch-leaf='1'\n[dev-dependencies]\nnot-consumed='9'\n")
	write(t, repo.Root, "vendor/patch-source/src/lib.rs", "compile_error!(\"must not compile\");\n")
	write(t, repo.Root, "vendor/patch-source/build.rs", "fn main() { std::fs::write(\"executed\", \"bad\").unwrap(); panic!(\"must not execute\"); }\n")
	write(t, repo.Root, "vendor/leaf/Cargo.toml", "[package]\nname='patch-leaf'\nversion='1.0.0'\nedition='2021'\n")
	write(t, repo.Root, "vendor/leaf/src/lib.rs", "pub fn value() {}\n")
	write(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\ndependencies=['patch-source']\n[[package]]\nname='patch-source'\nversion='1.0.0'\ndependencies=['patch-leaf']\n[[package]]\nname='patch-leaf'\nversion='1.0.0'\n")
	return repo
}
