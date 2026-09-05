package engine

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestReusedGateRetainsSuppressedAndReviewedOutcomes(t *testing.T) {
	t.Parallel()
	policyEngine := gateWarningCandidate(t, "recommended")
	addGateOutcomeEvidence(policyEngine, time.Now().UTC().Truncate(24*time.Hour))
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	first, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || HasFindings(first) || first.Summary.Suppressed != 1 || first.Summary.Reviewed != 2 {
		t.Fatalf("gate did not retain one outcome per policy occurrence: %+v, error=%v", first, err)
	}
	executed := len(commandRunner.commands)
	reused, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || reused.GateRunPolicy == nil || reused.GateRunPolicy.Status != "already-passed" || len(commandRunner.commands) != executed {
		t.Fatalf("outcome inspection reran the gate: %+v, error=%v", reused, err)
	}
	if !reflect.DeepEqual(reused.Suppressed, first.Suppressed) || !reflect.DeepEqual(reused.Assessed, first.Assessed) || !reflect.DeepEqual(reused.ReleaseAges, first.ReleaseAges) {
		t.Fatalf("gate reuse lost canonical outcome evidence: first=%+v, reused=%+v", first, reused)
	}
	if first.SourceDependencyGraph == nil || !reflect.DeepEqual(reused.SourceDependencyGraph, first.SourceDependencyGraph) {
		t.Fatalf("gate reuse lost source graph evidence: first=%+v, reused=%+v", first.SourceDependencyGraph, reused.SourceDependencyGraph)
	}
	reused, err = policyEngine.FinalizeReport("merge-gate", reused)
	if err != nil {
		t.Fatal(err)
	}
	assertGateOutcomeSerialization(t, reused)
}

func TestReportCoalescesOutcomeOccurrencesWithoutDiscardingEvidence(t *testing.T) {
	t.Parallel()
	policyEngine := &Engine{}
	addGateOutcomeEvidence(policyEngine, time.Now().UTC().Truncate(24*time.Hour))
	first := policyEngine.finish(policyEngine.PolicyModuleFindings, nil)
	second := policyEngine.finish(policyEngine.PolicyModuleFindings, nil)
	second.Suppressed[0].Finding.Related = []policy.FindingLocation{{Path: "content/related.json", Line: 2}}
	combined := policyEngine.combine(first, second)
	if combined.Summary.Suppressed != 1 || combined.Summary.Reviewed != 2 || len(combined.Suppressed[0].Finding.Related) != 1 {
		t.Fatalf("duplicate outcomes inflated counts or lost evidence: %+v", combined)
	}
	distinct := second.Assessed[0]
	distinct.Finding = distinct.Finding.Clone()
	distinct.Finding.Vulnerability.AffectedVersion = "1.2.4"
	distinct.Finding.Subject = policy.VulnerabilitySubject(*distinct.Finding.Vulnerability)
	distinct.Finding.Fingerprint = ""
	distinct.Assessment.ID, distinct.Assessment.AffectedVersion = "next-version", "1.2.4"
	combined = policyEngine.combine(combined, Report{Assessed: []policy.AssessedVulnerability{distinct}})
	if len(combined.Assessed) != 2 || combined.Summary.Reviewed != 3 {
		t.Fatalf("distinct affected versions were coalesced: %+v", combined.Assessed)
	}
}

