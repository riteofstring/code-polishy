package supplychain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDependencyInventoryRejectsOversizedOrCanceledInputs(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", strings.Repeat(" ", maximumDependencyInputBytes+1))
	_, err := dependencyInventory(t.Context(), repo, inventorySource{files: []string{"package.json"}, read: repo.Read})
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized input = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	source := inventorySource{files: []string{"package.json"}, read: func(string) ([]byte, error) {
		t.Fatal("canceled inventory read a manifest")
		return nil, nil
	}}
	if _, err := dependencyInventory(ctx, repo, source); err != context.Canceled {
		t.Fatalf("canceled inventory = %v", err)
	}
}

func TestDependencyAgeCandidatesKeepPEP440VersionsAndExcludeRanges(t *testing.T) {
	t.Parallel()
	change := DependencyChange{Ecosystem: "pypi", Directness: "direct", Usage: "runtime", Change: "updated"}
	for _, version := range []string{"1.0", "1!2.0", "2.0rc1", "2.0.post1"} {
		change.To = version
		if !newDependencyAgeCandidate(change) {
			t.Fatalf("exact PEP 440 version lost age advice: %s", version)
		}
	}
	for _, version := range []string{"<3,>=2.0", "!=1.0", "==2.*", "*", ""} {
		change.To = version
		if newDependencyAgeCandidate(change) {
			t.Fatalf("historical range selected registry lookup: %s", version)
		}
	}
}

func TestDependencyReviewComparesHistoricalTagRepairAndRecordedResolution(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	before, after := strings.Repeat("a", 40), strings.Repeat("b", 40)
	writeReviewPythonGit(t, repo, "v1.2.3", "tag", before)
	writeSupplyFile(t, repo.Root, "backend.py", "raise RuntimeError('repository code must not run')\n")
	base := commitReviewInventory(t, repo)
	writeReviewPythonGit(t, repo, after, "rev", after)
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 2 {
		t.Fatalf("tag repair changes = %+v, error = %v", changes, err)
	}
	for _, change := range changes {
		if change.Change != "updated" || change.Ecosystem != "git" || !strings.Contains(change.From, "@v1.2.3") || !strings.Contains(change.To, "@"+after) {
			t.Fatalf("repair lost its source/ref change: %+v", change)
		}
		if change.Scope == "uv.lock" && (!strings.Contains(change.From, "commit="+before) || !strings.Contains(change.From, "refKind=tag")) {
			t.Fatalf("historical resolution was discarded: %+v", change)
		}
	}
	if findings := checkPythonProject(repo, "pyproject.toml"); len(findings) != 0 {
		t.Fatalf("exact repair was not admitted: %+v", findings)
	}
}

func TestDependencyReviewAllowsHistoricalRemovalAndKeepsCandidatePinPolicy(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	commit := strings.Repeat("a", 40)
	writeReviewPythonGit(t, repo, "release/stable", "branch", commit)
	base := commitReviewInventory(t, repo)
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 0 {
		t.Fatalf("unchanged historical comparison = %+v, error = %v", changes, err)
	}
	if findings := checkPythonProject(repo, "pyproject.toml"); !supplyChecks(findings)["supplyChain.pythonManifest"] {
		t.Fatalf("unchanged candidate escaped strict pin checks: %+v", findings)
	}
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = []\n")
	writeSupplyFile(t, repo.Root, "uv.lock", "version = 1\npackage = []\n")
	changes, err = DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 2 {
		t.Fatalf("removal changes = %+v, error = %v", changes, err)
	}
	for _, change := range changes {
		if change.Change != "removed" || change.To != "-" || !strings.Contains(change.From, "@release/stable") {
			t.Fatalf("moving-ref removal = %+v", change)
		}
	}
	if findings := checkPythonProject(repo, "pyproject.toml"); len(findings) != 0 {
		t.Fatalf("removal candidate failed: %+v", findings)
	}
	writeReviewPythonGit(t, repo, commit, "rev", strings.Repeat("b", 40))
	if findings := checkPythonProject(repo, "pyproject.toml"); !supplyChecks(findings)["supplyChain.lockConsistency"] {
		t.Fatalf("candidate lock mismatch escaped admission: %+v", findings)
	}
}

