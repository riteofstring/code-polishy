package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type BudgetProposal struct {
	Scope     string
	Ceiling   int
	Rationale string
}

func validateBudgetProposals(proposals []BudgetProposal) error {
	seen := map[string]bool{}
	for _, proposal := range proposals {
		if !slices.Contains([]string{"production", "test", "total"}, proposal.Scope) || seen[proposal.Scope] || proposal.Ceiling <= 0 {
			return fmt.Errorf("budget proposals need unique production, test, or total scopes and positive ceilings")
		}
		if strings.TrimSpace(proposal.Rationale) == "" || len(proposal.Rationale) > 2000 || strings.ContainsAny(proposal.Rationale, "\x00\r\n") {
			return fmt.Errorf("budget proposal requires one growth rationale of at most 2000 bytes")
		}
		seen[proposal.Scope] = true
	}
	return nil
}

func (engine *Engine) prepareBudgetGrowth(analysis *repository.SizeAnalysis, proposals []BudgetProposal) error {
	for _, proposal := range proposals {
		index := slices.IndexFunc(analysis.SourceLines.Budget, func(scope repository.LineBudgetScope) bool { return scope.Scope == proposal.Scope })
		if index < 0 {
			analysis.SourceLines.Budget = append(analysis.SourceLines.Budget, repository.LineBudgetScope{Scope: proposal.Scope, Lines: sourceLinesForScope(analysis.SourceLines, proposal.Scope)})
			index = len(analysis.SourceLines.Budget) - 1
		}
		analysis.SourceLines.Budget[index].ProposedCeiling = proposal.Ceiling
		analysis.SourceLines.Budget[index].GrowthRationale = proposal.Rationale
	}
	if analysis.Comparison == nil || len(analysis.SourceLines.Budget) == 0 {
		return nil
	}
	base, err := engine.Repository.CountSourceLinesAt(analysis.Comparison.MergeBase)
	if err != nil {
		return err
	}
	for index := range analysis.SourceLines.Budget {
		count := sourceLinesForScope(base, analysis.SourceLines.Budget[index].Scope)
		analysis.SourceLines.Budget[index].BaseLines = &count
	}
	return nil
}

func sourceLinesForScope(lines repository.SourceLines, scope string) int {
	switch scope {
	case "production":
		return lines.Production
	case "test":
		return lines.Test
	default:
		return lines.Production + lines.Test
	}
}
