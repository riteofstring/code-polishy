package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

func pythonContractReferences(data json.RawMessage, scope analysisScope) ([]vultureReference, string, error) {
	input, err := decodePolicyInput(data)
	if err != nil {
		return nil, "", err
	}
	scopeData, err := decodePythonScopeData(scope.Data)
	if err != nil {
		return nil, "", err
	}
	references := []vultureReference{}
	for index, declaration := range input.Declarations {
		if !slices.Contains(declaration.Scopes, scope.Handle) {
			continue
		}
		reference, reason, err := pythonContractReference(declaration, scopeData.Manifest, index)
		if err != nil {
			return nil, "", err
		}
		if reason != "" {
			return nil, reason, nil
		}
		references = append(references, reference)
	}
	sort.Slice(references, func(left, right int) bool { return references[left].ID < references[right].ID })
	for index := 1; index < len(references); index++ {
		if references[index-1].ID == references[index].ID {
			return nil, "", errors.New("python contract declarations repeat an entry-point target")
		}
	}
	return references, "", nil
}

func pythonContractReference(declaration policyDeclarationInput, manifest string, index int) (vultureReference, string, error) {
	if declaration.Kind != "python.contract" {
		return vultureReference{}, "Python runtime reachability declaration " + declaration.Kind + " is not yet supported by the pack dead-code analyzer", nil
	}
	if declaration.Version != 1 {
		return vultureReference{}, "", fmt.Errorf("policy declaration %d uses unsupported python.contract version %d", index, declaration.Version)
	}
	contract, err := decodePythonContractDeclaration(declaration.Data)
	if err != nil {
		return vultureReference{}, "", fmt.Errorf("policy declaration %d: %w", index, err)
	}
	if contract.Kind != "entry-point" {
		return vultureReference{}, "Python contract kind " + contract.Kind + " is not yet supported by the pack dead-code analyzer", nil
	}
	reference, err := pythonEntryPointContractReference(manifest, contract)
	if err != nil {
		return vultureReference{}, "", fmt.Errorf("policy declaration %d: %w", index, err)
	}
	return reference, "", nil
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

func pythonEntryPointContractReference(manifest string, contract pythonContractDeclaration) (vultureReference, error) {
	module, symbol, validTarget := pythonEntryPointContractTarget(contract.Target)
	if !validTarget || !validPythonEntryPointContractShape(manifest, contract) {
		return vultureReference{}, errors.New("python entry-point contract is invalid")
	}
	members, err := pythonContractMembers(contract.Members)
	if err != nil {
		return vultureReference{}, err
	}
	id := "config:python.contract:entry-point:" + contract.Target
	return vultureReference{ID: id, Module: module, Symbol: symbol, Members: members, Contract: true}, nil
}

func pythonEntryPointContractTarget(target string) (string, string, bool) {
	module, symbol, found := strings.Cut(target, ":")
	valid := found && !strings.Contains(symbol, ":") && validPythonModuleParts(strings.Split(module, ".")) && validPythonModuleParts(strings.Split(symbol, "."))
	return module, symbol, valid
}

func validPythonEntryPointContractShape(manifest string, contract pythonContractDeclaration) bool {
	validReason := strings.TrimSpace(contract.Reason) != "" && len(contract.Reason) <= 4096
	return contract.Project == manifest && validReason && len(contract.Attributes) == 0 && len(contract.Decorators) == 0 && !contract.AnnotatedFields && len(contract.Keywords) == 0
}

func pythonContractMembers(values []string) ([]string, error) {
	members := append([]string{}, values...)
	for _, member := range members {
		if !validPythonModulePart(member) {
			return nil, errors.New("python entry-point contract contains an invalid member")
		}
	}
	sort.Strings(members)
	if len(slices.Compact(slices.Clone(members))) != len(members) {
		return nil, errors.New("python entry-point contract repeats a member")
	}
	return members, nil
}
