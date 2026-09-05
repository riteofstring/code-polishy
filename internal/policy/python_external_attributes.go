package policy

import (
	"fmt"
	"sort"
	"strings"
)

type PythonSourceLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type PythonExternalReceiver struct {
	Kind     string                  `json:"kind"`
	Name     string                  `json:"name"`
	Binding  PythonSourceLocation    `json:"binding"`
	Type     string                  `json:"type,omitempty"`
	Consumer *PythonExternalConsumer `json:"consumer,omitempty"`
}

type PythonExternalConsumer struct {
	Kind      string               `json:"kind"`
	Qualified string               `json:"qualified"`
	Site      PythonSourceLocation `json:"site"`
}

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
		attribute.Project, attribute.Module, attribute.Callable,
		fmt.Sprint(attribute.Write.Line), fmt.Sprint(attribute.Write.Column),
	}, "\x00")
}

func pythonExternalAttributeDescription(attribute PythonExternalAttribute) string {
	return fmt.Sprintf("%s:%s:%s:%s.%s:%d:%d", attribute.Project, attribute.Module, attribute.Callable, attribute.Receiver.Name, attribute.Attribute, attribute.Write.Line, attribute.Write.Column)
}
