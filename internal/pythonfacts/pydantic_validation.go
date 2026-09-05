package pythonfacts

import (
	"encoding/json"
	"fmt"
)

func validatePydanticMembers(members []ReachabilityDefinition, modules []TypeModule) error {
	definitions := map[ReachabilityDefinition]int{}
	for _, module := range modules {
		source, err := pydanticSourceDefinitions(module)
		if err != nil {
			return err
		}
		for _, definition := range source {
			definitions[definition]++
		}
	}
	for _, member := range members {
		if definitions[member] != 1 {
			return fmt.Errorf("python model evidence substitutes or duplicates a source member")
		}
		delete(definitions, member)
	}
	return nil
}

func pydanticSourceDefinitions(module TypeModule) ([]ReachabilityDefinition, error) {
	definitions := []ReachabilityDefinition{}
	var facts struct {
		Scopes []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"scopes"`
		Bindings []struct {
			Name           string         `json:"name"`
			Scope          string         `json:"scope"`
			Kind           string         `json:"kind"`
			Conditional    bool           `json:"conditional"`
			DefinitionLine int            `json:"definitionLine"`
			EndSite        SourceLocation `json:"endSite"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(module.Facts, &facts); err != nil {
		return nil, err
	}
	classes := map[string]bool{}
	for _, scope := range facts.Scopes {
		classes[scope.ID] = scope.Kind == "class"
	}
	for _, binding := range facts.Bindings {
		if !classes[binding.Scope] || binding.Conditional || (binding.Kind != "function" && binding.Kind != "alias" && binding.Kind != "annotated") {
			continue
		}
		end := binding.DefinitionLine
		if binding.Kind == "function" {
			end = binding.EndSite.Line
		}
		definitions = append(definitions, ReachabilityDefinition{Path: module.Path, Line: binding.DefinitionLine, End: end, Name: binding.Name})
	}
	return definitions, nil
}
