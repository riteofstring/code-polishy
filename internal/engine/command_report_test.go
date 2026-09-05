package engine

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	policyschema "github.com/riteofstring/code-polishy/schema"
)

func TestFailedCommandsRetainFindingsInManagedJSONAndSARIF(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		format   bool
		security bool
		rule     string
	}{
		{name: "configured check", rule: "quality.command"},
		{name: "formatter", format: true, rule: "quality.format"},
		{name: "security provider", security: true, rule: "policy.securityProvider"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			policyEngine := &Engine{
				Repository: repository.Repository{Root: root, PolicyRoot: enginePolicyRoot(t)},
				Runner:     runner.OSRunner{Stdout: io.Discard, Stderr: io.Discard},
			}
			command := "check"
			var report Report
			if test.format {
				command = "format"
				writeEngineFile(t, root, "broken.go", "package sample\nfunc broken(\n", 0o600)
				report = policyEngine.Format(t.Context(), repository.Selection{Files: []string{"broken.go"}})
			} else {
				check := policy.Command{Name: "missing-revision", Argv: []string{"git", "rev-parse", "--verify", "missing-revision"}, Cwd: ".", TimeoutSeconds: 30}
				if test.security {
					check.Provides = []string{"security"}
				}
				policyEngine.Repository.Config.Checks = []policy.Command{check}
				var err error
				report, err = policyEngine.CheckNamed(t.Context(), []string{check.Name})
				if err != nil {
					t.Fatal(err)
				}
			}
			verifyFailedCommandReport(t, policyEngine, command, test.rule, report)
		})
	}
}

func verifyFailedCommandReport(t *testing.T, policyEngine *Engine, command, rule string, report Report) {
	t.Helper()
	if len(report.Findings) != 1 || report.Findings[0].Check != rule || !HasFindings(report) {
		t.Fatalf("failed command report = %+v", report)
	}
	report, err := policyEngine.FinalizeReport(command, report)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(policyEngine.Repository.Root, filepath.FromSlash(report.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaDocument(t, ReportSchemaURL, policyschema.CodePolishyReport, data)
	var stored Report
	if err := json.Unmarshal(data, &stored); err != nil || len(stored.Findings) != 1 || stored.Findings[0].Check != rule || stored.Summary.Errors != 1 {
		t.Fatalf("stored report = %+v, error = %v", stored, err)
	}
	encoded, err := SARIF(report)
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaDocument(t, sarifSchema, policyschema.SARIF210, encoded)
	var sarif sarifLog
	if err := json.Unmarshal(encoded, &sarif); err != nil || len(sarif.Runs) != 1 || len(sarif.Runs[0].Results) != 1 || sarif.Runs[0].Results[0].RuleID != rule || sarif.Runs[0].Results[0].Level != "error" {
		t.Fatalf("SARIF = %+v, error = %v", sarif, err)
	}
}
