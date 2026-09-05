package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestOperationalHandoffsSelectExactSituationsSourcesAndModulesOnce(t *testing.T) {
	t.Parallel()
	repo := handoffRepository(t)
	repo.Config.Documentation.Handoffs[0].Modules = []string{"application"}
	repo.Config.Documentation.Handoffs[0].SourcePaths = []string{"src/release.go"}
	contexts, findings, err := repo.OperationalHandoffs(HandoffSelection{Files: []string{"src/release.go"}, Situations: []string{"release"}})
	if err != nil || len(findings) != 0 || len(contexts) != 1 {
		t.Fatalf("contexts=%+v findings=%+v error=%v", contexts, findings, err)
	}
	want := []HandoffReason{{Kind: "module", Value: "application"}, {Kind: "situation", Value: "release"}, {Kind: "source", Value: "src/release.go"}}
	if !slices.Equal(contexts[0].Reasons, want) || contexts[0].Document.Content != "# Release\nRun the repository release checks.\n" {
		t.Fatalf("selected handoff = %+v", contexts[0])
	}
	digest := sha256.Sum256([]byte(contexts[0].Document.Content))
	if contexts[0].Document.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("document identity does not bind content: %+v", contexts[0].Document)
	}
	for _, selection := range []HandoffSelection{{Files: []string{"src/release.go"}}, {Modules: []string{"application"}}, {Situations: []string{"release"}}} {
		selected, problems, err := repo.OperationalHandoffs(selection)
		if err != nil || len(problems) != 0 || len(selected) != 1 || selected[0].Name != "release" {
			t.Fatalf("selection=%+v contexts=%+v problems=%+v error=%v", selection, selected, problems, err)
		}
	}
}

func TestOperationalHandoffsLeaveUnselectedInvalidDocumentsUnloaded(t *testing.T) {
	t.Parallel()
	repo := handoffRepository(t)
	repo.Config.Documentation.Handoffs = append(repo.Config.Documentation.Handoffs, policy.OperationalHandoff{
		Name: "authentication", Description: "Authentication setup.", Path: "docs/operations/missing.md", Situations: []string{"authentication"},
	})
	contexts, findings, err := repo.OperationalHandoffs(HandoffSelection{Situations: []string{"release"}})
	if err != nil || len(findings) != 0 || len(contexts) != 1 || contexts[0].Name != "release" {
		t.Fatalf("unselected document blocked context: contexts=%+v findings=%+v error=%v", contexts, findings, err)
	}
	contexts, findings, err = repo.OperationalHandoffs(HandoffSelection{Situations: []string{"authentication", "release"}})
	if err != nil || len(contexts) != 0 || len(findings) != 1 || findings[0].Check != OperationalHandoffCheck || findings[0].Subject != "authentication:docs/operations/missing.md" {
		t.Fatalf("selected invalid handoff did not block complete context: contexts=%+v findings=%+v error=%v", contexts, findings, err)
	}
	if findings := repo.OperationalHandoffFindings(); len(findings) != 1 || findings[0].Fields["handoff"] != "authentication" {
		t.Fatalf("full inventory did not validate every declared handoff: %+v", findings)
	}
}

func TestOperationalHandoffsSelectGovernedWorkflowInputs(t *testing.T) {
	t.Parallel()
	repo := handoffRepository(t)
	repo.Config.Modules[0].Paths = append(repo.Config.Modules[0].Paths, ".github/workflows/**")
	path := ".github/workflows/release.yml"
	writeFile(t, repo.Root, path, "name: Release\n")
	repo.Config.Documentation.Handoffs[0].SourcePaths = []string{path}
	contexts, findings, err := repo.OperationalHandoffs(HandoffSelection{Files: []string{path}})
	if err != nil || len(findings) != 0 || len(contexts) != 1 || contexts[0].Name != "release" {
		t.Fatalf("workflow input context=%+v findings=%+v error=%v", contexts, findings, err)
	}
	repo.Config.Modules = append(repo.Config.Modules, policy.Module{Name: "other", Paths: []string{path}})
	contexts, findings, err = repo.OperationalHandoffs(HandoffSelection{Files: []string{path}})
	if err != nil || len(contexts) != 0 || len(findings) != 1 || !strings.Contains(findings[0].Message, "exactly one module") {
		t.Fatalf("ambiguous source context=%+v findings=%+v error=%v", contexts, findings, err)
	}
}

