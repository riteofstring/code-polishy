package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestAnalyzeSizePreservesSourceCountsWithUnreadableIgnoredDirectories(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config = policy.Config{
		Tests:   policy.Testing{Ownership: []policy.TestOwnership{{Module: "application", Paths: []string{"src/*_test.go"}}}},
		Quality: policy.Quality{LineBudget: &policy.LineBudget{Production: 10}},
	}
	writeFile(t, repo.Root, ".gitignore", "runs/\n")
	writeFile(t, repo.Root, "src/app.go", "package app\n\nfunc Run() {}\n")
	writeFile(t, repo.Root, "src/app_test.go", "package app\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "source")
	paths := []string{}
	for index := range maximumSizeUnreadablePaths + 2 {
		path := fmt.Sprintf("runs/private-%02d", index)
		writeFile(t, repo.Root, path+"/output", "unmeasured bytes\n")
		absolute := filepath.Join(repo.Root, filepath.FromSlash(path))
		denySizeDirectory(t, absolute)
		paths = append(paths, path)
	}
	writeFile(t, repo.Root, "runs/z-readable/output", "measured bytes\n")
	analysis, err := repo.AnalyzeSize("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	workspace := analysis.Workspace
	assertIncompleteSizeAnalysis(t, analysis, paths)
	for _, path := range paths {
		if err := os.Chmod(filepath.Join(repo.Root, filepath.FromSlash(path)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	recovered, err := repo.AnalyzeSize("")
	if err != nil || recovered.Workspace.Incomplete || recovered.Workspace.UnreadablePathCount != 0 || len(recovered.Workspace.UnreadablePaths) != 0 || recovered.Workspace.Bytes != workspace.Bytes+int64(len(paths)*17) {
		t.Fatalf("permission recovery: %+v, error = %v", recovered.Workspace, err)
	}
	source := filepath.Join(repo.Root, "src", "app.go")
	if err := os.Chmod(source, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(source, 0o600) })
	if _, err := repo.AnalyzeSize(""); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("unreadable governed source must fail: %v", err)
	}
}

func TestAnalyzeSizeRejectsUnreadableTrackedDirectory(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config.Quality.LineBudget = &policy.LineBudget{Production: 10}
	writeFile(t, repo.Root, "visible.go", "package app\n")
	writeFile(t, repo.Root, "src/app.go", "package app\n\nfunc Run() {}\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "source")
	baseline, err := repo.AnalyzeSize("")
	if err != nil || baseline.SourceLines.Production != 3 {
		t.Fatalf("baseline source counts = %+v, error = %v", baseline.SourceLines, err)
	}
	directory := filepath.Join(repo.Root, "src")
	denySizeDirectory(t, directory)
	if analysis, err := repo.AnalyzeSize(""); !errors.Is(err, os.ErrPermission) {
		t.Errorf("unreadable tracked directory returned source counts %+v, error = %v", analysis.SourceLines, err)
	}
	if lines, err := repo.CountSourceLines(); !errors.Is(err, os.ErrPermission) {
		t.Errorf("unreadable tracked directory returned line budget %+v, error = %v", lines, err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	recovered, err := repo.AnalyzeSize("")
	if err != nil || recovered.Workspace.Incomplete || recovered.SourceLines.Production != 3 || !slices.Equal(recovered.SourceLines.Budget, baseline.SourceLines.Budget) {
		t.Fatalf("permission recovery = %+v, error = %v", recovered, err)
	}
}

func assertIncompleteSizeAnalysis(t *testing.T, analysis SizeAnalysis, paths []string) {
	t.Helper()
	workspace := analysis.Workspace
	if !workspace.Incomplete || workspace.UnreadablePathCount != len(paths) || !slices.Equal(workspace.UnreadablePaths, paths[:maximumSizeUnreadablePaths]) {
		t.Fatalf("incomplete workspace = %+v", workspace)
	}
	if group := groupByName(workspace.TopLevel, "runs"); group.Files != 1 || group.Bytes != 15 {
		t.Fatalf("readable sibling was not measured: %+v", group)
	}
	if lines := analysis.SourceLines; lines.Production != 2 || lines.Test != 1 || len(lines.Budget) != 1 || lines.Budget[0].Budget != 10 || lines.Budget[0].Lines != 2 {
		t.Fatalf("source counts or budget lost: %+v", lines)
	}
	if analysis.Comparison == nil || analysis.Comparison.DeltaBytes != 0 {
		t.Fatalf("base comparison lost: %+v", analysis.Comparison)
	}
}

func denySizeDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o700) })
	if _, err := os.ReadDir(path); err == nil {
		t.Skip("this host does not enforce directory read permissions for the test user")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
}

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
