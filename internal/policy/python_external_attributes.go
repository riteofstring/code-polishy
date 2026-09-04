package policy

import (
	"fmt"
	"sort"
	"strings"
)

func validatePythonExternalAttributes(attributes []PythonExternalAttribute) error {
	seen := map[string]bool{}
	for index, attribute := range attributes {
		label := fmt.Sprintf("scope.pythonExternalAttributes[%d]", index)
		identity := pythonExternalAttributeIdentity(attribute)
		if seen[identity] {
			return fmt.Errorf("%s duplicates Python external attribute %q", label, pythonExternalAttributeDescription(attribute))
		}
		seen[identity] = true
	}
	sort.Slice(attributes, func(left, right int) bool {
		return pythonExternalAttributeIdentity(attributes[left]) < pythonExternalAttributeIdentity(attributes[right])
	})
	return nil
}

func pythonExternalAttributeIdentity(attribute PythonExternalAttribute) string {
	return strings.Join([]string{
		attribute.Project, attribute.Module, attribute.Callable, attribute.Receiver, attribute.Attribute,
		fmt.Sprint(attribute.Line), attribute.ConsumerType,
	}, "\x00")
}

func pythonExternalAttributeDescription(attribute PythonExternalAttribute) string {
	return fmt.Sprintf("%s:%s:%s:%s.%s:%d", attribute.Project, attribute.Module, attribute.Callable, attribute.Receiver, attribute.Attribute, attribute.Line)
}
