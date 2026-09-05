package pythonfacts

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func validateRuntimeCheckProject(modules []TypeModule, requests []RuntimeCheckInput, result RuntimeCheckProject) error {
	expected, err := runtimeCheckRequests(requests, result.Problems)
	if err != nil {
		return err
	}
	calls, err := objectImportCallInputs(modules)
	if err != nil {
		return err
	}
	bindings, err := runtimeCheckBindings(modules)
	if err != nil {
		return err
	}
	for _, evidence := range result.Evidence {
		input, found := expected[evidence.ID]
		if !found {
			return fmt.Errorf("runtime check evidence is duplicated or names an unresolved request")
		}
		delete(expected, evidence.ID)
		if err := validateRuntimeCheckInput(input, evidence, calls); err != nil {
			return err
		}
		if err := validateRuntimeCheckBindings(evidence, bindings, calls); err != nil {
			return err
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("runtime check evidence omits a resolved request")
	}
	return nil
}

func runtimeCheckRequests(requests []RuntimeCheckInput, problems []RuntimeCheckProblem) (map[string]RuntimeCheckInput, error) {
	remaining := map[string]RuntimeCheckInput{}
	for _, request := range requests {
		if _, found := remaining[request.ID]; found || request.ID == "" {
			return nil, fmt.Errorf("runtime check request identity is empty or duplicated")
		}
		remaining[request.ID] = request
	}
	for _, problem := range problems {
		if _, found := remaining[problem.ID]; !found || problem.Message == "" || len(problem.Message) > 16*1024 {
			return nil, fmt.Errorf("runtime check problem is not one exact bounded request")
		}
		delete(remaining, problem.ID)
	}
	return remaining, nil
}

func validateRuntimeCheckInput(input RuntimeCheckInput, evidence RuntimeCheckEvidence, calls map[objectImportCallSite]objectImportCallArgument) error {
	var consumer struct {
		Importer string         `json:"importer"`
		Site     SourceLocation `json:"site"`
		Argument string         `json:"argument"`
	}
	if err := json.Unmarshal(input.Consumer, &consumer); err != nil {
		return err
	}
	if evidence.Importer != consumer.Importer || evidence.LoaderSite != consumer.Site || evidence.CheckSite != input.Check.Site || evidence.Kind != input.Check.Kind || evidence.Contract != input.Check.Protocol {
		return fmt.Errorf("runtime check evidence changed its source or declared contract")
	}
	loader, found := calls[objectImportCallSite{Path: evidence.Importer, Site: evidence.LoaderSite, EndSite: evidence.LoaderEndSite}]
	if !found || loader.Text != consumer.Argument {
		return fmt.Errorf("runtime check evidence changed its loader span or argument")
	}
	if _, found := calls[objectImportCallSite{Path: evidence.Importer, Site: evidence.CheckSite, EndSite: evidence.CheckEndSite}]; !found {
		return fmt.Errorf("runtime check evidence changed its check span")
	}
	return validateRuntimeCheckIdentity(evidence)
}

func validateRuntimeCheckIdentity(evidence RuntimeCheckEvidence) error {
	if len(evidence.Identity) != 64 || evidence.Identity != strings.ToLower(evidence.Identity) || evidence.RuntimeType == "" {
		return fmt.Errorf("runtime check evidence omits its canonical identity or resolved type")
	}
	if _, err := hex.DecodeString(evidence.Identity); err != nil {
		return fmt.Errorf("runtime check evidence has an invalid identity")
	}
	return nil
}

type runtimeCheckBinding struct {
	Name           string          `json:"name"`
	DefinitionLine int             `json:"definitionLine"`
	EndSite        SourceLocation  `json:"endSite"`
	ValueSite      *SourceLocation `json:"valueSite"`
	ValueEndSite   *SourceLocation `json:"valueEndSite"`
	Value          struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"value"`
}

func runtimeCheckBindings(modules []TypeModule) (map[ReachabilityDefinition]runtimeCheckBinding, error) {
	result := map[ReachabilityDefinition]runtimeCheckBinding{}
	for _, module := range modules {
		var facts struct {
			Bindings []runtimeCheckBinding `json:"bindings"`
		}
		if err := json.Unmarshal(module.Facts, &facts); err != nil {
			return nil, err
		}
		for _, binding := range facts.Bindings {
			result[ReachabilityDefinition{Path: module.Path, Line: binding.DefinitionLine, End: binding.EndSite.Line, Name: binding.Name}] = binding
		}
	}
	return result, nil
}

func validateRuntimeCheckBindings(evidence RuntimeCheckEvidence, bindings map[ReachabilityDefinition]runtimeCheckBinding, calls map[objectImportCallSite]objectImportCallArgument) error {
	if evidence.Bindings == nil || len(evidence.Bindings) > 128 {
		return fmt.Errorf("runtime check omits its bounded loaded-value trace")
	}
	seen := map[ReachabilityDefinition]bool{}
	previous := ""
	for _, definition := range evidence.Bindings {
		binding, found := bindings[definition]
		if !found || definition.Path != evidence.Importer || seen[definition] {
			return fmt.Errorf("runtime check loaded-value trace has a foreign, changed, or repeated binding")
		}
		if !runtimeCheckBindingConnected(binding, previous, evidence) {
			return fmt.Errorf("runtime check loaded-value trace is disconnected from its loader")
		}
		seen[definition] = true
		previous = binding.Name
	}
	check := calls[objectImportCallSite{Path: evidence.Importer, Site: evidence.CheckSite, EndSite: evidence.CheckEndSite}]
	return validateRuntimeCheckedArgument(evidence, previous, check)
}

func runtimeCheckBindingConnected(binding runtimeCheckBinding, previous string, evidence RuntimeCheckEvidence) bool {
	if previous != "" {
		return binding.Value.Kind == "name" && binding.Value.Name == previous
	}
	return binding.ValueSite != nil && binding.ValueEndSite != nil && *binding.ValueSite == evidence.LoaderSite && *binding.ValueEndSite == evidence.LoaderEndSite
}

func validateRuntimeCheckedArgument(evidence RuntimeCheckEvidence, previous string, check objectImportCallArgument) error {
	if previous != "" {
		if check.Kind != "name" || check.Value != previous {
			return fmt.Errorf("runtime check consumes a different loaded value")
		}
		return nil
	}
	if check.Kind != "call" || check.Site != evidence.LoaderSite || check.EndSite != evidence.LoaderEndSite {
		return fmt.Errorf("runtime check omits its required loaded-value trace")
	}
	return nil
}
