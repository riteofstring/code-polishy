package policy

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

type PythonExternalPluginImport struct {
	Project       string                     `json:"project"`
	Consumer      PythonDynamicConsumer      `json:"consumer"`
	Distribution  string                     `json:"distribution"`
	Namespace     string                     `json:"namespace"`
	InputGrammar  string                     `json:"inputGrammar"`
	Check         PythonRuntimeCheck         `json:"check"`
	Configuration *PythonComputedImportInput `json:"configuration,omitempty"`
}

type PythonRuntimeCheck struct {
	Kind     string               `json:"kind"`
	Protocol string               `json:"protocol"`
	Site     PythonSourceLocation `json:"site"`
}

func validatePythonExternalPluginImports(scope *Scope) error {
	seen := map[string]bool{}
	for index, declaration := range scope.PythonExternalPluginImports {
		label := fmt.Sprintf("scope.pythonExternalPluginImports[%d]", index)
		paths := []string{declaration.Consumer.Importer}
		if declaration.Configuration != nil {
			paths = append(paths, declaration.Configuration.Path)
		}
		root := path.Dir(declaration.Project)
		for _, candidate := range paths {
			if root != "." && !strings.HasPrefix(candidate, root+"/") {
				return fmt.Errorf("%s consumer and configuration must belong to the declared project", label)
			}
		}
		identity := pythonExternalPluginIdentity(declaration)
		if seen[identity] {
			return fmt.Errorf("%s repeats a loader callsite", label)
		}
		seen[identity] = true
	}
	for _, declaration := range scope.PythonComputedImports {
		if seen[pythonComputedImportIdentity(declaration)] {
			return fmt.Errorf("one Python loader cannot declare both local computed imports and external plug-in admission")
		}
	}
	slices.SortFunc(scope.PythonExternalPluginImports, func(left, right PythonExternalPluginImport) int {
		return strings.Compare(pythonExternalPluginIdentity(left), pythonExternalPluginIdentity(right))
	})
	return nil
}

func pythonExternalPluginIdentity(declaration PythonExternalPluginImport) string {
	return fmt.Sprintf("%s\x00%s\x00%09d\x00%09d", declaration.Project, declaration.Consumer.Importer, declaration.Consumer.Site.Line, declaration.Consumer.Site.Column)
}
