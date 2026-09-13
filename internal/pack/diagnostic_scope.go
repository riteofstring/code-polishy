package pack

import (
	"fmt"
	"slices"
)

func validateDiagnosticCoverage(request Request, response Response) error {
	if request.Capability != "architecture" {
		return nil
	}
	reached := architectureClosure(request, response)
	accounted := map[string]bool{}
	for _, file := range response.Coverage.Analyzed {
		accounted[file] = true
	}
	for _, file := range response.Coverage.Unsupported {
		accounted[file.Path] = true
	}
	for file := range reached {
		if !accounted[file] {
			return fmt.Errorf("architecture omitted connected unit member %s", file)
		}
	}
	for file := range accounted {
		if !reached[file] {
			return fmt.Errorf("architecture reported an unrelated unit member %s", file)
		}
	}
	return validateArchitectureFindings(response.Findings, reached)
}

func validateArchitectureFindings(findings []ResponseFinding, reached map[string]bool) error {
	for _, finding := range findings {
		if finding.Path != "repository" && !reached[finding.Path] {
			return fmt.Errorf("architecture finding is outside the selected dependency closure: %s", finding.Path)
		}
	}
	return nil
}

func architectureClosure(request Request, response Response) map[string]bool {
	units := map[string]int{}
	for index, unit := range request.Units {
		for _, file := range unit.Members {
			units[file] = index
		}
	}
	imports := architectureTargets(response, units)
	reached, expanded := map[string]bool{}, map[int]bool{}
	queue := slices.Clone(request.Files)
	for len(queue) > 0 {
		file := queue[0]
		queue = queue[1:]
		if reached[file] {
			continue
		}
		reached[file] = true
		if index, exists := units[file]; exists && !expanded[index] {
			expanded[index] = true
			queue = append(queue, request.Units[index].Members...)
		}
		queue = append(queue, imports[file]...)
	}
	return reached
}

func architectureTargets(response Response, units map[string]int) map[string][]string {
	imports := map[string][]string{}
	if response.Facts == nil || response.Facts.Imports == nil {
		return imports
	}
	for _, fact := range *response.Facts.Imports {
		if _, exists := units[fact.Resolved]; exists {
			imports[fact.Path] = append(imports[fact.Path], fact.Resolved)
		}
	}
	return imports
}
