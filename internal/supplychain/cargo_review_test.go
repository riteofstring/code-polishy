package supplychain

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCargoDependencyReviewComparesDeclarationsAndLockedVersions(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\nrenamed={package='crate-name',version='1.0.0'}\n[build-dependencies]\ncc='=1.0.0'\n")
	writeSupplyFile(t, repo.Root, "Cargo.lock", reviewCargoLock("crate-name", "1.0.0", "registry+https://github.com/rust-lang/crates.io-index"))
	writeSupplyFile(t, repo.Root, "build.rs", "compile_error!(\"dependency comparison must not execute code\");\n")
	base := commitReviewInventory(t, repo)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\nrenamed={package='crate-name',version='=1.1.0'}\n[build-dependencies]\ncc='=1.1.0'\n")
	writeSupplyFile(t, repo.Root, "Cargo.lock", reviewCargoLock("crate-name", "1.1.0", "registry+https://github.com/rust-lang/crates.io-index"))
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 3 {
		t.Fatalf("Cargo comparison = %+v, %v", changes, err)
	}
	for _, change := range changes {
		if change.Change != "updated" || change.Ecosystem != "crates.io" || change.To != "1.1.0" {
			t.Fatalf("incorrect Cargo version change: %+v", change)
		}
		if change.Directness == "direct" && change.Package == "crate-name" && change.From != "^1.0.0" {
			t.Fatalf("Cargo bare version was mistaken for an exact pin: %+v", change)
		}
	}
	if _, err := os.Stat(filepath.Join(repo.Root, ".tools")); !os.IsNotExist(err) {
		t.Fatalf("dependency comparison installed tools or executed Cargo: %v", err)
	}
}

func TestCargoDependencyReviewDoesNotGuessInheritedVersions(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['app']\n[workspace.dependencies]\nrenamed={package='crate-name',version='=1.0.0'}\n")
	writeSupplyFile(t, repo.Root, "app/Cargo.toml", "[package]\nname='app'\n[dependencies]\nrenamed={workspace=true,optional=true}\n")
	base := commitReviewInventory(t, repo)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['app']\n[workspace.dependencies]\nrenamed={package='crate-name',version='=2.0.0'}\n")
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 1 || changes[0].Directness != "workspace" || changes[0].Package != "crate-name" || changes[0].From != "1.0.0" || changes[0].To != "2.0.0" {
		t.Fatalf("workspace definition comparison = %+v, %v", changes, err)
	}
	writeSupplyFile(t, repo.Root, "app/Cargo.toml", "[package]\nname='app'\n[dependencies]\nrenamed={workspace=true}\n")
	changes, err = DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 3 {
		t.Fatalf("inherited usage comparison = %+v, %v", changes, err)
	}
	for _, change := range changes {
		if change.Scope == "app/Cargo.toml" && (change.Ecosystem != "cargo-workspace" || change.Package != "renamed" || change.From != "workspace" && change.To != "workspace") {
			t.Fatalf("inherited declaration invented package/version facts: %+v", change)
		}
	}
}

func TestCargoDependencyReviewAllowsRemovingHistoricalMovingGitAndPrivateRegistrySources(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	commit := strings.Repeat("a", 40)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\nprivate-git={git='https://private.example.test/team/tool',branch='stable'}\nprivate-registry={version='1.0.0',registry='private'}\n")
	lock := reviewCargoLock("private-git", "1.0.0", "git+https://private.example.test/team/tool?branch=stable#"+commit)
	lock += strings.TrimPrefix(reviewCargoLock("private-registry", "1.0.0", "registry+https://private.example.test/index"), "version=4\n")
	writeSupplyFile(t, repo.Root, "Cargo.lock", lock)
	base := commitReviewInventory(t, repo)
	if findings := checkCargoManifest(repo, "Cargo.toml"); len(findings) == 0 {
		t.Fatal("historical source declarations bypassed current admission policy")
	}
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n")
	writeSupplyFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n")
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 4 {
		t.Fatalf("historical source removal = %+v, %v", changes, err)
	}
	for _, change := range changes {
		if change.Change != "removed" || newDependencyAgeCandidate(change) || change.Ecosystem == "crates.io" {
			t.Fatalf("private source was reclassified as public or admitted: %+v", change)
		}
	}
}

