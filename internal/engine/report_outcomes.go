package engine

import (
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func coalesceReportOutcomes(report *Report) {
	report.Suppressed = coalescePolicyOutcomes(report.Suppressed,
		func(outcome *policy.Suppressed) *policy.Finding { return &outcome.Finding },
		func(outcome policy.Suppressed) string {
			return findingKey(outcome.Finding) + "\x00" + outcome.Exception.ID
		})
	report.Assessed = coalescePolicyOutcomes(report.Assessed,
		func(outcome *policy.AssessedVulnerability) *policy.Finding { return &outcome.Finding },
		func(outcome policy.AssessedVulnerability) string {
			return findingKey(outcome.Finding) + "\x00" + outcome.Assessment.ID
		})
	report.ReleaseAges = coalescePolicyOutcomes(report.ReleaseAges,
		func(outcome *policy.AssessedReleaseAge) *policy.Finding { return &outcome.Finding },
		func(outcome policy.AssessedReleaseAge) string {
			return findingKey(outcome.Finding) + "\x00" + outcome.Assessment.ID
		})
}

func coalescePolicyOutcomes[T any](outcomes []T, finding func(*T) *policy.Finding, identity func(T) string) []T {
	result := make([]T, 0, len(outcomes))
	positions := map[string]int{}
	for _, outcome := range outcomes {
		key := identity(outcome)
		if index, present := positions[key]; present {
			previous := finding(&result[index])
			*previous = mergeFindingOccurrence(*previous, *finding(&outcome))
			continue
		}
		positions[key] = len(result)
		result = append(result, outcome)
	}
	slices.SortFunc(result, func(left, right T) int { return strings.Compare(identity(left), identity(right)) })
	return result
}
