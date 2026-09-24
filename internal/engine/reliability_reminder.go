package engine

import (
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const ReliabilityReminderPolicyDocument = "docs/policies/end-to-end-reliability.md"

const reliabilityReminderPrinciple = "Reliability machinery must address a demonstrated failure or requirement and improve the end-to-end system. Add or tighten limits, terminal states, retry ceilings, fail-closed gates, identity bindings, digests, receipts, or recovery machinery only after accounting for existing platform behavior and confirming that the change preserves valid results and ordinary recovery, reduces total complexity, and lowers total failure risk."

var reliabilityReminderQuestions = []string{
	"What demonstrated failure or requirement does this machinery address?",
	"What simpler solution or existing platform capability already addresses it?",
	"What valid results or ordinary recovery paths could the change discard, invalidate, or complicate?",
	"Across implementation, operation, and recovery, does the change reduce total complexity and lower total failure risk?",
}

type ReliabilityReminder struct {
	PolicyDocument string   `json:"policyDocument"`
	Principle      string   `json:"principle"`
	Questions      []string `json:"questions"`
	MatchedModules []string `json:"matchedModules"`
	MatchedPaths   []string `json:"matchedPaths"`
}

func (engine *Engine) reliabilityReminder(candidate repository.CandidateDelta) *ReliabilityReminder {
	configured := engine.Repository.Config.Quality.ReliabilityReminder
	if configured == nil {
		return nil
	}
	impact := engine.Repository.CandidateImpact(candidate)
	modules := matchedReliabilityReminderValues(configured.Modules, impact.DirectModules)
	paths := matchedReliabilityReminderValues(configured.SourcePaths, impact.Paths)
	if len(modules) == 0 && len(paths) == 0 {
		return nil
	}
	return newReliabilityReminder(modules, paths)
}

func newReliabilityReminder(modules, paths []string) *ReliabilityReminder {
	return &ReliabilityReminder{
		PolicyDocument: ReliabilityReminderPolicyDocument,
		Principle:      reliabilityReminderPrinciple,
		Questions:      slices.Clone(reliabilityReminderQuestions),
		MatchedModules: orderedReliabilityReminderValues(modules),
		MatchedPaths:   orderedReliabilityReminderValues(paths),
	}
}

func matchedReliabilityReminderValues(configured, selected []string) []string {
	matched := []string{}
	for _, value := range configured {
		if slices.Contains(selected, value) {
			matched = append(matched, value)
		}
	}
	return orderedReliabilityReminderValues(matched)
}

func orderedReliabilityReminderValues(values []string) []string {
	ordered := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			ordered = append(ordered, value)
		}
	}
	sort.Strings(ordered)
	return slices.Compact(ordered)
}

func combineReliabilityReminders(left, right *ReliabilityReminder) *ReliabilityReminder {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return newReliabilityReminder(
		append(slices.Clone(left.MatchedModules), right.MatchedModules...),
		append(slices.Clone(left.MatchedPaths), right.MatchedPaths...),
	)
}

func (engine *Engine) withReliabilityReminder(report Report, candidate repository.CandidateDelta) Report {
	report.ReliabilityReminder = engine.reliabilityReminder(candidate)
	return report
}
