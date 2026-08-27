package quality

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

// A target without JavaScript never launches the sealed bundle, so an
// uninstalled bundle is not its problem.
func TestJavaScriptBundleStatusIgnoresTargetsWithoutJavaScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	findings, notes := JavaScriptBundleStatus(repo, []string{"internal/sample/file.go", "docs/plan.md"})
	if len(findings) != 0 || len(notes) != 0 {
		t.Fatalf("findings = %+v, notes = %+v", findings, notes)
	}
}

// A JavaScript-bearing target fails closed when the bundle is absent instead of
// falling back to an ambient runtime or a target-installed tool.
func TestJavaScriptBundleStatusFailsClosedWithoutTheBundle(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	findings, notes := JavaScriptBundleStatus(repo, []string{"app/main.ts"})
	if len(findings) != 1 || findings[0].Check != "policy.tool" || findings[0].Subject != "javascript-bundle" {
		t.Fatalf("findings = %+v", findings)
	}
	if !strings.Contains(findings[0].Message, "install-policy-tools.sh") {
		t.Fatalf("the finding does not name the installer: %q", findings[0].Message)
	}
	if len(notes) != 0 {
		t.Fatalf("notes = %+v", notes)
	}
}

// What answered is reported exactly: the digest over the installed bytes, both
// pinned runtime versions, and every analyzer version.
func TestJavaScriptBundleStatusReportsExactProvenance(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = fakeInstalledBundle(t)
	findings, notes := JavaScriptBundleStatus(repo, []string{"app/component.tsx"})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	expected := "sealed JavaScript bundle a1b2c3: node 24.18.0, pnpm 11.13.0, eslint 9.39.5, prettier 3.9.5"
	if len(notes) != 1 || notes[0] != expected {
		t.Fatalf("notes = %+v", notes)
	}
}

// A target without JavaScript is never formatted by the sealed bundle, so a
// Go-only or content-only repository never has to install it.
func TestJavaScriptFormatIgnoresTargetsWithoutJavaScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeQualityFile(t, repo.Root, "docs/plan.md", "# plan\n")
	writeQualityFile(t, repo.Root, "config.json", "{}\n")
	findings := JavaScriptFormatFindings(t.Context(), repo, []string{"docs/plan.md", "config.json"})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

// Everything the sealed formatter reports becomes a finding: a file it would
// rewrite fails the check, and a file it could not decide fails closed instead
// of passing as formatted.
func TestJavaScriptFormatReportsChangedAndUndecidedFiles(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	result := `{"changed":["app/main.ts"],"unsupported":[{"path":"app/odd.json","reason":"no parser"}]}`
	repo.PolicyRoot, _ = fakeFileBundle(t, result)
	writeQualityFile(t, repo.Root, "app/main.ts", "export {};\n")
	writeQualityFile(t, repo.Root, "app/odd.json", "{}\n")
	findings := JavaScriptFormatFindings(t.Context(), repo, []string{"app/main.ts", "app/odd.json"})
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Check != "quality.formatCoverage" || findings[0].Path != "app/odd.json" ||
		!strings.Contains(findings[0].Message, "no parser") {
		t.Fatalf("unsupported finding = %+v", findings[0])
	}
	if findings[1].Check != "quality.format" || findings[1].Path != "app/main.ts" {
		t.Fatalf("changed finding = %+v", findings[1])
	}
}

// Writing reports what it could not decide, but a file it rewrote is not a
// failure: the caller asked for exactly that.
func TestJavaScriptFormatWriteReportsOnlyUndecidedFiles(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	result := `{"changed":["app/main.ts"],"unsupported":[]}`
	policyRoot, observed := fakeFileBundle(t, result)
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "app/main.ts", "export {};\n")
	if findings := JavaScriptFormatWrite(t.Context(), repo, []string{"app/main.ts"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request), `"operation":"format-write"`) {
		t.Fatalf("write did not ask for the write operation: %s", request)
	}
}

