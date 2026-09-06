package policy

import (
	"fmt"
	"path"
	"strings"
)

type PythonRuntimeLoader struct {
	Project      string                `json:"project"`
	Consumer     PythonDynamicConsumer `json:"consumer"`
	InputGrammar string                `json:"inputGrammar"`
	Check        PythonRuntimeCheck    `json:"check"`
	Reason       string                `json:"reason"`
}

func validatePythonRuntimeLoaders(scope *Scope) error {
	seen := map[string]bool{}
	for _, value := range scope.PythonComputedImports {
		seen[pythonComputedImportIdentity(value)] = true
	}
	for _, value := range scope.PythonExternalPluginImports {
		seen[pythonExternalPluginIdentity(value)] = true
	}
	for index, value := range scope.PythonRuntimeLoaders {
		label := fmt.Sprintf("scope.pythonRuntimeLoaders[%d]", index)
		root := path.Dir(value.Project)
		if root != "." && !strings.HasPrefix(value.Consumer.Importer, root+"/") {
			return fmt.Errorf("%s importer must belong to its project", label)
		}
		if err := validatePythonRuntimeLoaderShape(value, label); err != nil {
			return err
		}

		identity := fmt.Sprintf("%s\x00%s\x00%09d\x00%09d", value.Project, value.Consumer.Importer, value.Consumer.Site.Line, value.Consumer.Site.Column)
		if seen[identity] {
			return fmt.Errorf("%s duplicates a loader declaration", label)
		}
		seen[identity] = true
	}
	return nil
}

func validatePythonRuntimeLoaderShape(value PythonRuntimeLoader, label string) error {
	if value.Consumer.Kind != "callsite" || value.Consumer.Callee != "importlib.import_module" || value.Consumer.Shape != "call" || value.Consumer.Callable == "" || value.Check.Kind != "isinstance" {
		return fmt.Errorf("%s requires an import_module callsite and rejecting isinstance check", label)
	}
	return nil
}
