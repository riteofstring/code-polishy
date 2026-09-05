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

//go:embed contract_definitions.py
var contractDefinitionsSource string

type ContractDefinitionRequest struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Qualified string `json:"qualified"`
	Member    string `json:"member"`
}

type ContractDefinitionEvidence struct {
	ContractDefinitionRequest
	Definitions []ReachabilityDefinition `json:"definitions"`
	Identity    string                   `json:"identity"`
}

type ContractDefinitionProject struct {
	Evidence []ContractDefinitionEvidence
	Problems []ReachabilityProblem
}

func ResolveContractDefinitions(ctx context.Context, python string, modules []TypeModule, requests []ContractDefinitionRequest) (ContractDefinitionProject, error) {
	ordered := slices.Clone(modules)
	slices.SortFunc(ordered, func(left, right TypeModule) int { return strings.Compare(left.Path, right.Path) })
	if len(ordered) > maximumProjectSources || len(requests) > maximumItems || !filepath.IsAbs(python) {
		return ContractDefinitionProject{}, fmt.Errorf("dependency contract input exceeds its item boundary")
	}
	if _, err := contractDefinitionRequests(requests); err != nil {
		return ContractDefinitionProject{}, err
	}
	header, err := json.Marshal(struct {
		Protocol string                      `json:"protocol"`
		Count    int                         `json:"count"`
		Requests []ContractDefinitionRequest `json:"requests"`
	}{"python-contract-definitions/v1", len(ordered), append([]ContractDefinitionRequest{}, requests...)})
	if err != nil {
		return ContractDefinitionProject{}, err
	}
	if len(header)+1 > maximumResponseSize {
		return ContractDefinitionProject{}, fmt.Errorf("dependency contract header exceeds its byte boundary")
	}
	input := &typeProjectReader{modules: ordered, current: bytes.NewReader(append(header, '\n'))}
	program := TypeSupportSource() + ReachabilitySupportSource() + embeddedPythonModule("__main__", contractDefinitionsSource)
	data, err := runFactProject(ctx, python, input, program)
	if err != nil {
		return ContractDefinitionProject{}, err
	}
	return decodeContractDefinitions(data, ordered, requests)
}

func decodeContractDefinitions(data []byte, modules []TypeModule, requests []ContractDefinitionRequest) (ContractDefinitionProject, error) {
	if len(data) > maximumResponseSize {
		return ContractDefinitionProject{}, fmt.Errorf("dependency contract response exceeds its byte boundary")
	}
	var wire struct {
		Protocol string                       `json:"protocol"`
		Covered  []typeCoverage               `json:"covered"`
		Evidence []ContractDefinitionEvidence `json:"evidence"`
		Problems []ReachabilityProblem        `json:"problems"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return ContractDefinitionProject{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ContractDefinitionProject{}, fmt.Errorf("dependency contract response has trailing data")
	}
	if wire.Protocol != "python-contract-definitions/v1" || wire.Evidence == nil || wire.Problems == nil {
		return ContractDefinitionProject{}, fmt.Errorf("dependency contract response is incomplete")
	}
	files, err := projectCoveredFiles(wire.Covered, modules)
	if err != nil {
		return ContractDefinitionProject{}, err
	}
	result := ContractDefinitionProject{Evidence: wire.Evidence, Problems: wire.Problems}
	if err := validateContractDefinitions(files, requests, result); err != nil {
		return ContractDefinitionProject{}, err
	}
	return result, nil
}

func contractDefinitionRequests(requests []ContractDefinitionRequest) (map[string]ContractDefinitionRequest, error) {
	remaining := map[string]ContractDefinitionRequest{}
	for _, request := range requests {
		if _, duplicate := remaining[request.ID]; request.ID == "" || duplicate {
			return nil, fmt.Errorf("dependency contract request is empty or duplicated")
		}
		remaining[request.ID] = request
	}
	return remaining, nil
}

func validateContractDefinitions(files []string, requests []ContractDefinitionRequest, result ContractDefinitionProject) error {
	remaining, err := contractDefinitionRequests(requests)
	if err != nil {
		return err
	}
	for _, problem := range result.Problems {
		if _, found := remaining[problem.ID]; !found || problem.Message == "" {
			return fmt.Errorf("dependency contract problem is not one requested contract")
		}
		delete(remaining, problem.ID)
	}
	for _, evidence := range result.Evidence {
		request, found := remaining[evidence.ID]
		if !found || request != evidence.ContractDefinitionRequest {
			return fmt.Errorf("dependency contract evidence was substituted or duplicated")
		}
		delete(remaining, evidence.ID)
		if err := validateContractDefinitionEvidence(files, evidence); err != nil {
			return err
		}
	}
	if len(remaining) != 0 {
		return fmt.Errorf("dependency contract response omitted requested evidence")
	}
	return nil
}

func validateContractDefinitionEvidence(files []string, evidence ContractDefinitionEvidence) error {
	value := ReachabilityEvidence{Identity: evidence.Identity, Targets: []ReachabilityTarget{{Module: evidence.Qualified, Symbol: evidence.Member, Definitions: evidence.Definitions}}}
	return validatePythonReachabilityTargets(files, value)
}
