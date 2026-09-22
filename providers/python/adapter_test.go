package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type fakeRuff struct {
	formatted     map[string][]byte
	findings      []responseFinding
	functions     []functionFact
	completeGraph ruffGraph
	runtimeGraph  ruffGraph
	graphFiles    *[]string
	err           error
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

func (ruff fakeRuff) graph(_ context.Context, _ string, _ analysisScope, files []string, typeChecking bool) (ruffGraph, error) {
	if ruff.graphFiles != nil {
		*ruff.graphFiles = slices.Clone(files)
	}
	if typeChecking {
		return ruff.completeGraph, ruff.err
	}
	return ruff.runtimeGraph, ruff.err
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

type fakeVulture struct {
	result    vultureResult
	files     *[]string
	contracts *[]vultureContract
	err       error
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
				BackendPaths: []string{}, EntryPoints: []projectEntryPoint{}, Problems: []projectProblem{},
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

func (vulture fakeVulture) deadCode(_ context.Context, _ string, _ analysisScope, files []string, contracts []vultureContract) (vultureResult, error) {
	if vulture.files != nil {
		*vulture.files = slices.Clone(files)
	}
	if vulture.contracts != nil {
		*vulture.contracts = cloneVultureContracts(contracts)
	}
	return vulture.result, vulture.err
}

func (facts fakeFacts) comments(context.Context, string, []string) (factResult, error) {
	return facts.result, facts.err
}

func (facts fakeFacts) functions(context.Context, string, []string) (factResult, error) {
	return facts.result, facts.err
}

func (facts fakeFacts) imports(context.Context, string, []string) (factResult, error) {
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

func TestDiscoveryMarksUnsupportedPythonModuleLayoutsInvalid(t *testing.T) {
	t.Parallel()
	request := request{
		Provider: "pack.python.analyze.lint", Files: []string{"src/bad-name.py"},
		Inventory: []inventoryEntry{
			{Path: "pyproject.toml", Metadata: true},
			{Path: "src/bad-name.py", Language: "python", Source: true, Owner: "pack.python.analyze.lint"},
		},
	}
	result := discover(context.Background(), request, fakeProject{})
	if result.Status != "pass" || result.Discovery == nil || len(result.Discovery.Scopes) != 1 {
		t.Fatalf("result = %+v", result)
	}
	data, err := decodePythonScopeData(result.Discovery.Scopes[0].Data)
	if err != nil || len(data.Problems) != 1 || data.Problems[0].Path != "src/bad-name.py" {
		t.Fatalf("scope data = %+v, err = %v", data, err)
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
	request.Scopes[0].Data = json.RawMessage(`{"manifest":"pyproject.toml","requiresPython":"","targetVersion":"","sourceRoots":[".","src"],"backendPaths":[],"buildBackend":{"module":"","object":""},"entryPoints":[],"problems":[{"path":"pyproject.toml","message":"project.requires-python is required"}]}`)
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

func TestDeadCodeAnalyzesTheCompleteSelectedProjectScope(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("from .other import value\nprint(value)\n"))
	other := []byte("value = 1\nunneeded = 2\n")
	writePythonTestInput(t, root, &request, "src/other.py", other)
	request.Scopes[0].Members = append(request.Scopes[0].Members, "src/other.py")
	request.Scopes[0].Context = append(request.Scopes[0].Context, "src/other.py")
	request.DiagnosticFiles = append(request.DiagnosticFiles, "src/other.py")
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	fact := deadCodeFact{Analyzer: "vulture", Path: "src/other.py", Line: 2, EndLine: 2, Name: "unneeded", Kind: "variable", Confidence: 60, Message: "unused variable 'unneeded'"}
	checked := []string{}
	result := (adapter{vulture: fakeVulture{result: vultureResult{Facts: []deadCodeFact{fact}}, files: &checked}}).run(context.Background(), request)
	if result.Status != "pass" || result.Facts == nil || result.Facts.DeadCode == nil || !slices.Equal(*result.Facts.DeadCode, []deadCodeFact{fact}) {
		t.Fatalf("result = %+v", result)
	}
	if !slices.Equal(checked, []string{"src/app.py", "src/other.py"}) || !slices.Equal(result.Coverage.Analyzed, checked) {
		t.Fatalf("coverage = %+v, checked = %v", result.Coverage, checked)
	}
}

func TestDeadCodeRejectsUnimplementedRuntimeDeclarations(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("value = 1\n"))
	request.Policy = json.RawMessage(`{"quality":{},"modules":[],"files":[],"declarations":[{"kind":"python.contract","version":1,"scopes":["scope-1"],"inputs":["pyproject.toml"],"data":{"project":"pyproject.toml","kind":"type","target":"vendor.Model","attributes":["state"],"reason":"Runtime contract."}}]}`)
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	checked := []string{}
	result := (adapter{vulture: fakeVulture{files: &checked}}).run(context.Background(), request)
	if result.Status != "incomplete" || len(result.Coverage.Unsupported) != 1 || !strings.Contains(result.Coverage.Unsupported[0].Reason, "attributes") || len(checked) != 0 {
		t.Fatalf("result = %+v, checked = %v", result, checked)
	}
}

func TestDeadCodeCarriesRepositoryEntryPointContracts(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("class Handler:\n    def execute(self):\n        return 1\n"))
	request.Policy = json.RawMessage(`{"quality":{},"modules":[],"files":[],"declarations":[{"kind":"python.contract","version":1,"scopes":["scope-1"],"inputs":["pyproject.toml"],"data":{"project":"pyproject.toml","kind":"entry-point","target":"app:Handler","members":["execute"],"reason":"Runtime selects this handler."}}]}`)
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	contracts := []vultureContract{}
	result := (adapter{vulture: fakeVulture{contracts: &contracts}}).run(context.Background(), request)
	want := []vultureContract{{ID: "config:python.contract:entry-point:app:Handler", Kind: "entry-point", Target: "app:Handler", Members: []string{"execute"}, Attributes: []string{}, Decorators: []string{}, Keywords: map[string]bool{}}}
	if result.Status != "pass" || !reflect.DeepEqual(contracts, want) {
		t.Fatalf("result = %+v, contracts = %+v", result, contracts)
	}
}

func TestDeadCodeCarriesRepositoryDecoratorContracts(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("def handler():\n    return 1\n"))
	request.Policy = json.RawMessage(`{"quality":{},"modules":[],"files":[],"declarations":[{"kind":"python.contract","version":1,"scopes":["scope-1"],"inputs":["pyproject.toml"],"data":{"project":"pyproject.toml","kind":"decorator","target":"vendor.register","keywords":{"active":true},"reason":"Runtime calls registered handlers."}}]}`)
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	contracts := []vultureContract{}
	result := (adapter{vulture: fakeVulture{contracts: &contracts}}).run(context.Background(), request)
	want := []vultureContract{{ID: "config:python.contract:decorator:vendor.register", Kind: "decorator", Target: "vendor.register", Members: []string{}, Attributes: []string{}, Decorators: []string{}, Keywords: map[string]bool{"active": true}}}
	if result.Status != "pass" || !reflect.DeepEqual(contracts, want) {
		t.Fatalf("result = %+v, contracts = %+v", result, contracts)
	}
}

func TestDeadCodeCarriesRepositoryModuleBindingContracts(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("registry = []\n"))
	request.Policy = json.RawMessage(`{"quality":{},"modules":[],"files":[],"declarations":[{"kind":"python.contract","version":1,"scopes":["scope-1"],"inputs":["pyproject.toml"],"data":{"project":"pyproject.toml","kind":"module-binding","target":"vendor.api","members":["registry"],"reason":"Runtime reads this registry."}}]}`)
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	contracts := []vultureContract{}
	result := (adapter{vulture: fakeVulture{contracts: &contracts}}).run(context.Background(), request)
	want := []vultureContract{{ID: "config:python.contract:module-binding:vendor.api", Kind: "module-binding", Target: "vendor.api", Members: []string{"registry"}, Attributes: []string{}, Decorators: []string{}, Keywords: map[string]bool{}}}
	if result.Status != "pass" || !reflect.DeepEqual(contracts, want) {
		t.Fatalf("result = %+v, contracts = %+v", result, contracts)
	}
}

func TestDeadCodeCarriesRepositoryTypeMemberContracts(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("class Model:\n    def serialize(self):\n        return 1\n"))
	request.Policy = json.RawMessage(`{"quality":{},"modules":[],"files":[],"declarations":[{"kind":"python.contract","version":1,"scopes":["scope-1"],"inputs":["pyproject.toml"],"data":{"project":"pyproject.toml","kind":"type","target":"vendor.Model","members":["serialize"],"reason":"Runtime calls this method."}}]}`)
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	contracts := []vultureContract{}
	result := (adapter{vulture: fakeVulture{contracts: &contracts}}).run(context.Background(), request)
	want := []vultureContract{{ID: "config:python.contract:type:vendor.Model", Kind: "type", Target: "vendor.Model", Members: []string{"serialize"}, Attributes: []string{}, Decorators: []string{}, Keywords: map[string]bool{}}}
	if result.Status != "pass" || !reflect.DeepEqual(contracts, want) {
		t.Fatalf("result = %+v, contracts = %+v", result, contracts)
	}
}

