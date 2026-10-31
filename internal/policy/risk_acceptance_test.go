package policy

import (
	"strings"
	"testing"
	"time"
)

func riskAcceptedAssessment(now time.Time, ecosystem string) VulnerabilityAssessment {
	assessment := highNotAffectedAssessment(now)
	assessment.ID, assessment.Status, assessment.Basis, assessment.ApprovedBy = "accepted-high", RiskAcceptedAssessment, "temporary-no-fix", "A Maintainer"
	if ecosystem == ContainerEcosystem {
		assessment.Ecosystem, assessment.Scope = ContainerEcosystem, "api"
		assessment.Package = "pkg:deb/debian/libcurl4@7.88.1~rc1-10?arch=amd64&distro=debian-12"
		assessment.AffectedVersion = "7.88.1~rc1-10"
	}
	return assessment
}

func containerAssessmentConfig(t *testing.T, assessment VulnerabilityAssessment) string {
	t.Helper()
	target := `"artifactSecurity":{"targets":[{"name":"api","mode":"command","producer":{"argv":["build"]}}]}`
	return strings.Replace(vulnerabilityAssessmentConfig(t, assessment), `"vulnerabilityAssessments":`, target+`,"vulnerabilityAssessments":`, 1)
}

func TestRiskAcceptanceDecisionPath(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for _, ecosystem := range []string{"pnpm", ContainerEcosystem} {
		t.Run(ecosystem, func(t *testing.T) {
			t.Parallel()
			assessment := riskAcceptedAssessment(now, ecosystem)
			assessments := []VulnerabilityAssessment{assessment}
			finding := highNotAffectedFinding(assessment, "high")

			if kept, accepted := ApplyVulnerabilityAssessments([]Finding{finding}, nil, now, true); len(kept) != 1 || len(accepted) != 0 {
				t.Fatalf("unassessed high finding: kept=%+v accepted=%+v", kept, accepted)
			}
			kept, accepted := ApplyVulnerabilityAssessments([]Finding{finding}, assessments, now, true)
			if len(kept) != 0 || len(accepted) != 1 || accepted[0].Finding.Vulnerability.Severity != "high" || accepted[0].Assessment.ApprovedBy == "" {
				t.Fatalf("exact assessment: kept=%+v accepted=%+v", kept, accepted)
			}
			other := highNotAffectedFinding(assessment, "high")
			other.Vulnerability.Advisory, other.Vulnerability.Aliases = "CVE-2026-2000", nil
			kept, accepted = ApplyVulnerabilityAssessments([]Finding{finding, other}, assessments, now, true)
			if len(kept) != 1 || kept[0].Vulnerability == nil || kept[0].Vulnerability.Advisory != "CVE-2026-2000" || len(accepted) != 1 {
				t.Fatalf("uncovered blocker: kept=%+v accepted=%+v", kept, accepted)
			}
		})
	}
}

func TestRiskAcceptanceStopsWhenTheDecisionStopsMatching(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for _, ecosystem := range []string{"pnpm", ContainerEcosystem} {
		for name, testCase := range map[string]struct {
			now    time.Time
			change func(*VulnerabilityIdentity, *VulnerabilityAssessment)
			issue  string
		}{
			"expired":      {now: now.AddDate(0, 0, MaximumHighVulnerabilityDays+1), issue: "policy.vulnerabilityAssessmentExpired"},
			"package":      {change: func(identity *VulnerabilityIdentity, _ *VulnerabilityAssessment) { identity.Package += "-other" }},
			"version":      {change: func(identity *VulnerabilityIdentity, _ *VulnerabilityAssessment) { identity.AffectedVersion = "9.9.9" }},
			"scope":        {change: func(identity *VulnerabilityIdentity, _ *VulnerabilityAssessment) { identity.Scope = "other" }},
			"critical":     {change: func(identity *VulnerabilityIdentity, _ *VulnerabilityAssessment) { identity.Severity = "critical" }, issue: "policy.vulnerabilityAssessmentSeverity"},
			"unknown":      {change: func(identity *VulnerabilityIdentity, _ *VulnerabilityAssessment) { identity.Severity = "unknown" }, issue: "policy.vulnerabilityAssessmentSeverity"},
			"exploited":    {change: func(identity *VulnerabilityIdentity, _ *VulnerabilityAssessment) { identity.KnownExploited = true }, issue: "policy.vulnerabilityAssessmentKnownExploited"},
			"not approved": {change: func(_ *VulnerabilityIdentity, assessment *VulnerabilityAssessment) { assessment.ApprovedBy = "" }, issue: "policy.vulnerabilityAssessmentApproval"},
		} {
			t.Run(ecosystem+"/"+name, func(t *testing.T) {
				t.Parallel()
				assessment := riskAcceptedAssessment(now, ecosystem)
				finding := highNotAffectedFinding(assessment, "high")
				if testCase.change != nil {
					testCase.change(finding.Vulnerability, &assessment)
				}
				current := testCase.now
				if current.IsZero() {
					current = now
				}
				kept, accepted := ApplyVulnerabilityAssessments([]Finding{finding}, []VulnerabilityAssessment{assessment}, current, false)
				if len(accepted) != 0 {
					t.Fatalf("assessment still accepted the finding: %+v", accepted)
				}
				blocked, issued := false, testCase.issue == ""
				for _, item := range kept {
					blocked = blocked || item.Vulnerability != nil
					issued = issued || item.Check == testCase.issue
				}
				if !blocked || !issued {
					t.Fatalf("kept=%+v", kept)
				}
			})
		}
	}
}