func TestDependencyReviewPreservesHistoricalRegistryRanges(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = [\"requests>=2.0,<3\"]\n")
	writeSupplyFile(t, repo.Root, "package.json", "{\"packageManager\":\"npm@11.0.0\",\"dependencies\":{\"helper\":\"^1.0.0\"}}\n")
	writeSupplyFile(t, repo.Root, "go.mod", "module example.test/app\nrequire example.test/tool v1.0.0\n")
	base := commitReviewInventory(t, repo)
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = [\"requests==2.32.0\"]\n")
	writeSupplyFile(t, repo.Root, "package.json", "{\"packageManager\":\"npm@11.0.0\",\"dependencies\":{\"helper\":\"1.2.0\"}}\n")
	writeSupplyFile(t, repo.Root, "go.mod", "module example.test/app\nrequire example.test/tool v1.1.0\n")
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 3 {
		t.Fatalf("range repair changes = %+v, error = %v", changes, err)
	}
	observed := map[string]string{}
	for _, change := range changes {
		if change.Change != "updated" {
			t.Fatalf("range replacement appeared newly added: %+v", change)
		}
		observed[change.Ecosystem] = change.From
	}
	if observed["pypi"] != "<3,>=2.0" || observed["npm"] != "^1.0.0" || observed["go"] != "v1.0.0" {
		t.Fatalf("historical declarations changed: %+v", observed)
	}
}

func TestDependencyReviewRejectsUnsafeHistoryWithoutDisclosingCredentials(t *testing.T) {
	t.Parallel()
	for name, manifest := range map[string]string{
		"malformed":     "[project]\ndependencies = [",
		"credentials":   "[project]\ndependencies = [\"tool @ git+https://SENSITIVE_TOKEN@example.test/tool.git@v1\"]\n",
		"escape":        "[project]\ndependencies = [\"tool @ file:../outside\"]\n",
		"contradictory": "[project]\ndependencies = [\"tool @ git+https://example.test/tool.git@v1\", \"tool @ git+https://example.test/tool.git@v2\"]\n",
	} {
		t.Run(name, func(t *testing.T) {
			repo := supplyRepository(t)
			writeSupplyFile(t, repo.Root, "pyproject.toml", manifest)
			base := commitReviewInventory(t, repo)
			if err := os.Remove(filepath.Join(repo.Root, "pyproject.toml")); err != nil {
				t.Fatal(err)
			}
			if _, err := DependencyChanges(t.Context(), repo, base); err == nil || !strings.Contains(err.Error(), "comparison coverage at merge base") || strings.Contains(err.Error(), "SENSITIVE_TOKEN") {
				t.Fatalf("unsafe historical comparison = %v", err)
			}
		})
	}
}

func TestDependencyReviewRejectsAmbiguousHistoricalLockSources(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	for name, fields := range map[string]string{
		"multiple-kinds": "registry = \"https://pypi.org/simple\", git = \"https://example.test/tool.git?tag=v1#" + commit + "\"",
		"multiple-refs":  "git = \"https://example.test/tool.git?tag=v1&branch=main#" + commit + "\"",
		"credentials":    "git = \"https://SENSITIVE_TOKEN@example.test/tool.git?tag=v1#" + commit + "\"",
		"unresolved":     "git = \"https://example.test/tool.git?tag=v1#abcdef0\"",
		"unknown-field":  "git = \"https://example.test/tool.git?tag=v1#" + commit + "\", token = \"SENSITIVE_TOKEN\"",
	} {
		t.Run(name, func(t *testing.T) {
			repo := supplyRepository(t)
			writeSupplyFile(t, repo.Root, "uv.lock", "[[package]]\nname = \"tool\"\nversion = \"1.0.0\"\nsource = { "+fields+" }\n")
			base := commitReviewInventory(t, repo)
			writeSupplyFile(t, repo.Root, "uv.lock", "version = 1\npackage = []\n")
			if _, err := DependencyChanges(t.Context(), repo, base); err == nil || !strings.Contains(err.Error(), "comparison coverage at merge base") || strings.Contains(err.Error(), "SENSITIVE_TOKEN") {
				t.Fatalf("ambiguous historical lock = %v", err)
			}
		})
	}
}

func writeReviewPythonGit(t *testing.T, repo repository.Repository, reference, kind, resolved string) {
	t.Helper()
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = [\"tool @ git+https://example.test/team/tool.git@"+reference+"#subdirectory=python/tool\"]\n")
	writeSupplyFile(t, repo.Root, "uv.lock", "[[package]]\nname = \"tool\"\nversion = \"1.0.0\"\nsource = { git = \"https://example.test/team/tool.git?"+kind+"="+reference+"&subdirectory=python/tool#"+resolved+"\" }\n")
}

func commitReviewInventory(t *testing.T, repo repository.Repository) string {
	t.Helper()
	gitSupply(t, repo.Root, "init", "-b", "main")
	gitSupply(t, repo.Root, "config", "user.email", "tests@example.test")
	gitSupply(t, repo.Root, "config", "user.name", "Dependency Review Tests")
	gitSupply(t, repo.Root, "add", ".")
	gitSupply(t, repo.Root, "commit", "-m", "dependency comparison base")
	base, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	return base
}