func TestDeadCodeCarriesRepositoryAnnotatedTypeContracts(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("class Model:\n    field: str\n"))
	request.Policy = json.RawMessage(`{"quality":{},"modules":[],"files":[],"declarations":[{"kind":"python.contract","version":1,"scopes":["scope-1"],"inputs":["pyproject.toml"],"data":{"project":"pyproject.toml","kind":"type","target":"vendor.Model","annotatedFields":true,"reason":"Runtime reads annotated fields."}}]}`)
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	contracts := []vultureContract{}
	result := (adapter{vulture: fakeVulture{contracts: &contracts}}).run(context.Background(), request)
	want := []vultureContract{{ID: "config:python.contract:type:vendor.Model", Kind: "type", Target: "vendor.Model", Members: []string{}, Attributes: []string{}, Decorators: []string{}, AnnotatedFields: true, Keywords: map[string]bool{}}}
	if result.Status != "pass" || !reflect.DeepEqual(contracts, want) {
		t.Fatalf("result = %+v, contracts = %+v", result, contracts)
	}
}

func TestDeadCodeAcceptsManifestEntryPointReachability(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("value = 1\n"))
	request.Scopes[0].Data = json.RawMessage(`{"manifest":"pyproject.toml","requiresPython":"==3.12.*","targetVersion":"py312","sourceRoots":[".","src"],"backendPaths":[],"buildBackend":{"module":"","object":""},"entryPoints":[{"group":"sample.plugins","name":"first","module":"sample.plugin","symbol":"Plugin"}],"problems":[]}`)
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	checked := []string{}
	result := (adapter{vulture: fakeVulture{files: &checked}}).run(context.Background(), request)
	if result.Status != "pass" || !slices.Equal(checked, []string{"src/app.py"}) || !slices.Equal(result.Coverage.Analyzed, checked) {
		t.Fatalf("result = %+v, checked = %v", result, checked)
	}
}

