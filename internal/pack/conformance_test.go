package pack

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type conformanceTestExecutor struct {
	reference string
	candidate string
	mutation  string
}

func (executor conformanceTestExecutor) Run(_ context.Context, executable, root string, _ []string, _ int) (conformanceExecution, error) {
	findings := []map[string]any{conformanceTestFinding()}
	coverage := []string{"src/main.go"}
	candidate := filepath.Base(executable) == filepath.Base(executor.candidate)
	if candidate {
		switch executor.mutation {
		case "diagnostic":
			findings = []map[string]any{}
		case "coverage":
			coverage = []string{}
		case "write":
			if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("changed\n"), 0o644); err != nil {
				return conformanceExecution{}, err
			}
		}
	}
	report, err := json.Marshal(conformanceTestReport(root, findings, coverage, candidate))
	if err != nil {
		return conformanceExecution{}, err
	}
	return conformanceExecution{ExitStatus: 1, Stdout: report}, nil
}

func TestConformanceRunnerProvesReproducibilityAndDetectsSemanticLoss(t *testing.T) {
	ledgerPath := writeConformanceTestLedger(t)
	reference := writeConformanceTestExecutable(t, "reference")
	candidate := writeConformanceTestExecutable(t, "candidate")
	for _, test := range []struct {
		name     string
		mutation string
		paths    []string
		failure  string
	}{
		{name: "reference parity"},
		{name: "diagnostic loss", mutation: "diagnostic", paths: []string{"/report/findings/0"}, failure: "required rule quality.seeded was absent"},
		{name: "coverage loss", mutation: "coverage", paths: []string{"/report/analysisContext/0/paths/0"}, failure: "required path src/main.go was absent"},
		{name: "write safety", mutation: "write", paths: []string{"/after/1/sha256"}, failure: "protected[src/main.go]: file identity changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := conformanceTestExecutor{reference: reference, candidate: candidate, mutation: test.mutation}
			report, err := runConformance(context.Background(), ConformanceOptions{LedgerPath: ledgerPath, ReferenceExecutable: reference, CandidateExecutable: candidate}, executor)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Fixtures) != 1 {
				t.Fatalf("fixtures = %+v", report.Fixtures)
			}
			evidence := report.Fixtures[0]
			if test.mutation == "" {
				if report.Summary.Status != "passed" || evidence.Status != "passed" || len(evidence.Differences) != 0 || len(evidence.AssertionFailures) != 0 {
					t.Fatalf("reference parity failed: %+v", report)
				}
				return
			}
			if report.Summary.Status != "failed" || evidence.Status != "failed" {
				t.Fatalf("mutation passed: summary=%+v evidence=%+v", report.Summary, evidence)
			}
			for _, path := range test.paths {
				if !slices.ContainsFunc(evidence.Differences, func(difference ConformanceDifference) bool { return difference.Path == path }) {
					t.Fatalf("missing difference %s: %+v", path, evidence.Differences)
				}
			}
			if !slices.ContainsFunc(evidence.AssertionFailures, func(failure string) bool { return strings.Contains(failure, test.failure) }) {
				t.Fatalf("missing assertion %q: %v", test.failure, evidence.AssertionFailures)
			}
		})
	}
}

