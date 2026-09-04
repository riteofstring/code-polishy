package policy

import (
	"fmt"
	"sort"
)

func validatePythonDynamicReferences(references []PythonDynamicReference) error {
	seen := map[string]bool{}
	for index, reference := range references {
		label := fmt.Sprintf("scope.pythonDynamicReferences[%d]", index)
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
	return reference.Project + "\x00" + reference.Module + "\x00" + reference.Symbol
}

func pythonDynamicReferenceDescription(reference PythonDynamicReference) string {
	return reference.Project + ":" + reference.Module + ":" + reference.Symbol
}
