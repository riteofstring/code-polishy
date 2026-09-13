package pack

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func providerUnitRepository(t *testing.T, capability string) (repository.Repository, policy.Command) {
	t.Helper()
	root := t.TempDir()
	command := policy.Command{Name: "pack.javascript." + capability, Provides: []string{capability}, RunOn: []string{"check", "format"}, Paths: []string{"**/*.js", "**/*.ts", "**/*.tsx"}, Adapter: &policy.PackAdapter{PackName: "javascript", Capability: capability}}
	config := policy.Config{Checks: []policy.Command{command}, Scope: policy.Scope{Generated: []string{"python_pkg/generated/**"}, GeneratedJavaScript: []policy.GeneratedJavaScript{{Paths: []string{"python_pkg/generated/**"}, SourcePackage: "frontend/package.json"}}}, JavaScriptLintScopes: []policy.JavaScriptLintScope{{Root: "frontend", ReactHooks: true, JSXAccessibility: true}}}
	for file, content := range map[string]string{"frontend/package.json": "{}", "frontend/tsconfig.app.json": "{}", "frontend/index.ts": "export const value = 1;\n", "frontend/dependent.ts": "export const broken: number = 'wrong';\n", "python_pkg/generated/bundle.js": "export const value = 1;\n", "backend/app.py": "value = 1\n", "frontend/unclaimed.vue": "<unsupported>"} {
		writeTestFile(t, root, file, content, 0o644)
	}
	repo, err := repository.Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	return repo, command
}

func TestResolvedProviderContextRetainsGeneratedOwnershipAndEffectivePolicy(t *testing.T) {
	repo, command := providerUnitRepository(t, "typecheck")
	request := requestFor(repo, repository.Selection{Files: []string{"python_pkg/generated/bundle.js"}}, command, "check")
	if err := prepareInputs(repo, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Units) != 1 {
		t.Fatalf("units = %+v", request.Units)
	}
	unit := request.Units[0]
	if unit.Manifest != "frontend/package.json" || unit.Configuration != "frontend/tsconfig.app.json" || unit.WorkspaceRoot != "frontend" {
		t.Fatalf("incorrect effective context: %+v", unit)
	}
	if !slices.Contains(request.DiagnosticFiles, "frontend/dependent.ts") || slices.Contains(request.DiagnosticFiles, "frontend/unclaimed.vue") || slices.Contains(request.DiagnosticFiles, "backend/app.py") {
		t.Fatalf("incorrect diagnostic authority: %v", request.DiagnosticFiles)
	}
	index := slices.IndexFunc(request.Policy.Files, func(file SourceInput) bool { return file.Path == "python_pkg/generated/bundle.js" })
	if index < 0 {
		t.Fatal("missing original source")
	}
	source := request.Policy.Files[index]
	if source.SourcePackage != "frontend/package.json" || !source.Generated || source.Lint.ReactHooks || !source.Lint.JSXAccessibility {
		t.Fatalf("effective generated policy = %+v", source)
	}
	if len(request.WriteFiles) != 0 {
		t.Fatal("analysis received write authority")
	}
	finding := ResponseFinding{Capability: "typecheck", Path: "frontend/dependent.ts", Rule: "type-2322", Subject: "assignment", Message: "wrong type", Line: 1, Column: 1}
	if err := validateResponseFinding(finding, request); err != nil {
		t.Fatal(err)
	}
	finding.Path = "backend/app.py"
	if err := validateResponseFinding(finding, request); err == nil {
		t.Fatal("unowned diagnostic was accepted")
	}
}