func TestConformanceReportSchemaAcceptsSkippedFixture(t *testing.T) {
	ledgerPath := writeConformanceTestLedger(t)
	fixturePath := filepath.Join(filepath.Dir(ledgerPath), "fixtures", "seeded.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := map[string]any{}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture["platforms"] = []any{"unsupported-test-platform"}
	data, err = json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	reference := writeConformanceTestExecutable(t, "reference")
	candidate := writeConformanceTestExecutable(t, "candidate")
	report, err := runConformance(context.Background(), ConformanceOptions{LedgerPath: ledgerPath, ReferenceExecutable: reference, CandidateExecutable: candidate}, conformanceTestExecutor{reference: reference, candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Skipped != 1 || len(report.Fixtures) != 1 || report.Fixtures[0].SkipReason == "" {
		t.Fatalf("skipped report = %+v", report)
	}
}

func TestConformanceLedgerRejectsBrokenFixtureLinksWithFieldPath(t *testing.T) {
	ledgerPath := writeConformanceTestLedger(t)
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	ledger := map[string]any{}
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatal(err)
	}
	behavior := ledger["behaviors"].([]any)[0].(map[string]any)
	behavior["fixtures"] = []any{"missing-fixture"}
	data, err = json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConformanceLedger(ledgerPath); err == nil || !strings.Contains(err.Error(), "fixtures[0].behaviorIds[0]") {
		t.Fatalf("broken fixture link error = %v", err)
	}
}

func writeConformanceTestLedger(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "fixtures"), 0o700); err != nil {
		t.Fatal(err)
	}
	configuration := "{\"version\":4,\"project\":{\"kind\":\"service\"},\"modules\":[{\"name\":\"application\",\"paths\":[\"src/**\"]}],\"tests\":{\"ownership\":[],\"suites\":[]}}\n"
	source := "package sample\n"
	fixture := ConformanceFixture{
		Protocol:       ConformanceFixtureProtocol,
		ID:             "seeded-diagnostic",
		BehaviorIDs:    []string{"go.lint.seeded"},
		Files:          []ConformanceFixtureFile{{Path: ".code-polishy.json", Mode: "0644", Content: &configuration}, {Path: "src/main.go", Mode: "0644", Content: &source}},
		Arguments:      []string{"check", "--all", "--format", "json"},
		TimeoutSeconds: 30,
		Platforms:      []string{CurrentPlatform()},
		Expected: ConformanceExpectedOutcome{
			ExitStatus:       1,
			ReportStatus:     "failed",
			RequiredRules:    []string{"quality.seeded"},
			RequiredCoverage: []string{"src/main.go"},
			Writes:           []ConformanceExpectedFile{},
			Protected:        []string{".code-polishy.json", "src/main.go"},
		},
	}
	fixtureData, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixtures", "seeded.json"), fixtureData, 0o600); err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	ledger := ConformanceLedger{
		Protocol:      ConformanceLedgerProtocol,
		TaskBase:      revision,
		LockedRelease: "0.27.8",
		References: ConformanceReferences{
			CurrentMain:      ConformanceReference{Revision: revision, Version: "0.27.8"},
			ImmutableOrigin:  ConformanceReference{Revision: strings.Repeat("b", 40), Version: "0.25.0"},
			OptionalProvider: ConformanceReference{Revision: strings.Repeat("c", 40), Version: "0.25.0"},
		},
		FixtureFiles: []string{"fixtures/seeded.json"},
		Behaviors: []ConformanceBehavior{{
			ID:                    "go.lint.seeded",
			Language:              "go",
			Capability:            "lint",
			Guarantee:             "A seeded lint defect produces its stable rule.",
			CurrentImplementation: []string{"internal/quality/quality.go"},
			Helpers:               []string{},
			CLIRoutes:             []string{"check"},
			PolicyKeys:            []string{"checks"},
			Activation:            []string{"selected Go source"},
			Documentation:         []string{"docs/policies/code-quality.md"},
			Tests:                 []string{"TestConformanceRunnerProvesReproducibilityAndDetectsSemanticLoss"},
			FutureOwner:           "pack",
			BoundaryRationale:     "The pack owns Go syntax while core owns finding policy.",
			Fixtures:              []string{"seeded-diagnostic"},
			Commands:              []string{"check"},
			Profiles:              []string{"check"},
			Modes:                 []string{"check"},
			Platforms:             []string{CurrentPlatform()},
			Status:                "passing",
		}},
	}
	ledgerData, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(root, "ledger.json")
	if err := os.WriteFile(ledgerPath, ledgerData, 0o600); err != nil {
		t.Fatal(err)
	}
	return ledgerPath
}

func writeConformanceTestExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(name), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func conformanceTestReport(root string, findings []map[string]any, coverage []string, candidate bool) map[string]any {
	duration := 10
	if candidate {
		duration = 900
	}
	return map[string]any{
		"$schema":         "https://code-polishy.dev/schema/code-polishy-report.schema.json",
		"protocol":        "code-polishy-report/v1",
		"command":         "check",
		"reportPath":      filepath.Join(root, ".code-polishy-reports", "check", "run", "report.json"),
		"analysisContext": []map[string]any{{"analyzer": "go-lint", "root": root, "reason": "selected source", "paths": coverage}},
		"execution": map[string]any{
			"evaluationDurationMilliseconds": duration,
			"scope":                          map[string]any{"requestedOperands": 1, "selectedPaths": 1, "contextPaths": 1, "graphNodes": 0, "graphEdges": 0},
			"phases":                         []map[string]any{{"name": "quality", "durationMilliseconds": duration}},
			"commands":                       []any{},
			"caches":                         []map[string]any{{"name": "repository-path-facts", "scope": "repository", "hits": duration, "misses": 1, "builds": 1}},
		},
		"summary": map[string]any{
			"status": "failed", "errors": len(findings), "warnings": 0, "information": 0, "suppressed": 0, "reviewed": 0,
			"bySelectionRelation": map[string]any{"selected": len(findings)}, "byRule": map[string]any{"quality.seeded": len(findings)}, "byModule": map[string]any{"application": len(findings)},
		},
		"testCommands": []any{}, "testDiagnostics": []any{}, "testAggregations": []any{}, "findings": findings, "suppressed": []any{},
		"vulnerabilityAssessments": []any{}, "releaseAgeAssessments": []any{}, "tables": []any{}, "notes": []any{},
	}
}

func conformanceTestFinding() map[string]any {
	return map[string]any{
		"ruleId": "quality.seeded", "fingerprint": strings.Repeat("d", 64), "severity": "error", "status": "open",
		"scope": map[string]any{"kind": "path", "value": "src/main.go"}, "selectionRelation": "selected", "path": "src/main.go",
		"line": 1, "column": 1, "module": "application", "subject": "seeded", "message": "seeded defect",
		"remediation": map[string]any{"summary": "remove the seeded defect"},
	}
}
