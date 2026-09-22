package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeRuff struct {
	formatted map[string][]byte
	findings  []responseFinding
	functions []functionFact
	err       error
}

func (ruff fakeRuff) format(_ context.Context, _ string, _ analysisScope, file string, source []byte) ([]byte, error) {
	if ruff.err != nil {
		return nil, ruff.err
	}
	if formatted, found := ruff.formatted[file]; found {
		return formatted, nil
	}
	return source, nil
}

func (ruff fakeRuff) lint(context.Context, string, analysisScope, []string, map[string]bool) ([]responseFinding, error) {
	return slices.Clone(ruff.findings), ruff.err
}

func (ruff fakeRuff) complexity(context.Context, string, analysisScope, []string) ([]functionFact, error) {
	return slices.Clone(ruff.functions), ruff.err
}

type fakeFacts struct {
	result factResult
	err    error
}

type fakeTy struct {
	findings []responseFinding
	files    *[]string
	err      error
}

type fakeProject struct {
	facts map[string]projectFact
	err   error
}

func (project fakeProject) inspect(_ context.Context, _ request, inputs []projectInput) (map[string]projectFact, []inputFile, error) {
	if project.err != nil {
		return nil, nil, project.err
	}
	result := map[string]projectFact{}
	for _, input := range inputs {
		if input.Kind != "manifest" {
			continue
		}
		fact, found := project.facts[input.Manifest]
		if !found {
			fact = projectFact{
				Manifest: input.Manifest, RequiresPython: "==3.12.*", TargetVersion: "py312",
				BackendPaths: []string{}, Problems: []projectProblem{},
			}
		}
		result[input.Manifest] = fact
	}
	return result, []inputFile{}, nil
}

func (ty fakeTy) typecheck(_ context.Context, _ string, _ analysisScope, files []string) ([]responseFinding, error) {
	if ty.files != nil {
		*ty.files = slices.Clone(files)
	}
	return slices.Clone(ty.findings), ty.err
}

func (facts fakeFacts) comments(context.Context, string, []string) (factResult, error) {
	return facts.result, facts.err
}

func (facts fakeFacts) functions(context.Context, string, []string) (factResult, error) {
	return facts.result, facts.err
}

func TestDiscoverySelectsTheNearestPythonProject(t *testing.T) {
	t.Parallel()
	request := request{
		Provider: "pack.python.analyze.lint", Files: []string{"services/api/src/api.py"},
		Inventory: []inventoryEntry{
			{Path: "pyproject.toml", Metadata: true},
			{Path: "src/root.py", Language: "python", Source: true, Owner: "pack.python.analyze.lint"},
			{Path: "services/api/pyproject.toml", Metadata: true},
			{Path: "services/api/ruff.toml", Metadata: true},
			{Path: "services/api/src/api.py", Language: "python", Source: true, Owner: "pack.python.analyze.lint"},
			{Path: "services/api/src/other.py", Language: "python", Source: true, Owner: "pack.python.analyze.lint"},
		},
	}
	result := discover(context.Background(), request, fakeProject{})
	if result.Status != "pass" || result.Discovery == nil || len(result.Discovery.Scopes) != 1 {
		t.Fatalf("result = %+v", result)
	}
	scope := result.Discovery.Scopes[0]
	if scope.Root != "services/api" || !slices.Equal(scope.Members, []string{"services/api/src/api.py", "services/api/src/other.py"}) {
		t.Fatalf("scope = %+v", scope)
	}
	if !slices.Equal(scope.Selected, []string{"services/api/src/api.py"}) || !slices.Contains(scope.Context, "services/api/ruff.toml") || slices.Contains(scope.Context, "src/root.py") {
		t.Fatalf("scope context = %+v", scope)
	}
	data, err := decodePythonScopeData(scope.Data)
	if err != nil || data.TargetVersion != "py312" || !slices.Equal(data.SourceRoots, []string{"services/api", "services/api/src"}) {
		t.Fatalf("scope data = %+v, %v", data, err)
	}
}

func TestDiscoveryRejectsSelectedSourceWithoutAProject(t *testing.T) {
	t.Parallel()
	result := discover(context.Background(), request{
		Provider: "pack.python.format.format", Files: []string{"src/app.py"},
		Inventory: []inventoryEntry{{Path: "src/app.py", Language: "python", Source: true, Owner: "pack.python.format.format"}},
	}, fakeProject{})
	if result.Status != "operational-failure" || !strings.Contains(result.Failure, "no contained pyproject.toml") {
		t.Fatalf("result = %+v", result)
	}
}

