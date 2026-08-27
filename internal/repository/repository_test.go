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

// The pinned toolchain the policy root carries is the only Go Code Polishy runs.
// An ambient toolchain on PATH and an environment override are not other ways
// to answer this question, because a target would then be governed by whatever
// Go its machine happens to have.
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

// The pin a policy root carries is the version it requires, in the one form a
// release records: a leading `v` belongs to the distribution that prints it.
func TestToolPinReadsWhatThePolicyRootCarries(t *testing.T) {
	t.Parallel()
	policyRoot := t.TempDir()
	writeFile(t, policyRoot, "scripts/go_version.txt", "1.26.6\n")
	writeFile(t, policyRoot, "tools/osv-scanner-version.txt", "v2.4.0\n")
	writeFile(t, policyRoot, "tools/ruff-version.txt", "\n")
	repo := Repository{PolicyRoot: policyRoot}
	for name, want := range map[string]string{
		"go": "1.26.6", "osv-scanner": "2.4.0",
		// A policy root that records no usable pin pins nothing, so the caller
		// refuses the tool rather than accepting whatever is installed.
		"ruff": "", "staticcheck": "",
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
	if !selection.All || !slices.Contains(selection.Files, "a.go") {
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
	if !selection.All || !slices.Contains(selection.Files, "a.go") {
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
	if selection.Base != base || selection.All || !slices.Equal(selection.Files, []string{"src/value.go", "src/working.go"}) {
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
	if !selection.All || !slices.Contains(selection.Files, "docs/linked.md") {
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
	if !selection.All || !slices.Contains(selection.Files, "docs/index.md") {
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
	if !selection.All || !slices.Contains(selection.Files, "b.go") || !slices.Contains(selection.Deleted, "a.go") {
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
	if !selection.All || !slices.Contains(selection.Deleted, "a.go") || !slices.Contains(selection.Files, "moved.go") {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestPolicyControlPlaneTriggersFullSelection(t *testing.T) {
	t.Parallel()
	repo := Repository{}
	// Which release governs the repository decides what every check means.
	if !repo.policyInputChanged([]string{policy.LockFilename}) || !repo.policyInputChanged([]string{policy.ConfigFilename}) {
		t.Fatal("the configuration and the release a target locks must trigger a full check")
	}
	// A submodule map is not how a target names a release, so it is an ordinary
	// file rather than a policy input.
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
	for _, path := range []string{"desktop/eslint.config.mjs", "desktop/knip.json", "desktop/.prettierrc.json", "desktop/tsconfig.node.json", "tools/ruff.toml", "osv-scanner.toml"} {
		if !repo.policyInputChanged([]string{path}) {
			t.Fatalf("%s should trigger full selection", path)
		}
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
