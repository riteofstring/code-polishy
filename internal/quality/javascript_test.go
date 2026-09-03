package quality

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestJavaScriptBundleStatusIgnoresTargetsWithoutJavaScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	findings, notes := JavaScriptBundleStatus(repo, []string{"internal/sample/file.go", "config.json"})
	if len(findings) != 0 || len(notes) != 0 {
		t.Fatalf("findings = %+v, notes = %+v", findings, notes)
	}
}

func TestJavaScriptBundleStatusRequiresTheSealedBundleForMarkdown(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeQualityFile(t, repo.Root, "docs/plan.md", "# Plan\n")
	findings, notes := JavaScriptBundleStatus(repo, []string{"docs/plan.md"})
	if len(findings) != 1 || findings[0].Check != "policy.tool" || len(notes) != 0 {
		t.Fatalf("findings = %+v, notes = %+v", findings, notes)
	}
}

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

func TestJavaScriptBundleStatusReportsCompleteProvenance(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = fakeInstalledBundle(t)
	findings, notes := JavaScriptBundleStatus(repo, []string{"app/component.tsx"})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if len(notes) != 1 {
		t.Fatalf("notes = %+v", notes)
	}
	for _, fact := range []string{"sealed JavaScript bundle", "a1b2c3", "node 24.18.0", "pnpm 11.13.0", "eslint 9.39.5", "prettier 3.9.5"} {
		if !strings.Contains(notes[0], fact) {
			t.Fatalf("provenance note omitted %q: %q", fact, notes[0])
		}
	}
}

func TestJavaScriptFormatOwnsMarkdownWithoutJavaScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	policyRoot, observed := fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "docs/plan.md", "# plan\n")
	writeQualityFile(t, repo.Root, "config.json", "{}\n")
	findings := JavaScriptFormatFindings(t.Context(), repo, []string{"docs/plan.md", "config.json"})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request), `"paths":["docs/plan.md"]`) {
		t.Fatalf("formatter selection = %s", request)
	}
}

func TestJavaScriptFormatOwnsMarkdownExtensionCaseInsensitively(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	policyRoot, observed := fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "docs/guide.MARKDOWN", "# Guide\n")
	if findings := JavaScriptFormatFindings(t.Context(), repo, []string{"docs/guide.MARKDOWN"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request), `"paths":["docs/guide.MARKDOWN"]`) {
		t.Fatalf("formatter selection = %s", request)
	}
}

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

func TestJavaScriptFormatChecksGeneratedExecutableSource(t *testing.T) {
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
	want := `"paths":["app/main.ts","app/schema.generated.ts","app/style.css","docs/guide.md","package.json"]`
	if !strings.Contains(string(request), want) {
		t.Fatalf("selection = %s, want %s", request, want)
	}
}

func TestJavaScriptFormatWritePreservesGeneratedAndDeclaredData(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"app/schema.generated.ts"}
	repo.Config.Scope.Data = []string{"data/**/*.json"}
	policyRoot, observed := fakeFileBundle(t, `{"changed":[],"unsupported":[]}`)
	repo.PolicyRoot = policyRoot
	writeQualityFile(t, repo.Root, "app/main.ts", "export {}\n")
	writeQualityFile(t, repo.Root, "app/schema.generated.ts", "export {}\n")
	data := "{  \"identity\": \"preserve\"  }\n"
	writeQualityFile(t, repo.Root, "data/identity.json", data)

	if findings := JavaScriptFormatWrite(t.Context(), repo, []string{"app/main.ts", "app/schema.generated.ts", "data/identity.json"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	request, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request), `"paths":["app/main.ts"]`) {
		t.Fatalf("write selection = %s", request)
	}
	if strings.Contains(string(request), "schema.generated.ts") || strings.Contains(string(request), "identity.json") {
		t.Fatalf("write selected a protected path: %s", request)
	}
	preserved, err := os.ReadFile(filepath.Join(repo.Root, "data", "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(preserved) != data {
		t.Fatalf("data bytes changed: %q", preserved)
	}
}

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