func TestProviderFormattingDoesNotHashUnrelatedLargeAssets(t *testing.T) {
	repo, command := providerUnitRepository(t, "format")
	large, err := os.Create(filepath.Join(repo.Root, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(17 << 20); err != nil {
		t.Fatal(err)
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}
	request := requestFor(repo, repository.Selection{Files: []string{"frontend/index.ts", "python_pkg/generated/bundle.js"}}, command, "format")
	if err := prepareInputs(repo, &request); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(request.WriteFiles, []string{"frontend/index.ts"}) {
		t.Fatalf("write scope = %v", request.WriteFiles)
	}
	for _, input := range request.Context {
		if input.Path == "large.bin" || input.Path == "frontend/dependent.ts" || input.Path == "backend/app.py" {
			t.Fatalf("unrelated context was hashed: %s", input.Path)
		}
	}
	response := Response{Status: "pass", Edits: []Edit{{Path: "python_pkg/generated/bundle.js", Content: "changed"}}}
	if err := applyEdits(repo, request, response); err == nil {
		t.Fatal("generated edit accepted")
	}
}

func TestProviderDeadCodeSchedulesMetadataAndUnchangedPackageMembers(t *testing.T) {
	repo, command := providerUnitRepository(t, "dead-code")
	for _, file := range []string{"frontend/index.ts", "frontend/package.json", "frontend/tsconfig.app.json", policy.ConfigFilename} {
		selected := SelectedFiles(repo, repository.Selection{Files: []string{file}}, command, "check")
		if !slices.Contains(selected, "frontend/dependent.ts") || !slices.Contains(selected, "python_pkg/generated/bundle.js") {
			t.Fatalf("%s omitted package analysis: %v", file, selected)
		}
	}
	if files := SelectedFiles(repo, repository.Selection{Files: []string{"backend/app.py"}}, command, "check"); len(files) != 0 {
		t.Fatalf("Python scheduled JavaScript: %v", files)
	}
}

func TestFormatRejectsInvalidOriginalUTF8BeforeApplyingAnyEdits(t *testing.T) {
	repo, command := providerUnitRepository(t, "format")
	writeTestFile(t, repo.Root, "frontend/invalid.js", string([]byte{0xff}), 0o644)
	request := requestFor(repo, repository.Selection{Files: []string{"frontend/index.ts", "frontend/invalid.js"}}, command, "format")
	if err := prepareInputs(repo, &request); err != nil {
		t.Fatal(err)
	}
	response := Response{Status: "pass", Edits: []Edit{{Path: "frontend/index.ts", Content: "changed"}, {Path: "frontend/invalid.js", Content: "replacement"}}}
	if err := applyEdits(repo, request, response); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid source edit = %v", err)
	}
	data, err := repo.Read("frontend/index.ts")
	if err != nil || string(data) != "export const value = 1;\n" {
		t.Fatal("earlier edit was applied before validation finished")
	}
}

func TestTruncatedCommentsRetainPolicyFacts(t *testing.T) {
	request := Request{Capability: "lint", Files: []string{"source.js"}, DiagnosticFiles: []string{"source.js"}, Policy: PolicyInput{Quality: policy.EffectiveQuality(policy.Quality{})}}
	comments := []CommentFact{{Path: "source.js", Line: 1, Column: 1, Kind: "Block", Raw: "/* " + strings.Repeat("a", 65533), Complete: false}}
	response := Response{ProtocolVersion: ProtocolVersion, Status: "pass", Evidence: []string{"parsed"}, Coverage: &Coverage{Analyzed: request.Files, Unsupported: []Unsupported{}}, Facts: &SourceFacts{Comments: &comments}}
	if err := validateResponse(response, request); err != nil {
		t.Fatal(err)
	}
	comments[0].Raw += "a"
	if err := validateResponse(response, request); err == nil {
		t.Fatal("oversized comment accepted")
	}
}

func TestArchitectureRejectsUnrelatedUnitFindingsAndMissingClosureMembers(t *testing.T) {
	request := Request{Capability: "architecture", Files: []string{"app/main.ts"}, DiagnosticFiles: []string{"app/main.ts", "shared/value.ts", "unrelated/broken.ts"}, Units: []AnalysisUnit{{Members: []string{"app/main.ts"}}, {Members: []string{"shared/value.ts"}}, {Members: []string{"unrelated/broken.ts"}}}}
	imports := []ImportFact{{Path: "app/main.ts", Resolved: "shared/value.ts"}}
	response := Response{Coverage: &Coverage{Analyzed: []string{"app/main.ts", "shared/value.ts"}}, Facts: &SourceFacts{Imports: &imports}}
	if err := validateDiagnosticCoverage(request, response); err != nil {
		t.Fatal(err)
	}
	response.Coverage.Analyzed = append(response.Coverage.Analyzed, "unrelated/broken.ts")
	if err := validateDiagnosticCoverage(request, response); err == nil {
		t.Fatal("unrelated coverage accepted")
	}
	response.Coverage.Analyzed = []string{"app/main.ts"}
	if err := validateDiagnosticCoverage(request, response); err == nil {
		t.Fatal("missing dependent project accepted")
	}
}
