package engine

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type capabilityCandidate struct {
	entry CapabilityEntry
	score int
}

func selectCapabilityCandidates(entries []CapabilityEntry, query string) ([]CapabilityEntry, int, error) {
	if query == "" {
		return entries, len(entries), nil
	}
	candidates := []capabilityCandidate{}
	for _, entry := range entries {
		score, err := capabilityQueryScore(entry, query)
		if err != nil {
			return nil, 0, err
		}
		if score > 0 {
			candidates = append(candidates, capabilityCandidate{entry: entry, score: score})
		}
	}
	slices.SortFunc(candidates, func(left, right capabilityCandidate) int {
		if order := cmp.Compare(right.score, left.score); order != 0 {
			return order
		}
		return strings.Compare(left.entry.ID, right.entry.ID)
	})
	selected := make([]CapabilityEntry, 0, min(20, len(candidates)))
	for _, candidate := range candidates[:min(20, len(candidates))] {
		selected = append(selected, candidate.entry)
	}
	return selected, len(candidates), nil
}

func capabilityQueryScore(entry CapabilityEntry, query string) (int, error) {
	fields := append([]string{entry.Name}, entry.Aliases...)
	fields = append(fields, entry.Description)
	terms := map[string]bool{}
	exact := false
	for index, field := range fields {
		normalized, err := policy.NormalizeCapabilityQuery(field)
		if err != nil {
			return 0, fmt.Errorf("capability %q search metadata: %w", entry.ID, err)
		}
		if index < len(fields)-1 && normalized == query {
			exact = true
		}
		for _, term := range strings.Fields(normalized) {
			terms[strings.Trim(term, ".,;:!?()")] = true
		}
	}
	if exact {
		return 100, nil
	}
	score := 0
	for _, term := range strings.Fields(query) {
		if terms[strings.Trim(term, ".,;:!?()")] {
			score++
		}
	}
	return score, nil
}
