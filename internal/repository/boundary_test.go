package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestChangeBoundaryUsesTrustedBaseOwnership(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, "owned/value.md", "allowed\n")
	writeFile(t, repo.Root, "other/value.md", "forbidden\n")
	widened := strings.Replace(boundaryConfig("other/**"), `"paths":["owned/**"]`, `"paths":["owned/**","other/**"]`, 1)
	writeFile(t, repo.Root, ".code-polishy.json", widened)

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.ChangedPaths, []string{".code-polishy.json", "other/value.md", "owned/value.md"}) {
		t.Fatalf("changed paths = %v", result.ChangedPaths)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("findings = %+v", result.Findings)
	}
	if result.Findings[0].Subject != "control-plane" || result.Findings[1].Subject != "other" {
		t.Fatalf("candidate policy unexpectedly changed ownership: %+v", result.Findings)
	}
}

func TestChangeBoundaryAcceptsAllowedAddsEditsAndDeletes(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, "owned/value.md", "edited\n")
	writeFile(t, repo.Root, "owned/new.md", "new\n")
	if err := os.Remove(filepath.Join(repo.Root, "owned/remove.md")); err != nil {
		t.Fatal(err)
	}

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %+v", result.Findings)
	}
	want := []string{"owned/new.md", "owned/remove.md", "owned/value.md"}
	if !slices.Equal(result.ChangedPaths, want) {
		t.Fatalf("changed paths = %v, want %v", result.ChangedPaths, want)
	}
}

func TestChangeBoundaryChecksStagedChangesHiddenByWorkingTree(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, "other/value.md", "staged forbidden\n")
	git(t, repo.Root, "add", "other/value.md")
	writeFile(t, repo.Root, "other/value.md", "base\n")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.ChangedPaths, []string{"other/value.md"}) {
		t.Fatalf("changed paths = %v", result.ChangedPaths)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "other/value.md" || result.Findings[0].Subject != "other" {
		t.Fatalf("staged finding = %+v", result.Findings)
	}
}

func TestChangeBoundaryChecksCommittedChangesHiddenByIndexAndWorkingTree(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, "other/value.md", "committed forbidden\n")
	git(t, repo.Root, "add", "other/value.md")
	git(t, repo.Root, "commit", "-m", "change other module")
	git(t, repo.Root, "restore", "--source="+base, "--staged", "--worktree", "--", "other/value.md")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.ChangedPaths, []string{"other/value.md"}) {
		t.Fatalf("changed paths = %v", result.ChangedPaths)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "other/value.md" || result.Findings[0].Subject != "other" {
		t.Fatalf("committed finding = %+v", result.Findings)
	}
}

func TestChangeBoundaryChecksCommittedChangesRevertedBeforeHead(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, "other/value.md", "committed forbidden\n")
	git(t, repo.Root, "add", "other/value.md")
	git(t, repo.Root, "commit", "-m", "change other module")
	writeFile(t, repo.Root, "other/value.md", "base\n")
	git(t, repo.Root, "add", "other/value.md")
	git(t, repo.Root, "commit", "-m", "restore other module")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "other/value.md" || result.Findings[0].Subject != "other" {
		t.Fatalf("reverted committed finding = %+v", result.Findings)
	}
}

func TestChangeBoundaryTreatsBothSidesOfRenameAsChanges(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	git(t, repo.Root, "mv", "owned/value.md", "other/moved.md")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.ChangedPaths, []string{"other/moved.md", "owned/value.md"}) {
		t.Fatalf("changed paths = %v", result.ChangedPaths)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "other/moved.md" {
		t.Fatalf("findings = %+v", result.Findings)
	}
}

func TestChangeBoundaryAlwaysProtectsControlPlane(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, ".github/workflows/policy.yml", "changed\n")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"control"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Subject != "control-plane" {
		t.Fatalf("findings = %+v", result.Findings)
	}
}

