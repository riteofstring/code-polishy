package pack

import (
	"encoding/json"
	"fmt"
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
	command := policy.Command{
		Name: "pack.javascript.analyze." + capability, Provides: []string{capability}, RunOn: []string{"check", "format"}, Paths: []string{"**/*.js", "**/*.ts", "**/*.tsx"},
		Adapter: &policy.PackAdapter{
			PackName: "javascript", Capability: capability,
			Languages: []policy.LanguageRule{{Name: "typescript", Paths: []string{"**/*.js", "**/*.ts", "**/*.tsx"}}},
			Discovery: []policy.PackDiscovery{{Language: "typescript", Mode: "static", MetadataPatterns: []string{"**/package.json", "**/tsconfig*.json"}}},
		},
	}
	config := policy.Config{
		Checks: []policy.Command{command},
		Scope: policy.Scope{
			Generated:      []string{"python_pkg/generated/**"},
			SourceContexts: []policy.SourceContext{{Paths: []string{"python_pkg/generated/**"}, Context: "frontend/package.json"}},
		},
		JavaScriptLintScopes: []policy.JavaScriptLintScope{{Root: "frontend", ReactHooks: true, JSXAccessibility: true}},
	}
	for file, content := range map[string]string{
		".github/workflows/ci.yml": "name: fixture\n",
		"frontend/package.json":    "{}", "frontend/tsconfig.app.json": "{}", "frontend/index.ts": "export const value = 1;\n",
		"frontend/dependent.ts": "export const broken: number = 'wrong';\n", "python_pkg/generated/bundle.js": "export const value = 1;\n",
		"backend/app.py": "value = 1\n", "frontend/unclaimed.vue": "<unsupported>",
	} {
		writeTestFile(t, root, file, content, 0o644)
	}
	repo, err := repository.Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	return repo, command
}

func TestStaticDiscoveryKeepsEcosystemShapeOutOfCore(t *testing.T) {
	repo, command := providerUnitRepository(t, "typecheck")
	base := requestFor(repo, repository.Selection{Files: []string{"python_pkg/generated/bundle.js"}}, command, "check")
	paths, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := prepareDiscoveryRequest(repo, base, command, paths)
	if err != nil {
		t.Fatal(err)
	}
	generated := inventoryByPath(discovery.Inventory)["python_pkg/generated/bundle.js"]
	if generated.Context != "frontend/package.json" || !generated.Generated || generated.Owner != command.Name {
		t.Fatalf("generated inventory = %+v", generated)
	}
	if slices.ContainsFunc(discovery.Inventory, func(entry InventoryEntry) bool { return entry.Path == "backend/app.py" }) {
		t.Fatal("foreign language entered pack inventory")
	}
	control := inventoryByPath(discovery.Inventory)[".github/workflows/ci.yml"]
	if !control.Control || control.Source || control.Metadata || control.Dependency {
		t.Fatalf("generic control inventory = %+v", control)
	}
	if !slices.ContainsFunc(discovery.Context, func(input InputFile) bool { return input.Path == ".github/workflows/ci.yml" }) {
		t.Fatal("policy-sensitive control input was not bound into discovery")
	}
	scopeData := json.RawMessage(`{"configuration":"frontend/tsconfig.app.json","manifest":"frontend/package.json","workspace":"frontend"}`)
	response := Response{ProtocolVersion: ProtocolVersion, Status: "pass", Evidence: []string{"static discovery"}, Discovery: &DiscoveryResult{Scopes: []DiscoveredScope{{
		ID: "frontend", Language: "typescript", Root: "frontend", Members: []string{"frontend/dependent.ts", "frontend/index.ts", "python_pkg/generated/bundle.js"},
		EntryFiles: []string{"frontend/index.ts"}, Context: []string{"frontend/package.json", "frontend/tsconfig.app.json"}, Selected: []string{"python_pkg/generated/bundle.js"}, Data: scopeData,
	}}}}
	if err := validateResponse(response, discovery); err != nil {
		t.Fatal(err)
	}
	request, err := capabilityRequest(base, response)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareInputPaths(repo, &request, command, paths); err != nil {
		t.Fatal(err)
	}
	if len(request.Scopes) != 1 || !slices.Contains(request.DiagnosticFiles, "frontend/dependent.ts") || slices.Contains(request.DiagnosticFiles, "backend/app.py") {
		t.Fatalf("validated scope = %+v, diagnostics = %v", request.Scopes, request.DiagnosticFiles)
	}
	index := slices.IndexFunc(request.Policy.Files, func(file SourceInput) bool { return file.Path == "python_pkg/generated/bundle.js" })
	if index < 0 || request.Policy.Files[index].Context != "frontend/package.json" || !request.Policy.Files[index].Generated {
		t.Fatalf("source policy = %+v", request.Policy.Files)
	}
	if len(request.WriteFiles) != 0 {
		t.Fatal("analysis received write authority")
	}
}

