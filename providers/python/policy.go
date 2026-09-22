package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

const maximumPythonPolicyDeclarations = 4096

type scopedComputedImport struct {
	declaration computedImportDeclaration
	inputs      []string
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