func TestProtectedControlPathsIncludeIgnoreAndDeliveryFiles(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		".code-polishy.json", ".code-polishy.lock.json", "CODEOWNERS",
		".github/CODEOWNERS", ".github/workflows/policy.yml", ".gitignore", "nested/.gitignore",
		".gitattributes", "nested/.gitattributes",
	} {
		if !protectedControlPath(path, ".code-polishy.json") {
			t.Errorf("%s was not protected", path)
		}
	}
	if protectedControlPath("owned/value.md", ".code-polishy.json") {
		t.Fatal("ordinary owned file was treated as control-plane")
	}
	// A target installs a release and locks it; retired submodule and wrapper
	// artifacts are ordinary paths rather than reserved control-plane paths.
	for _, path := range []string{".gitmodules", "check_policy.sh"} {
		if protectedControlPath(path, ".code-polishy.json") {
			t.Errorf("%s is submodule-era control plane and must not be protected", path)
		}
	}
}

func TestProtectedNewArtifactPathsIncludeControlNamespaces(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		".code-polishy.json", ".code-polishy.json/override", ".code-polishy.lock.json", ".code-polishy.lock.json/override",
		"CODEOWNERS", "CODEOWNERS/override", ".github", ".github/CODEOWNERS", ".github/CODEOWNERS/override",
		".github/workflows", ".github/workflows/policy.yml", ".gitignore", "nested/.gitignore", "nested/.gitignore/override",
		".gitattributes", "nested/.gitattributes", "nested/.gitattributes/override",
	} {
		if !protectedNewArtifactPath(path, ".code-polishy.json") {
			t.Errorf("%s was not protected for a new artifact", path)
		}
	}
	if protectedNewArtifactPath("owned/value.md", ".code-polishy.json") {
		t.Fatal("ordinary owned file was treated as a new artifact control-plane path")
	}
}

func TestChangeBoundaryRejectsUnknownModuleAndMovingReference(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	if _, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"missing"}, nil, nil); err == nil || !strings.Contains(err.Error(), "no module") {
		t.Fatalf("expected unknown-module error, got %v", err)
	}
	if _, err := CheckChangeBoundary(repo.Root, repo.Root, "", "main", []string{"owned"}, nil, nil); err == nil || !strings.Contains(err.Error(), "exact") {
		t.Fatalf("expected exact-base error, got %v", err)
	}
}

func TestChangeBoundaryAllowsOnlyExplicitExistingArtifactPath(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, "plan.md", "updated\n")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, []string{"plan.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 || !slices.Equal(result.AllowedPaths, []string{"plan.md"}) {
		t.Fatalf("result = %+v", result)
	}
	if _, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, []string{"missing.md"}, nil); err == nil || !strings.Contains(err.Error(), "tracked file") {
		t.Fatalf("expected missing-artifact error, got %v", err)
	}
	if _, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, []string{".code-polishy.json"}, nil); err == nil || !strings.Contains(err.Error(), "control-plane") {
		t.Fatalf("expected control-artifact error, got %v", err)
	}
	if _, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, []string{"plan-link.md"}, nil); err == nil || !strings.Contains(err.Error(), "tracked file") {
		t.Fatalf("expected symlink-artifact error, got %v", err)
	}
}

func TestChangeBoundaryAllowsOnlyExplicitNewArtifactPath(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, "new-plan.md", "new plan\n")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 || !slices.Equal(result.AllowedNewPaths, []string{"new-plan.md"}) {
		t.Fatalf("result = %+v", result)
	}

	writeFile(t, repo.Root, "new-plan-sibling.md", "not allowed\n")
	result, err = CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "new-plan-sibling.md" || result.Findings[0].Subject != "unowned" {
		t.Fatalf("unused new-path permission granted a sibling: %+v", result.Findings)
	}
}