func TestFormatReturnsFindingOrAuthorizedEditWithoutWritingSource(t *testing.T) {
	root := t.TempDir()
	source := []byte("def identity( value ):\n    return value\n")
	formatted := []byte("def identity(value):\n    return value\n")
	request := pythonTestRequest(t, root, "format", source)
	t.Setenv("CODE_POLISHY_TOOL_RUFF", filepath.Join(root, "ruff"))
	adapter := adapter{ruff: fakeRuff{formatted: map[string][]byte{"src/app.py": formatted}}, facts: fakeFacts{}}
	checked := adapter.run(context.Background(), request)
	if checked.Status != "findings" || len(checked.Findings) != 1 || checked.Findings[0].Rule != "format" || len(checked.Edits) != 0 {
		t.Fatalf("check result = %+v", checked)
	}
	request.Mode = "write"
	request.WriteFiles = []string{"src/app.py"}
	written := adapter.run(context.Background(), request)
	if written.Status != "pass" || len(written.Findings) != 0 || !slices.Equal(written.Edits, []edit{{Path: "src/app.py", Content: string(formatted)}}) {
		t.Fatalf("write result = %+v", written)
	}
	current, err := os.ReadFile(filepath.Join(root, "src", "app.py"))
	if err != nil || !slices.Equal(current, source) {
		t.Fatalf("source changed: %q, %v", current, err)
	}
}

func TestInvalidProjectMetadataStopsOnlyItsScopeWithoutRequiringTools(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "lint", []byte("value = 1\n"))
	request.Tools = nil
	request.Scopes[0].Data = json.RawMessage(`{"manifest":"pyproject.toml","requiresPython":"","targetVersion":"","sourceRoots":[".","src"],"problems":[{"path":"pyproject.toml","message":"project.requires-python is required"}]}`)
	result := (adapter{}).run(context.Background(), request)
	if result.Status != "incomplete" || len(result.Findings) != 1 || result.Findings[0].Rule != "project.configuration" || len(result.Coverage.Unsupported) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestLintReturnsRuffFindingAndPythonCommentFacts(t *testing.T) {
	root := t.TempDir()
	source := []byte("# prose\nimport os\n")
	request := pythonTestRequest(t, root, "lint", source)
	t.Setenv("CODE_POLISHY_TOOL_RUFF", filepath.Join(root, "ruff"))
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	finding := responseFinding{Capability: "lint", Path: "src/app.py", Line: 2, Column: 8, Subject: "F401", Message: "unused import", Rule: "ruff.f401"}
	comment := commentFact{Path: "src/app.py", Line: 1, Column: 1, Kind: "Line", Raw: "# prose", Complete: true, BeforeCode: true, Preamble: true, ByteZero: true}
	adapter := adapter{
		ruff:  fakeRuff{findings: []responseFinding{finding}},
		facts: fakeFacts{result: factResult{Comments: []commentFact{comment}, Failures: map[string]string{}}},
	}
	result := adapter.run(context.Background(), request)
	if result.Status != "findings" || !slices.Contains(result.Findings, finding) || result.Facts == nil || result.Facts.Comments == nil || !slices.Equal(*result.Facts.Comments, []commentFact{comment}) {
		t.Fatalf("result = %+v", result)
	}
	if !slices.Equal(result.Coverage.Analyzed, []string{"src/app.py"}) || len(result.Inputs) != 2 {
		t.Fatalf("coverage = %+v, inputs = %+v", result.Coverage, result.Inputs)
	}
}