func TestDeadCodeWithholdsFactsForUnresolvedManifestReachability(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("value = 1\n"))
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	problem := vultureProblem{ID: "manifest:pyproject.toml:console_scripts:sample:sample:missing", Message: "symbol is stale or ambiguous"}
	result := (adapter{vulture: fakeVulture{result: vultureResult{Problems: []vultureProblem{problem}}}}).run(context.Background(), request)
	if result.Status != "incomplete" || len(result.Coverage.Unsupported) != 1 || !strings.Contains(result.Coverage.Unsupported[0].Reason, problem.ID) || result.Facts == nil || result.Facts.DeadCode == nil || len(*result.Facts.DeadCode) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestDeadCodeRequiresEveryProjectSource(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "dead-code", []byte("value = 1\n"))
	large := bytes.Repeat([]byte("x"), maximumPythonBytes+1)
	writePythonTestInput(t, root, &request, "src/large.py", large)
	request.Scopes[0].Members = append(request.Scopes[0].Members, "src/large.py")
	request.Scopes[0].Context = append(request.Scopes[0].Context, "src/large.py")
	request.DiagnosticFiles = append(request.DiagnosticFiles, "src/large.py")
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	checked := []string{}
	result := (adapter{vulture: fakeVulture{files: &checked}}).run(context.Background(), request)
	if result.Status != "incomplete" || len(result.Coverage.Unsupported) != 2 || len(checked) != 0 {
		t.Fatalf("result = %+v, checked = %v", result, checked)
	}
}

