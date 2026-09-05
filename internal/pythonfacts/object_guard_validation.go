package pythonfacts

import (
	"encoding/json"
	"fmt"
	"slices"
)

type objectGuardCall struct {
	Scope     string                     `json:"scope"`
	Site      SourceLocation             `json:"site"`
	EndSite   SourceLocation             `json:"endSite"`
	Statement int                        `json:"statement"`
	Guard     bool                       `json:"guard"`
	Arguments []objectImportCallArgument `json:"arguments"`
	Keywords  []json.RawMessage          `json:"keywords"`
}

type objectGuardBinding struct {
	Name            string         `json:"name"`
	Kind            string         `json:"kind"`
	Scope           string         `json:"scope"`
	Conditional     bool           `json:"conditional"`
	DefinitionLine  int            `json:"definitionLine"`
	EndSite         SourceLocation `json:"endSite"`
	ObjectPredicate *struct {
		Namespace string `json:"namespace"`
	} `json:"objectPredicate"`
}

type objectGuardSource struct {
	Calls    []objectGuardCall    `json:"calls"`
	Bindings []objectGuardBinding `json:"bindings"`
}

type objectGuardBindingKey struct {
	Scope string
	Name  string
}

type objectGuardIndex struct {
	Calls      map[objectImportCallSite]objectGuardCall
	Bindings   map[objectGuardBindingKey][]objectGuardBinding
	Predicates map[ReachabilityDefinition]objectGuardBinding
}

func indexObjectGuardSource(module TypeModule) (objectGuardIndex, error) {
	var source objectGuardSource
	if err := json.Unmarshal(module.Facts, &source); err != nil {
		return objectGuardIndex{}, err
	}
	result := objectGuardIndex{Calls: map[objectImportCallSite]objectGuardCall{}, Bindings: map[objectGuardBindingKey][]objectGuardBinding{}, Predicates: map[ReachabilityDefinition]objectGuardBinding{}}
	for _, call := range source.Calls {
		result.Calls[objectImportCallSite{Path: module.Path, Site: call.Site, EndSite: call.EndSite}] = call
	}
	for _, binding := range source.Bindings {
		key := objectGuardBindingKey{Scope: binding.Scope, Name: binding.Name}
		result.Bindings[key] = append(result.Bindings[key], binding)
		if binding.ObjectPredicate != nil && binding.Kind == "function" {
			result.Predicates[ReachabilityDefinition{Path: module.Path, Name: binding.Name, Line: binding.DefinitionLine, End: binding.EndSite.Line}] = binding
		}
	}
	return result, nil
}

func validateObjectInputGuards(modules []TypeModule, imports []ObjectImport) error {
	if !slices.ContainsFunc(imports, func(value ObjectImport) bool { return value.Guard != nil }) {
		return nil
	}
	sources, digests := map[string]objectGuardIndex{}, map[string]string{}
	for _, module := range modules {
		source, err := indexObjectGuardSource(module)
		if err != nil {
			return err
		}
		sources[module.Path], digests[module.Path] = source, module.SourceSHA256
	}
	for _, imported := range imports {
		if imported.Guard == nil {
			continue
		}
		if err := validateObjectGuardCalls(sources[imported.Path], imported); err != nil {
			return err
		}
		guard := *imported.Guard
		if digests[guard.Predicate.Path] != guard.PredicateSHA256 || guard.PredicateSHA256 == "" {
			return fmt.Errorf("object input guard has a changed predicate source")
		}
		if err := validateObjectGuardPredicate(sources[guard.Predicate.Path], guard); err != nil {
			return err
		}
	}
	return nil
}

func validateObjectGuardCalls(source objectGuardIndex, imported ObjectImport) error {
	guard := source.Calls[objectImportCallSite{Path: imported.Path, Site: imported.Guard.Site, EndSite: imported.Guard.EndSite}]
	loader := source.Calls[objectImportCallSite{Path: imported.Path, Site: imported.Site, EndSite: imported.EndSite}]
	if !validObjectGuardCall(guard, loader) {
		return fmt.Errorf("object input guard does not precede its exact loader")
	}
	name := loader.Arguments[0].Value
	if guard.Arguments[0].Kind != "name" || guard.Arguments[0].Value != name {
		return fmt.Errorf("object input guard checks a different argument")
	}
	bindings := source.Bindings[objectGuardBindingKey{Scope: loader.Scope, Name: name}]
	if len(bindings) != 1 || bindings[0].Kind != "parameter" || bindings[0].Conditional {
		return fmt.Errorf("object input guard does not bind one unchanged parameter")
	}
	return nil
}

func validObjectGuardCall(guard, loader objectGuardCall) bool {
	return guard.Guard && guard.Scope == loader.Scope && guard.Statement >= 0 && guard.Statement < loader.Statement && len(guard.Arguments) == 1 && len(guard.Keywords) == 0 && len(loader.Arguments) == 1
}

func validateObjectGuardPredicate(source objectGuardIndex, guard ObjectInputGuard) error {
	binding, found := source.Predicates[guard.Predicate]
	if found && binding.ObjectPredicate.Namespace == guard.Namespace && guard.Namespace != "" {
		return nil
	}
	return fmt.Errorf("object input guard has no matching predicate contract")
}
