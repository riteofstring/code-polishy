package engine

import (
	"encoding/json"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func (engine *Engine) lineBudgetScopes() ([]repository.LineBudgetScope, error) {
	if engine.Repository.Config.Quality.LineBudget == nil {
		return nil, nil
	}
	lines, err := engine.Repository.CountSourceLines()
	return lines.Budget, err
}

func (engine *Engine) lineBudgetChangeFindings(base string) []policy.Finding {
	if base == "" {
		return nil
	}
	data, _, present, err := engine.configFileAt(base)
	if err != nil {
		return []policy.Finding{{Check: "quality.lineBudgetChange", Path: policy.ConfigFilename, Subject: "base", Message: err.Error()}}
	}
	if !present {
		return nil
	}
	var baseConfig struct {
		Quality struct {
			LineBudget *policy.LineBudget `json:"lineBudget"`
		} `json:"quality"`
	}
	if err := json.Unmarshal(data, &baseConfig); err != nil {
		return []policy.Finding{{Check: "quality.lineBudgetChange", Path: policy.ConfigFilename, Subject: "base", Message: fmt.Sprintf("parse base configuration: %v", err)}}
	}
	return lineBudgetChanges(baseConfig.Quality.LineBudget, engine.Repository.Config.Quality.LineBudget)
}

func lineBudgetChanges(base, candidate *policy.LineBudget) []policy.Finding {
	findings := []policy.Finding{}
	for _, scope := range []struct {
		name            string
		base, candidate int
	}{
		{"production", lineBudgetValue(base).Production, lineBudgetValue(candidate).Production},
		{"test", lineBudgetValue(base).Test, lineBudgetValue(candidate).Test},
		{"total", lineBudgetValue(base).Total, lineBudgetValue(candidate).Total},
	} {
		if scope.base == scope.candidate {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "quality.lineBudgetChange", Path: policy.ConfigFilename, Subject: scope.name, Severity: policy.FindingWarning,
			Message: fmt.Sprintf("%s line budget changed from %s at base to %s; change it only at the caller's explicit request", scope.name, lineBudgetLabel(scope.base), lineBudgetLabel(scope.candidate)),
		})
	}
	return findings
}

func lineBudgetValue(budget *policy.LineBudget) policy.LineBudget {
	if budget == nil {
		return policy.LineBudget{}
	}
	return *budget
}

func lineBudgetLabel(value int) string {
	if value == 0 {
		return "unset"
	}
	return fmt.Sprint(value)
}