func TestArchitectureReturnsResolvedRuntimeImportFactsForTheCompleteProjectScope(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "architecture", []byte("from helper import value\n"))
	helper := []byte("value = 1\n")
	writePythonTestInput(t, root, &request, "src/helper.py", helper)
	request.Scopes[0].Members = append(request.Scopes[0].Members, "src/helper.py")
	request.Scopes[0].Context = append(request.Scopes[0].Context, "src/helper.py")
	request.DiagnosticFiles = append(request.DiagnosticFiles, "src/helper.py")
	t.Setenv("CODE_POLISHY_TOOL_RUFF", filepath.Join(root, "ruff"))
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	complete := ruffGraph{"src/app.py": {"src/helper.py": true}, "src/helper.py": {}}
	runtime := ruffGraph{"src/app.py": {"src/helper.py": true}, "src/helper.py": {}}
	authored := authoredImport{Path: "src/app.py", Module: "helper", Names: []string{"value"}, Line: 1, Column: 1, Kind: "runtime"}
	checked := []string{}
	adapter := adapter{
		ruff:  fakeRuff{completeGraph: complete, runtimeGraph: runtime, graphFiles: &checked},
		facts: fakeFacts{result: factResult{Imports: []authoredImport{authored}, Failures: map[string]string{}}},
	}
	result := adapter.run(context.Background(), request)
	want := importFact{Path: "src/app.py", Line: 1, Column: 1, Specifier: "helper", Resolved: "src/helper.py", Kind: "runtime"}
	if result.Status != "pass" || result.Facts == nil || result.Facts.Imports == nil || !slices.Equal(*result.Facts.Imports, []importFact{want}) {
		t.Fatalf("result = %+v", result)
	}
	if !slices.Equal(checked, []string{"src/app.py", "src/helper.py"}) || !slices.Equal(result.Coverage.Analyzed, checked) {
		t.Fatalf("coverage = %+v, checked = %v", result.Coverage, checked)
	}
}

func TestArchitectureRejectsUndeclaredComputedImports(t *testing.T) {
	root := t.TempDir()
	request := pythonTestRequest(t, root, "architecture", []byte("__import__(name)\n"))
	t.Setenv("CODE_POLISHY_TOOL_RUFF", filepath.Join(root, "ruff"))
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	dynamic := dynamicImport{Path: "src/app.py", Line: 1, Column: 1, Callee: "__import__"}
	adapter := adapter{ruff: fakeRuff{}, facts: fakeFacts{result: factResult{DynamicImports: []dynamicImport{dynamic}, Failures: map[string]string{}}}}
	result := adapter.run(context.Background(), request)
	if result.Status != "incomplete" || len(result.Coverage.Unsupported) != 1 || !strings.Contains(result.Coverage.Unsupported[0].Reason, "explicit pack declaration") {
		t.Fatalf("result = %+v", result)
	}
}