func TestCapabilityScopesRetainTransportBounds(t *testing.T) {
	request := Request{Scopes: make([]AnalysisScope, maximumDiscoveryScopes+1)}
	if err := validateAnalysisScopes(request); err == nil || !strings.Contains(err.Error(), "scopes") {
		t.Fatalf("oversized scope inventory = %v", err)
	}

	data := json.RawMessage(`"` + strings.Repeat("a", maximumScopeBytes-2) + `"`)
	request.Scopes = make([]AnalysisScope, 33)
	request.Inventory = make([]InventoryEntry, 33)
	for index := range request.Scopes {
		file := fmt.Sprintf("source-%d.fixture", index)
		handle := fmt.Sprintf("scope-%d", index+1)
		request.Inventory[index] = InventoryEntry{Path: file, Language: "fixture", Owner: "provider", Source: true}
		request.Scopes[index] = AnalysisScope{Handle: handle, Language: "fixture", Root: ".", Members: []string{file}, Data: data}
	}
	request.Provider = "provider"
	if err := validateAnalysisScopes(request); err == nil || !strings.Contains(err.Error(), "aggregate scope data") {
		t.Fatalf("oversized scope data = %v", err)
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
	if err := prepareInputs(repo, &request, command); err != nil {
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

func TestStaticProviderSelectionIncludesMetadataAndExcludesForeignSource(t *testing.T) {
	repo, command := providerUnitRepository(t, "dead-code")
	for _, file := range []string{"frontend/index.ts", "frontend/package.json", "frontend/tsconfig.app.json", policy.ConfigFilename} {
		if !AdapterSelected(repo, repository.Selection{Files: []string{file}}, command, "check") {
			t.Fatalf("%s did not trigger static discovery", file)
		}
	}
	if AdapterSelected(repo, repository.Selection{Files: []string{"backend/app.py"}}, command, "check") {
		t.Fatal("Python scheduled JavaScript")
	}
}

func TestFormatRejectsInvalidOriginalUTF8BeforeApplyingAnyEdits(t *testing.T) {
	repo, command := providerUnitRepository(t, "format")
	writeTestFile(t, repo.Root, "frontend/invalid.js", string([]byte{0xff}), 0o644)
	request := requestFor(repo, repository.Selection{Files: []string{"frontend/index.ts", "frontend/invalid.js"}}, command, "format")
	if err := prepareInputs(repo, &request, command); err != nil {
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
	request := Request{Capability: "lint", Files: []string{"source.js"}, DiagnosticFiles: []string{"source.js"}, Scopes: []AnalysisScope{{Handle: "scope-1", Members: []string{"source.js"}}}, Policy: PolicyInput{Quality: policy.EffectiveQuality(policy.Quality{})}}
	comments := []CommentFact{{Path: "source.js", Line: 1, Column: 1, Kind: "Block", Raw: "/* " + strings.Repeat("a", 65533), Complete: false}}
	response := Response{ProtocolVersion: ProtocolVersion, Status: "pass", ScopeHandles: []string{"scope-1"}, Evidence: []string{"parsed"}, Coverage: &Coverage{Analyzed: request.Files, Unsupported: []Unsupported{}}, Facts: &SourceFacts{Comments: &comments}}
	if err := validateResponse(response, request); err != nil {
		t.Fatal(err)
	}
	comments[0].Raw += "a"
	if err := validateResponse(response, request); err == nil {
		t.Fatal("oversized comment accepted")
	}
}

func TestArchitectureRejectsUnrelatedScopeFindingsAndMissingClosureMembers(t *testing.T) {
	request := Request{Capability: "architecture", Files: []string{"app/main.ts"}, DiagnosticFiles: []string{"app/main.ts", "shared/value.ts", "unrelated/broken.ts"}, Scopes: []AnalysisScope{{Members: []string{"app/main.ts"}}, {Members: []string{"shared/value.ts"}}, {Members: []string{"unrelated/broken.ts"}}}}
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
