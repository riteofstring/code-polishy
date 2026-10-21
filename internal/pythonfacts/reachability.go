package pythonfacts

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed reachability.py
var reachabilitySource string

//go:embed external_contracts.py
var externalContractsSource string

type ReachabilityInput struct {
	ID          string                  `json:"id"`
	Declaration json.RawMessage         `json:"declaration"`
	Registry    string                  `json:"registry"`
	Error       string                  `json:"error"`
	Dependency  *ReachabilityDependency `json:"dependency"`
}

type ReachabilityDependency struct {
	Distribution string                      `json:"distribution"`
	Identity     string                      `json:"identity"`
	Contract     *ContractDefinitionEvidence `json:"contract"`
}

type ReachabilityDefinition struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	End  int    `json:"end"`
	Name string `json:"name"`
}

type ReachabilityTarget struct {
	Module      string                   `json:"module"`
	Symbol      string                   `json:"symbol"`
	Definitions []ReachabilityDefinition `json:"definitions"`
}

type ReachabilityEvidence struct {
	DependencyIdentity string               `json:"dependencyIdentity"`
	RegistrySHA256     string               `json:"registrySha256"`
	ID                 string               `json:"id"`
	Identity           string               `json:"identity"`
	Targets            []ReachabilityTarget `json:"targets"`
}

func ReachabilitySupportSource() string {
	return embeddedPythonModule("external_contracts", externalContractsSource) + embeddedPythonModule("reachability", reachabilitySource)
}

type ReachabilityProblem struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

func projectCoveredFiles(covered []typeCoverage, modules []TypeModule) ([]string, error) {
	if covered == nil || len(covered) != len(modules) {
		return nil, fmt.Errorf("project output has incomplete source coverage")
	}
	files := make([]string, 0, len(modules))
	for index, module := range modules {
		if covered[index].Path != module.Path || covered[index].SourceSHA256 != module.SourceSHA256 {
			return nil, fmt.Errorf("project source coverage is stale or reordered")
		}
		files = append(files, module.Path)
	}
	return files, nil
}