func TestArchitectureResolvesDeclaredComputedImportsAsProvenDynamicFacts(t *testing.T) {
	root := t.TempDir()
	source := []byte("import importlib\nimportlib.import_module(name)\n")
	request := pythonTestRequest(t, root, "architecture", source)
	target := []byte("value = 1\n")
	writePythonTestInput(t, root, &request, "src/app/plugins/first.py", target)
	request.Scopes[0].Members = append(request.Scopes[0].Members, "src/app/plugins/first.py")
	request.Scopes[0].Context = append(request.Scopes[0].Context, "src/app/plugins/first.py")
	request.DiagnosticFiles = append(request.DiagnosticFiles, "src/app/plugins/first.py")
	digest := sha256.Sum256(source)
	declaration := computedImportDeclaration{
		Project: "pyproject.toml", Importer: "src/app.py", Module: "app", ModuleScope: true,
		Callee: "importlib.import_module", Line: 2, Column: 1, Shape: "call", Argument: "name",
		SourceSHA256: hex.EncodeToString(digest[:]), Namespace: "app.plugins", Targets: []string{"app.plugins.first"},
	}
	setPythonComputedImportPolicy(t, &request, declaration)
	t.Setenv("CODE_POLISHY_TOOL_RUFF", filepath.Join(root, "ruff"))
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	graph := ruffGraph{"src/app.py": {}, "src/app/plugins/first.py": {}}
	dynamic := dynamicImport{Path: "src/app.py", Line: 2, Column: 1, Callee: "importlib.import_module"}
	result := (adapter{ruff: fakeRuff{completeGraph: graph, runtimeGraph: graph}, facts: fakeFacts{result: factResult{DynamicImports: []dynamicImport{dynamic}, Failures: map[string]string{}}}}).run(context.Background(), request)
	want := importFact{Path: "src/app.py", Line: 2, Column: 1, Specifier: "app.plugins.first", Resolved: "src/app/plugins/first.py", Kind: "proven-dynamic"}
	if result.Status != "pass" || result.Facts == nil || result.Facts.Imports == nil || !slices.Equal(*result.Facts.Imports, []importFact{want}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestArchitectureReadsDigestBoundComputedImportConfiguration(t *testing.T) {
	root := t.TempDir()
	source := []byte("from importlib import import_module\nimport_module(name)\n")
	request := pythonTestRequest(t, root, "architecture", source)
	target := []byte("value = 1\n")
	configuration := []byte(`{"enabled":["app.plugins.first"]}`)
	writePythonTestInput(t, root, &request, "src/app/plugins/first.py", target)
	writePythonTestInput(t, root, &request, "plugins.json", configuration)
	request.Scopes[0].Members = append(request.Scopes[0].Members, "src/app/plugins/first.py")
	request.Scopes[0].Context = append(request.Scopes[0].Context, "src/app/plugins/first.py")
	request.DiagnosticFiles = append(request.DiagnosticFiles, "src/app/plugins/first.py")
	sourceDigest := sha256.Sum256(source)
	configurationDigest := sha256.Sum256(configuration)
	declaration := computedImportDeclaration{
		Project: "pyproject.toml", Importer: "src/app.py", Module: "app", ModuleScope: true,
		Callee: "importlib.import_module", Line: 2, Column: 1, Shape: "call", Argument: "name",
		SourceSHA256: hex.EncodeToString(sourceDigest[:]), Namespace: "app.plugins",
		Configuration: []computedImportInput{{Path: "plugins.json", JSONPointer: "/enabled", SHA256: hex.EncodeToString(configurationDigest[:])}},
	}
	setPythonComputedImportPolicy(t, &request, declaration)
	t.Setenv("CODE_POLISHY_TOOL_RUFF", filepath.Join(root, "ruff"))
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	graph := ruffGraph{"src/app.py": {}, "src/app/plugins/first.py": {}}
	dynamic := dynamicImport{Path: "src/app.py", Line: 2, Column: 1, Callee: "importlib.import_module"}
	result := (adapter{ruff: fakeRuff{completeGraph: graph, runtimeGraph: graph}, facts: fakeFacts{result: factResult{DynamicImports: []dynamicImport{dynamic}, Failures: map[string]string{}}}}).run(context.Background(), request)
	if result.Status != "pass" || result.Facts == nil || result.Facts.Imports == nil || len(*result.Facts.Imports) != 1 || !slices.ContainsFunc(result.Inputs, func(input inputFile) bool { return input.Path == "plugins.json" }) {
		t.Fatalf("result = %+v", result)
	}
}

func TestArchitectureRejectsStaleComputedImportDeclarations(t *testing.T) {
	root := t.TempDir()
	source := []byte("import importlib\nimportlib.import_module(name)\n")
	request := pythonTestRequest(t, root, "architecture", source)
	declaration := computedImportDeclaration{
		Project: "pyproject.toml", Importer: "src/app.py", Module: "app", ModuleScope: true,
		Callee: "importlib.import_module", Line: 2, Column: 1, Shape: "call", Argument: "name",
		SourceSHA256: strings.Repeat("0", 64), Namespace: "app.plugins", Targets: []string{"app.plugins.first"},
	}
	setPythonComputedImportPolicy(t, &request, declaration)
	t.Setenv("CODE_POLISHY_TOOL_RUFF", filepath.Join(root, "ruff"))
	t.Setenv("CODE_POLISHY_TOOL_PYTHON", filepath.Join(root, "python"))
	dynamic := dynamicImport{Path: "src/app.py", Line: 2, Column: 1, Callee: "importlib.import_module"}
	result := (adapter{ruff: fakeRuff{}, facts: fakeFacts{result: factResult{DynamicImports: []dynamicImport{dynamic}, Failures: map[string]string{}}}}).run(context.Background(), request)
	if result.Status != "incomplete" || len(result.Coverage.Unsupported) != 1 || !strings.Contains(result.Coverage.Unsupported[0].Reason, "source digest is stale") {
		t.Fatalf("result = %+v", result)
	}
}

func TestComputedImportTargetsUseEntryPointsAndObjectRegistries(t *testing.T) {
	configuration := []byte(`{"plugins":{"first":"app.plugins.first:Plugin"}}`)
	digest := sha256.Sum256(configuration)
	input := computedImportInput{Path: "plugins.json", JSONPointer: "/plugins", SHA256: hex.EncodeToString(digest[:])}
	objectTargets, reason := computedImportTargets(pythonScopeData{}, map[string][]byte{"plugins.json": configuration}, computedImportDeclaration{
		Callee: "pkgutil.resolve_name", Namespace: "app.plugins", Configuration: []computedImportInput{input},
	})
	if reason != "" || !slices.Equal(objectTargets, []string{"app.plugins.first"}) {
		t.Fatalf("object targets = %v, reason = %q", objectTargets, reason)
	}
	entryTargets, reason := computedImportTargets(pythonScopeData{EntryPoints: []projectEntryPoint{{Group: "app.plugins", Name: "first", Module: "app.plugins.first", Symbol: "Plugin"}}}, nil, computedImportDeclaration{EntryPointGroup: "app.plugins"})
	if reason != "" || !slices.Equal(entryTargets, []string{"app.plugins.first"}) {
		t.Fatalf("entry targets = %v, reason = %q", entryTargets, reason)
	}
	if _, err := decodeComputedJSON([]byte(`{"value":1,"value":2}`)); err == nil {
		t.Fatal("duplicate configuration key passed")
	}
}

func TestPythonImportResolutionPreservesTypeOnlyAndReExportKinds(t *testing.T) {
	t.Parallel()
	scope := analysisScope{
		Root: ".", Members: []string{"src/pkg/__init__.py", "src/pkg/model.py", "src/pkg/types.py"},
		Data: json.RawMessage(`{"manifest":"pyproject.toml","requiresPython":"==3.12.*","targetVersion":"py312","sourceRoots":[".","src"],"backendPaths":[],"buildBackend":{"module":"","object":""},"entryPoints":[],"problems":[]}`),
	}
	imports := []authoredImport{
		{Path: "src/pkg/__init__.py", Module: ".types", Names: []string{"Thing"}, Line: 1, Column: 1, Kind: "re-export"},
		{Path: "src/pkg/model.py", Module: "pkg.types", Names: []string{"Thing"}, Line: 2, Column: 5, Kind: "type-only"},
	}
	complete := ruffGraph{
		"src/pkg/__init__.py": {"src/pkg/types.py": true},
		"src/pkg/model.py":    {"src/pkg/types.py": true},
	}
	runtime := ruffGraph{"src/pkg/__init__.py": {"src/pkg/types.py": true}, "src/pkg/model.py": {}}
	result, err := resolvePythonImports(scope, []string{"src/pkg/__init__.py", "src/pkg/model.py"}, imports, complete, runtime)
	if err != nil || len(result.failures) != 0 || len(result.facts) != 2 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if result.facts[0].Kind != "re-export" || result.facts[1].Kind != "type-only" {
		t.Fatalf("facts = %+v", result.facts)
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
	case "lint", "complexity", "architecture":
		tools = append(tools,
			toolIdentity{ID: "ruff", Name: "ruff", Version: "0.16.0", SHA256: strings.Repeat("a", 64)},
			toolIdentity{ID: "python", Name: "python", Version: "3.12.13+20260728", SHA256: strings.Repeat("b", 64)},
		)
	case "dead-code":
		tools = append(tools, toolIdentity{ID: "python", Name: "python", Version: "3.12.13+20260728", SHA256: strings.Repeat("b", 64)})
	case "typecheck":
		tools = append(tools, toolIdentity{ID: "ty", Name: "ty", Version: "0.0.65", SHA256: strings.Repeat("c", 64)})
	}
	return request{
		ProtocolVersion: protocolVersion, Operation: operation, Capability: capability, ProjectRoot: root,
		Files: []string{"src/app.py"}, DiagnosticFiles: []string{"src/app.py"}, Mode: "check",
		Scopes: []analysisScope{{
			Handle: "scope-1", Language: "python", Root: ".", Members: []string{"src/app.py"}, Context: []string{"pyproject.toml", "src/app.py"},
			Data: json.RawMessage(`{"manifest":"pyproject.toml","requiresPython":"==3.12.*","targetVersion":"py312","sourceRoots":[".","src"],"backendPaths":[],"buildBackend":{"module":"","object":""},"entryPoints":[],"problems":[]}`),
		}},
		Context: context, Inventory: []inventoryEntry{{Path: "src/app.py", Language: "python", Source: true}},
		Policy: json.RawMessage(`{"quality":{},"modules":[],"files":[],"declarations":[]}`), Tools: tools,
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

func setPythonComputedImportPolicy(t *testing.T, request *request, declaration computedImportDeclaration) {
	t.Helper()
	data, err := json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []string{declaration.Project, declaration.Importer}
	for _, configuration := range declaration.Configuration {
		inputs = append(inputs, configuration.Path)
	}
	policy, err := json.Marshal(policyInput{
		Quality: json.RawMessage(`{}`), Modules: []json.RawMessage{}, Files: []json.RawMessage{},
		Declarations: []policyDeclarationInput{{Kind: "python.computed-import", Version: 1, Scopes: []string{"scope-1"}, Inputs: uniqueSorted(inputs), Data: data}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request.Policy = policy
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
