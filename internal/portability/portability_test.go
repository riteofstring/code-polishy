package portability

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestAdvisoriesFindMachineAndSiblingPaths(t *testing.T) {
	t.Parallel()
	repo := portabilityRepository(t)
	writePortabilityFile(t, repo.Root, "src/config.mjs", `const home = "/Users/alice/work/data";
const sibling = path.join(ROOT_DIR, "../shared-content");
`)
	advisories := Advisories(repo, []string{"src/config.mjs"})
	checks := []string{}
	for _, advisory := range advisories {
		checks = append(checks, advisory.Check)
	}
	if !slices.Equal(checks, []string{"portability.machinePath", "portability.siblingReference"}) {
		t.Fatalf("advisories = %+v", advisories)
	}
}

func TestDeclaredExternalInputGovernsSiblingFallback(t *testing.T) {
	t.Parallel()
	repo := portabilityRepository(t)
	repo.Config.Portability.ExternalInputs = []policy.ExternalInput{{
		Name: "catalog", Kind: "repository", Module: "app",
		SourcePaths: []string{"src/config.mjs"}, SiblingFallback: "../shared-content",
	}}
	writePortabilityFile(t, repo.Root, "src/config.mjs", `const sibling = path.join(ROOT_DIR, "../shared-content");
`)
	if advisories := Advisories(repo, []string{"src/config.mjs"}); len(advisories) != 0 {
		t.Fatalf("declared fallback should be governed: %+v", advisories)
	}
	writePortabilityFile(t, repo.Root, "src/config.mjs", `const sibling = path.join(ROOT_DIR, "../different-checkout");
`)
	if advisories := Advisories(repo, []string{"src/config.mjs"}); len(advisories) != 1 {
		t.Fatalf("a different sibling must remain visible: %+v", advisories)
	}
}

func TestMachinePathVariants(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "/Users/alice/project", want: true},
		{value: "/home/alice/project", want: true},
		{value: `C:\Users\alice\project`, want: true},
		{value: `D:/Documents and Settings/Alice/project`, want: true},
		{value: "/opt/application", want: false},
		{value: "../sibling", want: false},
	} {
		if got := machinePath(test.value); got != test.want {
			t.Errorf("machinePath(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestAdvisoriesSkipTestsGeneratedAndDynamicParents(t *testing.T) {
	t.Parallel()
	repo := portabilityRepository(t)
	repo.Config.Scope.Generated = []string{"generated/**"}
	for path, source := range map[string]string{
		"src/config.test.mjs":  `const fixture = "/home/tester/data";` + "\n",
		"generated/config.mjs": `const generated = "../sibling";` + "\n",
		"src/runtime.mjs":      `const dynamic = "../" + projectName;` + "\n",
		"src/import.mjs":       `import component from "../components/widget.js";` + "\n",
		"src/comment.mjs":      `// path.join(ROOT_DIR, "../old-checkout")` + "\n",
		"src/name.mjs":         `const label = "../repo-root";` + "\n",
	} {
		writePortabilityFile(t, repo.Root, path, source)
	}
	if advisories := Advisories(repo, []string{"src/config.test.mjs", "generated/config.mjs", "src/runtime.mjs", "src/import.mjs", "src/comment.mjs", "src/name.mjs"}); len(advisories) != 0 {
		t.Fatalf("expected no advisories: %+v", advisories)
	}
}

func TestCoverageRequiresOwnedMatchingSource(t *testing.T) {
	t.Parallel()
	repo := portabilityRepository(t)
	repo.Config.Portability.ExternalInputs = []policy.ExternalInput{{
		Name: "catalog", Module: "app", SourcePaths: []string{"src/config.mjs"},
	}}
	writePortabilityFile(t, repo.Root, "src/config.mjs", "export const value = 1;\n")
	if findings := CoverageFindings(repo, []string{"src/config.mjs"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}

	repo.Config.Portability.ExternalInputs[0].SourcePaths = []string{"src/missing.mjs"}
	findings := CoverageFindings(repo, []string{"src/config.mjs"})
	if len(findings) != 1 || findings[0].Subject != "catalog" {
		t.Fatalf("missing-source findings = %+v", findings)
	}

	repo.Config.Portability.ExternalInputs[0].SourcePaths = []string{"src/config.test.mjs"}
	writePortabilityFile(t, repo.Root, "src/config.test.mjs", "export const value = 1;\n")
	findings = CoverageFindings(repo, []string{"src/config.test.mjs"})
	if len(findings) != 1 || findings[0].Path != policy.ConfigFilename {
		t.Fatalf("test-only source must not cover a runtime input: %+v", findings)
	}

	repo.Config.Portability.ExternalInputs[0].SourcePaths = []string{"other/config.mjs"}
	writePortabilityFile(t, repo.Root, "other/config.mjs", "export const value = 1;\n")
	findings = CoverageFindings(repo, []string{"other/config.mjs"})
	if len(findings) != 1 || findings[0].Path != "other/config.mjs" {
		t.Fatalf("wrong-owner findings = %+v", findings)
	}
}

func portabilityRepository(t *testing.T) repository.Repository {
	t.Helper()
	root := t.TempDir()
	return repository.Repository{
		Root: root, PolicyRoot: root,
		Config: policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"src/**"}}}},
	}
}

func writePortabilityFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
