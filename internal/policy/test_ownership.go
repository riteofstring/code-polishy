package policy

import (
	"fmt"
	"slices"
	"strings"
)

func validateTestOwnership(config *Config) error {
	if err := validateTestPatterns(config.Tests.Paths, "tests.paths"); err != nil {
		return err
	}
	patterns := []string{}
	for index, ownership := range config.Tests.Ownership {
		label := fmt.Sprintf("tests.ownership[%d]", index)
		if err := validateTestPatterns(ownership.Paths, label+".paths"); err != nil {
			return err
		}
		if _, exists := config.ModuleByName[ownership.Module]; !exists {
			return fmt.Errorf("%s.module references unknown production module %q", label, ownership.Module)
		}
		suite, err := referencedSuite(config.Tests.Suites, ownership.FocusedSuite, label+".focusedSuite")
		if err != nil {
			return err
		}
		if !IsPrimaryTestSuite(suite, ownership.Module) {
			return fmt.Errorf("%s.focusedSuite must name a quick module-scoped suite for %q in focused, recommended, and full", label, ownership.Module)
		}
		if len(suite.Paths) == 0 {
			return fmt.Errorf("%s.focusedSuite %q requires explicit execution paths covering its owned tests", label, suite.Name)
		}
		for _, pattern := range ownership.Paths {
			if len(patterns) >= 4096 {
				return fmt.Errorf("tests.ownership exceeds the 4096-pattern resource limit")
			}
			if err := nonOverlappingTestPattern(pattern, patterns, label); err != nil {
				return err
			}
			patterns = append(patterns, pattern)
		}
	}
	return nil
}

func IsPrimaryTestSuite(suite TestSuite, module string) bool {
	return suite.Scope == "module" && suite.Cost == "quick" &&
		slices.Equal(suite.Modules, []string{module}) &&
		slices.Contains(suite.RunOn, "focused") && slices.Contains(suite.RunOn, "recommended") &&
		slices.Contains(suite.RunOn, "full") && !slices.Contains(suite.RunOn, "supplemental")
}

func nonOverlappingTestPattern(pattern string, previous []string, label string) error {
	for _, other := range previous {
		if PatternsOverlap(pattern, other) {
			return fmt.Errorf("%s.paths pattern %q overlaps ownership pattern %q", label, pattern, other)
		}
	}
	return nil
}

func validateTestPatterns(patterns []string, label string) error {
	if err := rejectUniversalPatterns(patterns, label); err != nil {
		return err
	}
	for _, pattern := range patterns {
		if len(pattern) == 0 || len(pattern) > 1024 || strings.ContainsAny(pattern, "\\:\x00\r\n\t") {
			return fmt.Errorf("%s pattern must be a bounded portable repository path", label)
		}
		for _, segment := range strings.Split(pattern, "/") {
			if segment == "" || segment == "." || segment == ".." {
				return fmt.Errorf("%s pattern %q must be canonical and contained", label, pattern)
			}
		}
	}
	return nil
}