func assertGateOutcomeSerialization(t *testing.T, report Report) {
	t.Helper()
	data, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Summary.Suppressed != 1 || decoded.Summary.Reviewed != 2 {
		t.Fatalf("JSON outcome counts = %+v, error=%v", decoded.Summary, err)
	}
	if !reflect.DeepEqual(decoded.SourceDependencyGraph, report.SourceDependencyGraph) {
		t.Fatal("JSON omitted source graph evidence")
	}
	data, err = SARIF(report)
	if err != nil {
		t.Fatal(err)
	}
	var sarif struct {
		Runs []struct {
			Results []struct {
				Suppressions []struct {
					Status string `json:"status"`
				} `json:"suppressions"`
			} `json:"results"`
			Properties struct {
				Report Report `json:"codePolishyReport"`
			} `json:"properties"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &sarif); err != nil || len(sarif.Runs) != 1 {
		t.Fatalf("SARIF outcomes = %+v, error=%v", sarif, err)
	}
	retained := sarif.Runs[0].Properties.Report
	if !reflect.DeepEqual(retained.SourceDependencyGraph, report.SourceDependencyGraph) {
		t.Fatal("SARIF omitted source graph evidence")
	}
	if retained.Summary.Suppressed != 1 || retained.Summary.Reviewed != 2 || !reflect.DeepEqual(retained.Suppressed, decoded.Suppressed) || !reflect.DeepEqual(retained.Assessed, decoded.Assessed) || !reflect.DeepEqual(retained.ReleaseAges, decoded.ReleaseAges) {
		t.Fatalf("SARIF lost canonical outcome evidence: %+v", retained)
	}
	accepted := 0
	for _, result := range sarif.Runs[0].Results {
		for _, suppression := range result.Suppressions {
			if suppression.Status == "accepted" {
				accepted++
			}
		}
	}
	if accepted != 3 {
		t.Fatalf("SARIF omitted suppressed or reviewed results: %d accepted suppressions", accepted)
	}
}

func addGateOutcomeEvidence(policyEngine *Engine, now time.Time) {
	expires := policy.Date{Time: now.AddDate(0, 0, 7)}
	policyEngine.Repository.Config.Exceptions = []policy.Exception{{
		ID: "lint", Check: "quality.lint", Path: "content/data.json", Subject: "fixture", Reason: "Reviewed fixture content.", Owner: "content", Expires: expires,
	}}
	vulnerability := policy.VulnerabilityIdentity{
		Ecosystem: "pnpm", Advisory: "GHSA-abcd-1234-5678", Aliases: []string{"CVE-2026-1000"}, Package: "example", AffectedVersion: "1.2.3", Scope: "content/data.json", Severity: "low",
	}
	policyEngine.Repository.Config.SupplyChain.VulnerabilityAssessments = []policy.VulnerabilityAssessment{{
		ID: "dependency-review", Ecosystem: vulnerability.Ecosystem, Advisory: vulnerability.Advisory, Package: vulnerability.Package,
		AffectedVersion: vulnerability.AffectedVersion, Scope: vulnerability.Scope, Severity: "low", Status: "not-affected", Basis: "unreachable",
		Reason: "The affected path is unreachable.", Impact: "The affected feature is not shipped.", Evidence: "https://example.test/evidence",
		Tracking: "https://example.test/tracking", Owner: "content", ApprovedBy: "security", Approval: "https://example.test/approval",
		Reviewed: policy.Date{Time: now}, Expires: expires,
	}}
	age := policy.ReleaseAgeIdentity{Ecosystem: "pnpm", Package: "example", Version: "1.2.3", Scope: "content/data.json", Released: now.AddDate(0, 0, -10), Eligible: now.AddDate(0, 0, 20)}
	policyEngine.Repository.Config.SupplyChain.ReleaseAgeAssessments = []policy.ReleaseAgeAssessment{{
		ID: "supported-release", Ecosystem: age.Ecosystem, Package: age.Package, Version: age.Version, Scope: age.Scope, Category: "supported-release",
		Evidence: "https://example.test/support", Reason: "Required supported release.", Owner: "content", Reviewed: policy.Date{Time: now}, Expires: expires,
	}}
	policyEngine.PolicyModuleFindings = append(policyEngine.PolicyModuleFindings,
		policy.Finding{Check: "quality.lint", Path: "content/data.json", Subject: "fixture", Message: "Fixture lint finding."},
		policy.Finding{Check: "supplyChain.osvVulnerability", Path: vulnerability.Scope, Subject: policy.VulnerabilitySubject(vulnerability), Message: "Observed dependency vulnerability.", Vulnerability: &vulnerability},
		policy.Finding{Check: "supplyChain.releaseAge", Path: age.Scope, Subject: policy.ReleaseAgeSubject(age), Message: "Observed young release.", ReleaseAge: &age},
	)
}
