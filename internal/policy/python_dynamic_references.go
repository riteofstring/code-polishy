package policy

import (
	"fmt"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
)

var pythonIdentifierChainPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*$`)

func validatePythonDynamicReferences(references []PythonDynamicReference) error {
	seen := map[string]bool{}
	for index, reference := range references {
		label := fmt.Sprintf("scope.pythonDynamicReferences[%d]", index)
		if err := validatePythonDynamicReference(reference, label); err != nil {
			return err
		}
		identity := pythonDynamicReferenceIdentity(reference)
		if seen[identity] {
			return fmt.Errorf("%s duplicates Python dynamic reference %q", label, pythonDynamicReferenceDescription(reference))
		}
		seen[identity] = true
	}
	sort.Slice(references, func(left, right int) bool {
		return pythonDynamicReferenceIdentity(references[left]) < pythonDynamicReferenceIdentity(references[right])
	})
	return nil
}

func validatePythonDynamicReference(reference PythonDynamicReference, label string) error {
	if err := validatePythonDynamicReferenceProject(reference.Project, label+".project"); err != nil {
		return err
	}
	if err := validatePythonIdentifierChain(reference.Module, label+".module"); err != nil {
		return err
	}
	return validatePythonIdentifierChain(reference.Symbol, label+".symbol")
}

func validatePythonDynamicReferenceProject(project, label string) error {
	if err := concreteRepositoryPath(project, label); err != nil {
		return err
	}
	if strings.ContainsAny(project, "{}") || pathpkg.Base(project) != "pyproject.toml" {
		return fmt.Errorf("%s must name an exact pyproject.toml file", label)
	}
	return nil
}

func validatePythonIdentifierChain(value, label string) error {
	if !pythonIdentifierChainPattern.MatchString(value) {
		return fmt.Errorf("%s must be a Python identifier chain", label)
	}
	return nil
}

func pythonDynamicReferenceIdentity(reference PythonDynamicReference) string {
	return reference.Project + "\x00" + reference.Module + "\x00" + reference.Symbol
}

func pythonDynamicReferenceDescription(reference PythonDynamicReference) string {
	return reference.Project + ":" + reference.Module + ":" + reference.Symbol
}
