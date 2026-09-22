package pack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const maximumPolicyDeclarations = 4096
const maximumPolicyDeclarationBytes = 64 << 10
const maximumPolicyDeclarationDataBytes = 2 << 20

func policyDeclarations(scope policy.Scope, request Request) ([]PolicyDeclarationInput, error) {
	result := []PolicyDeclarationInput{}
	if request.Pack.Name != "python" {
		return result, nil
	}
	var err error
	switch request.Capability {
	case "architecture":
		err = appendPythonArchitectureDeclarations(&result, scope, request.Scopes)
	case "dead-code":
		err = appendPythonDeadCodeDeclarations(&result, scope, request.Scopes)
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(left, right int) bool { return policyDeclarationLess(result[left], result[right]) })
	return result, nil
}

func appendPythonArchitectureDeclarations(result *[]PolicyDeclarationInput, scope policy.Scope, scopes []AnalysisScope) error {
	for _, value := range scope.PythonComputedImports {
		inputs := []string{value.Project, value.Importer}
		for _, configuration := range value.Configuration {
			inputs = append(inputs, configuration.Path)
		}
		if err := addPolicyDeclaration(result, "python.computed-import", value.Project, inputs, value, scopes); err != nil {
			return err
		}
	}
	for _, value := range scope.PythonExternalPluginImports {
		inputs := []string{value.Project, value.Consumer.Importer}
		if value.Configuration != nil {
			inputs = append(inputs, value.Configuration.Path)
		}
		if err := addPolicyDeclaration(result, "python.external-plugin-import", value.Project, inputs, value, scopes); err != nil {
			return err
		}
	}
	for _, value := range scope.PythonRuntimeLoaders {
		inputs := []string{value.Project, value.Consumer.Importer}
		if err := addPolicyDeclaration(result, "python.runtime-loader", value.Project, inputs, value, scopes); err != nil {
			return err
		}
	}
	return nil
}

func appendPythonDeadCodeDeclarations(result *[]PolicyDeclarationInput, scope policy.Scope, scopes []AnalysisScope) error {
	for _, value := range scope.PythonContracts {
		if err := addPolicyDeclaration(result, "python.contract", value.Project, []string{value.Project}, value, scopes); err != nil {
			return err
		}
	}
	for _, value := range scope.PythonDynamicReferences {
		inputs := []string{value.Project, value.Consumer.Importer}
		if value.Registry != nil {
			inputs = append(inputs, value.Registry.Path)
		}
		if err := addPolicyDeclaration(result, "python.dynamic-reference", value.Project, inputs, value, scopes); err != nil {
			return err
		}
	}
	for _, value := range scope.PythonExternalAttributes {
		if err := addPolicyDeclaration(result, "python.external-attribute", value.Project, []string{value.Project}, value, scopes); err != nil {
			return err
		}
	}
	return nil
}

func policyDeclarationLess(left, right PolicyDeclarationInput) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if compared := strings.Compare(strings.Join(left.Scopes, "\x00"), strings.Join(right.Scopes, "\x00")); compared != 0 {
		return compared < 0
	}
	if compared := strings.Compare(strings.Join(left.Inputs, "\x00"), strings.Join(right.Inputs, "\x00")); compared != 0 {
		return compared < 0
	}
	return bytes.Compare(left.Data, right.Data) < 0
}

func addPolicyDeclaration(result *[]PolicyDeclarationInput, kind, project string, inputs []string, value any, scopes []AnalysisScope) error {
	handles := policyDeclarationScopes(project, scopes)
	if len(handles) == 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s policy declaration: %w", kind, err)
	}
	canonical, err := canonicalPolicyDeclarationData(data)
	if err != nil {
		return fmt.Errorf("%s policy declaration: %w", kind, err)
	}
	inputs = sortedUnique(slices.DeleteFunc(inputs, func(value string) bool { return value == "" }))
	*result = append(*result, PolicyDeclarationInput{Kind: kind, Version: 1, Scopes: handles, Inputs: inputs, Data: canonical})
	return nil
}

func policyDeclarationScopes(project string, scopes []AnalysisScope) []string {
	root := path.Dir(project)
	result := []string{}
	for _, scope := range scopes {
		if scope.Root == root || slices.Contains(scope.Context, project) {
			result = append(result, scope.Handle)
		}
	}
	return result
}

func validatePolicyDeclarations(input *PolicyInput, request Request) error {
	if input.Declarations == nil || len(input.Declarations) > maximumPolicyDeclarations {
		return expected("policy.declarations", fmt.Sprintf("an explicit array of at most %d items", maximumPolicyDeclarations))
	}
	validation := newPolicyDeclarationValidation(request)
	totalBytes := 0
	totalInputs := 0
	seen := map[string]bool{}
	for index := range input.Declarations {
		declaration := &input.Declarations[index]
		label := indexed("policy.declarations", index)
		dataBytes, err := validatePolicyDeclaration(declaration, label, validation)
		if err != nil {
			return err
		}
		totalBytes += dataBytes
		if totalBytes > maximumPolicyDeclarationDataBytes {
			return expected(label+".data", fmt.Sprintf("aggregate declaration data of at most %d bytes", maximumPolicyDeclarationDataBytes))
		}
		totalInputs += len(declaration.Inputs)
		if totalInputs > maximumInventoryEntries {
			return expected(label+".inputs", fmt.Sprintf("at most %d declaration inputs in aggregate", maximumInventoryEntries))
		}
		identity := policyDeclarationIdentity(*declaration)
		if seen[identity] {
			return expected(label, "a unique declaration")
		}
		seen[identity] = true
	}
	return nil
}

