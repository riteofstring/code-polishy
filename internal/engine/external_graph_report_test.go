package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
)

func TestExternalCompositionSurvivesCanonicalJSONAndSARIF(t *testing.T) {
	graph := externalReportTestGraph(t)
	report := (&Engine{}).normalizeReport(Report{Command: "architecture", ReportPath: ".code-polishy-reports/architecture/external/report.json", SourceDependencyGraph: &graph})
	data, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.SourceDependencyGraph, &graph) {
		t.Fatal("canonical JSON omitted external contract evidence")
	}
	sarif, err := SARIF(report)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Runs []struct {
			Properties struct {
				Report Report `json:"codePolishyReport"`
			} `json:"properties"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(sarif, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Runs) != 1 || !reflect.DeepEqual(document.Runs[0].Properties.Report.SourceDependencyGraph, &graph) {
		t.Fatal("SARIF omitted external contract evidence")
	}
	var altered map[string]any
	if err := json.Unmarshal(data, &altered); err != nil {
		t.Fatal(err)
	}
	contract := altered["sourceDependencyGraph"].(map[string]any)["externalCompositions"].([]any)[0].(map[string]any)["contract"].(map[string]any)
	delete(contract, "runtimeSha256")
	invalid, err := json.Marshal(altered)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCanonicalReport(invalid); err == nil {
		t.Fatal("report schema admitted external composition without runtime proof")
	}
}

func externalReportTestGraph(t *testing.T) sourcegraph.Graph {
	t.Helper()
	edge := sourcegraph.ExternalComposition{Source: "app.py", SourceResolution: "file:app.py", Line: 4, Column: 14, Dependency: sourcegraph.ExternalDependency{Project: "pyproject.toml", Lock: "uv.lock", ManifestSHA256: strings.Repeat("a", 64), LockSHA256: strings.Repeat("b", 64), Distribution: "plug-dist", Version: "1.0", Kind: "git", Source: "git+ssh://git@private.example.test/team/plugin.git@" + strings.Repeat("a", 40), Namespace: "third_party.plugins"}, Contract: sourcegraph.ExternalContract{InputGrammar: "python-module-object/v1", CheckKind: "issubclass", Protocol: "app.Contract", RuntimeType: "app.Contract", CheckLine: 5, CheckColumn: 12, SourceSHA256: strings.Repeat("c", 64), RuntimeSHA256: strings.Repeat("d", 64), InputSHA256: strings.Repeat("e", 64)}}
	graph, err := sourcegraph.New([]sourcegraph.Node{{Path: "app.py", Language: "python", Root: ".", Module: "app", Resolution: "file:app.py"}}, nil, []sourcegraph.FactInput{{Analyzer: "python-facts", Protocol: "python-facts/v3", Project: "pyproject.toml", Root: ".", Paths: []string{"app.py"}, FactsSHA256: strings.Repeat("a", 64), PartitionsSHA256: strings.Repeat("b", 64), ResolutionSHA256: strings.Repeat("c", 64)}}, []sourcegraph.ExternalComposition{edge})
	if err != nil {
		t.Fatal(err)
	}
	return graph
}
