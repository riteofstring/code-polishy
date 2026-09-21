package pack

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestAnalysisCoverageCannotOmitOrInventWork(t *testing.T) {
	request := Request{Capability: "typecheck", Files: []string{"src/a.fixture", "src/b.fixture"}, DiagnosticFiles: []string{"src/a.fixture", "src/b.fixture"}, WriteFiles: []string{"src/a.fixture", "src/b.fixture"}}
	for _, test := range []struct {
		name            string
		coverage        *Coverage
		status, problem string
	}{
		{"omitted file", &Coverage{Analyzed: []string{"src/a.fixture"}, Unsupported: []Unsupported{}}, "pass", "requested path"},
		{"duplicate", &Coverage{Analyzed: []string{"src/a.fixture", "src/a.fixture"}, Unsupported: []Unsupported{}}, "pass", "accounted for exactly once"},
		{"unselected", &Coverage{Analyzed: []string{"other.fixture"}, Unsupported: []Unsupported{}}, "pass", "path from diagnosticFiles"},
		{"false pass", &Coverage{Analyzed: []string{"src/a.fixture"}, Unsupported: []Unsupported{{Path: "src/b.fixture", Reason: "syntax unsupported"}}}, "pass", "incomplete when"},
		{"missing coverage", nil, "pass", "explicit"},
		{"overlapping coverage", &Coverage{Analyzed: request.Files, Unsupported: []Unsupported{{Path: "src/a.fixture", Reason: "ignored"}}}, "incomplete", "accounted for exactly once"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := Response{ProtocolVersion: ProtocolVersion, Status: test.status, Evidence: []string{"type checker completed"}, Coverage: test.coverage}
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseResponse(data, request); err == nil || !strings.Contains(err.Error(), test.problem) {
				t.Fatalf("expected %s, got %v", test.problem, err)
			}
		})
	}
	response := Response{ProtocolVersion: ProtocolVersion, Status: "incomplete", Coverage: &Coverage{Analyzed: []string{}, Unsupported: []Unsupported{{Path: request.Files[0], Reason: "not supported"}, {Path: request.Files[1], Reason: "not supported"}}}}
	data, _ := json.Marshal(response)
	parsed, err := parseResponse(data, request)
	if err != nil {
		t.Fatal(err)
	}
	findings := findingsForResponse(&policy.PackAdapter{PackName: "sample", Capability: "typecheck"}, parsed)
	if len(findings) != 2 || findings[0].Check != "policy.packCoverage" {
		t.Fatalf("unsupported analysis was accepted: %+v", findings)
	}
}

