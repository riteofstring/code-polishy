package repository

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestAnalyzeSizeSeparatesWorkspaceConsumersFromGovernedContent(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config = policy.Config{
		Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}},
		Tests:   policy.Testing{Ownership: []policy.TestOwnership{{Module: "application", Paths: []string{"tests/**"}}}},
	}
	writeFile(t, repo.Root, "src/app.go", strings.Repeat("s", 200))
	writeFile(t, repo.Root, "tests/app_test.go", strings.Repeat("t", 120))
	writeFile(t, repo.Root, "docs/guide.md", strings.Repeat("d", 80))
	writeFile(t, repo.Root, "assets/banner.png", strings.Repeat("a", 60))
	writeFile(t, repo.Root, "node_modules/library/index.js", strings.Repeat("n", 800))
	writeFile(t, repo.Root, "build/bundle.js", strings.Repeat("b", 600))
	writeFile(t, repo.Root, ".code-polishy-reports/old/report.json", strings.Repeat("r", 2000))

	external := filepath.Join(t.TempDir(), "external.bin")
	if err := os.WriteFile(external, []byte(strings.Repeat("x", 4096)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(repo.Root, "linked.bin")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	analysis, err := repo.AnalyzeSize("")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Governed.Files != 5 || analysis.Governed.Bytes != 460+int64(len(external)) {
		t.Fatalf("governed = %+v", analysis.Governed)
	}
	if analysis.Workspace.Bytes < analysis.Governed.Bytes+1400 {
		t.Fatalf("workspace bytes = %d, governed bytes = %d", analysis.Workspace.Bytes, analysis.Governed.Bytes)
	}
	for _, expected := range []string{"dependencies-and-toolchains", "build-cache-and-test-output", "git-metadata"} {
		if groupByName(analysis.Workspace.Categories, expected).Files == 0 {
			t.Errorf("workspace categories = %+v, missing %s", analysis.Workspace.Categories, expected)
		}
	}
	for _, expected := range []string{"source", "tests", "documentation", "assets", "other"} {
		if groupByName(analysis.Governed.Categories, expected).Files != 1 {
			t.Errorf("governed categories = %+v, missing %s", analysis.Governed.Categories, expected)
		}
	}
	if group := groupByName(analysis.Governed.Modules, "application"); group.Files != 2 || group.Bytes != 320 {
		t.Fatalf("application size = %+v", group)
	}
	if slices.ContainsFunc(analysis.Workspace.LargestFiles, func(file SizeFile) bool {
		return strings.HasPrefix(file.Path, ".code-polishy-reports/")
	}) {
		t.Fatalf("managed report artifacts were measured: %+v", analysis.Workspace.LargestFiles)
	}
	if linked := groupByName(analysis.Workspace.TopLevel, "linked.bin"); linked.Files != 1 || linked.Bytes != int64(len(external)) {
		t.Fatalf("symbolic link was followed or omitted: %+v", analysis.Workspace.TopLevel)
	}
}

func TestAnalyzeSizeComparesWorkingTreeWithMergeBase(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	git(t, repo.Root, "config", "core.autocrlf", "false")
	repo.Config = policy.Config{Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}}}
	writeFile(t, repo.Root, "src/app.go", strings.Repeat("a", 100))
	writeFile(t, repo.Root, "docs/old.md", strings.Repeat("d", 50))
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	baseOutput, err := repo.gitLines("rev-parse", "HEAD")
	if err != nil || len(baseOutput) != 1 {
		t.Fatalf("base = %v, error = %v", baseOutput, err)
	}

	writeFile(t, repo.Root, "src/app.go", strings.Repeat("a", 160))
	writeFile(t, repo.Root, "src/new.go", strings.Repeat("n", 90))
	if err := os.Remove(filepath.Join(repo.Root, "docs", "old.md")); err != nil {
		t.Fatal(err)
	}

	analysis, err := repo.AnalyzeSize(baseOutput[0])
	if err != nil {
		t.Fatal(err)
	}
	comparison := analysis.Comparison
	if comparison == nil {
		t.Fatal("comparison is missing")
	}
	assertSizeComparisonTotals(t, *comparison, baseOutput[0])
	assertSizeComparisonChanges(t, *comparison)
	states := map[string]string{}
	for _, change := range comparison.LargestChanges {
		states[change.Path] = change.State
	}
	assertSizeChangeStates(t, states, comparison.LargestChanges)
	if delta := deltaByName(comparison.Modules, "application"); delta.BaseBytes != 100 || delta.CurrentBytes != 250 || delta.DeltaBytes != 150 {
		t.Fatalf("application delta = %+v", delta)
	}
}

