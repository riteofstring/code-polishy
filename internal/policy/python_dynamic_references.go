package policy

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

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

func pythonDynamicReferenceIdentity(reference PythonDynamicReference) string {
	identity := fmt.Sprintf("%s\x00%s\x00%09d\x00%09d", reference.Project, reference.Consumer.Importer, reference.Consumer.Site.Line, reference.Consumer.Site.Column)
	if reference.Consumer.Kind != "callsite" {
		identity += "\x00" + reference.Consumer.Implementation + "\x00" + reference.Consumer.Member
	}
	return identity
}

func pythonDynamicReferenceDescription(reference PythonDynamicReference) string {
	return fmt.Sprintf("%s:%s:%d:%d", reference.Project, reference.Consumer.Importer, reference.Consumer.Site.Line, reference.Consumer.Site.Column)
}

func validatePythonDynamicReference(reference PythonDynamicReference, label string) error {
	root := path.Dir(reference.Project)
	paths := []string{reference.Consumer.Importer}
	if reference.Registry != nil {
		paths = append(paths, reference.Registry.Path)
	}
	for _, candidate := range paths {
		if root != "." && !strings.HasPrefix(candidate, root+"/") {
			return fmt.Errorf("%s consumer and registry must belong to the declared project", label)
		}
	}
	return nil
}
