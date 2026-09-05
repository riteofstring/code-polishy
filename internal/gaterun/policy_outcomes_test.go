package gaterun

import (
	"errors"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGateReportRetainsIndependentPolicyOutcomeEvidence(t *testing.T) {
	t.Parallel()
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, t.TempDir(), identity)
	recordAttempt(t, run, 0, Passed, 0, "ok", "", 16)
	options := policyOutcomeFinalization(identity)
	prepared, err := run.PrepareFinalization(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Suppressed[0].Finding.Remediation.NextCommand.Argv[0] = "altered"
	options.Assessed[0].Finding.Vulnerability.Aliases[0] = "altered"
	options.ReleaseAges[0].Finding.Fields["package"] = "altered"
	committed, err := prepared.Commit()
	if err != nil {
		t.Fatal(err)
	}
	if committed.Suppressed[0].Finding.Remediation.NextCommand.Argv[0] != "code-polishy" || committed.Assessed[0].Finding.Vulnerability.Aliases[0] != "CVE-2026-1234" || committed.ReleaseAges[0].Finding.Fields["package"] != "example" {
		t.Fatalf("caller mutation changed prepared policy outcomes: %+v", committed)
	}
	loaded, err := LoadReport(run.repositoryRoot, identity)
	if err != nil || len(loaded.Suppressed) != 1 || len(loaded.Assessed) != 1 || len(loaded.ReleaseAges) != 1 {
		t.Fatalf("loaded policy outcomes = %+v, error = %v", loaded, err)
	}
	if loaded.Suppressed[0].Exception.ID != "lint" || loaded.Assessed[0].Assessment.ID != "vulnerability" || loaded.ReleaseAges[0].Assessment.ID != "young-release" {
		t.Fatalf("gate lost assessment identities: %+v", loaded)
	}
}

func TestGateReportRejectsMissingOrInconsistentPolicyOutcomes(t *testing.T) {
	t.Parallel()
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, t.TempDir(), identity)
	recordAttempt(t, run, 0, Passed, 0, "ok", "", 16)
	report, err := run.Finalize(policyOutcomeFinalization(identity))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Report){
		func(report *Report) { report.Suppressed = nil },
		func(report *Report) { report.Assessed = nil },
		func(report *Report) { report.ReleaseAges = nil },
		func(report *Report) { report.Suppressed[0].Finding.Status = policy.FindingOpen },
		func(report *Report) { report.Assessed[0].Finding.Status = policy.FindingSuppressed },
		func(report *Report) { report.ReleaseAges[0].Finding.Line = -1 },
	} {
		changed := cloneReport(report)
		mutate(&changed)
		if err := validateReport(changed, identity); !errors.Is(err, ErrInvalidArtifact) {
			t.Fatalf("accepted malformed policy outcomes: %v", err)
		}
	}
	changed := cloneReport(report)
	changed.Assessed[0].Assessment.Evidence = "altered evidence"
	digest, err := reportDigest(changed)
	if err != nil || digest == report.SHA256 {
		t.Fatalf("assessment evidence was not bound to the report digest: %s, %v", digest, err)
	}
}

func policyOutcomeFinalization(identity Identity) FinalizeOptions {
	return FinalizeOptions{
		Status: RunPassed, BehaviorReview: identity.BehaviorReview,
		Suppressed: []policy.Suppressed{{
			Finding: policy.Finding{Check: "quality.lint", Status: policy.FindingSuppressed,
				Remediation: policy.FindingRemediation{NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "check"}, Cwd: "."}}},
			Exception: policy.Exception{ID: "lint"},
		}},
		Assessed: []policy.AssessedVulnerability{{
			Finding: policy.Finding{Check: "supplyChain.vulnerability", Status: policy.FindingReviewed,
				Vulnerability: &policy.VulnerabilityIdentity{Advisory: "GHSA-2345-6789-abcd", Aliases: []string{"CVE-2026-1234"}}},
			Assessment: policy.VulnerabilityAssessment{ID: "vulnerability"},
		}},
		ReleaseAges: []policy.AssessedReleaseAge{{
			Finding:    policy.Finding{Check: "supplyChain.releaseAge", Status: policy.FindingReviewed, Fields: map[string]string{"package": "example"}},
			Assessment: policy.ReleaseAgeAssessment{ID: "young-release"},
		}},
	}
}
