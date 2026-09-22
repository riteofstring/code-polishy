package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

const maximumPythonPolicyDeclarations = 4096

type scopedComputedImport struct {
	declaration computedImportDeclaration
	inputs      []string
}

type pythonContractDeclaration struct {
	Project         string          `json:"project"`
	Kind            string          `json:"kind"`
	Target          string          `json:"target"`
	Members         []string        `json:"members,omitempty"`
	Attributes      []string        `json:"attributes,omitempty"`
	Decorators      []string        `json:"decorators,omitempty"`
	AnnotatedFields bool            `json:"annotatedFields,omitempty"`
	Keywords        map[string]bool `json:"keywords,omitempty"`
	Reason          string          `json:"reason"`
}

func decodePolicyInput(data json.RawMessage) (policyInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	value := policyInput{}
	if err := decoder.Decode(&value); err != nil {
		return policyInput{}, fmt.Errorf("decode Python policy input: %w", err)
	}
	if err := requireEnd(decoder); err != nil {
		return policyInput{}, err
	}
	if value.Quality == nil || value.Modules == nil || value.Files == nil || value.Declarations == nil || len(value.Declarations) > maximumPythonPolicyDeclarations {
		return policyInput{}, errors.New("python policy input omits an explicit bounded collection")
	}
	return value, nil
}

func policyDeclarationPaths(request request) ([]string, error) {
	input, err := decodePolicyInput(request.Policy)
	if err != nil {
		return nil, err
	}
	handles := map[string]bool{}
	for _, scope := range request.Scopes {
		handles[scope.Handle] = true
	}
	paths := []string{}
	for index, declaration := range input.Declarations {
		if declaration.Scopes == nil || declaration.Inputs == nil {
			return nil, fmt.Errorf("policy declaration %d omits explicit scope or input collections", index)
		}
		bound := false
		for _, handle := range declaration.Scopes {
			if !handles[handle] {
				return nil, fmt.Errorf("policy declaration %d names unknown scope %s", index, handle)
			}
			bound = true
		}
		if !bound {
			return nil, fmt.Errorf("policy declaration %d has no invocation scope", index)
		}
		for _, input := range declaration.Inputs {
			if err := exactPath(input); err != nil {
				return nil, fmt.Errorf("policy declaration %d input: %w", index, err)
			}
			paths = append(paths, input)
		}
	}
	return uniqueSorted(paths), nil
}

func computedImportsForScope(data json.RawMessage, handle string) ([]scopedComputedImport, error) {
	input, err := decodePolicyInput(data)
	if err != nil {
		return nil, err
	}
	result := []scopedComputedImport{}
	for index, declaration := range input.Declarations {
		if declaration.Kind != "python.computed-import" || !slices.Contains(declaration.Scopes, handle) {
			continue
		}
		if declaration.Version != 1 {
			return nil, fmt.Errorf("policy declaration %d uses unsupported python.computed-import version %d", index, declaration.Version)
		}
		value, err := decodeComputedImportDeclaration(declaration.Data)
		if err != nil {
			return nil, fmt.Errorf("policy declaration %d: %w", index, err)
		}
		result = append(result, scopedComputedImport{declaration: value, inputs: slices.Clone(declaration.Inputs)})
	}
	return result, nil
}

func decodeComputedImportDeclaration(data json.RawMessage) (computedImportDeclaration, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	value := computedImportDeclaration{}
	if err := decoder.Decode(&value); err != nil {
		return computedImportDeclaration{}, fmt.Errorf("decode computed import declaration: %w", err)
	}
	if err := requireEnd(decoder); err != nil {
		return computedImportDeclaration{}, err
	}
	return value, nil
}

func pythonContractsForScope(data json.RawMessage, scope analysisScope) ([]vultureContract, string, error) {
	input, err := decodePolicyInput(data)
	if err != nil {
		return nil, "", err
	}
	scopeData, err := decodePythonScopeData(scope.Data)
	if err != nil {
		return nil, "", err
	}
	contracts := []vultureContract{}
	for index, declaration := range input.Declarations {
		if !slices.Contains(declaration.Scopes, scope.Handle) {
			continue
		}
		contract, reason, err := pythonContractInput(declaration, scopeData.Manifest, index)
		if err != nil {
			return nil, "", err
		}
		if reason != "" {
			return nil, reason, nil
		}
		contracts = append(contracts, contract)
	}
	sort.Slice(contracts, func(left, right int) bool { return contracts[left].ID < contracts[right].ID })
	for index := 1; index < len(contracts); index++ {
		if contracts[index-1].ID == contracts[index].ID {
			return nil, "", errors.New("python contract declarations repeat a target")
		}
	}
	return contracts, "", nil
}