// Code Polishy owns the file set. A generated file and a lockfile are never
// selected, and the governed set is not limited to JavaScript source.
func TestJavaScriptFormatSelectsOnlyTheGovernedFiles(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"app/schema.generated.ts"}
	policyRoot, observed := fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	repo.PolicyRoot = policyRoot
	files := []string{
		"app/main.ts", "app/style.css", "docs/guide.md", "package.json",
		"pnpm-lock.yaml", "app/schema.generated.ts", "Makefile", "main.go",
	}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "\n")
	}
	if findings := JavaScriptFormatFindings(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	want := `"paths":["app/main.ts","app/style.css","docs/guide.md","package.json"]`
	if !strings.Contains(string(request), want) {
		t.Fatalf("selection = %s, want %s", request, want)
	}
}

// A target that ships formatter configuration is told the configuration is not
// read, rather than having it silently ignored.
func TestJavaScriptFormatRejectsTargetFormatterConfiguration(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "app/main.ts", "export {};\n")
	writeQualityFile(t, repo.Root, ".prettierrc.json", "{}\n")
	findings := JavaScriptFormatFindings(t.Context(), repo, []string{"app/main.ts", ".prettierrc.json"})
	if len(findings) != 1 || findings[0].Check != "quality.formatConfiguration" || findings[0].Path != ".prettierrc.json" {
		t.Fatalf("findings = %+v", findings)
	}
}

