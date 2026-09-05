package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestLanguageDetectionIsEcosystemAgnostic(t *testing.T) {
	t.Parallel()
	repo := Repository{}
	cases := map[string]string{"main.go": "go", "app.tsx": "typescript", "tool.py": "python", "lib.rs": "rust", "Main.java": "jvm", "query.sql": "sql", "widget.dart": "dart"}
	for path, want := range cases {
		if got := repo.Language(path); got != want {
			t.Errorf("Language(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestCustomLanguageRulesExtendDetectionWithoutOverridingBuiltIns(t *testing.T) {
	t.Parallel()
	repo := Repository{Config: policy.Config{Scope: policy.Scope{Languages: []policy.LanguageRule{
		{Name: "elixir", Paths: []string{"lib/**/*.ex"}},
		{Name: "template", Paths: []string{"lib/**"}},
	}}}}
	if got := repo.Languages("lib/sample.ex"); !slices.Equal(got, []string{"elixir", "template"}) {
		t.Fatalf("custom languages = %v", got)
	}
	if got := repo.Languages("lib/main.go"); !slices.Equal(got, []string{"go"}) {
		t.Fatalf("built-in language must win, got %v", got)
	}
}

func TestShellShebangDetectsSourceWithAnUnknownExtension(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "jobs/train.sbatch", "#!/usr/bin/env bash\nprintf '%s\\n' train\n")
	writeFile(t, root, "jobs/train.sbatch.template", "#!/bin/sh\nprintf '%s\\n' train\n")
	writeFile(t, root, "jobs/not-shell.template", "rendered input\n")
	writeFile(t, root, "jobs/mislabeled.py", "#!/bin/sh\nprintf('%s', 'python')\n")
	repo := Repository{Root: root}
	for _, path := range []string{"jobs/train.sbatch", "jobs/train.sbatch.template"} {
		if got := repo.Language(path); got != "shell" {
			t.Errorf("Language(%q) = %q, want shell", path, got)
		}
	}
	if got := repo.Language("jobs/not-shell.template"); got != "" {
		t.Errorf("unknown non-shell source language = %q", got)
	}
	if got := repo.Language("jobs/mislabeled.py"); got != "python" {
		t.Errorf("known extension language = %q, want python", got)
	}
}

func TestDeclaredDataRemainsGovernedAndOverridesDefaultGeneratedClassification(t *testing.T) {
	t.Parallel()
	repo := Repository{Config: policy.Config{Scope: policy.Scope{Data: []string{"data/**/*.json"}}}}
	if !repo.IsData("data/identity.generated.json") {
		t.Fatal("declared structured data was not classified as data")
	}
	if repo.IsGenerated("data/identity.generated.json") {
		t.Fatal("declared structured data was classified as generated")
	}
	if !repo.IsGenerated("generated/identity.generated.json") {
		t.Fatal("ordinary generated data lost default generated classification")
	}
}

func TestDeclaredDataCannotHideAnActualNestedControlInput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "data/package.json", "{}\n")
	repo := Repository{Root: root, Config: policy.Config{Scope: policy.Scope{Data: []string{"data/**/*.json"}}}}
	if repo.IsData("data/package.json") {
		t.Fatal("policy-sensitive control input was classified as data")
	}
	_, err := repo.AllFiles()
	if err == nil || !strings.Contains(err.Error(), "scope.data must not classify policy-sensitive control input data/package.json") {
		t.Fatalf("AllFiles error = %v", err)
	}
}

func TestDeclaredDataCannotHideExecutableOrShebangSource(t *testing.T) {
	t.Parallel()
	for name, contents := range map[string]string{
		"shebang":    "#!/bin/sh\necho runnable\n",
		"executable": "name: executable\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "data/catalog.yaml", contents)
			if name == "executable" {
				if err := os.Chmod(filepath.Join(root, "data", "catalog.yaml"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			repo := Repository{Root: root, Config: policy.Config{Scope: policy.Scope{Data: []string{"data/catalog.yaml"}}}}
			_, err := repo.AllFiles()
			if err == nil || !strings.Contains(err.Error(), "scope.data must not classify") {
				t.Fatalf("AllFiles error = %v", err)
			}
		})
	}
}

func TestVirtualEnvironmentContentsStayOutsideGovernedInventory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "src/app.py", "value = 1\n")
	writeFile(t, root, "apps/api/.venv/lib/python/site-packages/dependency.py", "value = 1\n")
	files, err := (Repository{Root: root}).AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(files, []string{"src/app.py"}) {
		t.Fatalf("governed files = %v", files)
	}
}

