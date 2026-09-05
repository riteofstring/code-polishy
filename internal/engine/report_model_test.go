package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	policyschema "github.com/riteofstring/code-polishy/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestReportCoalescesMessageChangesAndCountsWarnings(t *testing.T) {
	policyEngine := &Engine{}
	report := policyEngine.finishWithAdvisories([]policy.Finding{
		{Check: "quality.lint", Path: "src/app.go", Line: 4, Subject: "unused", Message: "first"},
		{Check: "quality.lint", Path: "src/app.go", Line: 4, Subject: "unused", Message: "second"},
	}, []policy.Advisory{{Check: "portability.machinePath", Path: "src/app.go", Subject: "path", Message: "warning"}}, nil)
	if len(report.Findings) != 2 || report.Summary.Errors != 1 || report.Summary.Warnings != 1 || !HasFindings(report) {
		t.Fatalf("report = %+v", report)
	}
}

func TestReportCoalescesOneSemanticOccurrenceAndRetainsWitnesses(t *testing.T) {
	policyEngine := &Engine{}
	identity := []string{"app/a.go", "app/b.go"}
	report := policyEngine.finish([]policy.Finding{
		{Check: "architecture.fileCycle", Path: "app/a.go", Line: 4, Subject: "cycle", Message: "cycle", SemanticIdentity: identity},
		{Check: "architecture.fileCycle", Path: "app/b.go", Line: 8, Subject: "cycle", Message: "cycle", SemanticIdentity: identity},
		{Check: "quality.lint", Path: "app/a.go", Line: 4, Subject: "first", Message: "first"},
		{Check: "quality.lint", Path: "app/b.go", Line: 8, Subject: "second", Message: "second"},
	}, nil)
	if len(report.Findings) != 3 {
		t.Fatalf("findings = %+v", report.Findings)
	}
	cycle := report.Findings[slices.IndexFunc(report.Findings, func(finding policy.Finding) bool { return finding.Check == "architecture.fileCycle" })]
	if len(cycle.Related) != 1 || cycle.Related[0].Path != "app/b.go" {
		t.Fatalf("cycle = %+v", cycle)
	}
}

func TestReportSelectionDistinguishesSelectedContextAndGlobal(t *testing.T) {
	policyEngine := &Engine{Repository: repository.Repository{Config: policy.Config{
		Modules: []policy.Module{{Name: "app", Paths: []string{"src/**"}}}, ModuleByName: map[string]int{"app": 0},
	}}}
	report := policyEngine.withSelection(policyEngine.finish([]policy.Finding{
		{Check: "quality.lint", Path: "src/selected.go", Subject: "selected", Message: "selected"},
		{Check: "quality.lint", Path: "src/context.go", Subject: "context", Message: "context"},
		{Check: "policy.scope", Path: policy.ConfigFilename, Subject: "global", Message: "global"},
	}, nil), repository.Selection{Requested: repository.RequestedSelection{
		Mode: "files", Operands: []string{"src/selected.go"}, Expanded: []string{"src/selected.go"},
	}})
	relations := map[string]policy.SelectionRelation{}
	for _, finding := range report.Findings {
		relations[finding.Subject] = finding.SelectionRelation
	}
	if relations["selected"] != policy.SelectionSelected || relations["context"] != policy.SelectionContext || relations["global"] != policy.SelectionGlobal {
		t.Fatalf("relations = %+v", relations)
	}
}

