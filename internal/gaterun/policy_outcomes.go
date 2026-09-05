package gaterun

import (
	"fmt"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func validatePolicyOutcomes(report Report) error {
	if report.Suppressed == nil || report.Assessed == nil || report.ReleaseAges == nil {
		return fmt.Errorf("%w: gate run policy outcome collections are missing", ErrInvalidArtifact)
	}
	if err := validateFindingLocations(report.Findings); err != nil {
		return err
	}
	for _, outcome := range report.Suppressed {
		if err := validatePolicyOutcome(outcome.Finding, policy.FindingSuppressed); err != nil {
			return err
		}
	}
	for _, outcome := range report.Assessed {
		if err := validatePolicyOutcome(outcome.Finding, policy.FindingReviewed); err != nil {
			return err
		}
	}
	for _, outcome := range report.ReleaseAges {
		if err := validatePolicyOutcome(outcome.Finding, policy.FindingReviewed); err != nil {
			return err
		}
	}
	return nil
}

func validatePolicyOutcome(finding policy.Finding, status policy.FindingStatus) error {
	if finding.Status != status {
		return fmt.Errorf("%w: gate run policy outcome has an inconsistent status", ErrInvalidArtifact)
	}
	return validateFindingLocations([]policy.Finding{finding})
}

func cloneSuppressedOutcomes(outcomes []policy.Suppressed) []policy.Suppressed {
	result := make([]policy.Suppressed, len(outcomes))
	for index, outcome := range outcomes {
		result[index] = outcome
		result[index].Finding = outcome.Finding.Clone()
	}
	return result
}

func cloneVulnerabilityOutcomes(outcomes []policy.AssessedVulnerability) []policy.AssessedVulnerability {
	result := make([]policy.AssessedVulnerability, len(outcomes))
	for index, outcome := range outcomes {
		result[index] = outcome
		result[index].Finding = outcome.Finding.Clone()
	}
	return result
}

func cloneReleaseAgeOutcomes(outcomes []policy.AssessedReleaseAge) []policy.AssessedReleaseAge {
	result := make([]policy.AssessedReleaseAge, len(outcomes))
	for index, outcome := range outcomes {
		result[index] = outcome
		result[index].Finding = outcome.Finding.Clone()
	}
	return result
}