type policyDeclarationValidation struct {
	handleOrder    map[string]int
	scopesByHandle map[string]AnalysisScope
	inventory      map[string]InventoryEntry
}

func newPolicyDeclarationValidation(request Request) policyDeclarationValidation {
	result := policyDeclarationValidation{handleOrder: map[string]int{}, scopesByHandle: map[string]AnalysisScope{}, inventory: inventoryByPath(request.Inventory)}
	for index, scope := range request.Scopes {
		result.handleOrder[scope.Handle] = index
		result.scopesByHandle[scope.Handle] = scope
	}
	return result
}

func validatePolicyDeclaration(declaration *PolicyDeclarationInput, label string, validation policyDeclarationValidation) (int, error) {
	if len(declaration.Kind) > 256 || !identifierPattern.MatchString(declaration.Kind) || !strings.Contains(declaration.Kind, ".") {
		return 0, expected(label+".kind", "a namespaced identifier of at most 256 bytes")
	}
	if declaration.Version < 1 {
		return 0, expected(label+".version", "a positive contract version")
	}
	if err := validatePolicyDeclarationScopes(*declaration, label, validation.handleOrder); err != nil {
		return 0, err
	}
	if err := validatePolicyDeclarationInputs(*declaration, label, validation); err != nil {
		return 0, err
	}
	dataBytes := len(declaration.Data)
	canonical, err := canonicalPolicyDeclarationData(declaration.Data)
	if err != nil {
		return 0, expected(label+".data", strings.TrimPrefix(err.Error(), "expected "))
	}
	declaration.Data = canonical
	return dataBytes, nil
}

func validatePolicyDeclarationScopes(declaration PolicyDeclarationInput, label string, handleOrder map[string]int) error {
	if len(declaration.Scopes) == 0 || len(declaration.Scopes) > maximumDiscoveryScopes {
		return expected(label+".scopes", fmt.Sprintf("1 to %d invocation scope handles", maximumDiscoveryScopes))
	}
	seen := map[string]bool{}
	previousOrder := -1
	for index, handle := range declaration.Scopes {
		order, found := handleOrder[handle]
		if !found || seen[handle] || order <= previousOrder {
			return expected(indexed(label+".scopes", index), "a unique handle from scopes in invocation order")
		}
		seen[handle] = true
		previousOrder = order
	}
	return nil
}

func validatePolicyDeclarationInputs(declaration PolicyDeclarationInput, label string, validation policyDeclarationValidation) error {
	if declaration.Inputs == nil || len(declaration.Inputs) > maximumDiscoveryScopes {
		return expected(label+".inputs", fmt.Sprintf("an explicit array of at most %d governed paths", maximumDiscoveryScopes))
	}
	previous := ""
	for index, input := range declaration.Inputs {
		entry, found := validation.inventory[input]
		if !found || index > 0 && input <= previous || !policyDeclarationInputAuthorized(input, entry, declaration.Scopes, validation.scopesByHandle) {
			return expected(indexed(label+".inputs", index), "a unique governed path authorized by a bound scope in lexical order")
		}
		previous = input
	}
	return nil
}

func policyDeclarationIdentity(declaration PolicyDeclarationInput) string {
	return declaration.Kind + "\x00" + fmt.Sprint(declaration.Version) + "\x00" + strings.Join(declaration.Scopes, "\x00") + "\x00" + string(declaration.Data)
}

func policyDeclarationInputAuthorized(input string, entry InventoryEntry, handles []string, scopes map[string]AnalysisScope) bool {
	for _, handle := range handles {
		scope := scopes[handle]
		if entry.Source {
			if slices.Contains(scope.Members, input) {
				return true
			}
			continue
		}
		if scope.Root == "." || input == scope.Root || strings.HasPrefix(input, scope.Root+"/") {
			return true
		}
	}
	return false
}

func canonicalPolicyDeclarationData(data json.RawMessage) (json.RawMessage, error) {
	if len(data) > maximumPolicyDeclarationBytes {
		return nil, fmt.Errorf("expected an object of at most %d bytes", maximumPolicyDeclarationBytes)
	}
	canonical, err := canonicalScopeData(data)
	if err != nil {
		return nil, err
	}
	object := map[string]json.RawMessage{}
	if err := json.Unmarshal(canonical, &object); err != nil || object == nil {
		return nil, errors.New("expected a JSON object")
	}
	return canonical, nil
}
