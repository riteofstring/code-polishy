package policy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var pythonIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validatePythonExternalAttributes(attributes []PythonExternalAttribute) error {
	seen := map[string]bool{}
	for index, attribute := range attributes {
		label := fmt.Sprintf("scope.pythonExternalAttributes[%d]", index)
		if err := validatePythonDynamicReferenceProject(attribute.Project, label+".project"); err != nil {
			return err
		}
		for _, field := range []struct{ name, value string }{
			{"module", attribute.Module}, {"callable", attribute.Callable}, {"consumerType", attribute.ConsumerType},
		} {
			if err := validatePythonIdentifierChain(field.value, label+"."+field.name); err != nil {
				return err
			}
		}
		if !strings.Contains(attribute.ConsumerType, ".") {
			return fmt.Errorf("%s.consumerType must be a qualified external Python type", label)
		}
		for _, field := range []struct{ name, value string }{{"receiver", attribute.Receiver}, {"attribute", attribute.Attribute}} {
			if !pythonIdentifierPattern.MatchString(field.value) {
				return fmt.Errorf("%s.%s must be one Python identifier", label, field.name)
			}
		}
		if attribute.Line < 1 {
			return fmt.Errorf("%s.line must be positive", label)
		}
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
