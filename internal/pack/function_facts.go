package pack

import (
	"fmt"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/portability"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func analysisFindings(repo repository.Repository, adapter *policy.PackAdapter, response Response) []policy.Finding {
	findings := findingsForResponse(adapter, response)
	for _, note := range response.Notes {
		findings = append(findings, policy.Finding{Check: "pack." + adapter.PackName + ".note", Path: policy.ConfigFilename, Subject: adapter.Capability, Message: note, Severity: policy.FindingInformation, SemanticIdentity: []string{note}})
	}
	if response.Facts != nil && response.Facts.Functions != nil {
		findings = append(findings, functionFindings(repo, *response.Facts.Functions)...)
	}
	if response.Facts != nil && response.Facts.Literals != nil {
		literals := make([]portability.Literal, 0, len(*response.Facts.Literals))
		for _, literal := range *response.Facts.Literals {
			literals = append(literals, portability.Literal{Path: literal.Path, Line: literal.Line, Column: literal.Column, Value: literal.Value, RootContext: literal.RootContext})
		}
		for _, advisory := range portability.LiteralAdvisories(repo, literals) {
			findings = append(findings, policy.Finding{Check: advisory.Check, Path: advisory.Path, Line: advisory.Line, Column: advisory.Column, Subject: advisory.Subject, Message: advisory.Message, Severity: policy.FindingWarning})
		}
	}
	if response.Facts != nil && response.Facts.DeadCode != nil {
		findings = append(findings, deadCodeFindings(*response.Facts.DeadCode)...)
	}
	return findings
}

func functionFindings(repo repository.Repository, functions []FunctionFact) []policy.Finding {
	findings := []policy.Finding{}
	for _, function := range functions {
		if repo.IsGenerated(function.Path) {
			continue
		}
		quality := policy.EffectiveQuality(repo.Config.Quality)
		complexity, depth, parameters := quality.Complexity.TypeScript, quality.MaxDepth, quality.MaxParams
		if repo.IsTest(function.Path) {
			complexity, depth, parameters = quality.Complexity.TypeScriptTest, quality.MaxTestDepth, quality.MaxTestParams
		}
		switch repo.Language(function.Path) {
		case "go":
			complexity = quality.Complexity.Go
			if repo.IsTest(function.Path) {
				complexity = quality.Complexity.GoTest
			}
		case "python":
			complexity = quality.Complexity.Python
		}
		for _, metric := range []struct {
			name           string
			value, maximum int
		}{
			{"complexity", function.Complexity, complexity - 1}, {"depth", function.Depth, depth}, {"parameters", function.Parameters, parameters},
		} {
			if metric.value > metric.maximum {
				findings = append(findings, policy.Finding{Check: "quality.function" + metric.name, Path: function.Path, Line: function.Line, Column: function.Column, Subject: function.Name, Message: fmt.Sprintf("function %s has %s %d; maximum is %d", function.Name, metric.name, metric.value, metric.maximum)})
			}
		}
	}
	return findings
}