func TestComplexityCombinesRuffAndPythonFunctionMeasurements(t *testing.T) {
	root := t.TempDir()
	source := []byte("def branch(value):\n    if value:\n        return 1\n    return 0\n")
	request := pythonTestRequest(t, root, "complexity", source)
	t.Setenv("CODE_POLISHY_TOOL_RUFF", filepath.Join(root, "ruff"))
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	location := functionFact{Path: "src/app.py", Line: 1, Column: 5, Name: "branch"}
	metric := location
	metric.Complexity = 2
	shape := location
	shape.Depth = 1
	shape.Parameters = 1
	adapter := adapter{
		ruff:  fakeRuff{functions: []functionFact{metric}},
		facts: fakeFacts{result: factResult{Functions: []functionFact{shape}, Failures: map[string]string{}}},
	}
	result := adapter.run(context.Background(), request)
	want := shape
	want.Complexity = 2
	if result.Status != "pass" || result.Facts == nil || result.Facts.Functions == nil || !slices.Equal(*result.Facts.Functions, []functionFact{want}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestTypecheckAnalyzesTheCompleteSelectedProjectScope(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "typecheck", []byte("from .other import value\n"))
	other := []byte("value: int = 'wrong'\n")
	writePythonTestInput(t, root, &request, "src/other.py", other)
	request.Scopes[0].Members = append(request.Scopes[0].Members, "src/other.py")
	request.Scopes[0].Context = append(request.Scopes[0].Context, "src/other.py")
	request.DiagnosticFiles = append(request.DiagnosticFiles, "src/other.py")
	t.Setenv("CODE_POLISHY_TOOL_TY", filepath.Join(root, "ty"))
	finding := responseFinding{Capability: "typecheck", Path: "src/other.py", Line: 1, Column: 14, Subject: "invalid-assignment", Message: "wrong type", Rule: "ty.invalid-assignment"}
	checked := []string{}
	adapter := adapter{ty: fakeTy{findings: []responseFinding{finding}, files: &checked}}
	result := adapter.run(context.Background(), request)
	if result.Status != "findings" || !slices.Equal(checked, []string{"src/app.py", "src/other.py"}) || !slices.Equal(result.Coverage.Analyzed, checked) {
		t.Fatalf("result = %+v, checked = %v", result, checked)
	}
}

func TestDecodeRequestRejectsUnknownFieldsAndTrailingDocuments(t *testing.T) {
	t.Parallel()
	if _, err := decodeRequest(strings.NewReader(`{"protocolVersion":4,"operation":"discover","unknown":true}`)); err == nil {
		t.Fatal("unknown request field passed")
	}
	if _, err := decodeRequest(strings.NewReader(`{"protocolVersion":4,"operation":"discover"}{}`)); err == nil {
		t.Fatal("trailing request document passed")
	}
}

func TestRuffFindingsStayWithinTheSelectedMaterializedScope(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	scope := analysisScope{Root: "project"}
	inside := filepath.Join(workspace, "project", "src", "app.py")
	diagnostic := `[{
		"code":"F401",
		"filename":` + mustJSON(t, inside) + `,
		"location":{"row":1,"column":8},
		"message":"unused import"
	}]`
	findings, err := parseRuffFindings(workspace, scope, []string{"project/src/app.py"}, map[string]bool{}, [][]byte{[]byte(diagnostic), []byte("[]")})
	if err != nil || len(findings) != 1 || findings[0].Path != "project/src/app.py" || findings[0].Rule != "ruff.f401" {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
	escaping := strings.Replace(diagnostic, inside, filepath.Join(filepath.Dir(workspace), "outside.py"), 1)
	if _, err := parseRuffFindings(workspace, scope, []string{"project/src/app.py"}, map[string]bool{}, [][]byte{[]byte(escaping)}); err == nil {
		t.Fatal("escaping Ruff path passed")
	}
}

func pythonTestRequest(t *testing.T, root, capability string, source []byte) request {
	t.Helper()
	files := map[string][]byte{
		"pyproject.toml": []byte("[project]\nname='sample'\nversion='1.0.0'\nrequires-python='==3.12.*'\n"),
		"src/app.py":     source,
	}
	context := make([]inputFile, 0, len(files))
	for name, data := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		context = append(context, inputFile{Path: name, SHA256: hex.EncodeToString(digest[:])})
	}
	slices.SortFunc(context, func(left, right inputFile) int { return strings.Compare(left.Path, right.Path) })
	operation := "check"
	tools := []toolIdentity{}
	switch capability {
	case "format":
		operation = "format"
		tools = append(tools, toolIdentity{ID: "ruff", Name: "ruff", Version: "0.16.0", SHA256: strings.Repeat("a", 64)})
	case "lint", "complexity":
		tools = append(tools,
			toolIdentity{ID: "ruff", Name: "ruff", Version: "0.16.0", SHA256: strings.Repeat("a", 64)},
			toolIdentity{ID: "python", Name: "python", Version: "3.12.13+20260728", SHA256: strings.Repeat("b", 64)},
		)
	case "typecheck":
		tools = append(tools, toolIdentity{ID: "ty", Name: "ty", Version: "0.0.65", SHA256: strings.Repeat("c", 64)})
	}
	return request{
		ProtocolVersion: protocolVersion, Operation: operation, Capability: capability, ProjectRoot: root,
		Files: []string{"src/app.py"}, DiagnosticFiles: []string{"src/app.py"}, Mode: "check",
		Scopes: []analysisScope{{
			Handle: "scope-1", Language: "python", Root: ".", Members: []string{"src/app.py"}, Context: []string{"pyproject.toml", "src/app.py"},
			Data: json.RawMessage(`{"manifest":"pyproject.toml","requiresPython":"==3.12.*","targetVersion":"py312","sourceRoots":[".","src"],"problems":[]}`),
		}},
		Context: context, Inventory: []inventoryEntry{{Path: "src/app.py", Language: "python", Source: true}}, Tools: tools,
	}
}

func writePythonTestInput(t *testing.T, root string, request *request, name string, data []byte) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	request.Context = append(request.Context, inputFile{Path: name, SHA256: hex.EncodeToString(digest[:])})
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
