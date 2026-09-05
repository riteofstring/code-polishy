package policy

import (
	"fmt"
	"slices"
	"strings"
)

const MaximumOperationalHandoffs = 64

func HandoffContextSituations(requested []string, workflow string) ([]string, error) {
	if err := ValidateHandoffSituations(requested); err != nil {
		return nil, err
	}
	situations := slices.Clone(requested)
	if workflow != "" {
		if err := ValidateHandoffSituations([]string{workflow}); err != nil {
			return nil, err
		}
		if !slices.Contains(situations, workflow) {
			situations = append(situations, workflow)
		}
	}
	slices.Sort(situations)
	return situations, nil
}

func ValidateHandoffSituations(situations []string) error {
	if len(situations) > 32 {
		return fmt.Errorf("at most 32 exact handoff situations may be selected")
	}
	seen := map[string]bool{}
	for _, situation := range situations {
		if len(situation) > 128 || !identifierPattern.MatchString(situation) {
			return fmt.Errorf("handoff situation %q must be an exact identifier of at most 128 bytes", situation)
		}
		if seen[situation] {
			return fmt.Errorf("handoff situation %q was selected more than once", situation)
		}
		seen[situation] = true
	}
	return nil
}

func validateOperationalHandoffs(config *Config) error {
	names := map[string]bool{}
	paths := map[string]bool{}
	for index, handoff := range config.Documentation.Handoffs {
		label := fmt.Sprintf("documentation.handoffs[%d]", index)
		if names[handoff.Name] {
			return fmt.Errorf("%s.name duplicates handoff %q", label, handoff.Name)
		}
		names[handoff.Name] = true
		if paths[handoff.Path] {
			return fmt.Errorf("%s.path has more than one handoff declaration: %q", label, handoff.Path)
		}
		paths[handoff.Path] = true
		if len(handoff.Description) > 512 || strings.TrimSpace(handoff.Description) != handoff.Description {
			return fmt.Errorf("%s.description must be concise, trimmed text of at most 512 UTF-8 bytes", label)
		}
		for _, module := range handoff.Modules {
			if _, exists := config.ModuleByName[module]; !exists {
				return fmt.Errorf("%s.modules references unknown module %q", label, module)
			}
		}
	}
	return nil
}
