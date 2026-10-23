package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

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

func currentSecurityEvidence(evidence *policy.SecurityVerification, now time.Time) bool {
	return evidence != nil && evidence.Status == "current" && !evidence.CheckedAt.After(now) &&
		evidence.RefreshAfter.After(evidence.CheckedAt) && !evidence.RefreshAfter.After(evidence.CheckedAt.Add(policy.SecurityEvidenceLifetime)) && now.Before(evidence.RefreshAfter)
}

func (engine *Engine) reuseGateSecurity(ctx context.Context, controller *gateRunController, report Report) (Report, error) {
	prior := controller.alreadyPassed
	path := ".code-polishy-reports/security-refresh/" + prior.SHA256 + "/report.json"
	requiresScan := len(engine.Repository.Config.SupplyChain.ArtifactSecurity.Targets) > 0 || slices.ContainsFunc(engine.Repository.Config.ActivePolicyModules, func(module policy.ActivePolicyModule) bool {
		return module.Name == "osv"
	})
	if requiresScan {
		return engine.refreshGateSecurity(ctx, controller, report, path)
	}
	var refreshed securityRefreshRecord
	present, err := readSecurityEvidence(engine.Repository, path, &refreshed)
	if err != nil {
		return Report{}, err
	}
	if present && refreshed.GateSHA256 != prior.SHA256 {
		return Report{}, fmt.Errorf("security refresh does not match the historical verification pass")
	}
	if !present && currentSecurityEvidence(prior.Security, engine.securityNow()) {
		return report, nil
	}
	if present && currentSecurityEvidence(refreshed.Report.Security, engine.securityNow()) && !HasFindings(refreshed.Report) {
		return engine.withRefreshedSecurity(report, refreshed.Report, "already-passed"), nil
	}
	return engine.refreshGateSecurity(ctx, controller, report, path)
}

func (engine *Engine) refreshGateSecurity(ctx context.Context, controller *gateRunController, report Report, path string) (Report, error) {
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
