package policy

import (
	"fmt"
	"strings"
)

func identifier(value, label string) error {
	if err := nonempty(value, label); err != nil {
		return err
	}
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%s must be a lowercase dotted or dashed identifier", label)
	}
	return nil
}

func nonempty(value, label string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must be a non-empty string", label)
	}
	return nil
}
