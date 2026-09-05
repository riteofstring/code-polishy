package policy

import (
	"slices"
	"strings"
	"testing"
)

func TestPackFindingsRetainCapabilitySpecificRecoveryAndIdentity(t *testing.T) {
	for _, test := range []struct {
		capability string
		argv       []string
	}{
		{"lint", []string{"code-polishy", "check", "--files", "app/source.ex"}},
		{"architecture", []string{"code-polishy", "architecture", "--files", "app/source.ex"}},
		{"lock-sync", []string{"code-polishy", "supply-chain"}},
		{"security", []string{"code-polishy", "supply-chain"}},
		{"build", []string{"code-polishy", "verify"}},
	} {
		t.Run(test.capability, func(t *testing.T) {
			rule := "pack." + test.capability
			finding := NormalizeFinding(Finding{Check: rule, Path: "app/source.ex", Subject: "adapter diagnostic"})
			if finding.Check != rule || strings.HasPrefix(finding.Remediation.Summary, "Resolve ") || finding.Remediation.NextCommand == nil || !slices.Equal(finding.Remediation.NextCommand.Argv, test.argv) {
				t.Fatalf("pack recovery lost capability or rule identity: %+v", finding)
			}
		})
	}
}

func TestNormalizeFindingCreatesStableSemanticIdentityAndRemediation(t *testing.T) {
	first := NormalizeFinding(Finding{
		Check: "quality.lint", Path: `src\app.go`, Line: 7, Column: 3, Subject: "unused", Message: "first wording",
	})
	second := NormalizeFinding(Finding{
		Check: "quality.lint", Path: "src/app.go", Line: 7, Column: 3, Subject: "unused", Message: "different wording",
	})
	if first.Fingerprint == "" || first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprints = %q and %q", first.Fingerprint, second.Fingerprint)
	}
	if first.Severity != FindingError || first.Status != FindingOpen || first.Scope != (FindingScope{Kind: "path", Value: "src/app.go"}) {
		t.Fatalf("normalized finding = %+v", first)
	}
	want := []string{"code-polishy", "check", "--files", "src/app.go"}
	if first.Remediation.NextCommand == nil || !slices.Equal(first.Remediation.NextCommand.Argv, want) || first.Remediation.NextCommand.Cwd != "." {
		t.Fatalf("remediation = %+v", first.Remediation)
	}
}

func TestExplicitRecoverySummaryStillProvidesItsOwningRerun(t *testing.T) {
	summary := "Obtain signed evidence from the authorized provider before retrying."
	finding := NormalizeFinding(Finding{Check: "supplyChain.gitEvidence", Path: "uv.lock", Remediation: FindingRemediation{Summary: summary}})
	if finding.Remediation.Summary != summary || finding.Remediation.NextCommand == nil || !slices.Equal(finding.Remediation.NextCommand.Argv, []string{"code-polishy", "supply-chain"}) {
		t.Fatalf("provider recovery lost its instruction or rerun: %+v", finding.Remediation)
	}
}

func TestNormalizeFindingUsesRuleOwnedSemanticComponentIdentity(t *testing.T) {
	left := NormalizeFinding(Finding{
		Check: "architecture.fileCycle", Path: "src/a.py", Line: 1, Subject: "cycle",
		SemanticIdentity: []string{"src/a.py", "src/b.py"},
	})
	right := NormalizeFinding(Finding{
		Check: "architecture.fileCycle", Path: "src/b.py", Line: 20, Subject: "another witness",
		SemanticIdentity: []string{"src/b.py", "src/a.py"},
	})
	if left.Fingerprint != right.Fingerprint {
		t.Fatalf("semantic fingerprints = %q and %q", left.Fingerprint, right.Fingerprint)
	}
}

func TestFindingFingerprintChangesWithOwnerAndGeneratedProducer(t *testing.T) {
	base := Finding{Check: "quality.format", Path: "generated/out.go", Module: "generator", Subject: "output", Message: "message"}
	first := NormalizeFinding(base)
	changedModule := base
	changedModule.Module = "application"
	changedProducer := base
	changedProducer.GeneratedProducer = "generate-output"
	if first.Fingerprint == NormalizeFinding(changedModule).Fingerprint || first.Fingerprint == NormalizeFinding(changedProducer).Fingerprint {
		t.Fatal("owner or generated producer did not change the semantic fingerprint")
	}
}

func TestArtifactFinalizationFailureDoesNotOfferExecutionIDAsSuite(t *testing.T) {
	finding := NormalizeFinding(Finding{
		Check: "test.artifacts", Path: ".code-polishy-test-artifacts", Subject: "execution-20260905", Message: "cannot publish completed manifest: permission denied",
	})
	command := finding.Remediation.NextCommand
	if command == nil || !slices.Equal(command.Argv, []string{"code-polishy", "test-plan"}) || command.Cwd != "." {
		t.Fatalf("artifact recovery invents an executable suite: %+v", finding.Remediation)
	}
}

func TestFindingRerunsTheAnalyzerThatOwnsItsRule(t *testing.T) {
	for _, test := range []struct {
		rule    string
		command string
	}{
		{"portability.machinePath", "check"},
		{"portability.siblingReference", "check"},
		{"testing.fileCycle", "architecture"},
	} {
		t.Run(test.rule, func(t *testing.T) {
			finding := NormalizeFinding(Finding{Check: test.rule, Path: "checks/contract_test.go", Subject: "diagnostic"})
			want := []string{"code-polishy", test.command, "--files", "checks/contract_test.go"}
			if finding.Remediation.NextCommand == nil || !slices.Equal(finding.Remediation.NextCommand.Argv, want) {
				t.Fatalf("rerun cannot reproduce the selected finding: %+v", finding.Remediation)
			}
		})
	}
}
