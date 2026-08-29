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

func TestAdvisoriesPreserveLocationsAndCanonicalOrder(t *testing.T) {
	t.Parallel()
	repo := portabilityRepository(t)
	writePortabilityFile(t, repo.Root, "src/z.mjs", "\nconst home = \"/Users/alice/project\";\n")
	writePortabilityFile(t, repo.Root, "src/a.mjs", "const sibling = path.join(PROJECT_ROOT, \"../catalog\");\n")
	advisories := Advisories(repo, []string{"src/z.mjs", "src/a.mjs"})
	if len(advisories) != 2 {
		t.Fatalf("advisories = %+v", advisories)
	}
	if advisories[0].Path != "src/z.mjs" || advisories[0].Subject != "2" {
		t.Fatalf("first advisory = %+v", advisories[0])
	}
	if advisories[1].Path != "src/a.mjs" || advisories[1].Subject != "1" {
		t.Fatalf("second advisory = %+v", advisories[1])
	}
}

func TestAdvisoriesSkipBinaryAndUnreadableSources(t *testing.T) {
	t.Parallel()
	repo := portabilityRepository(t)
	writePortabilityFile(t, repo.Root, "src/binary.mjs", "\x00const home = \"/Users/alice/project\";\n")
	if advisories := Advisories(repo, []string{"src/binary.mjs", "src/missing.mjs"}); len(advisories) != 0 {
		t.Fatalf("advisories = %+v", advisories)
	}
}

func TestQuotedLiteralsRespectEscapesCommentsAndMalformedInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		line         string
		hashComments bool
		want         []string
	}{
		{name: "escaped-double", line: `const value = "a\"b";`, want: []string{`a"b`}},
		{name: "escaped-single", line: `value = 'it\'s'`, hashComments: true, want: []string{"it's"}},
		{name: "line-comment", line: `const kept = "yes"; // "ignored"`, want: []string{"yes"}},
		{name: "hash-comment", line: `value = "yes" # "ignored"`, hashComments: true, want: []string{"yes"}},
		{name: "unterminated", line: `value = "unfinished`, want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := quotedLiterals(test.line, test.hashComments); !slices.Equal(got, test.want) {
				t.Fatalf("quotedLiterals(%q) = %q, want %q", test.line, got, test.want)
			}
		})
	}
}

func TestLiteralAndCommentBoundaries(t *testing.T) {
	t.Parallel()
	if got := literalValue(`"\\q"`, '"'); got != `\q` {
		t.Fatalf("invalid double-quoted literal = %q", got)
	}
	commentCases := []struct {
		line         string
		index        int
		hashComments bool
		want         bool
	}{
		{line: "# prose", index: 0, hashComments: true, want: true},
		{line: "# prose", index: 0, hashComments: false, want: false},
		{line: "// prose", index: 0, want: true},
		{line: "/", index: 0, want: false},
		{line: "x// prose", index: 1, want: true},
	}
	for _, test := range commentCases {
		if got := lineCommentAt(test.line, test.index, test.hashComments); got != test.want {
			t.Errorf("lineCommentAt(%q, %d, %v) = %v, want %v", test.line, test.index, test.hashComments, got, test.want)
		}
	}
}

func TestHashCommentLanguages(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"python", "shell", "ruby"} {
		if !hashCommentLanguage(language) {
			t.Errorf("%s should use hash comments", language)
		}
	}
	if hashCommentLanguage("javascript") {
		t.Fatal("javascript should not use hash comments")
	}
}

func TestSiblingReferenceRequiresStaticRootContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line  string
		value string
		want  bool
	}{
		{line: `path.join(PROJECT_ROOT, "../catalog")`, value: "../catalog", want: true},
		{line: `path.join(process.cwd(), "..\\catalog")`, value: `..\catalog`, want: true},
		{line: `"../catalog"`, value: "../catalog", want: false},
		{line: `"../catalog" + PROJECT_ROOT`, value: "../catalog", want: false},
		{line: `path.join(base, "../catalog")`, value: "../catalog", want: false},
		{line: `path.join(PROJECT_ROOT, "../catalog/*")`, value: "../catalog/*", want: false},
	}
	for _, test := range tests {
		if got := siblingReference(test.line, test.value); got != test.want {
			t.Errorf("siblingReference(%q, %q) = %v, want %v", test.line, test.value, got, test.want)
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
