package engine

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type securityRefreshRecord struct {
	GateSHA256 string `json:"gateSha256"`
	Report     Report `json:"report"`
}

func defaultFindingPhase(findings []policy.Finding, phase policy.VerificationPhase) {
	for index := range findings {
		findings[index].VerificationPhase = cmp.Or(findings[index].VerificationPhase, phase)
	}
}

func (engine *Engine) refreshGateSecurity(ctx context.Context, controller *gateRunController, report Report) (Report, error) {
	prior := controller.alreadyPassed
	path := ".code-polishy-reports/security-refresh/" + prior.SHA256 + "/report.json"
	supply, scanErr := engine.SupplyChain(ctx, false)
	scanErr = errors.Join(scanErr, controller.candidateIntegrityError(engine))
	if supply.Security == nil {
		supply.Security = &policy.SecurityVerification{Status: "unavailable", CheckedAt: engine.securityNow()}
	}
	if scanErr != nil {
		supply.Findings = append(supply.Findings, securityEvidenceFinding(scanErr))
		supply.Security.Status = "unavailable"
	}
	supply.Security.ReportPath = path
	if err := writeSecurityEvidence(engine.Repository, path, securityRefreshRecord{GateSHA256: controller.alreadyPassed.SHA256, Report: supply}); err != nil {
		return Report{}, err
	}
	status := "security-refreshed"
	if HasFindings(supply) || scanErr != nil {
		status = "failed"
	}
	return engine.withRefreshedSecurity(report, supply, status), scanErr
}

func (engine *Engine) withRefreshedSecurity(report, security Report, status string) Report {
	report.Findings = slices.DeleteFunc(slices.Clone(report.Findings), securityOutcome)
	report.Suppressed = slices.DeleteFunc(slices.Clone(report.Suppressed), func(outcome policy.Suppressed) bool { return securityOutcome(outcome.Finding) })
	report.Assessed = slices.DeleteFunc(slices.Clone(report.Assessed), func(outcome policy.AssessedVulnerability) bool { return securityOutcome(outcome.Finding) })
	report.ReleaseAges = slices.DeleteFunc(slices.Clone(report.ReleaseAges), func(outcome policy.AssessedReleaseAge) bool { return securityOutcome(outcome.Finding) })
	report.Notes = []string{"historical source and test verification retained; current security acceptance is reported separately"}
	report.GateRunPolicy.Status = status
	return engine.combine(report, security)
}

func securityOutcome(finding policy.Finding) bool {
	return finding.VerificationPhase == policy.VerificationSecurity
}
