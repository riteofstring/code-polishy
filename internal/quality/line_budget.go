package quality

import (
	"fmt"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func LineBudgetFindings(repo repository.Repository) []policy.Finding {
	if repo.Config.Quality.LineBudget == nil {
		return nil
	}
	lines, err := repo.CountSourceLines()
	if err != nil {
		return []policy.Finding{{Check: "quality.lineBudget", Path: "repository", Subject: "inventory", Message: err.Error()}}
	}
	findings := []policy.Finding{}
	for _, scope := range lines.Budget {
		if scope.Exceeded() {
			findings = append(findings, policy.Finding{
				Check: "quality.lineBudget", Path: "repository", Subject: scope.Scope,
				Message: fmt.Sprintf("%s source has %d non-blank lines; budget is %d (%d over)", scope.Scope, scope.Lines, scope.Budget, scope.Lines-scope.Budget),
			})
		}
	}
	return findings
}
