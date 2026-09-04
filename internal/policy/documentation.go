package policy

import (
	"fmt"
	pathpkg "path"
	"strings"
)

type documentationValidation struct {
	documents map[string]bool
	modules   map[string]bool
	sources   map[string]bool
}

func validateDocumentation(config *Config) error {
	state := documentationValidation{
		documents: map[string]bool{},
		modules:   map[string]bool{},
		sources:   map[string]bool{},
	}
	for index, document := range config.Documentation.Design {
		if err := state.validateDocument(config, index, document); err != nil {
			return err
		}
	}
	return nil
}

func IsMarkdownPath(path string) bool {
	return strings.EqualFold(pathpkg.Ext(path), ".md") || strings.EqualFold(pathpkg.Ext(path), ".markdown")
}

func (state documentationValidation) validateDocument(config *Config, index int, document DesignDocument) error {
	label := fmt.Sprintf("documentation.design[%d]", index)
	if state.documents[document.Path] {
		return fmt.Errorf("duplicate design document path %q", document.Path)
	}
	state.documents[document.Path] = true
	return state.validateTarget(config, document, label)
}

func (state documentationValidation) validateTarget(config *Config, document DesignDocument, label string) error {
	if document.Module != "" {
		return state.validateModule(config, document.Module, label)
	}
	return validateDesignSourcePaths(config, document.SourcePaths, label+".sourcePaths", state.sources)
}

func (state documentationValidation) validateModule(config *Config, module, label string) error {
	if _, exists := config.ModuleByName[module]; !exists {
		return fmt.Errorf("%s.module references unknown module %q", label, module)
	}
	if state.modules[module] {
		return fmt.Errorf("module %q has more than one design document", module)
	}
	state.modules[module] = true
	return nil
}

func validateDesignSourcePaths(config *Config, paths []string, label string, seen map[string]bool) error {
	for index, path := range paths {
		pathLabel := fmt.Sprintf("%s[%d]", label, index)
		if seen[path] {
			return fmt.Errorf("source path %q has more than one design document", path)
		}
		seen[path] = true
		if owners := configuredModuleNames(config, path); len(owners) != 1 {
			return fmt.Errorf("%s must match exactly one module, matched %d", pathLabel, len(owners))
		}
	}
	return nil
}

func configuredModuleNames(config *Config, path string) []string {
	owners := []string{}
	for _, module := range config.Modules {
		if MatchesAny(path, module.Paths) {
			owners = append(owners, module.Name)
		}
	}
	return owners
}