func TestEverySelectionRejectsInvalidDeclaredData(t *testing.T) {
	for _, mode := range []string{"files", "changes", "staged"} {
		t.Run(mode, func(t *testing.T) {
			repo := newGitRepository(t)
			repo.Config.Scope.Data = []string{"data/catalog.yaml"}
			writeFile(t, repo.Root, "README.md", "base\n")
			git(t, repo.Root, "add", "README.md")
			git(t, repo.Root, "commit", "-m", "base")
			writeFile(t, repo.Root, "data/catalog.yaml", "#!/bin/sh\necho runnable\n")
			if mode == "staged" {
				git(t, repo.Root, "add", "data/catalog.yaml")
			}
			explicit := []string(nil)
			if mode == "files" {
				explicit = []string{"data/catalog.yaml"}
			}
			_, err := repo.Select(mode, explicit)
			if err == nil || !strings.Contains(err.Error(), "scope.data must not classify executable source") {
				t.Fatalf("Select(%q) error = %v", mode, err)
			}
		})
	}
}

func TestGoToolResolvesOnlyThePinnedToolchainThePolicyRootCarries(t *testing.T) {
	policyRoot := t.TempDir()
	ambient := t.TempDir()
	for _, name := range []string{"go", "gofmt"} {
		if err := os.WriteFile(filepath.Join(ambient, name), []byte("tool"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", ambient)
	t.Setenv("CODE_POLISHY_GO_BIN", filepath.Join(ambient, "go"))
	repo := Repository{PolicyRoot: policyRoot}
	pinned := filepath.Join(policyRoot, ".tools", "go", runtime.GOOS+"-"+runtime.GOARCH, "go", "bin")
	for _, name := range []string{"go", "gofmt"} {
		want := filepath.Join(pinned, name)
		if runtime.GOOS == "windows" {
			want += ".exe"
		}
		if got := repo.GoTool(name); got != want {
			t.Fatalf("GoTool(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestToolPinReadsWhatThePolicyRootCarries(t *testing.T) {
	t.Parallel()
	policyRoot := t.TempDir()
	writeFile(t, policyRoot, "scripts/go_version.txt", "1.26.6\n")
	writeFile(t, policyRoot, "tools/osv-scanner-version.txt", "v2.4.0\n")
	writeFile(t, policyRoot, "tools/python-version.txt", "3.12.13+20260728\n")
	writeFile(t, policyRoot, "tools/ruff-version.txt", "\n")
	writeFile(t, policyRoot, "tools/ty-version.txt", "0.0.65\n")
	writeFile(t, policyRoot, "tools/vulture-version.txt", "2.16\n")
	repo := Repository{PolicyRoot: policyRoot}
	for name, want := range map[string]string{
		"go": "1.26.6", "osv-scanner": "2.4.0", "python": "3.12.13+20260728",

		"ruff": "", "staticcheck": "", "ty": "0.0.65", "vulture": "2.16",
	} {
		if got := repo.ToolPin(name); got != want {
			t.Fatalf("ToolPin(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCommandEnvironmentPutsReleaseOwnedToolsFirst(t *testing.T) {
	t.Parallel()
	policyRoot := t.TempDir()
	writeFile(t, policyRoot, "scripts/go_version.txt", "1.26.6\n")
	repo := Repository{PolicyRoot: policyRoot}
	goDirectory := filepath.Dir(repo.GoTool("go"))
	for _, name := range []string{"go", "gofmt"} {
		path := filepath.Join(goDirectory, executableName(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("tool"), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	environment := repo.CommandEnvironment()
	if len(environment.PathEntries) == 0 || environment.PathEntries[0] != goDirectory {
		t.Fatalf("path entries = %v", environment.PathEntries)
	}
	if len(environment.Tools) != 2 || environment.Tools[0].Name != "go" || environment.Tools[1].Name != "gofmt" {
		t.Fatalf("tools = %+v", environment.Tools)
	}
	for _, tool := range environment.Tools {
		if tool.Version != "1.26.6" || filepath.Dir(tool.Path) != goDirectory {
			t.Fatalf("tool = %+v", tool)
		}
	}
}

func TestCommandDiscoveryNoteReportsTheStableLauncherAndCopyableAction(t *testing.T) {
	prefix := t.TempDir()
	policyRoot := filepath.Join(prefix, "releases", "fixture")
	launcherDirectory := filepath.Join(prefix, "bin")
	writeFile(t, policyRoot, "release-manifest.json", "{}\n")
	launcher := filepath.Join(launcherDirectory, executableName("code-polishy"))
	if err := os.MkdirAll(launcherDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("launcher"), 0o700); err != nil {
		t.Fatal(err)
	}
	repo := Repository{PolicyRoot: policyRoot}
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("PATH", launcherDirectory)
	if note := repo.CommandDiscoveryNote(); !strings.Contains(note, "resolves to "+launcher) {
		t.Fatalf("correct launcher note = %q", note)
	}

	ambient := t.TempDir()
	ambientLauncher := filepath.Join(ambient, executableName("code-polishy"))
	if err := os.WriteFile(ambientLauncher, []byte("ambient"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", ambient)
	note := repo.CommandDiscoveryNote()
	if !strings.Contains(note, "selects "+ambientLauncher+" instead of "+launcher) ||
		!strings.Contains(note, "export PATH='") || !strings.Contains(note, launcherDirectory) {
		t.Fatalf("wrong launcher note = %q", note)
	}

	emptyPath := t.TempDir()
	t.Setenv("PATH", emptyPath)
	note = repo.CommandDiscoveryNote()
	if !strings.Contains(note, launcher+" is not on PATH") || !strings.Contains(note, launcherDirectory) {
		t.Fatalf("missing launcher note = %q", note)
	}
	if action := commandPathAction(launcherDirectory, "/opt/homebrew/bin/fish"); !strings.HasPrefix(action, "fish_add_path '") || !strings.Contains(action, launcherDirectory) {
		t.Fatalf("fish discovery action = %q", action)
	}
}

func TestAllFilesIncludesTrackedAndVisibleUntracked(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "tracked.go", "package sample\n")
	git(t, repo.Root, "add", "tracked.go")
	git(t, repo.Root, "commit", "-m", "base")
	writeFile(t, repo.Root, "new.go", "package sample\n")
	files, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(files, "tracked.go") || !slices.Contains(files, "new.go") {
		t.Fatalf("files = %v", files)
	}
}

func TestExplicitSelectionRejectsMissingAndExcludedPaths(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir(), Config: policy.Config{Scope: policy.Scope{Exclude: []string{"vendor/**"}}}}
	writeFile(t, repo.Root, "vendor/file.go", "package vendor\n")
	if _, err := repo.Select("files", []string{"missing.go"}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing-path error, got %v", err)
	}
	if _, err := repo.Select("files", []string{"vendor/file.go"}); err == nil || !strings.Contains(err.Error(), "outside governed scope") {
		t.Fatalf("expected excluded-path error, got %v", err)
	}
}

func TestExplicitSelectionExpandsContainedDirectoriesWithoutFollowingLinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "src/a.go", "package sample\n")
	writeFile(t, root, "src/nested/b.go", "package nested\n")
	writeFile(t, root, "src/ignored/c.go", "package ignored\n")
	if err := os.Symlink(filepath.Join(root, "src", "nested"), filepath.Join(root, "src", "linked")); err != nil {
		t.Skipf("create directory symbolic link: %v", err)
	}
	repo := Repository{Root: root, Config: policy.Config{Scope: policy.Scope{Exclude: []string{"src/ignored/**"}}}}
	selection, err := repo.Select("files", []string{"src"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/a.go", "src/nested/b.go"}
	if !slices.Equal(selection.Files, want) || !slices.Equal(selection.Requested.Expanded, want) ||
		selection.Requested.Mode != "files" || !slices.Equal(selection.Requested.Operands, []string{"src"}) {
		t.Fatalf("selection = %+v", selection)
	}
	rootSelection, err := repo.Select("files", []string{"."})
	if err != nil || !slices.Equal(rootSelection.Files, want) {
		t.Fatalf("root selection = %+v, error = %v", rootSelection, err)
	}
}

func TestExplicitSelectionRejectsRepeatedOverlappingEmptyAndSymbolicLinkOperands(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "src/a.go", "package sample\n")
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "src", "a.go"), filepath.Join(root, "linked.go")); err != nil {
		t.Skipf("create symbolic link: %v", err)
	}
	repo := Repository{Root: root}
	cases := []struct {
		name     string
		operands []string
		message  string
	}{
		{name: "repeated", operands: []string{"src/a.go", "./src/a.go"}, message: "selection operand is repeated"},
		{name: "overlapping", operands: []string{"src", "src/a.go"}, message: "selection operands overlap"},
		{name: "empty", operands: []string{"empty"}, message: "no governed regular files"},
		{name: "symbolic link", operands: []string{"linked.go"}, message: "symbolic link"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := repo.Select("files", testCase.operands)
			if err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("Select(files, %v) error = %v", testCase.operands, err)
			}
		})
	}
}

func TestModuleSelectionExpandsExactlyTheNamedDeclaredModules(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "alpha/a.go", "package alpha\n")
	writeFile(t, root, "beta/b.go", "package beta\n")
	writeFile(t, root, "other/c.go", "package other\n")
	repo := Repository{Root: root, Config: policy.Config{Modules: []policy.Module{
		{Name: "alpha", Paths: []string{"alpha/**"}},
		{Name: "beta", Paths: []string{"beta/**"}},
	}}}
	selection, err := repo.Select("modules", []string{"beta", "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(selection.Files, []string{"alpha/a.go", "beta/b.go"}) ||
		!slices.Equal(selection.Requested.Operands, []string{"beta", "alpha"}) ||
		!slices.Equal(selection.Requested.Modules, []string{"alpha", "beta"}) || selection.Requested.Mode != "module" {
		t.Fatalf("selection = %+v", selection)
	}
	for _, testCase := range []struct {
		names   []string
		message string
	}{
		{names: nil, message: "at least one"},
		{names: []string{"missing"}, message: "unknown module"},
		{names: []string{"alpha", "alpha"}, message: "repeated"},
	} {
		if _, err := repo.Select("modules", testCase.names); err == nil || !strings.Contains(err.Error(), testCase.message) {
			t.Fatalf("Select(modules, %v) error = %v", testCase.names, err)
		}
	}
	emptyRepo := Repository{Root: t.TempDir(), Config: policy.Config{Modules: []policy.Module{{Name: "empty", Paths: []string{"src/**"}}}}}
	if _, err := emptyRepo.Select("modules", []string{"empty"}); err == nil || !strings.Contains(err.Error(), "no governed files") {
		t.Fatalf("empty module error = %v", err)
	}
}

func TestChangedSelectionExpandsPolicyInputs(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "a.go", "package sample\n")
	writeFile(t, repo.Root, "go.mod", "module example.test/sample\n\ngo 1.26.5\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	writeFile(t, repo.Root, "go.mod", "module example.test/sample\n\ngo 1.26.5\n\n// changed\n")
	selection, err := repo.Select("changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.All || !selection.PolicySensitive || !slices.Equal(selection.Candidate.AddedOrModified, []string{"go.mod"}) ||
		len(selection.Candidate.Deleted) != 0 || !slices.Contains(selection.Files, "a.go") {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestCandidateDeltaStaysExactWhenAnalysisExpands(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "README.md", "# Project\n")
	writeFile(t, repo.Root, "source.go", "package sample\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	if err := os.Remove(filepath.Join(repo.Root, "README.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, "docs/guide.MARKDOWN", "# Guide\n")

	selection, err := repo.Select("changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.All || selection.PolicySensitive ||
		!slices.Equal(selection.Candidate.AddedOrModified, []string{"docs/guide.MARKDOWN"}) ||
		!slices.Equal(selection.Candidate.Deleted, []string{"README.md"}) ||
		!slices.Equal(selection.Files, []string{"docs/guide.MARKDOWN", "source.go"}) {
		t.Fatalf("selection = %+v", selection)
	}
	classification := repo.ClassifyDocumentationCandidate(selection)
	if !classification.Ordinary || !strings.Contains(classification.Reason, "Markdown only") {
		t.Fatalf("classification = %+v", classification)
	}
}

func TestChangedSelectionExcludesReviewArtifactsFromTheCandidate(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "README.md", "# Project\n")
	writeFile(t, repo.Root, ".code-polishy-reports/behavior-review/tracked.json", "{}\n")
	writeFile(t, repo.Root, ".code-polishy-reports/behavior-review/deleted.json", "{}\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	writeFile(t, repo.Root, ".code-polishy-reports/behavior-review/tracked.json", "{\"updated\":true}\n")
	if err := os.Remove(filepath.Join(repo.Root, ".code-polishy-reports/behavior-review/deleted.json")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, ".code-polishy-reports/behavior-review/receipt.json", "{}\n")
	writeFile(t, repo.Root, "source.go", "package sample\n")

	selection, err := repo.Select("changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if selection.All || selection.PolicySensitive ||
		!slices.Equal(selection.Candidate.AddedOrModified, []string{"source.go"}) || len(selection.Candidate.Deleted) != 0 ||
		!slices.Equal(selection.Files, []string{"source.go"}) {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestChangedSelectionExpandsReleaseArtifactVersionFiles(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config.SupplyChain.ReleaseArtifacts = []policy.ReleaseArtifact{{
		Name: "tool", VersionFile: "tools/tool-version.txt", Source: "github-release", Locator: "example/tool",
	}}
	writeFile(t, repo.Root, "a.go", "package sample\n")
	writeFile(t, repo.Root, "tools/tool-version.txt", "1.2.3\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	writeFile(t, repo.Root, "tools/tool-version.txt", "1.2.4\n")
	selection, err := repo.Select("changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.All || !selection.PolicySensitive ||
		!slices.Equal(selection.Candidate.AddedOrModified, []string{"tools/tool-version.txt"}) || !slices.Contains(selection.Files, "a.go") {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestSelectBaseIncludesCommittedWorkingTreeAndUntrackedChanges(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "src/value.go", "package sample\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	git(t, repo.Root, "switch", "-c", "feature")
	writeFile(t, repo.Root, "src/value.go", "package sample\n// committed\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "feature")
	writeFile(t, repo.Root, "src/working.go", "package sample\n")

	selection, err := repo.SelectBase("main")
	if err != nil {
		t.Fatal(err)
	}
	base, err := repo.MergeBase("main")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Base != base || selection.All ||
		!slices.Equal(selection.Candidate.AddedOrModified, []string{"src/value.go", "src/working.go"}) ||
		len(selection.Candidate.Deleted) != 0 ||
		!slices.Equal(selection.Files, []string{"src/value.go", "src/working.go"}) {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestChangedSelectionExpandsNonRegularPaths(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "docs/index.md", "# Documentation\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	if err := os.Symlink("index.md", filepath.Join(repo.Root, "docs", "linked.md")); err != nil {
		t.Skipf("create documentation symlink: %v", err)
	}

	selection, err := repo.Select("changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	classification := repo.ClassifyDocumentationCandidate(selection)
	if !selection.All || !slices.Contains(selection.Candidate.AddedOrModified, "docs/linked.md") ||
		!slices.Contains(selection.Files, "docs/linked.md") || !classification.Ordinary {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestStagedSelectionExpandsIndexSymlinkRemovedFromWorkingTree(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "docs/index.md", "# Documentation\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	linked := filepath.Join(repo.Root, "docs", "linked.md")
	if err := os.Symlink("index.md", linked); err != nil {
		t.Skipf("create documentation symlink: %v", err)
	}
	git(t, repo.Root, "add", "docs/linked.md")
	if err := os.Remove(linked); err != nil {
		t.Fatal(err)
	}

	selection, err := repo.Select("staged", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.All || !slices.Equal(selection.Candidate.AddedOrModified, []string{"docs/linked.md"}) ||
		!slices.Contains(selection.Files, "docs/index.md") {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestSelectBaseRejectsOptionLikeReference(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "a.go", "package sample\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	if _, err := repo.SelectBase("--all"); err == nil || !strings.Contains(err.Error(), "invalid base") {
		t.Fatalf("expected invalid-base error, got %v", err)
	}
}

func TestMergeBaseFilesAtAndReadAtUseExactCommittedTree(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "package.json", "{\"version\":\"1.0.0\"}\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	git(t, repo.Root, "switch", "-c", "feature")
	writeFile(t, repo.Root, "package.json", "{\"version\":\"2.0.0\"}\n")

	base, err := repo.MergeBase("main")
	if err != nil {
		t.Fatal(err)
	}
	files, err := repo.FilesAt(base)
	if err != nil || !slices.Contains(files, "package.json") {
		t.Fatalf("files=%v err=%v", files, err)
	}
	data, err := repo.ReadAt(base, "package.json")
	if err != nil || !strings.Contains(string(data), "1.0.0") {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if _, err := repo.ReadAt("main", "package.json"); err == nil {
		t.Fatal("non-exact revision was accepted")
	}
}

func TestReadRegularFileAtDistinguishesAbsentAndNonRegularPaths(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "policy.json", "{\"version\":3}\n")
	writeFile(t, repo.Root, "nested/value.txt", "value\n")
	if err := os.Symlink("policy.json", filepath.Join(repo.Root, "linked.json")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	command := exec.Command("git", "-C", repo.Root, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.TrimSpace(string(output))
	data, present, err := repo.ReadRegularFileAt(revision, "policy.json")
	if err != nil || !present || string(data) != "{\"version\":3}\n" {
		t.Fatalf("regular file data=%q present=%t err=%v", data, present, err)
	}
	if data, present, err = repo.ReadRegularFileAt(revision, "missing.json"); err != nil || present || data != nil {
		t.Fatalf("absent file data=%q present=%t err=%v", data, present, err)
	}
	for _, path := range []string{"linked.json", "nested"} {
		if _, _, err = repo.ReadRegularFileAt(revision, path); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("%s error = %v", path, err)
		}
	}
	if _, _, err = repo.ReadRegularFileAt("main", "policy.json"); err == nil {
		t.Fatal("non-exact revision was accepted")
	}
}

func TestDeletionExpandsChangedSelection(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "a.go", "package sample\n")
	writeFile(t, repo.Root, "b.go", "package sample\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	if err := os.Remove(filepath.Join(repo.Root, "a.go")); err != nil {
		t.Fatal(err)
	}
	selection, err := repo.Select("changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.All || !slices.Contains(selection.Files, "b.go") || !slices.Contains(selection.Candidate.Deleted, "a.go") {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestRenameParticipatesAsDeletionAndExpandsSelection(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "a.go", "package sample\n")
	writeFile(t, repo.Root, "b.go", "package sample\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	git(t, repo.Root, "mv", "a.go", "moved.go")
	selection, err := repo.Select("changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.All || !slices.Equal(selection.Candidate.AddedOrModified, []string{"moved.go"}) ||
		!slices.Equal(selection.Candidate.Deleted, []string{"a.go"}) || !slices.Contains(selection.Files, "moved.go") {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestPolicyControlPlaneTriggersFullSelection(t *testing.T) {
	t.Parallel()
	repo := Repository{}

	if !repo.policyInputChanged([]string{policy.LockFilename}) || !repo.policyInputChanged([]string{policy.ConfigFilename}) {
		t.Fatal("the configuration and the release a target locks must trigger a full check")
	}

	if repo.policyInputChanged([]string{".gitmodules"}) {
		t.Fatal("a submodule map must not trigger a full check")
	}
}

func TestAllDependencyAndContainerInputsTriggerFullSelection(t *testing.T) {
	t.Parallel()
	repo := Repository{}
	for _, path := range []string{"Cargo.toml", "pyproject.toml", "requirements-prod.txt", "Dockerfile.release", "Package.resolved"} {
		if !repo.policyInputChanged([]string{path}) {
			t.Fatalf("%s should trigger full selection", path)
		}
	}
	if repo.policyInputChanged([]string{"internal/block.go"}) {
		t.Fatal("ordinary source containing 'lock' must not trigger manifest expansion")
	}
}

func TestConditionalPolicyToolConfigsTriggerFullSelection(t *testing.T) {
	t.Parallel()
	repo := Repository{}
	for _, path := range []string{"desktop/eslint.config.mjs", "desktop/knip.json", "desktop/.prettierrc.json", "desktop/tsconfig.node.json", "tools/ruff.toml", "services/ty.toml", "osv-scanner.toml"} {
		if !repo.policyInputChanged([]string{path}) {
			t.Fatalf("%s should trigger full selection", path)
		}
	}
}

func TestClassifyDocumentationCandidate(t *testing.T) {
	t.Parallel()
	repo := Repository{Config: policy.Config{Documentation: policy.Documentation{ProductInputs: []string{"docs/product.MD"}}}}
	cases := map[string]struct {
		candidate CandidateDelta
		ordinary  bool
		reason    string
	}{
		"ordinary additions deletions and mixed case extensions": {
			candidate: CandidateDelta{AddedOrModified: []string{"README.MD", "docs/guide.markdown"}, Deleted: []string{"docs/removed.Md"}},
			ordinary:  true,
			reason:    "candidate contains ordinary Markdown only",
		},
		"empty": {
			reason: "no candidate paths were detected",
		},
		"non markdown": {
			candidate: CandidateDelta{AddedOrModified: []string{"docs/guide.md", "source.go"}},
			reason:    `changed path "source.go" is not Markdown`,
		},
		"agent control document": {
			candidate: CandidateDelta{AddedOrModified: []string{"docs/AGENTS.MD"}},
			reason:    `changed path "docs/AGENTS.MD" is a documentation control input`,
		},
		"nested skills control document": {
			candidate: CandidateDelta{AddedOrModified: []string{"plugins/skills/guide.MARKDOWN"}},
			reason:    `changed path "plugins/skills/guide.MARKDOWN" is a documentation control input`,
		},
		"nested templates control document": {
			candidate: CandidateDelta{Deleted: []string{"plugins/templates/guide.md"}},
			reason:    `changed path "plugins/templates/guide.md" is a documentation control input`,
		},
		"declared product input": {
			candidate: CandidateDelta{AddedOrModified: []string{"docs/product.MD"}},
			reason:    `changed path "docs/product.MD" is a declared documentation product input`,
		},
		"different product input case remains ordinary": {
			candidate: CandidateDelta{AddedOrModified: []string{"docs/product.md"}},
			ordinary:  true,
			reason:    "candidate contains ordinary Markdown only",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			classification := repo.ClassifyDocumentationCandidate(Selection{Candidate: test.candidate})
			if classification.Ordinary != test.ordinary || classification.Reason != test.reason {
				t.Fatalf("classification = %+v", classification)
			}
		})
	}
}

func TestStagedSelectionRefusesDivergentWorkingTree(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "a.go", "package sample\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	writeFile(t, repo.Root, "a.go", "package sample\n// staged\n")
	git(t, repo.Root, "add", "a.go")
	writeFile(t, repo.Root, "a.go", "package sample\n// unstaged\n")
	_, err := repo.Select("staged", nil)
	if err == nil || !strings.Contains(err.Error(), "unstaged changes") {
		t.Fatalf("expected divergence error, got %v", err)
	}
}

func TestStagedSelectionRefusesStagedFileDeletedFromWorkingTree(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "a.go", "package sample\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	writeFile(t, repo.Root, "a.go", "package sample\n// staged\n")
	git(t, repo.Root, "add", "a.go")
	if err := os.Remove(filepath.Join(repo.Root, "a.go")); err != nil {
		t.Fatal(err)
	}

	_, err := repo.Select("staged", nil)
	if err == nil || !strings.Contains(err.Error(), "unstaged changes") {
		t.Fatalf("expected divergence error, got %v", err)
	}
}

func TestResolveRejectsEscapingSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret", "no\n")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	repo := Repository{Root: root}
	_, err := repo.Resolve("escape")
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected containment error, got %v", err)
	}
}

func TestNestedGoModuleOwnershipUsesDeepestRoot(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "go.mod", "module root.example\n")
	writeFile(t, repo.Root, "nested/go.mod", "module nested.example\n")
	modules := repo.GoModules([]string{"go.mod", "nested/go.mod"})
	module, found := repo.OwningGoModule("nested/service/main.go", modules)
	if !found || module.Name != "nested.example" {
		t.Fatalf("module = %+v, found=%v", module, found)
	}
}

func TestDesignDocumentsCombineModuleAndSourceContextAndRemainStable(t *testing.T) {
	t.Parallel()
	repo := designDocumentRepository(t, []policy.Module{
		{Name: "api", Paths: []string{"api/**"}},
		{Name: "domain", Paths: []string{"domain/**"}},
	}, []policy.DesignDocument{
		{Path: "docs/design/domain.md", Module: "domain"},
		{Path: "docs/design/api.md", Module: "api"},
		{Path: "docs/design/entry.md", SourcePaths: []string{"domain/entry.go"}},
	})
	files := map[string]string{
		"docs/design/domain.md": "# Domain\n",
		"docs/design/api.md":    "# API\n",
		"docs/design/entry.md":  "# Entry\n",
		"api/handler.go":        "package api\n",
		"domain/model.go":       "package domain\n",
		"domain/entry.go":       "package domain\n",
	}
	for path, contents := range files {
		writeFile(t, repo.Root, path, contents)
	}
	if findings := repo.DesignDocumentFindings(); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if got, want := repo.DesignDocumentsForFiles([]string{"domain/entry.go", "api/handler.go", "domain/model.go", "domain/entry.go"}), []string{
		"docs/design/api.md", "docs/design/domain.md", "docs/design/entry.md",
	}; !slices.Equal(got, want) {
		t.Fatalf("file design documents = %v, want %v", got, want)
	}
	if got, err := repo.DesignDocumentsForModules([]string{"domain", "api"}); err != nil || !slices.Equal(got, []string{
		"docs/design/api.md", "docs/design/domain.md", "docs/design/entry.md",
	}) {
		t.Fatalf("module design documents = %v, err = %v", got, err)
	}
	if _, err := repo.DesignDocumentsForModules([]string{"missing"}); err == nil || !strings.Contains(err.Error(), "unknown module") {
		t.Fatalf("unknown module error = %v", err)
	}
}

func TestDesignDocumentFindingsRejectInvalidCurrentMappings(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		configure func(*testing.T, Repository)
		want      string
	}{
		"missing document": {
			configure: func(_ *testing.T, _ Repository) {},
			want:      "mapped document is missing",
		},
		"nonregular document": {
			configure: func(t *testing.T, repo Repository) {
				if err := os.MkdirAll(filepath.Join(repo.Root, "docs", "design", "domain.md"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: "mapped document is missing, non-regular, or escapes the repository",
		},
		"escaped document": {
			configure: func(t *testing.T, repo Repository) {
				outside := filepath.Join(t.TempDir(), "outside.md")
				if err := os.WriteFile(outside, []byte("# Outside\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(repo.Root, "docs", "design", "domain.md")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Skipf("create design-document symlink: %v", err)
				}
			},
			want: "mapped document is missing, non-regular, or escapes the repository",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := designDocumentRepository(t, []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}}, []policy.DesignDocument{{Path: "docs/design/domain.md", Module: "domain"}})
			test.configure(t, repo)
			findings := repo.DesignDocumentFindings()
			if len(findings) != 1 || findings[0].Check != DesignDocumentationCheck || findings[0].Path != policy.ConfigFilename ||
				findings[0].Subject != "docs/design/domain.md" || !strings.Contains(findings[0].Message, test.want) {
				t.Fatalf("findings = %+v, want %q", findings, test.want)
			}
		})
	}
}

func TestDesignDocumentFindingsRejectStaleOrAmbiguousSources(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		modules  []policy.Module
		scope    policy.Scope
		file     string
		contents string
		want     string
	}{
		"missing": {
			modules: []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}},
			file:    "domain/value.go",
			want:    "mapped source path is missing",
		},
		"excluded": {
			modules:  []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}},
			scope:    policy.Scope{Exclude: []string{"domain/**"}},
			file:     "domain/value.go",
			contents: "package domain\n",
			want:     "excluded from governed scope",
		},
		"not executable source": {
			modules:  []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}},
			file:     "domain/value.txt",
			contents: "value\n",
			want:     "not governed source",
		},
		"multiple owners": {
			modules: []policy.Module{
				{Name: "domain", Paths: []string{"domain/**"}},
				{Name: "duplicate", Paths: []string{"domain/**"}},
			},
			file:     "domain/value.go",
			contents: "package domain\n",
			want:     "must belong to exactly one module",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := designDocumentRepository(t, test.modules, []policy.DesignDocument{{
				Path: "docs/design/value.md", SourcePaths: []string{test.file},
			}})
			repo.Config.Scope = test.scope
			writeFile(t, repo.Root, "docs/design/value.md", "# Value\n")
			if test.contents != "" {
				writeFile(t, repo.Root, test.file, test.contents)
			}
			findings := repo.DesignDocumentFindings()
			if len(findings) != 1 || findings[0].Check != DesignDocumentationCheck || findings[0].Path != policy.ConfigFilename ||
				findings[0].Subject != test.file || !strings.Contains(findings[0].Message, test.want) {
				t.Fatalf("findings = %+v, want %q", findings, test.want)
			}
		})
	}
}

func TestDesignDocumentFindingsAdmitEverySupportedSourceKind(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"view/style.css", "view/page.html", "view/tool.ps1"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			repo := designDocumentRepository(t, []policy.Module{{Name: "view", Paths: []string{"view/**"}}}, []policy.DesignDocument{{
				Path: "docs/design/view.md", SourcePaths: []string{path},
			}})
			writeFile(t, repo.Root, "docs/design/view.md", "# View\n")
			writeFile(t, repo.Root, path, "value\n")
			if findings := repo.DesignDocumentFindings(); len(findings) != 0 {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func designDocumentRepository(t *testing.T, modules []policy.Module, documents []policy.DesignDocument) Repository {
	t.Helper()
	return Repository{Root: t.TempDir(), Config: policy.Config{
		Scope: policy.Scope{}, Modules: modules, Documentation: policy.Documentation{Design: documents},
	}}
}

func newGitRepository(t *testing.T) Repository {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	git(t, root, "config", "user.email", "tests@example.test")
	git(t, root, "config", "user.name", "Code Polishy Tests")
	return Repository{Root: root, Config: policy.Config{Scope: policy.Scope{}}}
}

func writeFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
