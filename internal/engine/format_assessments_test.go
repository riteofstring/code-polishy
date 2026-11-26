package engine

import (
	"slices"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestFormatIgnoresUnrelatedAssessmentExpiryButSecurityStillEnforcesIt(t *testing.T) {
	policyEngine, _, _ := generatedFormatEngine(t)
	writeEngineFile(t, policyEngine.Repository.Root, "README.md", "# Example\n\nText.\n", 0o600)
	selection := repository.Selection{Files: []string{"README.md"}}
	for _, expiry := range []time.Time{time.Now().Add(24 * time.Hour), time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)} {
		policyEngine.Repository.Config.SupplyChain.VulnerabilityAssessments = []policy.VulnerabilityAssessment{{ID: "unrelated", Expires: policy.Date{Time: expiry}}}
		policyEngine.Repository.Config.SupplyChain.ReleaseAgeAssessments = []policy.ReleaseAgeAssessment{{ID: "unrelated", Expires: policy.Date{Time: expiry}}}
		policyEngine.Repository.Config.Exceptions = []policy.Exception{{ID: "unrelated", Check: "quality.lint", Path: "other.go", Subject: "unused", Expires: policy.Date{Time: expiry}}}
		report := policyEngine.Format(t.Context(), selection)
		if HasFindings(report) || report.Formatting == nil || report.Formatting.Unchanged != 1 || report.Security != nil {
			t.Fatalf("format at expiry %v = %+v", expiry, report)
		}
	}
	report, err := policyEngine.SupplyChain(t.Context(), true)
	if err != nil || !HasFindings(report) || !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool { return finding.Check == "policy.vulnerabilityAssessmentExpired" }) {
		t.Fatalf("security accepted expired assessment: %+v, %v", report.Findings, err)
	}
}

func TestFormatRetainsApplicableExceptionExpiry(t *testing.T) {
	policyEngine, _, _ := generatedFormatEngine(t)
	writeEngineFile(t, policyEngine.Repository.Root, "broken.go", "package sample\nfunc broken(\n", 0o600)
	selection := repository.Selection{Files: []string{"broken.go"}}
	report := policyEngine.Format(t.Context(), selection)
	if len(report.Findings) != 1 || report.Findings[0].Check != "quality.format" {
		t.Fatalf("formatter failure = %+v", report.Findings)
	}
	finding := report.Findings[0]
	policyEngine.Repository.Config.Exceptions = []policy.Exception{{ID: "format", Check: finding.Check, Path: finding.Path, Subject: finding.Subject, Expires: policy.Date{Time: time.Now().Add(24 * time.Hour)}}}
	report = policyEngine.Format(t.Context(), selection)
	if HasFindings(report) || len(report.Suppressed) != 1 || report.Suppressed[0].Finding.Status != policy.FindingSuppressed {
		t.Fatalf("applicable format exception = %+v", report)
	}
	policyEngine.Repository.Config.Exceptions[0].Expires = policy.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	report = policyEngine.Format(t.Context(), selection)
	if !HasFindings(report) || !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool { return finding.Check == "policy.exceptionExpired" }) {
		t.Fatalf("expired formatting exception = %+v", report)
	}
}
