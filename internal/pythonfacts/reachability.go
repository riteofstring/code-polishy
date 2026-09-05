package pythonfacts

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
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

type ReachabilityProject struct {
	Evidence []ReachabilityEvidence `json:"evidence"`
	Problems []ReachabilityProblem  `json:"problems"`
}

func ResolveReachability(ctx context.Context, python string, modules []TypeModule, requests []ReachabilityInput) (ReachabilityProject, error) {
	ordered := slices.Clone(modules)
	slices.SortFunc(ordered, func(left, right TypeModule) int { return strings.Compare(left.Path, right.Path) })
	if len(ordered) > maximumProjectSources || !filepath.IsAbs(python) {
		return ReachabilityProject{}, fmt.Errorf("reachability project source count or interpreter is invalid")
	}
	header, err := json.Marshal(struct {
		Protocol string              `json:"protocol"`
		Count    int                 `json:"count"`
		Requests []ReachabilityInput `json:"requests"`
	}{"python-reachability-project/v1", len(ordered), append([]ReachabilityInput{}, requests...)})
	if err != nil {
		return ReachabilityProject{}, err
	}
	if len(header)+1 > maximumResponseSize {
		return ReachabilityProject{}, fmt.Errorf("reachability header exceeds its request boundary")
	}
	input := &typeProjectReader{modules: ordered, current: bytes.NewReader(append(header, '\n'))}
	program := TypeSupportSource() + embeddedPythonModule("external_contracts", externalContractsSource) + embeddedPythonModule("__main__", reachabilitySource)
	data, err := runFactProject(ctx, python, input, program)
	if err != nil {
		return ReachabilityProject{}, err
	}
	return decodeReachabilityProject(data, ordered, requests)
}

func decodeReachabilityProject(data []byte, modules []TypeModule, requests []ReachabilityInput) (ReachabilityProject, error) {
	var wire struct {
		Protocol string                 `json:"protocol"`
		Covered  []typeCoverage         `json:"covered"`
		Evidence []ReachabilityEvidence `json:"evidence"`
		Problems []ReachabilityProblem  `json:"problems"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return ReachabilityProject{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ReachabilityProject{}, fmt.Errorf("reachability output has trailing data")
	}
	if wire.Protocol != "python-reachability-project/v1" || wire.Evidence == nil || wire.Problems == nil {
		return ReachabilityProject{}, fmt.Errorf("reachability output is incomplete")
	}
	files, err := projectCoveredFiles(wire.Covered, modules)
	if err != nil {
		return ReachabilityProject{}, err
	}
	resolved, err := reachabilityResolvedRequests(requests, wire.Problems)
	if err != nil {
		return ReachabilityProject{}, err
	}
	if err := ValidateReachabilityEvidence(files, requests, resolved, wire.Evidence); err != nil {
		return ReachabilityProject{}, err
	}
	return ReachabilityProject{Evidence: wire.Evidence, Problems: wire.Problems}, nil
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

func reachabilityResolvedRequests(requests []ReachabilityInput, problems []ReachabilityProblem) ([]string, error) {
	remaining := map[string]bool{}
	for _, request := range requests {
		if request.ID == "" || remaining[request.ID] {
			return nil, fmt.Errorf("reachability request identity is empty or duplicated")
		}
		remaining[request.ID] = true
	}
	for _, problem := range problems {
		if !remaining[problem.ID] || problem.Message == "" {
			return nil, fmt.Errorf("reachability problem is not one exact request")
		}
		delete(remaining, problem.ID)
	}
	resolved := make([]string, 0, len(remaining))
	for id := range remaining {
		resolved = append(resolved, id)
	}
	slices.Sort(resolved)
	return resolved, nil
}
