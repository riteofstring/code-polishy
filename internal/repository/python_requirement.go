package repository

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

func NormalizePythonPackageName(name string) (string, error) {
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return "", err
	}
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Names: []string{name}})
	if err != nil {
		return "", err
	}
	result := response.Names[0]
	if result.Error != "" || result.Normalized == "" {
		if result.Error == "" {
			result.Error = "package name is empty"
		}
		return "", fmt.Errorf("package name %q is invalid: %s", name, result.Error)
	}
	return result.Normalized, nil
}

func ParsePythonRequirement(value string) (PythonRequirement, error) {
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return PythonRequirement{}, err
	}
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Requirements: []string{value}})
	if err != nil {
		return PythonRequirement{}, err
	}
	return pythonRequirementFact(response.Requirements[0])
}

func pythonRequirementFact(fact pythonfacts.Requirement) (PythonRequirement, error) {
	requirement, err := pythonRequirementInventoryFact(fact)
	if err != nil {
		return PythonRequirement{}, err
	}
	if requirement.Kind == PythonGitRequirement {
		if err := requirement.Git.ValidateExactPin(); err != nil {
			return PythonRequirement{}, err
		}
	}
	return requirement, nil
}

func pythonRequirementInventoryFact(fact pythonfacts.Requirement) (PythonRequirement, error) {
	if fact.Error != "" {
		return PythonRequirement{}, fmt.Errorf("invalid Python requirement: %s", fact.Error)
	}
	if fact.Input == "" || fact.Name == "" {
		return PythonRequirement{}, fmt.Errorf("python-facts returned an invalid requirement identity")
	}
	requirement := PythonRequirement{
		Raw: fact.Input, Name: fact.Name, Extras: fact.Extras, Marker: fact.Marker,
		Kind: PythonRegistryRequirement, Version: fact.Specifier, markerKey: fact.Marker,
		markerVariable: fact.MarkerVariable, markerValue: fact.MarkerValue,
	}
	requirement.Specifiers = make([]PythonVersionSpecifier, 0, len(fact.Specifiers))
	for _, specifier := range fact.Specifiers {
		if specifier.Operator == "" || specifier.Version == "" {
			return PythonRequirement{}, fmt.Errorf("python-facts returned an invalid version specifier")
		}
		requirement.Specifiers = append(requirement.Specifiers, PythonVersionSpecifier{Operator: specifier.Operator, Version: specifier.Version})
	}
	if fact.URL != "" {
		if err := pythonDirectRequirementURL(fact.URL, &requirement); err != nil {
			return PythonRequirement{}, err
		}
	}
	return requirement, nil
}

func pythonDirectRequirementURL(value string, requirement *PythonRequirement) error {
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "git+") {
		return pythonGitRequirementURL(value, requirement)
	}
	if strings.HasPrefix(lower, "file:") {
		return pythonFileRequirementURL(value, requirement)
	}
	if err := pythonOrdinaryRequirementURL(value); err != nil {
		return err
	}
	requirement.Kind = PythonURLRequirement
	requirement.URL = value
	return nil
}

func pythonGitRequirementURL(value string, requirement *PythonRequirement) error {
	git, err := parsePythonDirectGitURL(value)
	if err != nil {
		return err
	}
	requirement.Kind = PythonGitRequirement
	requirement.Git = git
	requirement.URL = git.InventoryIdentity()
	return nil
}

func pythonFileRequirementURL(value string, requirement *PythonRequirement) error {
	filePath, err := parsePythonFileURL(value)
	if err != nil {
		return err
	}
	requirement.Kind = PythonFileRequirement
	requirement.FilePath = filePath
	requirement.URL = value
	return nil
}

func pythonOrdinaryRequirementURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || hasPythonDynamicValue(value) {
		return fmt.Errorf("direct URL is malformed")
	}
	if strings.EqualFold(parsed.Scheme, "git") {
		return fmt.Errorf("git URL must use git+https:// or git+ssh://")
	}
	return nil
}