func TestExistingRiskAcceptanceWithoutApprovalLoadsButDoesNotApply(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	assessment := riskAcceptedAssessment(now, "pnpm")
	assessment.Severity, assessment.Basis, assessment.ApprovedBy = "moderate", "mitigated", ""
	config, err := Load(writeConfig(t, vulnerabilityAssessmentConfig(t, assessment)), "")
	if err != nil {
		t.Fatalf("assessment predating approvedBy must still load so upgrades can plan: %v", err)
	}
	kept, accepted := ApplyVulnerabilityAssessments([]Finding{highNotAffectedFinding(assessment, "moderate")}, config.SupplyChain.VulnerabilityAssessments, now, true)
	if len(accepted) != 0 || len(kept) != 2 {
		t.Fatalf("kept=%+v accepted=%+v", kept, accepted)
	}
}

func TestContainerAssessmentNamesOneExactHighRiskInADeclaredTarget(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	valid := riskAcceptedAssessment(now, ContainerEcosystem)
	if _, err := Load(writeConfig(t, containerAssessmentConfig(t, valid)), ""); err != nil {
		t.Fatalf("valid container assessment: %v", err)
	}
	for name, mutate := range map[string]func(*VulnerabilityAssessment){
		"non-exposure": func(a *VulnerabilityAssessment) {
			a.Status, a.Basis, a.ApprovedBy = NotAffectedAssessment, "unreachable", ""
		},
		"moderate":     func(a *VulnerabilityAssessment) { a.Severity = "moderate" },
		"critical":     func(a *VulnerabilityAssessment) { a.Severity = "critical" },
		"undeclared":   func(a *VulnerabilityAssessment) { a.Scope = "worker" },
		"scanner":      func(a *VulnerabilityAssessment) { a.Scope = LockFilename },
		"package name": func(a *VulnerabilityAssessment) { a.Package = "libcurl4" },
		"unversioned":  func(a *VulnerabilityAssessment) { a.Package = "pkg:deb/debian/libcurl4" },
		"wildcard":     func(a *VulnerabilityAssessment) { a.Package = "pkg:deb/debian/*@7.88.1" },
		"version":      func(a *VulnerabilityAssessment) { a.AffectedVersion = ">=7" },
		"overlong": func(a *VulnerabilityAssessment) {
			a.Expires = Date{Time: now.AddDate(0, 0, MaximumHighVulnerabilityDays+1)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assessment := valid
			mutate(&assessment)
			if _, err := Load(writeConfig(t, containerAssessmentConfig(t, assessment)), ""); err == nil {
				t.Fatal("inadmissible container assessment loaded")
			}
		})
	}
}

func TestApprovalIsRecordedOnlyForRiskAcceptance(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	assessment := highNotAffectedAssessment(now)
	assessment.ApprovedBy = "A Maintainer"
	if _, err := Load(writeConfig(t, vulnerabilityAssessmentConfig(t, assessment)), ""); err == nil {
		t.Fatal("not-affected assessment recorded an approval it does not need")
	}
}