func TestJavaScriptLintIgnoresTargetsWithoutJavaScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeQualityFile(t, repo.Root, "main.go", "package main\n")
	if findings := JavaScriptLintFindings(t.Context(), repo, []string{"main.go"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestJavaScriptLintReportsSourceCommentViolationsAndUndecidedFiles(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	result := `{"findings":[` +
		`{"path":"app/main.ts","line":4,"column":2,"rule":"max-depth","message":"too deep"},` +
		`{"path":"app/view.tsx","line":9,"column":1,"rule":"jsx-a11y/alt-text","message":"missing alt"}],` +
		`"comments":[{"path":"app/main.ts","kind":"Line","raw":"// prose","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true}],` +
		`"unsupported":[{"path":"app/broken.ts","reason":"line 2: Unexpected token"}]}`
	repo.PolicyRoot, _ = fakeFileBundle(t, result)
	for _, path := range []string{"app/main.ts", "app/view.tsx", "app/broken.ts"} {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	findings := JavaScriptLintFindings(t.Context(), repo, []string{"app/main.ts", "app/view.tsx", "app/broken.ts"})
	if len(findings) != 4 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Check != "policy.sourceCommentCoverage" || findings[0].Path != "app/broken.ts" ||
		!strings.Contains(findings[0].Message, "Unexpected token") {
		t.Fatalf("coverage finding = %+v", findings[0])
	}
	if findings[1].Check != "policy.sourceComment" || findings[1].Path != "app/main.ts" || findings[1].Subject != "1:1" ||
		!strings.Contains(findings[1].Message, "prose comments") {
		t.Fatalf("source comment finding = %+v", findings[1])
	}
	if findings[2].Check != "quality.complexity" || findings[2].Subject != "max-depth" ||
		!strings.Contains(findings[2].Message, "line 4, column 2") {
		t.Fatalf("budget finding = %+v", findings[2])
	}
	if findings[3].Check != "quality.lint" || findings[3].Subject != "jsx-a11y/alt-text" {
		t.Fatalf("rule finding = %+v", findings[3])
	}
}

func TestJavaScriptLintAllowsCommentsWithoutWeakeningLint(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	allowComments := true
	repo.Config.Quality.AllowComments = &allowComments
	result := `{"findings":[{"path":"app/main.ts","line":4,"column":2,"rule":"max-depth","message":"too deep"}],` +
		`"comments":[{"path":"app/main.ts","kind":"Line","raw":"// prose","complete":true,"line":1,"column":1,"beforeCode":true,"preamble":true,"byteZero":true}],` +
		`"unsupported":[{"path":"app/broken.ts","reason":"line 2: Unexpected token"}]}`
	repo.PolicyRoot, _ = fakeFileBundle(t, result)
	for _, path := range []string{"app/main.ts", "app/broken.ts"} {
		writeQualityFile(t, repo.Root, path, "export {};\n")
	}
	findings := JavaScriptLintFindings(t.Context(), repo, []string{"app/main.ts", "app/broken.ts"})
	if len(findings) != 1 || findings[0].Check != "quality.complexity" || findings[0].Subject != "max-depth" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestJavaScriptLintClassifiesParserCommentFacts(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	allowed := []javascript.LintComment{
		{Path: "src/cli.ts", Kind: "Shebang", Raw: "#!/usr/bin/env node", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true, ByteZero: true},
		{Path: "src/declarations.d.ts", Kind: "Line", Raw: `/// <reference path="./types.d.ts" />`, Complete: true, Line: 1, Column: 1, BeforeCode: true},
		{Path: "src/types.ts", Kind: "Line", Raw: `/// <reference types="vitest" />`, Complete: true, Line: 1, Column: 1, BeforeCode: true},
		{Path: "src/libraries.mts", Kind: "Line", Raw: `/// <reference lib="es2022" />`, Complete: true, Line: 1, Column: 1, BeforeCode: true},
		{Path: "src/defaults.cts", Kind: "Line", Raw: `/// <reference no-default-lib="true" />`, Complete: true, Line: 1, Column: 1, BeforeCode: true},
		{Path: "src/environment.test.ts", Kind: "Line", Raw: "// @vitest-environment jsdom", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true},
		{Path: "tests/environment.ts", Kind: "Block", Raw: "/* @vitest-environment jsdom */", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true},
	}
	if findings := javascriptLintResultFindings(repo, javascript.LintResult{Comments: allowed}); len(findings) != 0 {
		t.Fatalf("allowed comments = %+v", findings)
	}
	rejected := []javascript.LintComment{
		{Path: "src/cli.ts", Kind: "Shebang", Raw: "#!", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true, ByteZero: true},
		{Path: "src/cli.ts", Kind: "Shebang", Raw: "#!/usr/bin/env node", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true},
		{Path: "src/types.ts", Kind: "Line", Raw: `/// <reference path='./types.d.ts' />`, Complete: true, Line: 1, Column: 1, BeforeCode: true},
		{Path: "src/types.ts", Kind: "Line", Raw: `/// <reference lib = "es2022" />`, Complete: true, Line: 1, Column: 1, BeforeCode: true},
		{Path: "src/types.ts", Kind: "Line", Raw: `/// <reference types="vitest" /> prose`, Complete: true, Line: 1, Column: 1, BeforeCode: true},
		{Path: "src/types.ts", Kind: "Line", Raw: `/// <reference types="vitest" /> eslint-disable`, Complete: true, Line: 1, Column: 1, BeforeCode: true},
		{Path: "src/types.ts", Kind: "Line", Raw: `/// <reference types="vitest" />`, Complete: true, Line: 2, Column: 1},
		{Path: "src/environment.ts", Kind: "Line", Raw: "// @vitest-environment jsdom", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true},
		{Path: "src/environment.test.ts", Kind: "Line", Raw: "//  @vitest-environment jsdom", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true},
		{Path: "src/environment.test.ts", Kind: "Block", Raw: "/* @vitest-environment jsdom  */", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true},
		{Path: "src/environment.test.ts", Kind: "Line", Raw: "// @vitest-environment jsdom", Complete: true, Line: 2, Column: 1, Preamble: true},
		{Path: "src/environment.test.ts", Kind: "Line", Raw: "// @vitest-environment jsdom", Complete: true, Line: 2, Column: 1, BeforeCode: true},
		{Path: "src/environment.test.ts", Kind: "Line", Raw: "// @vitest-environment jsdom", Line: 1, Column: 1, BeforeCode: true, Preamble: true},
		{Path: "src/prose.ts", Kind: "Line", Raw: "// prose", Complete: true, Line: 3, Column: 5},
	}
	findings := javascriptLintResultFindings(repo, javascript.LintResult{Comments: rejected})
	if len(findings) != len(rejected) {
		t.Fatalf("rejected comments = %+v", findings)
	}
	for index, finding := range findings {
		if finding.Check != "policy.sourceComment" || finding.Subject != fmt.Sprintf("%d:%d", rejected[index].Line, rejected[index].Column) {
			t.Fatalf("finding %d = %+v", index, finding)
		}
	}
}

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

func TestJavaScriptLintChecksGeneratedSourceWithoutComplexityBudget(t *testing.T) {
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
	generated := `"paths":["app/schema.generated.ts"]`
	ordinary := `"paths":["app/component.tsx","app/legacy.cjs","app/main.ts"]`
	if len(requests) != 2 || !strings.Contains(strings.Join(requests, "\n"), generated) ||
		!strings.Contains(strings.Join(requests, "\n"), ordinary) {
		t.Fatalf("requests = %v, want generated %s and ordinary %s", requests, generated, ordinary)
	}
}

func TestJavaScriptLintSkipsComplexityFindingsForGeneratedSource(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"app/schema.generated.ts"}
	result := `{"findings":[{"path":"app/schema.generated.ts","line":1,"column":1,"rule":"complexity","message":"too complex"},{"path":"app/schema.generated.ts","line":2,"column":1,"rule":"no-undef","message":"missing name"}],"comments":[],"unsupported":[]}`
	repo.PolicyRoot, _ = fakeFileBundle(t, result)
	writeQualityFile(t, repo.Root, "app/schema.generated.ts", "export {}\n")

	findings := JavaScriptLintFindings(t.Context(), repo, []string{"app/schema.generated.ts"})
	if len(findings) != 1 || findings[0].Check != "quality.lint" || findings[0].Subject != "no-undef" {
		t.Fatalf("findings = %+v", findings)
	}
}

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

func TestJavaScriptTypeCheckSelectsOnlyGovernedTypeScript(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Generated = []string{"app/generated/api.ts"}
	policyRoot, observed := fakeFileBundle(t, typeCheckResult("app/generated/api.ts", "app/main.ts"))
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
	if len(requests) != 1 || !strings.Contains(requests[0], `"paths":["app/generated/api.ts","app/main.ts"]`) {
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

func deadCodeResult(covered ...string) string {
	quoted := make([]string, 0, len(covered))
	for _, path := range covered {
		quoted = append(quoted, `"`+path+`"`)
	}
	return `{"unusedFiles":[],"unusedExports":[],"covered":[` + strings.Join(quoted, ",") + `],"unsupported":[]}`
}

const emptyLintResult = `{"findings":[],"comments":[],"unsupported":[]}`

const emptyTypeCheckResult = `{"diagnostics":[],"covered":[],"unsupported":[]}`

func typeCheckResult(covered ...string) string {
	quoted := make([]string, 0, len(covered))
	for _, path := range covered {
		quoted = append(quoted, `"`+path+`"`)
	}
	return `{"diagnostics":[],"covered":[` + strings.Join(quoted, ",") + `],"unsupported":[]}`
}

func fakeInstalledBundle(t *testing.T) string {
	t.Helper()
	response := `{"protocolVersion":3,"operation":"provenance","result":{"bundleDigest":"a1b2c3",` +
		`"node":"24.18.0","pnpm":"11.13.0","tools":{"prettier":"3.9.5","eslint":"9.39.5"}}}`
	return installBundleScript(t, "#!/bin/sh\nprintf '%s\\n' '"+response+"'\n")
}

func fakeFileBundle(t *testing.T, result string) (policyRoot, observedRequests string) {
	t.Helper()
	observed := filepath.Join(t.TempDir(), "requests.jsonl")
	current := observed + ".current"

	script := "#!/bin/sh\n/bin/cat >" + current + "\n" +
		`operation=$(/usr/bin/sed -n 's/.*"operation":"\([a-z-]*\)".*/\1/p' ` + current + ")\n" +
		"/bin/cat " + current + " >>" + observed + "\n" +
		"printf '\\n' >>" + observed + "\n" +
		`printf '{"protocolVersion":3,"operation":"%s","result":%s}\n' "${operation}" '` + result + "'\n"
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