func TestCargoDependencyReviewKeepsLockScopeAndSourceIdentity(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\ncrate-name='=1.0.0'\n")
	writeSupplyFile(t, repo.Root, "Cargo.lock", reviewCargoLock("crate-name", "1.0.0", "registry+https://github.com/rust-lang/crates.io-index"))
	base := commitReviewInventory(t, repo)
	writeSupplyFile(t, repo.Root, "nested/Cargo.lock", reviewCargoLock("crate-name", "1.0.0", "registry+https://github.com/rust-lang/crates.io-index"))
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 1 || changes[0].Scope != "nested/Cargo.lock" || changes[0].Directness != "resolved" {
		t.Fatalf("direct declaration in another scope hid a lock change: %+v, %v", changes, err)
	}
	writeSupplyFile(t, repo.Root, "Cargo.lock", reviewCargoLock("crate-name", "1.0.0", "git+https://example.test/team/tool#"+strings.Repeat("a", 40)))
	changes, err = DependencyChanges(t.Context(), repo, base)
	if err != nil || !slices.ContainsFunc(changes, func(change DependencyChange) bool {
		return change.Ecosystem == "git" && strings.Contains(change.To, "commit=")
	}) {
		t.Fatalf("Git source change was lost: %+v, %v", changes, err)
	}
}

func TestCargoDependencyReviewRetainsSparseAndInsecureHistoricalRegistryIdentity(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"sparse+https://private.example.test/index/", "registry+http://private.example.test/index"} {
		repo := supplyRepository(t)
		writeSupplyFile(t, repo.Root, "Cargo.lock", reviewCargoLock("private", "1.0.0", source))
		base := commitReviewInventory(t, repo)
		writeSupplyFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n")
		changes, err := DependencyChanges(t.Context(), repo, base)
		if err != nil || len(changes) != 1 || changes[0].From != source+"@1.0.0" || changes[0].Ecosystem != "cargo-registry" || changes[0].Change != "removed" {
			t.Fatalf("historical registry comparison = %+v, %v", changes, err)
		}
	}
}

func TestCargoDependencyReviewRejectsUnsafeHistoryWithoutLeakingCredentials(t *testing.T) {
	t.Parallel()
	for _, declaration := range []string{
		"{git='https://SENSITIVE_TOKEN@example.test/tool',rev='" + strings.Repeat("a", 40) + "'}",
		"{path='../outside'}",
		"{git='https://example.test/tool',tag='v1',branch='main'}",
		"{version='=1.0.0',token='SENSITIVE_TOKEN'}",
	} {
		repo := supplyRepository(t)
		writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\ncrate="+declaration+"\n")
		base := commitReviewInventory(t, repo)
		writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n")
		if _, err := DependencyChanges(t.Context(), repo, base); err == nil || strings.Contains(err.Error(), "SENSITIVE_TOKEN") {
			t.Fatalf("unsafe historical inventory = %v", err)
		}
	}
}

func TestCargoDependencyReviewAddsAgeAdviceOnlyForExactPublicRuntimeDeclarations(t *testing.T) {
	repo := supplyRepository(t)
	repo.Config.SupplyChain.PreferredNewDependencyAgeDays = 90
	transport := &cargoAgeTransport{released: map[string]time.Time{"1.0.0": time.Now().UTC().AddDate(0, 0, -60)}}
	installCargoAgeTransport(t, transport)
	changes := []DependencyChange{
		{Ecosystem: "crates.io", Scope: "Cargo.toml", Directness: "direct", Usage: "runtime", Change: "added", Package: "crate-name", To: "1.0.0"},
		{Ecosystem: "crates.io", Scope: "app/Cargo.toml", Directness: "direct", Usage: "optional", Change: "updated", Package: "crate-name", To: "1.0.0"},
		{Ecosystem: "crates.io", Scope: "Cargo.toml", Directness: "direct", Usage: "build", Change: "added", Package: "build-tool", To: "1.0.0"},
		{Ecosystem: "crates.io", Scope: "Cargo.toml", Directness: "workspace", Usage: "definition", Change: "added", Package: "unused", To: "1.0.0"},
		{Ecosystem: "cargo-registry", Scope: "Cargo.toml", Directness: "direct", Usage: "runtime", Change: "added", Package: "private", To: "1.0.0"},
		{Ecosystem: "crates.io", Scope: "Cargo.toml", Directness: "direct", Usage: "runtime", Change: "added", Package: "range", To: "^1.0.0"},
	}
	advisories := NewDependencyAgeAdvisories(t.Context(), repo, changes)
	if len(advisories) != 2 || len(transport.requests) != 1 {
		t.Fatalf("Cargo preferred-age advice = %+v, requests = %+v", advisories, transport.requests)
	}
	for _, advisory := range advisories {
		if advisory.Subject != "crate-name@1.0.0" || advisory.Check != "supplyChain.newDependencyAge" {
			t.Fatalf("incorrect Cargo age advice: %+v", advisory)
		}
	}
}

func reviewCargoLock(name, version, source string) string {
	lock := fmt.Sprintf("version=4\n[[package]]\nname=%q\nversion=%q\nsource=%q\n", name, version, source)
	if strings.HasPrefix(source, "registry+") || strings.HasPrefix(source, "sparse+") {
		lock += fmt.Sprintf("checksum=%q\n", strings.Repeat("a", 64))
	}
	return lock
}