func TestReportRemediationRetainsBoundedEvaluationSelectors(t *testing.T) {
	policyEngine := &Engine{}
	for _, test := range []struct {
		name      string
		finding   policy.Finding
		selection repository.RequestedSelection
		want      []string
	}{
		{"directory", policy.Finding{Check: "quality.lint", Path: "src/nested/app.go"}, repository.RequestedSelection{Mode: "files", Operands: []string{"src"}, Expanded: []string{"src/nested/app.go"}}, []string{"code-polishy", "check", "--files", "src"}},
		{"module", policy.Finding{Check: "quality.lint", Path: "src/app.go", Module: "application"}, repository.RequestedSelection{Mode: "module", Modules: []string{"application"}}, []string{"code-polishy", "check", "--module", "application"}},
		{"context", policy.Finding{Check: "quality.lint", Path: "other/app.go"}, repository.RequestedSelection{Mode: "files", Operands: []string{"src"}}, []string{"code-polishy", "check", "--files", "other/app.go"}},
		{"global", policy.Finding{Check: "architecture.inventory", Path: "repository"}, repository.RequestedSelection{Mode: "all"}, []string{"code-polishy", "architecture", "--all"}},
		{"explicit recovery", policy.Finding{Check: "quality.lint", Path: "src/app.go", Remediation: policy.FindingRemediation{Summary: "Restore exact inputs first.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "doctor"}, Cwd: "."}}}, repository.RequestedSelection{Mode: "files", Operands: []string{"src"}}, []string{"code-polishy", "doctor"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := policyEngine.withSelection(policyEngine.finish([]policy.Finding{test.finding}, nil), repository.Selection{Requested: test.selection})
			report = policyEngine.normalizeReport(report)
			command := report.Findings[0].Remediation.NextCommand
			if command == nil || !slices.Equal(command.Argv, test.want) {
				t.Fatalf("remediation changed the evaluation boundary: %+v", command)
			}
		})
	}
}

func TestReportViewFiltersWithoutChangingCompleteSummary(t *testing.T) {
	policyEngine := &Engine{}
	report := policyEngine.finish([]policy.Finding{
		{Check: "quality.lint", Path: "src/a.go", Subject: "a", Message: "a"},
		{Check: "architecture.moduleDependency", Path: "src/b.go", Subject: "b", Message: "b"},
	}, nil)
	options := DisplayOptions{Filters: ReportFilters{Rules: []string{"quality.lint"}}, GroupBy: "rule", Limit: 7}
	complete := PrepareReportDisplay(report, options)
	view := ReportView(complete, options)
	if complete.Summary.Errors != 2 || len(complete.Findings) != 2 || len(view.Findings) != 1 || view.Display.Total != 2 || view.Display.Displayed != 1 || view.Display.Omitted != 1 ||
		!HasFindings(complete) || !HasFindings(view) {
		t.Fatalf("complete = %+v view = %+v", complete, view)
	}
	hidden := ReportView(complete, DisplayOptions{Filters: ReportFilters{Rules: []string{"missing.rule"}}})
	if HasFindings(hidden) || hidden.Summary.Errors != 2 || hidden.Display.Total != 2 || hidden.Display.Displayed != 0 {
		t.Fatalf("hidden view = %+v", hidden)
	}
}

func TestFinalizeReportWritesDeterministicManagedDocument(t *testing.T) {
	root := t.TempDir()
	policyEngine := &Engine{Repository: repository.Repository{Root: root}}
	report, err := policyEngine.FinalizeReport("check", policyEngine.finish([]policy.Finding{{
		Check: "quality.lint", Path: "src/app.go", Subject: "unused", Message: "unused",
	}}, nil))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(report.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	decoded := Report{}
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Protocol != ReportProtocol || decoded.Command != "check" || len(decoded.Findings) != 1 {
		t.Fatalf("decoded = %+v error = %v", decoded, err)
	}
	second, err := policyEngine.FinalizeReport("check", policyEngine.finish([]policy.Finding{{
		Check: "quality.lint", Path: "src/app.go", Subject: "unused", Message: "unused",
	}}, nil))
	if err != nil || second.ReportPath != report.ReportPath {
		t.Fatalf("second = %+v error = %v", second, err)
	}
}

func TestReportOutputRejectsEscapesAndSymbolicLinks(t *testing.T) {
	root := t.TempDir()
	policyEngine, err := OpenReportCustodian(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policyEngine.WriteReportOutput("../escape.json", []byte("{}\n")); err == nil {
		t.Fatal("repository escape was accepted")
	}
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte("target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := policyEngine.WriteReportOutput("link.json", []byte("{}\n")); err == nil {
		t.Fatal("symbolic-link output was accepted")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "target\n" {
		t.Fatalf("target changed: %q error=%v", data, err)
	}
}

func TestManagedReportRejectsSymbolicReportRoot(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, ".code-polishy-reports")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	policyEngine, err := OpenReportCustodian(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policyEngine.FinalizeReport("check", Report{}); err == nil {
		t.Fatal("symbolic managed report root was accepted")
	}
}

func TestSARIFCarriesEquivalentFindingAndCanonicalReport(t *testing.T) {
	finding := policy.NormalizeFinding(policy.Finding{
		Check: "quality.lint", Path: "src/app.go", Line: 3, Column: 2, Subject: "unused", Message: "unused", Severity: policy.FindingWarning,
	})
	report := (&Engine{}).normalizeReport(Report{Command: "check", ReportPath: ".code-polishy-reports/check/example/report.json", Findings: []policy.Finding{finding}})
	data, err := SARIF(report)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Version string `json:"version"`
		Runs    []struct {
			Results []struct {
				RuleID              string            `json:"ruleId"`
				Level               string            `json:"level"`
				PartialFingerprints map[string]string `json:"partialFingerprints"`
			} `json:"results"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.Version != "2.1.0" || len(document.Runs) != 1 || len(document.Runs[0].Results) != 1 {
		t.Fatalf("document = %+v error = %v", document, err)
	}
	result := document.Runs[0].Results[0]
	if result.RuleID != finding.Check || result.Level != "warning" || result.PartialFingerprints["codePolishy/v1"] != finding.Fingerprint || !slices.ContainsFunc([]string{"codePolishyReport"}, func(key string) bool { _, found := document.Runs[0].Properties[key]; return found }) {
		t.Fatalf("result = %+v properties = %+v", result, document.Runs[0].Properties)
	}
}

func TestManagedJSONAndSARIFValidateAgainstPinnedSchemas(t *testing.T) {
	root := t.TempDir()
	policyEngine := &Engine{Repository: repository.Repository{Root: root}}
	report, err := policyEngine.FinalizeReport("check", policyEngine.finish([]policy.Finding{{
		Check: "quality.lint", Path: "src/app.go", Line: 3, Column: 2, Subject: "unused", Message: "unused", Severity: policy.FindingWarning,
	}}, nil))
	if err != nil {
		t.Fatal(err)
	}
	jsonData, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaDocument(t, ReportSchemaURL, policyschema.CodePolishyReport, jsonData)
	sarifData, err := SARIF(report)
	if err != nil {
		t.Fatal(err)
	}
	const sarifSchemaSHA256 = "2b19d2358baef0251d7d24e208d05ffabf1b2a3ab5e1b3a816066fc57fd4a7e8"
	digest := sha256.Sum256(policyschema.SARIF210)
	if hex.EncodeToString(digest[:]) != sarifSchemaSHA256 {
		t.Fatalf("SARIF schema digest = %s", hex.EncodeToString(digest[:]))
	}
	validateSchemaDocument(t, sarifSchema, policyschema.SARIF210, sarifData)
}

func TestSourceGraphAndCycleEvidenceValidateAgainstPinnedReportSchema(t *testing.T) {
	node := sourcegraph.Node{Path: "src/app.ts", Language: "typescript", Root: ".", Module: "app", Resolution: "file:src/app.ts"}
	edge := sourcegraph.Edge{
		Source: "src/app.ts", Target: "src/app.ts", SourceResolution: node.Resolution, TargetResolution: node.Resolution,
		Line: 1, Column: 1, Ecosystem: "javascript", Kind: sourcegraph.EdgeProvenDynamic,
	}
	graph, err := sourcegraph.New([]sourcegraph.Node{node}, []sourcegraph.Edge{edge}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	components, err := sourcegraph.CyclicComponents(graph)
	if err != nil || len(components) != 1 {
		t.Fatalf("components = %+v, error = %v", components, err)
	}
	component := components[0]
	report := (&Engine{}).normalizeReport(Report{
		Command: "architecture", ReportPath: ".code-polishy-reports/architecture/example/report.json", SourceDependencyGraph: &graph,
		Findings: []policy.Finding{{
			Check: "architecture.fileCycle", Path: node.Path, Line: 1, Column: 1, Subject: component.Classification,
			Message: "cycle", SemanticIdentity: []string{component.Identity}, Scope: policy.FindingScope{Kind: "graph-component", Value: component.Identity},
			DependencyComponent: &policy.DependencyComponent{
				Protocol: "source-dependency-component/v1", Classification: component.Classification, Identity: component.Identity,
				Members: []policy.DependencyNode{{Path: node.Path, Language: node.Language, Root: node.Root, Module: node.Module, Resolution: node.Resolution}},
				Edges: []policy.DependencyEdge{{
					Source: edge.Source, Target: edge.Target, SourceResolution: edge.SourceResolution, TargetResolution: edge.TargetResolution,
					Line: edge.Line, Column: edge.Column, Ecosystem: edge.Ecosystem, Kind: string(edge.Kind),
				}},
				Witness: []policy.DependencyEdge{{
					Source: edge.Source, Target: edge.Target, SourceResolution: edge.SourceResolution, TargetResolution: edge.TargetResolution,
					Line: edge.Line, Column: edge.Column, Ecosystem: edge.Ecosystem, Kind: string(edge.Kind),
				}},
			},
		}},
	})
	data, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaDocument(t, ReportSchemaURL, policyschema.CodePolishyReport, data)
}

func TestPythonGraphFactEvidenceValidatesAgainstPinnedReportSchema(t *testing.T) {
	graph, err := sourcegraph.New([]sourcegraph.Node{{Path: "app.py", Language: "python", Root: ".", Module: "app", Resolution: "file:app.py"}}, nil,
		[]sourcegraph.FactInput{{Analyzer: "python-facts", Protocol: "python-facts/v3", Project: "pyproject.toml", Root: ".", Paths: []string{"app.py"}, FactsSHA256: strings.Repeat("a", 64), PartitionsSHA256: strings.Repeat("b", 64), ResolutionSHA256: strings.Repeat("c", 64)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	report := (&Engine{}).normalizeReport(Report{Command: "architecture", ReportPath: ".code-polishy-reports/architecture/example/report.json", SourceDependencyGraph: &graph})
	data, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaDocument(t, ReportSchemaURL, policyschema.CodePolishyReport, data)
}

func TestJSONAndSARIFMatchConsumerGoldenFacts(t *testing.T) {
	finding := policy.NormalizeFinding(policy.Finding{
		Check: "quality.lint", Path: "src/app.go", Line: 3, Column: 2, Subject: "unused", Message: "unused", Severity: policy.FindingWarning,
	})
	report := (&Engine{}).normalizeReport(Report{
		Command: "check", ReportPath: ".code-polishy-reports/check/example/report.json", Findings: []policy.Finding{finding},
	})
	jsonData, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	decodedReport := Report{}
	if err := json.Unmarshal(jsonData, &decodedReport); err != nil {
		t.Fatal(err)
	}
	jsonFacts := consumerFactsFromFinding(decodedReport.Findings[0])
	sarifData, err := SARIF(report)
	if err != nil {
		t.Fatal(err)
	}
	var sarifDocument struct {
		Runs []struct {
			Results []struct {
				RuleID              string                     `json:"ruleId"`
				Level               string                     `json:"level"`
				Message             sarifMessage               `json:"message"`
				Locations           []sarifLocation            `json:"locations"`
				PartialFingerprints map[string]string          `json:"partialFingerprints"`
				Properties          map[string]json.RawMessage `json:"properties"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(sarifData, &sarifDocument); err != nil {
		t.Fatal(err)
	}
	result := sarifDocument.Runs[0].Results[0]
	remediation := policy.FindingRemediation{}
	if err := json.Unmarshal(result.Properties["remediation"], &remediation); err != nil {
		t.Fatal(err)
	}
	sarifFacts := reportConsumerFacts{
		RuleID: result.RuleID, Fingerprint: result.PartialFingerprints["codePolishy/v1"], Severity: result.Level,
		Message: result.Message.Text, Path: result.Locations[0].PhysicalLocation.ArtifactLocation.URI,
		Line: result.Locations[0].PhysicalLocation.Region.StartLine, Column: result.Locations[0].PhysicalLocation.Region.StartColumn,
		RemediationSummary: remediation.Summary, NextCommand: remediation.NextCommand.Argv,
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "report-consumer.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, facts := range map[string]reportConsumerFacts{"json": jsonFacts, "sarif": sarifFacts} {
		data, err := json.MarshalIndent(facts, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, '\n')
		if !bytes.Equal(data, golden) {
			t.Fatalf("%s facts differ from golden\nwant: %s\ngot: %s", name, golden, data)
		}
	}
}

type reportConsumerFacts struct {
	RuleID             string   `json:"ruleId"`
	Fingerprint        string   `json:"fingerprint"`
	Severity           string   `json:"severity"`
	Message            string   `json:"message"`
	Path               string   `json:"path"`
	Line               int      `json:"line"`
	Column             int      `json:"column"`
	RemediationSummary string   `json:"remediationSummary"`
	NextCommand        []string `json:"nextCommand"`
}

func consumerFactsFromFinding(finding policy.Finding) reportConsumerFacts {
	return reportConsumerFacts{
		RuleID: finding.Check, Fingerprint: finding.Fingerprint, Severity: string(finding.Severity), Message: finding.Message,
		Path: finding.Path, Line: finding.Line, Column: finding.Column,
		RemediationSummary: finding.Remediation.Summary, NextCommand: finding.Remediation.NextCommand.Argv,
	}
}

func validateSchemaDocument(t *testing.T, url string, schemaData, documentData []byte) {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	schemaDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaData))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource(url, schemaDocument); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(documentData))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(document); err != nil {
		t.Fatal(err)
	}
}