func TestFactsRequiredByPolicyAreNotOptional(t *testing.T) {
	for _, capability := range []string{"lint", "architecture", "complexity"} {
		t.Run(capability, func(t *testing.T) {
			forbidden := false
			request := Request{Capability: capability, Files: []string{"src/a.fixture"}, DiagnosticFiles: []string{"src/a.fixture"}, WriteFiles: []string{"src/a.fixture"}, Policy: PolicyInput{Quality: policy.Quality{AllowComments: &forbidden}}}
			response := Response{ProtocolVersion: ProtocolVersion, Status: "pass", Evidence: []string{"analyzer completed"}, Coverage: &Coverage{Analyzed: request.Files, Unsupported: []Unsupported{}}}
			if err := validateResponse(response, request); err == nil {
				t.Fatal("missing policy facts passed")
			}
			comments, imports, functions := []CommentFact{}, []ImportFact{}, []FunctionFact{}
			response.Facts = &SourceFacts{}
			switch capability {
			case "lint":
				response.Facts.Comments = &comments
			case "architecture":
				response.Facts.Imports = &imports
			case "complexity":
				response.Facts.Functions = &functions
			}
			if err := validateResponse(response, request); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCommentFactDiagnosticsIdentifyTheExactConstraint(t *testing.T) {
	forbidden := false
	request := Request{
		Capability:      "lint",
		Files:           []string{"src/a.fixture"},
		DiagnosticFiles: []string{"src/a.fixture"},
		WriteFiles:      []string{"src/a.fixture"},
		Policy:          PolicyInput{Quality: policy.Quality{AllowComments: &forbidden}},
	}
	valid := CommentFact{Path: "src/a.fixture", Line: 1, Column: 1, Kind: "Line", Raw: "// prose", Complete: true}
	tests := []struct {
		name   string
		change func(*CommentFact)
		want   string
	}{
		{"path", func(fact *CommentFact) { fact.Path = "src/other.fixture" }, "facts.comments[0].path: expected a path from coverage.analyzed"},
		{"line", func(fact *CommentFact) { fact.Line = 0 }, "facts.comments[0].line: expected a one-based line"},
		{"column", func(fact *CommentFact) { fact.Column = 0 }, "facts.comments[0].column: expected a one-based UTF-8 byte column"},
		{"kind", func(fact *CommentFact) { fact.Kind = "line" }, "facts.comments[0].kind: expected Line, Block, Docstring, HTML, or Shebang"},
		{"raw", func(fact *CommentFact) { fact.Raw = "" }, "facts.comments[0].raw: expected 1 to 65536 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fact := valid
			test.change(&fact)
			comments := []CommentFact{fact}
			response := Response{
				ProtocolVersion: ProtocolVersion,
				Status:          "pass",
				Evidence:        []string{"lint completed"},
				Coverage:        &Coverage{Analyzed: request.Files, Unsupported: []Unsupported{}},
				Facts:           &SourceFacts{Comments: &comments},
			}
			if err := validateResponse(response, request); err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCollectionDiagnosticsRetainIndexes(t *testing.T) {
	request := Request{Capability: "architecture", Files: []string{"src/a.fixture"}, DiagnosticFiles: []string{"src/a.fixture"}}
	imports := []ImportFact{{Path: "src/a.fixture", Line: 1, Column: 1, Specifier: "./b", Kind: "runtime"}, {Path: "src/a.fixture", Line: 2, Column: 1, Specifier: "./c", Kind: "dynamic"}}
	response := Response{ProtocolVersion: ProtocolVersion, Status: "pass", Evidence: []string{"graph completed"}, Coverage: &Coverage{Analyzed: request.Files, Unsupported: []Unsupported{}}, Facts: &SourceFacts{Imports: &imports}}
	if err := validateResponse(response, request); err == nil || err.Error() != "facts.imports[1].kind: expected runtime, type-only, re-export, or proven-dynamic" {
		t.Fatalf("indexed import error = %v", err)
	}
	response.Facts = &SourceFacts{Imports: &[]ImportFact{}}
	response.Inputs = []InputFile{{Path: "src/a.fixture", SHA256: strings.Repeat("a", 64)}, {Path: "src/a.fixture", SHA256: strings.Repeat("b", 64)}}
	if err := validateResponse(response, request); err == nil || err.Error() != "inputs[1].path: expected a path listed only once" {
		t.Fatalf("indexed input error = %v", err)
	}
}

func TestProviderInputsLocationsAndWritesUseOriginalSelectedSource(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.fixture", "// prose\nvalue\n", 0o644)
	writeTestFile(t, root, "context.fixture", "context\n", 0o644)
	repo, err := repository.Open(root, root, policy.Config{})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Capability: "lint", Files: []string{"a.fixture"}, DiagnosticFiles: []string{"a.fixture"}, WriteFiles: []string{"a.fixture"}}
	if err := prepareInputs(repo, &request); err != nil {
		t.Fatal(err)
	}
	response := Response{Status: "pass", Inputs: request.Context, Facts: &SourceFacts{Comments: &[]CommentFact{{Path: "a.fixture", Line: 1, Column: 1, Raw: "// prose"}}}}
	if err := verifyAnalysisInputs(repo, request, response); err != nil {
		t.Fatal(err)
	}
	(*response.Facts.Comments)[0].Column = 2
	if err := verifyAnalysisInputs(repo, request, response); err == nil {
		t.Fatal("invented original coordinate passed")
	}
	response.Facts = nil
	writeTestFile(t, root, "a.fixture", "changed source\n", 0o644)
	if err := verifyAnalysisInputs(repo, request, response); err == nil {
		t.Fatal("changed selected source passed")
	}
	writeTestFile(t, root, "context.fixture", "changed\n", 0o644)
	request.Capability, request.Mode = "format", "write"
	request.WriteFiles = []string{"a.fixture"}
	response.Edits = []Edit{{Path: "context.fixture", Content: "unauthorized\n"}}
	if err := applyEdits(repo, request, response); err == nil {
		t.Fatal("context write passed")
	}
	data, err := repo.Read("context.fixture")
	if err != nil || string(data) != "changed\n" {
		t.Fatalf("context changed: %q %v", data, err)
	}
	response.Edits = []Edit{{Path: "a.fixture", Content: "formatted\n"}}
	if err := applyEdits(repo, request, response); err != nil {
		t.Fatal(err)
	}
	data, err = repo.Read("a.fixture")
	if err != nil || string(data) != "formatted\n" {
		t.Fatalf("selected edit was not applied: %q %v", data, err)
	}
}