func pythonContractInput(declaration policyDeclarationInput, manifest string, index int) (vultureContract, string, error) {
	if declaration.Kind != "python.contract" {
		return vultureContract{}, "Python runtime reachability declaration " + declaration.Kind + " is not yet supported by the pack dead-code analyzer", nil
	}
	if declaration.Version != 1 {
		return vultureContract{}, "", fmt.Errorf("policy declaration %d uses unsupported python.contract version %d", index, declaration.Version)
	}
	contract, err := decodePythonContractDeclaration(declaration.Data)
	if err != nil {
		return vultureContract{}, "", fmt.Errorf("policy declaration %d: %w", index, err)
	}
	if contract.Kind != "entry-point" && contract.Kind != "decorator" && contract.Kind != "module-binding" {
		return vultureContract{}, "Python contract kind " + contract.Kind + " is not yet supported by the pack dead-code analyzer", nil
	}
	input, err := newVultureContract(manifest, contract)
	if err != nil {
		return vultureContract{}, "", fmt.Errorf("policy declaration %d: %w", index, err)
	}
	return input, "", nil
}

func decodePythonContractDeclaration(data json.RawMessage) (pythonContractDeclaration, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	value := pythonContractDeclaration{}
	if err := decoder.Decode(&value); err != nil {
		return pythonContractDeclaration{}, fmt.Errorf("decode Python contract declaration: %w", err)
	}
	if err := requireEnd(decoder); err != nil {
		return pythonContractDeclaration{}, err
	}
	return value, nil
}

func newVultureContract(manifest string, contract pythonContractDeclaration) (vultureContract, error) {
	if !validPythonContractCommon(manifest, contract) {
		return vultureContract{}, errors.New("python contract is invalid")
	}
	members, err := pythonContractMembers(contract.Members)
	if err != nil {
		return vultureContract{}, err
	}
	if !validPythonContractShape(contract, members) {
		return vultureContract{}, errors.New("python contract has invalid fields for its kind")
	}
	keywords := maps.Clone(contract.Keywords)
	if keywords == nil {
		keywords = map[string]bool{}
	}
	id := "config:python.contract:" + contract.Kind + ":" + contract.Target
	return vultureContract{
		ID: id, Kind: contract.Kind, Target: contract.Target, Members: members,
		Attributes: []string{}, Decorators: []string{}, AnnotatedFields: false, Keywords: keywords,
	}, nil
}

func validPythonContractCommon(manifest string, contract pythonContractDeclaration) bool {
	validReason := strings.TrimSpace(contract.Reason) != "" && len(contract.Reason) <= 4096
	validTarget := len(contract.Target) <= 4096
	if contract.Kind == "entry-point" {
		_, _, validTarget = pythonEntryPointContractTarget(contract.Target)
	} else {
		validTarget = validTarget && validPythonModuleParts(strings.Split(contract.Target, "."))
	}
	return contract.Project == manifest && validReason && validTarget && validPythonContractKeywords(contract.Keywords)
}

func pythonEntryPointContractTarget(target string) (string, string, bool) {
	module, symbol, found := strings.Cut(target, ":")
	valid := len(target) <= 4096 && found && !strings.Contains(symbol, ":") && validPythonModuleParts(strings.Split(module, ".")) && validPythonModuleParts(strings.Split(symbol, "."))
	return module, symbol, valid
}

func validPythonContractShape(contract pythonContractDeclaration, members []string) bool {
	if len(contract.Attributes) > 0 || len(contract.Decorators) > 0 || contract.AnnotatedFields {
		return false
	}
	if contract.Kind == "entry-point" {
		return len(contract.Keywords) == 0
	}
	if contract.Kind == "decorator" {
		return len(members) == 0
	}
	return contract.Kind == "module-binding" && len(members) > 0 && len(contract.Keywords) == 0
}

func pythonContractMembers(values []string) ([]string, error) {
	members := append([]string{}, values...)
	if len(members) > 128 {
		return nil, errors.New("python contract has too many members")
	}
	for _, member := range members {
		if len(member) > 255 || !validPythonModulePart(member) {
			return nil, errors.New("python contract contains an invalid member")
		}
	}
	sort.Strings(members)
	if len(slices.Compact(slices.Clone(members))) != len(members) {
		return nil, errors.New("python contract repeats a member")
	}
	return members, nil
}

func validPythonContractKeywords(keywords map[string]bool) bool {
	if len(keywords) > 32 {
		return false
	}
	for name := range keywords {
		if len(name) > 255 || !validPythonModulePart(name) {
			return false
		}
	}
	return true
}