// A target without JavaScript is never linted by the sealed bundle either.
func TestJavaScriptLintIgnoresTargetsWithoutJavaScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeQualityFile(t, repo.Root, "main.go", "package main\n")
	if findings := JavaScriptLintFindings(t.Context(), repo, []string{"main.go"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

// Everything the sealed linter reports becomes a finding: a budget violation is
// a complexity finding, another rule is a lint finding, an ignored inline
// directive is reported rather than silently honored, and a file the linter
// could not decide fails closed.
func TestJavaScriptLintReportsViolationsDirectivesAndUndecidedFiles(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	result := `{"findings":[` +
		`{"path":"app/main.ts","line":4,"column":2,"rule":"max-depth","message":"too deep"},` +
		`{"path":"app/view.tsx","line":9,"column":1,"rule":"jsx-a11y/alt-text","message":"missing alt"}],` +
		`"directives":[{"path":"app/main.ts","line":1}],` +
		`"unsupported":[{"path":"app/broken.ts","reason":"line 2: Unexpected token"}]}`
	repo.PolicyRoot, _ = fakeFileBundle(t, result)
	for _, path := range []string{"app/main.ts", "app/view.tsx", "app/broken.ts"} {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	findings := JavaScriptLintFindings(t.Context(), repo, []string{"app/main.ts", "app/view.tsx", "app/broken.ts"})
	if len(findings) != 4 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Check != "quality.lintCoverage" || findings[0].Path != "app/broken.ts" ||
		!strings.Contains(findings[0].Message, "Unexpected token") {
		t.Fatalf("coverage finding = %+v", findings[0])
	}
	if findings[1].Check != "quality.lintDirective" || findings[1].Path != "app/main.ts" ||
		!strings.Contains(findings[1].Message, "line 1") {
		t.Fatalf("directive finding = %+v", findings[1])
	}
	if findings[2].Check != "quality.complexity" || findings[2].Subject != "max-depth" ||
		!strings.Contains(findings[2].Message, "line 4, column 2") {
		t.Fatalf("budget finding = %+v", findings[2])
	}
	if findings[3].Check != "quality.lint" || findings[3].Subject != "jsx-a11y/alt-text" {
		t.Fatalf("rule finding = %+v", findings[3])
	}
}

// Production and test source are linted under their own budgets, in ESLint's
// allowed-maximum form, and neither selection can be handed the other's.
func TestJavaScriptLintSeparatesProductionAndTestBudgets(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Quality.Complexity.TypeScript = 6
	repo.Config.Quality.Complexity.TypeScriptTest = 10
	repo.Config.Quality.MaxDepth, repo.Config.Quality.MaxTestDepth = 3, 8
	repo.Config.Quality.MaxParams, repo.Config.Quality.MaxTestParams = 4, 8
	policyRoot, observed := fakeFileBundle(t, emptyLintResult)
	repo.PolicyRoot = policyRoot
	files := []string{"src/index.ts", "src/index.test.ts"}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	if findings := JavaScriptLintFindings(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	requests := observedRequestLines(t, observed)
	if len(requests) != 2 {
		t.Fatalf("requests = %v", requests)
	}
	production := `"paths":["src/index.ts"],"limits":{"complexity":5,"depth":3,"parameters":4}`
	tests := `"paths":["src/index.test.ts"],"limits":{"complexity":9,"depth":8,"parameters":8}`
	if !strings.Contains(requests[0], production) || !strings.Contains(requests[1], tests) {
		t.Fatalf("requests = %v", requests)
	}
}

// Framework rules follow the resolved policy modules, and only over the source
// the declaring root owns: an unrelated package is linted without them.
func TestJavaScriptLintActivatesFrameworkRulesOnlyInsideTheirRoot(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.JavaScriptLintScopes = []policy.JavaScriptLintScope{
		{Root: "web", ReactHooks: true, JSXAccessibility: true},
	}
	policyRoot, observed := fakeFileBundle(t, emptyLintResult)
	repo.PolicyRoot = policyRoot
	files := []string{"server/main.ts", "web/App.tsx"}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	if findings := JavaScriptLintFindings(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	requests := observedRequestLines(t, observed)
	if len(requests) != 2 {
		t.Fatalf("requests = %v", requests)
	}
	inactive := `"paths":["server/main.ts"]`
	active := `"paths":["web/App.tsx"]`
	if !strings.Contains(requests[0], inactive) ||
		!strings.Contains(requests[0], `"activation":{"reactHooks":false,"jsxAccessibility":false}`) {
		t.Fatalf("unrelated source was not linted plainly: %s", requests[0])
	}
	if !strings.Contains(requests[1], active) ||
		!strings.Contains(requests[1], `"activation":{"reactHooks":true,"jsxAccessibility":true}`) {
		t.Fatalf("React source did not activate the framework rules: %s", requests[1])
	}
}

// Code Polishy owns the linted file set: generated source is never selected, and
// neither is a file the sealed linter does not parse.
func TestJavaScriptLintSelectsOnlyGovernedSource(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"app/schema.generated.ts"}
	policyRoot, observed := fakeFileBundle(t, emptyLintResult)
	repo.PolicyRoot = policyRoot
	files := []string{
		"app/main.ts", "app/component.tsx", "app/legacy.cjs", "app/schema.generated.ts",
		"app/style.css", "docs/guide.md", "package.json", "main.go",
	}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	if findings := JavaScriptLintFindings(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	requests := observedRequestLines(t, observed)
	want := `"paths":["app/component.tsx","app/legacy.cjs","app/main.ts"]`
	if len(requests) != 1 || !strings.Contains(requests[0], want) {
		t.Fatalf("requests = %v, want %s", requests, want)
	}
}

// A target that ships lint configuration is told it is not read, rather than
// having it silently ignored.
func TestJavaScriptLintRejectsTargetLintConfiguration(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, emptyLintResult)
	writeQualityFile(t, repo.Root, "app/main.ts", "export {};\n")
	writeQualityFile(t, repo.Root, "eslint.config.mjs", "export default [];\n")
	findings := JavaScriptLintFindings(t.Context(), repo, []string{"app/main.ts", "eslint.config.mjs"})
	if len(findings) != 1 || findings[0].Check != "quality.lintConfiguration" || findings[0].Path != "eslint.config.mjs" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestJavaScriptTypeCheckIgnoresTargetsWithoutJavaScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeQualityFile(t, repo.Root, "main.go", "package main\n")
	if findings := JavaScriptTypeCheckFindings(t.Context(), repo, []string{"main.go"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

// Every diagnostic the compiler produced becomes a finding attributed to its
// exact TypeScript code, and a project the bundle refused to analyze is missing
// coverage rather than a clean project.
func TestJavaScriptTypeCheckReportsDiagnosticsAndRefusedProjects(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	result := `{"diagnostics":[` +
		`{"path":"app/main.ts","line":4,"column":2,"code":2322,"message":"Type 'string' is not assignable to type 'number'."}],` +
		`"covered":["app/main.ts"],` +
		`"unsupported":[{"path":"tsconfig.json","reason":"the project extends configuration outside the repository"}]}`
	repo.PolicyRoot, _ = fakeFileBundle(t, result)
	writeQualityFile(t, repo.Root, "tsconfig.json", "{}\n")
	writeQualityFile(t, repo.Root, "app/main.ts", "export {};\n")
	findings := JavaScriptTypeCheckFindings(t.Context(), repo, []string{"app/main.ts"})
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Check != "quality.typecheckCoverage" || findings[0].Path != "tsconfig.json" ||
		!strings.Contains(findings[0].Message, "outside the repository") {
		t.Fatalf("coverage finding = %+v", findings[0])
	}
	if findings[1].Check != "quality.typecheck" || findings[1].Subject != "TS2322" ||
		!strings.Contains(findings[1].Message, "line 4, column 2") {
		t.Fatalf("diagnostic finding = %+v", findings[1])
	}
}

// A governed file the project's program never contained was never checked, so
// it is reported rather than counted as passing.
func TestJavaScriptTypeCheckFailsClosedOnUncoveredSource(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, `{"diagnostics":[],"covered":["app/included.ts"],"unsupported":[]}`)
	writeQualityFile(t, repo.Root, "tsconfig.json", "{}\n")
	for _, path := range []string{"app/included.ts", "app/excluded.ts"} {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	findings := JavaScriptTypeCheckFindings(t.Context(), repo, []string{"app/included.ts", "app/excluded.ts"})
	if len(findings) != 1 || findings[0].Check != "quality.typecheckCoverage" || findings[0].Path != "app/excluded.ts" {
		t.Fatalf("findings = %+v", findings)
	}
	if !strings.Contains(findings[0].Message, "tsconfig.json does not check this file") {
		t.Fatalf("coverage finding = %+v", findings[0])
	}
}

// Source no project governs at all is reported without launching the bundle for
// it, because there is no project to check it under.
func TestJavaScriptTypeCheckReportsSourceNoProjectGoverns(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	policyRoot, observed := fakeFileBundle(t, emptyTypeCheckResult)
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "app/main.ts", "export {};\n")
	findings := JavaScriptTypeCheckFindings(t.Context(), repo, []string{"app/main.ts"})
	if len(findings) != 1 || findings[0].Check != "quality.typecheckCoverage" ||
		!strings.Contains(findings[0].Message, "no contained TypeScript project") {
		t.Fatalf("findings = %+v", findings)
	}
	if _, err := os.Stat(observed); !os.IsNotExist(err) {
		t.Fatalf("the bundle was launched for ungoverned source: %v", err)
	}
}

// Each file is checked under the nearest project above it, so a package in a
// monorepo is checked under its own settings rather than a sibling's or the
// root's. A directory declaring several projects and no tsconfig.json declares
// none of them, and the search continues upward instead of guessing.
func TestJavaScriptTypeCheckSelectsTheNearestProject(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	covered := typeCheckResult("packages/api/src/a.ts", "packages/web/src/b.ts", "shared/c.ts")
	policyRoot, observed := fakeFileBundle(t, covered)
	repo.PolicyRoot = policyRoot
	for _, path := range []string{"tsconfig.json", "packages/api/tsconfig.json"} {
		writeQualityFile(t, repo.Root, path, "{}\n")
	}
	for _, path := range []string{"packages/web/tsconfig.app.json", "packages/web/tsconfig.node.json"} {
		writeQualityFile(t, repo.Root, path, "{}\n")
	}
	files := []string{"packages/api/src/a.ts", "packages/web/src/b.ts", "shared/c.ts"}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	if findings := JavaScriptTypeCheckFindings(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	requests := observedRequestLines(t, observed)
	if len(requests) != 2 {
		t.Fatalf("requests = %v", requests)
	}
	api := `"paths":["packages/api/src/a.ts"],"project":"packages/api/tsconfig.json"`
	root := `"paths":["packages/web/src/b.ts","shared/c.ts"],"project":"tsconfig.json"`
	if !strings.Contains(requests[0], api) || !strings.Contains(requests[1], root) {
		t.Fatalf("requests = %v", requests)
	}
}

// Only TypeScript source requires coverage. A generated file is never selected,
// and JavaScript is checked only when the target's own project asks for it.
func TestJavaScriptTypeCheckSelectsOnlyGovernedTypeScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"app/generated/api.ts"}
	policyRoot, observed := fakeFileBundle(t, typeCheckResult("app/main.ts"))
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "tsconfig.json", "{}\n")
	files := []string{"app/main.ts", "app/legacy.js", "app/generated/api.ts", "docs/guide.md"}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	if findings := JavaScriptTypeCheckFindings(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	requests := observedRequestLines(t, observed)
	if len(requests) != 1 || !strings.Contains(requests[0], `"paths":["app/main.ts"]`) {
		t.Fatalf("requests = %v", requests)
	}
}

func TestJavaScriptDeadCodeIgnoresTargetsWithoutJavaScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeQualityFile(t, repo.Root, "main.go", "package main\n")
	if findings := JavaScriptDeadCodeFindings(t.Context(), repo, []string{"main.go"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

// Reachability is a whole-tree fact, so the analysis is never narrowed to the
// selection. It is skipped outright when the selection contains nothing it
// reads, which is the only way a change avoids paying for it.
func TestJavaScriptDeadCodeRunsOnlyForSelectionsItDependsOn(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	policyRoot, observed := fakeFileBundle(t, deadCodeResult("src/index.ts"))
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "package.json", "{}\n")
	writeQualityFile(t, repo.Root, "src/index.ts", "export {};\n")
	writeQualityFile(t, repo.Root, "docs/guide.md", "# guide\n")
	if findings := JavaScriptDeadCodeFindings(t.Context(), repo, []string{"docs/guide.md"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if _, err := os.Stat(observed); !os.IsNotExist(err) {
		t.Fatalf("the bundle was launched for an unrelated selection: %v", err)
	}
	if findings := JavaScriptDeadCodeFindings(t.Context(), repo, []string{"package.json"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if requests := observedRequestLines(t, observed); len(requests) != 1 {
		t.Fatalf("requests = %v", requests)
	}
}

// Everything the analysis depends on is decided here: the nearest package owns
// a file, the outermost package decides which tree the analysis covers, and the
// entry points are the ones Code Polishy and the target declare.
func TestJavaScriptDeadCodeDeclaresPackagesAndEntryPoints(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.EntryPoints = []string{"packages/web/src/worker.ts"}
	policyRoot, observed := fakeFileBundle(t, deadCodeResult(
		"src/helper.test.ts", "src/helper.ts", "src/index.ts", "vite.config.ts",
		"packages/web/src/index.ts", "packages/web/src/lib.ts", "packages/web/src/worker.ts"))
	repo.PolicyRoot = policyRoot
	for _, path := range []string{"package.json", "packages/web/package.json"} {
		writeQualityFile(t, repo.Root, path, "{}\n")
	}
	files := []string{
		"src/index.ts", "src/helper.ts", "src/helper.test.ts", "vite.config.ts",
		"packages/web/src/lib.ts", "packages/web/src/worker.ts", "packages/web/src/index.ts",
	}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	if findings := JavaScriptDeadCodeFindings(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	requests := observedRequestLines(t, observed)
	if len(requests) != 1 {
		t.Fatalf("requests = %v", requests)
	}
	root := `{"root":".","entry":["src/helper.test.ts","src/index.ts","vite.config.ts"],` +
		`"project":["src/helper.test.ts","src/helper.ts","src/index.ts","vite.config.ts"]}`
	web := `{"root":"packages/web","entry":["packages/web/src/index.ts","packages/web/src/worker.ts"],` +
		`"project":["packages/web/src/index.ts","packages/web/src/lib.ts","packages/web/src/worker.ts"]}`
	if !strings.Contains(requests[0], `"directory":".","workspaces":[`+root+","+web+"]") {
		t.Fatalf("request = %v", requests[0])
	}
}

// Two package trees with no package above them are two analyses, because
// neither can resolve the other and pretending otherwise would report a
// sibling's used export as dead.
func TestJavaScriptDeadCodeAnalyzesIndependentTreesSeparately(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	policyRoot, observed := fakeFileBundle(t, deadCodeResult("apps/one/index.ts", "apps/two/index.ts"))
	repo.PolicyRoot = policyRoot
	for _, path := range []string{"apps/one/package.json", "apps/two/package.json"} {
		writeQualityFile(t, repo.Root, path, "{}\n")
	}
	files := []string{"apps/one/index.ts", "apps/two/index.ts"}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	if findings := JavaScriptDeadCodeFindings(t.Context(), repo, files); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	requests := observedRequestLines(t, observed)
	if len(requests) != 2 || !strings.Contains(requests[0], `"directory":"apps/one"`) ||
		!strings.Contains(requests[1], `"directory":"apps/two"`) {
		t.Fatalf("requests = %v", requests)
	}
}

// Governed source the analyzer cannot reach fails closed instead of passing:
// source no package declares is never analyzed at all, and a file type the
// analyzer does not read is reported rather than assumed reachable.
func TestJavaScriptDeadCodeFailsClosedOnUnanalyzableSource(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	policyRoot, observed := fakeFileBundle(t, emptyDeadCodeResult)
	repo.PolicyRoot = policyRoot
	files := []string{"tools/script.mjs", "web/App.vue"}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	findings := JavaScriptDeadCodeFindings(t.Context(), repo, files)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Check != "quality.deadCodeCoverage" || findings[0].Path != "tools/script.mjs" ||
		!strings.Contains(findings[0].Message, "no package.json governs this file") {
		t.Fatalf("ungoverned finding = %+v", findings[0])
	}
	if findings[1].Path != "web/App.vue" || !strings.Contains(findings[1].Message, "does not analyze this file") {
		t.Fatalf("unanalyzable finding = %+v", findings[1])
	}
	if _, err := os.Stat(observed); !os.IsNotExist(err) {
		t.Fatalf("the bundle was launched with nothing to analyze: %v", err)
	}
}

// Everything the analyzer reported becomes a finding, and a selected file it
// answered about neither way is missing coverage rather than reachable.
func TestJavaScriptDeadCodeReportsUnusedFilesExportsAndCoverage(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	result := `{"unusedFiles":["src/orphan.ts"],` +
		`"unusedExports":[` +
		`{"path":"src/helper.ts","line":2,"column":14,"symbol":"unused","kind":"exports"},` +
		`{"path":"src/helper.ts","line":3,"column":13,"symbol":"Spare","kind":"types"},` +
		`{"path":"src/helper.ts","line":4,"column":1,"symbol":"one|two","kind":"duplicates"}],` +
		`"covered":["src/helper.ts","src/orphan.ts"],` +
		`"unsupported":[{"path":"src/broken.ts","reason":"the file is unreadable"}]}`
	repo.PolicyRoot, _ = fakeFileBundle(t, result)
	writeQualityFile(t, repo.Root, "package.json", "{}\n")
	files := []string{"src/helper.ts", "src/orphan.ts", "src/broken.ts", "src/silent.ts"}
	for _, path := range files {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	findings := JavaScriptDeadCodeFindings(t.Context(), repo, files)
	if len(findings) != 6 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Check != "quality.deadCodeCoverage" || findings[0].Path != "src/broken.ts" {
		t.Fatalf("refusal finding = %+v", findings[0])
	}
	if findings[1].Check != "quality.deadCodeCoverage" || findings[1].Path != "src/silent.ts" ||
		!strings.Contains(findings[1].Message, "did not analyze this file") {
		t.Fatalf("undecided finding = %+v", findings[1])
	}
	if findings[2].Check != "quality.deadCode" || findings[2].Path != "src/orphan.ts" ||
		!strings.Contains(findings[2].Message, "no entry point reaches this file") {
		t.Fatalf("unused file finding = %+v", findings[2])
	}
	messages := []string{
		"line 2, column 14: the export unused is exported but never used",
		"line 3, column 13: the exported type Spare is exported but never used",
		"line 4, column 1: the same symbol is exported more than once, as one|two",
	}
	for index, message := range messages {
		if findings[index+3].Check != "quality.deadCode" || findings[index+3].Message != message {
			t.Fatalf("unused export finding = %+v", findings[index+3])
		}
	}
}

// A target that ships dead-code configuration is told it decides nothing, for
// the same reason a target lint or formatter configuration is.
func TestJavaScriptDeadCodeReportsTargetConfiguration(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot, _ = fakeFileBundle(t, deadCodeResult("src/index.ts"))
	writeQualityFile(t, repo.Root, "package.json", "{}\n")
	writeQualityFile(t, repo.Root, "knip.json", "{}\n")
	writeQualityFile(t, repo.Root, "src/index.ts", "export {};\n")
	findings := JavaScriptDeadCodeFindings(t.Context(), repo, []string{"knip.json", "src/index.ts"})
	if len(findings) == 0 || findings[0].Check != "quality.deadCodeConfiguration" ||
		findings[0].Path != "knip.json" {
		t.Fatalf("findings = %+v", findings)
	}
}

const emptyDeadCodeResult = `{"unusedFiles":[],"unusedExports":[],"covered":[],"unsupported":[]}`

// deadCodeResult is a clean tree in which exactly these files were analyzed.
func deadCodeResult(covered ...string) string {
	quoted := make([]string, 0, len(covered))
	for _, path := range covered {
		quoted = append(quoted, `"`+path+`"`)
	}
	return `{"unusedFiles":[],"unusedExports":[],"covered":[` + strings.Join(quoted, ",") + `],"unsupported":[]}`
}

const emptyLintResult = `{"findings":[],"directives":[],"unsupported":[]}`

const emptyTypeCheckResult = `{"diagnostics":[],"covered":[],"unsupported":[]}`

// typeCheckResult is a clean project that covered exactly these files.
func typeCheckResult(covered ...string) string {
	quoted := make([]string, 0, len(covered))
	for _, path := range covered {
		quoted = append(quoted, `"`+path+`"`)
	}
	return `{"diagnostics":[],"covered":[` + strings.Join(quoted, ",") + `],"unsupported":[]}`
}

// fakeInstalledBundle stands in for one installed policy checkout: a runtime and
// a runner at the one policy-owned path, answering the closed protocol.
func fakeInstalledBundle(t *testing.T) string {
	t.Helper()
	response := `{"protocolVersion":2,"operation":"provenance","result":{"bundleDigest":"a1b2c3",` +
		`"node":"24.18.0","pnpm":"11.13.0","tools":{"prettier":"3.9.5","eslint":"9.39.5"}}}`
	return installBundleScript(t, "#!/bin/sh\nprintf '%s\\n' '"+response+"'\n")
}

// fakeFileBundle answers every file operation with one result and records each
// request it was handed on its own line, so a test can assert exactly what Go
// selected and how many exchanges it took.
func fakeFileBundle(t *testing.T, result string) (policyRoot, observedRequests string) {
	t.Helper()
	observed := filepath.Join(t.TempDir(), "requests.jsonl")
	current := observed + ".current"
	// The adapter refuses an answer to another operation, so the stand-in
	// answers with the operation it was actually handed.
	script := "#!/bin/sh\n/bin/cat >" + current + "\n" +
		`operation=$(/usr/bin/sed -n 's/.*"operation":"\([a-z-]*\)".*/\1/p' ` + current + ")\n" +
		"/bin/cat " + current + " >>" + observed + "\n" +
		"printf '\\n' >>" + observed + "\n" +
		`printf '{"protocolVersion":2,"operation":"%s","result":%s}\n' "${operation}" '` + result + "'\n"
	return installBundleScript(t, script), observed
}

func observedRequestLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func installBundleScript(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("the sealed JavaScript bundle does not support %s", runtime.GOOS)
	}
	architecture := runtime.GOARCH
	if architecture == "amd64" {
		architecture = "x64"
	}
	root := t.TempDir()
	installed := filepath.Join(root, ".tools", "javascript")
	writeBundleScript(t, filepath.Join(installed, runtime.GOOS+"-"+architecture, "node", "bin", "node"),
		"#!/bin/sh\nexec /bin/sh \"$1\"\n")
	writeBundleScript(t, filepath.Join(installed, "bundle", "runner.mjs"), script)
	return root
}

func writeBundleScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