func TestOperationalHandoffsBindDocumentChangesAndRejectStaleSourceReferences(t *testing.T) {
	t.Parallel()
	repo := handoffRepository(t)
	selection := HandoffSelection{Situations: []string{"release"}}
	before, findings, err := repo.OperationalHandoffs(selection)
	if err != nil || len(findings) != 0 || len(before) != 1 {
		t.Fatalf("initial context: findings=%+v error=%v", findings, err)
	}
	writeFile(t, repo.Root, "docs/operations/release.md", "# Release\nVerify the candidate before tagging.\n")
	after, findings, err := repo.OperationalHandoffs(selection)
	if err != nil || len(findings) != 0 || len(after) != 1 || before[0].Document.SHA256 == after[0].Document.SHA256 {
		t.Fatalf("changed context was not rebound: findings=%+v error=%v", findings, err)
	}
	repo.Config.Documentation.Handoffs[0].SourcePaths = []string{"src/missing.go"}
	contexts, findings, err := repo.OperationalHandoffs(selection)
	if err != nil || len(contexts) != 0 || len(findings) != 1 || findings[0].Subject != "release:src/missing.go" || findings[0].Remediation.NextCommand == nil {
		t.Fatalf("stale source mapping was accepted: contexts=%+v findings=%+v error=%v", contexts, findings, err)
	}
}

func TestContextDocumentsRejectUnsafeOrInvalidContent(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"empty": "", "whitespace": " \n\t", "invalid-utf8": string([]byte{0xff}), "nul": "# Ops\x00\n",
		"oversize": strings.Repeat("x", MaximumContextDocumentBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			repo := handoffRepository(t)
			writeFile(t, repo.Root, "docs/operations/release.md", content)
			contexts, findings, err := repo.OperationalHandoffs(HandoffSelection{Situations: []string{"release"}})
			if err != nil || len(contexts) != 0 || len(findings) != 1 {
				t.Fatalf("invalid context accepted: contexts=%d findings=%+v error=%v", len(contexts), findings, err)
			}
		})
	}
	repo := handoffRepository(t)
	for _, path := range []string{"../outside.md", "./docs/operations/release.md", "docs/operations", "src/release.go"} {
		if _, err := repo.ReadContextDocument(path); err == nil {
			t.Fatalf("accepted unsafe document path %q", path)
		}
	}
}

func TestContextDocumentsRejectSymlinkTargetsAndParents(t *testing.T) {
	t.Parallel()
	repo := handoffRepository(t)
	for name, target := range map[string]string{
		"alias.md": filepath.Join(repo.Root, "docs", "operations", "release.md"),
		"linked":   filepath.Join(repo.Root, "docs", "operations"),
	} {
		if err := os.Symlink(target, filepath.Join(repo.Root, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"alias.md", "linked/release.md"} {
		if _, err := repo.ReadContextDocument(path); err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("symlink %q error=%v", path, err)
		}
	}
}

func TestOperationalHandoffsBoundTheCombinedDocumentSet(t *testing.T) {
	t.Parallel()
	repo := handoffRepository(t)
	repo.Config.Documentation.Handoffs = nil
	for index := range 9 {
		path := fmt.Sprintf("docs/operations/%d.md", index)
		writeFile(t, repo.Root, path, strings.Repeat("x", MaximumContextDocumentBytes))
		repo.Config.Documentation.Handoffs = append(repo.Config.Documentation.Handoffs, policy.OperationalHandoff{
			Name: fmt.Sprintf("release-%d", index), Description: "Release procedure.", Path: path, Situations: []string{"release"},
		})
	}
	contexts, findings, err := repo.OperationalHandoffs(HandoffSelection{Situations: []string{"release"}})
	if err != nil || len(contexts) != 0 || len(findings) != 1 || !strings.Contains(findings[0].Message, "selected handoff documents exceed") {
		t.Fatalf("oversized context set accepted: contexts=%d findings=%+v error=%v", len(contexts), findings, err)
	}
}

func handoffRepository(t *testing.T) Repository {
	t.Helper()
	repo := Repository{Root: t.TempDir(), Config: policy.Config{
		Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}},
		Documentation: policy.Documentation{Handoffs: []policy.OperationalHandoff{{
			Name: "release", Description: "Release procedure.", Path: "docs/operations/release.md", Situations: []string{"release"},
		}}},
	}}
	writeFile(t, repo.Root, "docs/operations/release.md", "# Release\nRun the repository release checks.\n")
	writeFile(t, repo.Root, "src/release.go", "package release\n")
	return repo
}