func TestChangeBoundaryRejectsInvalidNewArtifactPermissions(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	for _, test := range []struct {
		name  string
		paths []string
		new   []string
		want  string
	}{
		{name: "existing path", new: []string{"plan.md"}, want: "use --allow-path"},
		{name: "missing existing path", paths: []string{"missing.md"}, want: "tracked file"},
		{name: "glob", new: []string{"new-*.md"}, want: "not a glob"},
		{name: "control plane", new: []string{".code-polishy.json"}, want: "control-plane"},
		{name: "workflow namespace", new: []string{".github/workflows"}, want: "control-plane"},
		{name: "duplicate path classes", paths: []string{"new-plan.md"}, new: []string{"new-plan.md"}, want: "duplicates --allow-path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, test.paths, test.new)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestChangeBoundaryRejectsNewArtifactWhenTrustedBasePathCannotBeRead(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)

	_, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan\x00.md"})
	if err == nil || !strings.Contains(err.Error(), "read allowed new path at the trusted base") {
		t.Fatalf("error = %v", err)
	}
}

func TestChangeBoundaryRejectsNonRegularNewArtifacts(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	if err := os.Symlink("plan.md", filepath.Join(repo.Root, "new-plan-link.md")); err != nil {
		t.Fatal(err)
	}
	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan-link.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Subject != "new-path" {
		t.Fatalf("symlink findings = %+v", result.Findings)
	}

	if err := os.Mkdir(filepath.Join(repo.Root, "new-plan-directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	findings := boundaryFindings(
		repo, ".code-polishy.json", "new-plan-directory", map[string]bool{}, map[string]bool{},
		map[string]bool{"new-plan-directory": true}, map[string]bool{"new-plan-directory": true}, map[string]bool{"new-plan-directory": true},
	)
	if len(findings) != 1 || findings[0].Subject != "new-path" {
		t.Fatalf("directory findings = %+v", findings)
	}

	findings = boundaryFindings(
		repo, ".code-polishy.json", "deleted-new-plan.md", map[string]bool{}, map[string]bool{},
		map[string]bool{"deleted-new-plan.md": true}, map[string]bool{}, map[string]bool{"deleted-new-plan.md": true},
	)
	if len(findings) != 1 || findings[0].Subject != "new-path" {
		t.Fatalf("deletion findings = %+v", findings)
	}
}

func TestChangeBoundaryRejectsStagedNonRegularNewArtifact(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	path := filepath.Join(repo.Root, "new-plan.md")
	if err := os.Symlink("plan.md", path); err != nil {
		t.Fatal(err)
	}
	git(t, repo.Root, "add", "new-plan.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, "new-plan.md", "regular working tree file\n")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "new-plan.md" || result.Findings[0].Subject != "new-path" {
		t.Fatalf("staged non-regular finding = %+v", result.Findings)
	}
}

func TestChangeBoundaryRejectsCommittedNonRegularNewArtifactMaskedByRegularIndexAndWorktree(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	path := filepath.Join(repo.Root, "new-plan.md")
	if err := os.Symlink("plan.md", path); err != nil {
		t.Fatal(err)
	}
	git(t, repo.Root, "add", "new-plan.md")
	git(t, repo.Root, "commit", "-m", "add symlink artifact")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, "new-plan.md", "regular artifact\n")
	git(t, repo.Root, "add", "new-plan.md")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "new-plan.md" || result.Findings[0].Subject != "new-path" {
		t.Fatalf("committed non-regular finding = %+v", result.Findings)
	}
}

func TestChangeBoundaryRejectsNewArtifactWithCommittedNonRegularHistory(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	path := filepath.Join(repo.Root, "new-plan.md")
	if err := os.Symlink("plan.md", path); err != nil {
		t.Fatal(err)
	}
	git(t, repo.Root, "add", "new-plan.md")
	git(t, repo.Root, "commit", "-m", "add symlink artifact")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, "new-plan.md", "regular artifact\n")
	git(t, repo.Root, "add", "new-plan.md")
	git(t, repo.Root, "commit", "-m", "replace artifact with regular file")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "new-plan.md" || result.Findings[0].Subject != "new-path" {
		t.Fatalf("committed non-regular history finding = %+v", result.Findings)
	}
}

func TestChangeBoundaryRejectsStagedNewArtifactDeletionMaskedByRegularWorktree(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, "new-plan.md", "regular artifact\n")
	git(t, repo.Root, "add", "new-plan.md")
	git(t, repo.Root, "commit", "-m", "add regular artifact")
	git(t, repo.Root, "rm", "--cached", "new-plan.md")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "new-plan.md" || result.Findings[0].Subject != "new-path" {
		t.Fatalf("staged deletion finding = %+v", result.Findings)
	}
}