func TestAnalyzeSizeComparesGitBlobsAcrossCRLFCheckout(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	git(t, repo.Root, "config", "core.autocrlf", "false")
	writeFile(t, repo.Root, ".gitattributes", "*.txt text eol=crlf\n")
	writeFile(t, repo.Root, "content.txt", "one\ntwo\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	baseOutput, err := repo.gitLines("rev-parse", "HEAD")
	if err != nil || len(baseOutput) != 1 {
		t.Fatalf("base = %v, error = %v", baseOutput, err)
	}
	if err := os.Remove(filepath.Join(repo.Root, "content.txt")); err != nil {
		t.Fatal(err)
	}
	git(t, repo.Root, "checkout", "--", "content.txt")
	contents, err := os.ReadFile(filepath.Join(repo.Root, "content.txt"))
	if err != nil || !strings.Contains(string(contents), "\r\n") {
		t.Fatalf("checkout contents = %q, error = %v", contents, err)
	}

	analysis, err := repo.AnalyzeSize(baseOutput[0])
	if err != nil {
		t.Fatal(err)
	}
	comparison := analysis.Comparison
	if comparison == nil || comparison.DeltaFiles != 0 || comparison.DeltaBytes != 0 || len(comparison.LargestChanges) != 0 {
		t.Fatalf("comparison = %+v", comparison)
	}
	if analysis.Governed.Bytes <= comparison.Current.Bytes {
		t.Fatalf("working tree bytes = %d, Git blob bytes = %d", analysis.Governed.Bytes, comparison.Current.Bytes)
	}
}

func TestAnalyzeSizeComparisonIncludesSparseTrackedFiles(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "visible.go", "package visible\n")
	writeFile(t, repo.Root, "sparse/hidden.go", "package hidden\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	baseOutput, err := repo.gitLines("rev-parse", "HEAD")
	if err != nil || len(baseOutput) != 1 {
		t.Fatalf("base = %v, error = %v", baseOutput, err)
	}
	git(t, repo.Root, "update-index", "--skip-worktree", "sparse/hidden.go")
	if err := os.Remove(filepath.Join(repo.Root, "sparse", "hidden.go")); err != nil {
		t.Fatal(err)
	}

	analysis, err := repo.AnalyzeSize(baseOutput[0])
	if err != nil {
		t.Fatal(err)
	}
	comparison := analysis.Comparison
	if comparison == nil || comparison.Current.Files != 2 || comparison.DeltaFiles != 0 || comparison.DeltaBytes != 0 || len(comparison.LargestChanges) != 0 {
		t.Fatalf("comparison = %+v", comparison)
	}
	if analysis.Governed.Files != 1 {
		t.Fatalf("working tree governed files = %d", analysis.Governed.Files)
	}
}

func TestAnalyzeSizeRequiresGitTransformedWorkingContentToBeStaged(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	git(t, repo.Root, "config", "core.autocrlf", "false")
	writeFile(t, repo.Root, ".gitattributes", "*.txt text eol=crlf\n*.bin filter=lfs -text\n")
	git(t, repo.Root, "add", ".gitattributes")
	git(t, repo.Root, "commit", "-m", "base")
	baseOutput, err := repo.gitLines("rev-parse", "HEAD")
	if err != nil || len(baseOutput) != 1 {
		t.Fatalf("base = %v, error = %v", baseOutput, err)
	}

	writeFile(t, repo.Root, "content.txt", "payload\r\n")
	if _, err := repo.AnalyzeSize(baseOutput[0]); err == nil || !strings.Contains(err.Error(), "stage or commit") {
		t.Fatalf("transformed working-tree error = %v", err)
	}
	git(t, repo.Root, "add", "content.txt")
	analysis, err := repo.AnalyzeSize(baseOutput[0])
	if err != nil {
		t.Fatal(err)
	}
	if comparison := analysis.Comparison; comparison == nil || comparison.AddedFiles != 1 || comparison.AddedBytes != int64(len("payload\n")) {
		t.Fatalf("staged comparison = %+v", comparison)
	}

	writeFile(t, repo.Root, "asset.bin", "materialized asset")
	if _, err := repo.AnalyzeSize(baseOutput[0]); err == nil || !strings.Contains(err.Error(), "asset.bin") || !strings.Contains(err.Error(), "stage or commit") {
		t.Fatalf("filtered working-tree error = %v", err)
	}
}

func assertSizeComparisonTotals(t *testing.T, comparison SizeComparison, mergeBase string) {
	t.Helper()
	if comparison.MergeBase != mergeBase || comparison.Base != (SizeTotal{Files: 2, Bytes: 150}) ||
		comparison.Current != (SizeTotal{Files: 2, Bytes: 250}) || comparison.DeltaFiles != 0 || comparison.DeltaBytes != 100 {
		t.Fatalf("comparison = %+v", comparison)
	}
}

func assertSizeComparisonChanges(t *testing.T, comparison SizeComparison) {
	t.Helper()
	if comparison.AddedFiles != 1 || comparison.AddedBytes != 90 || comparison.RemovedFiles != 1 || comparison.RemovedBytes != 50 ||
		comparison.ResizedFiles != 1 || comparison.ResizedBytes != 60 {
		t.Fatalf("comparison changes = %+v", comparison)
	}
}

func assertSizeChangeStates(t *testing.T, states map[string]string, changes []SizeChange) {
	t.Helper()
	if states["src/app.go"] != "resized" || states["src/new.go"] != "added" || states["docs/old.md"] != "removed" {
		t.Fatalf("largest changes = %+v", changes)
	}
}

func groupByName(groups []SizeGroup, name string) SizeGroup {
	for _, group := range groups {
		if group.Name == name {
			return group
		}
	}
	return SizeGroup{}
}

func deltaByName(groups []SizeGroupDelta, name string) SizeGroupDelta {
	for _, group := range groups {
		if group.Name == name {
			return group
		}
	}
	return SizeGroupDelta{}
}
