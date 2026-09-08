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
		if _, err := TestOwnershipExecutionSuite(config.Tests.Suites, ownership); err != nil {
			return fmt.Errorf("%s: %w", label, err)
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

func TestOwnershipExecutionSuite(suites []TestSuite, ownership TestOwnership) (TestSuite, error) {
	focused, err := testOwnershipFocusedSuite(suites, ownership)
	if err != nil || ownership.ExecutionSuite == "" {
		return focused, err
	}
	execution, err := referencedSuite(suites, ownership.ExecutionSuite, "executionSuite")
	if err != nil {
		return TestSuite{}, err
	}
	if !isTestExecutionSuite(execution, ownership.Module) {
		return TestSuite{}, fmt.Errorf("executionSuite must name a full-profile suite for module %q or repository scope, without supplemental execution", ownership.Module)
	}
	if len(execution.Paths) == 0 {
		return TestSuite{}, fmt.Errorf("executionSuite %q requires explicit execution paths covering its owned tests", execution.Name)
	}
	return execution, nil
}

func testOwnershipFocusedSuite(suites []TestSuite, ownership TestOwnership) (TestSuite, error) {
	focused, err := referencedSuite(suites, ownership.FocusedSuite, "focusedSuite")
	if err != nil {
		return TestSuite{}, err
	}
	if !IsPrimaryTestSuite(focused, ownership.Module) {
		return TestSuite{}, fmt.Errorf("focusedSuite must name a quick module-scoped suite for %q in focused, recommended, and full", ownership.Module)
	}
	if len(focused.Paths) == 0 {
		return TestSuite{}, fmt.Errorf("focusedSuite %q requires explicit execution paths", focused.Name)
	}
	return focused, nil
}

func isTestExecutionSuite(suite TestSuite, module string) bool {
	return slices.Contains(suite.RunOn, "full") && !slices.Contains(suite.RunOn, "supplemental") &&
		(suite.Scope == "repository" || suite.Scope == "module" && slices.Equal(suite.Modules, []string{module}))
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
