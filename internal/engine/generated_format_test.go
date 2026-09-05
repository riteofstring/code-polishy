package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	policyschema "github.com/riteofstring/code-polishy/schema"
)

func TestFormatProtectsGeneratedOutputAndReportsActualHandwrittenRewrites(t *testing.T) {
	t.Parallel()
	policyEngine, generated, original := generatedFormatEngine(t)
	writeEngineFile(t, policyEngine.Repository.Root, "app/main.ts", "export const value={  answer:42}\n", 0o600)
	selection := repository.Selection{Files: []string{"app/main.ts", generated}}
	report := policyEngine.Format(t.Context(), selection)
	if HasFindings(report) || report.Formatting == nil || report.Formatting.Rewritten != 1 || report.Formatting.Protected != 1 || report.Formatting.Unchanged != 0 {
		t.Fatalf("format result = %+v, findings = %+v", report.Formatting, report.Findings)
	}
	data, err := os.ReadFile(filepath.Join(policyEngine.Repository.Root, generated))
	if err != nil || string(data) != original {
		t.Fatalf("generated content changed: %q, %v", data, err)
	}
	protected := report.Formatting.Files[1]
	if protected.Path != generated || protected.Producer != "contracts" || !protected.StyleExempt || protected.State != "protected" {
		t.Fatalf("protected result = %+v", protected)
	}
	if _, err := os.Stat(filepath.Join(policyEngine.Repository.Root, "producer-ran")); !os.IsNotExist(err) {
		t.Fatalf("format executed the producer: %v", err)
	}
	report, err = policyEngine.FinalizeReport("format", report)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaDocument(t, ReportSchemaURL, policyschema.CodePolishyReport, encoded)
}

func TestGeneratedOnlyFormatRequiresOwnershipAndNoFormatterInstallation(t *testing.T) {
	t.Parallel()
	policyEngine, generated, _ := generatedFormatEngine(t)
	policyEngine.Repository.PolicyRoot = t.TempDir()
	selection := repository.Selection{Files: []string{generated}}
	report := policyEngine.Format(t.Context(), selection)
	if HasFindings(report) || report.Formatting == nil || report.Formatting.Protected != 1 || report.Formatting.Rewritten != 0 {
		t.Fatalf("generated-only format = %+v", report)
	}
	policyEngine.Repository.Config.Generation.Producers = nil
	report = policyEngine.Format(t.Context(), selection)
	if !HasFindings(report) || report.Formatting != nil || !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool { return finding.Check == "policy.generationOwnership" }) {
		t.Fatalf("unowned generated output received protection evidence: %+v", report)
	}
}

func TestGeneratedFindingsIdentifyProducerInputsCommandsAndUnavailablePrerequisites(t *testing.T) {
	t.Parallel()
	policyEngine, generated, _ := generatedFormatEngine(t)
	policyEngine.Repository.Config.Generation.Producers[0].Generate.Argv = []string{"unavailable-code-polishy-test-generator"}
	report := policyEngine.finish([]policy.Finding{{Check: "quality.typecheck", Path: generated, Subject: "undefined", Message: "undefined symbol"}}, nil)
	finding := report.Findings[0]
	remediation := finding.Remediation.Generation
	if finding.GeneratedProducer != "contracts" || remediation == nil || !slices.Equal(remediation.Inputs, []string{"source/schema.json"}) || len(remediation.Prerequisites) != 1 || remediation.Prerequisites[0].Operation != "generate" {
		t.Fatalf("generated producer = %q, remediation = %+v", finding.GeneratedProducer, remediation)
	}
	if finding.Remediation.NextCommand != nil || strings.Contains(finding.Remediation.Summary, "code-polishy format") || !slices.Equal(remediation.Verify.Argv, []string{"sh", "scripts/verify.sh"}) {
		t.Fatalf("generated repair instruction = %+v", finding.Remediation)
	}
	report, err := policyEngine.FinalizeReport("check", report)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaDocument(t, ReportSchemaURL, policyschema.CodePolishyReport, encoded)
	copy := finding.Clone()
	copy.Remediation.Generation.Generate.Argv[0] = "changed"
	copy.Remediation.Generation.Inputs[0] = "changed"
	if remediation.Generate.Argv[0] == "changed" || remediation.Inputs[0] == "changed" {
		t.Fatal("copy changed the original generated evidence")
	}
}

func generatedFormatEngine(t *testing.T) (*Engine, string, string) {
	t.Helper()
	root := t.TempDir()
	generated := "app/value.generated.ts"
	original := "export const value={  answer:42}\n"
	writeEngineFile(t, root, generated, original, 0o600)
	writeEngineFile(t, root, "source/schema.json", "{\"answer\":42}\n", 0o600)
	writeEngineFile(t, root, "scripts/generate.sh", "touch producer-ran\n", 0o600)
	writeEngineFile(t, root, "scripts/verify.sh", "touch producer-ran\n", 0o600)
	generate := policy.GenerationCommand{Argv: []string{"sh", "scripts/generate.sh"}, Cwd: ".", TimeoutSeconds: 900}
	verify := policy.GenerationCommand{Argv: []string{"sh", "scripts/verify.sh"}, Cwd: ".", TimeoutSeconds: 900}
	config := policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"app/**"}}}, ModuleByName: map[string]int{"app": 0},
		Generation: policy.Generation{Producers: []policy.GenerationProducer{{Name: "contracts", Inputs: []string{"source/schema.json"}, Outputs: []string{generated}, Generate: generate, Verify: verify}}},
	}
	repo := repository.Repository{Root: root, PolicyRoot: enginePolicyRoot(t), Config: config}
	return &Engine{Repository: repo, Runner: runner.OSRunner{}}, generated, original
}