func TestChangeBoundaryRejectsNewSubmodule(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	submodule := filepath.Join(repo.Root, "new-plan-submodule")
	if err := os.Mkdir(submodule, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, submodule, "init", "-b", "main")
	git(t, submodule, "config", "user.email", "tests@example.test")
	git(t, submodule, "config", "user.name", "Code Polishy Tests")
	writeFile(t, submodule, "README.md", "submodule\n")
	git(t, submodule, "add", "README.md")
	git(t, submodule, "commit", "-m", "initial")
	git(t, repo.Root, "add", "new-plan-submodule")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan-submodule"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Subject != "new-path" {
		t.Fatalf("submodule findings = %+v", result.Findings)
	}
}

func TestChangeBoundaryNewPathDoesNotAuthorizeUnownedRename(t *testing.T) {
	t.Parallel()
	repo, _ := boundaryRepository(t)
	writeFile(t, repo.Root, "unowned-source.md", "source\n")
	git(t, repo.Root, "add", "unowned-source.md")
	git(t, repo.Root, "commit", "-m", "add unowned source")
	base := strings.TrimSpace(gitOutput(t, repo.Root, "rev-parse", "HEAD"))
	git(t, repo.Root, "mv", "unowned-source.md", "new-plan.md")

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "unowned-source.md" || result.Findings[0].Subject != "unowned" {
		t.Fatalf("rename findings = %+v", result.Findings)
	}
}

func TestChangeBoundaryUsesTrustedBaseNewPathPermissions(t *testing.T) {
	t.Parallel()
	repo, base := boundaryRepository(t)
	writeFile(t, repo.Root, "new-plan.md", "allowed\n")
	writeFile(t, repo.Root, "unowned-sibling.md", "forbidden\n")
	writeFile(t, repo.Root, ".code-polishy.json", strings.Replace(boundaryConfig("other/**"), `"paths":["owned/**"]`, `"paths":["owned/**","unowned-*.md"]`, 1))

	result, err := CheckChangeBoundary(repo.Root, repo.Root, "", base, []string{"owned"}, nil, []string{"new-plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 2 || result.Findings[0].Subject != "control-plane" || result.Findings[1].Path != "unowned-sibling.md" || result.Findings[1].Subject != "unowned" {
		t.Fatalf("candidate configuration widened new-path permission: %+v", result.Findings)
	}
}

func boundaryRepository(t *testing.T) (Repository, string) {
	t.Helper()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, ".code-polishy.json", boundaryConfig("other/**"))
	writeFile(t, repo.Root, "owned/value.md", "base\n")
	writeFile(t, repo.Root, "owned/remove.md", "remove\n")
	writeFile(t, repo.Root, "other/value.md", "base\n")
	writeFile(t, repo.Root, "plan.md", "base\n")
	if err := os.Symlink("plan.md", filepath.Join(repo.Root, "plan-link.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, ".github/workflows/policy.yml", "base\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	command := gitOutput(t, repo.Root, "rev-parse", "HEAD")
	return repo, strings.TrimSpace(command)
}

func boundaryConfig(otherPath string) string {
	return `{"version":3,"project":{"kind":"content"},"modules":[` +
		`{"name":"owned","paths":["owned/**"]},` +
		`{"name":"other","paths":["` + otherPath + `"]},` +
		`{"name":"control","paths":[".github/**"]}],` +
		`"checks":[],"tests":{"suites":[` +
		`{"name":"owned-test","kind":"content","scope":"module","modules":["owned"],"argv":["go","test","./..."]},` +
		`{"name":"full","kind":"content","scope":"repository","argv":["go","test","./..."]}]},` +
		`"supplyChain":{},"exceptions":[]}`
}

func gitOutput(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return string(output)
}
