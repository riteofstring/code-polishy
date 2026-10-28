package supplychain

import (
	"fmt"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonLibraryCoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, manifest := range repo.Config.SupplyChain.PythonLibraries {
		if !slices.Contains(files, manifest) {
			findings = append(findings, policy.Finding{
				Check: "policy.pythonLibrary", Path: manifest, Subject: manifest,
				Message: "declared Python library must name a governed pyproject.toml manifest",
			})
		}
	}
	return findings
}

func pythonCompatibilityDeclaration(repo repository.Repository, path string, dependency repository.PythonRequirement) bool {
	return dependency.Kind == repository.PythonRegistryRequirement &&
		(dependency.Usage == "runtime" || dependency.Usage == "optional") &&
		slices.Contains(repo.Config.SupplyChain.PythonLibraries, path)
}

func pythonLibraryFindings(repo repository.Repository, project repository.PythonProject) []policy.Finding {
	if !slices.Contains(repo.Config.SupplyChain.PythonLibraries, project.Manifest) {
		return nil
	}
	checks, declarations, findings := pythonLibraryChecks(repo, project)
	if len(checks) == 0 {
		return findings
	}
	python := repo.PythonTool()
	if repo.PolicyRoot == "" {
		var err error
		python, err = pythonfacts.DefaultInterpreter()
		if err != nil {
			return append(findings, pythonCompatibilityFinding(declarations[0], err.Error()))
		}
	}
	return append(findings, pythonLibraryVersionFindings(python, checks, declarations)...)
}

func pythonLibraryChecks(repo repository.Repository, project repository.PythonProject) ([]pythonfacts.VersionCheck, []repository.PythonRequirement, []policy.Finding) {
	findings := []policy.Finding{}
	checks := []pythonfacts.VersionCheck{}
	declarations := []repository.PythonRequirement{}
	for _, requirement := range project.Requirements {
		if !pythonCompatibilityDeclaration(repo, project.Manifest, requirement) {
			continue
		}
		pins := repository.PythonDevelopmentRequirements(requirement, project.Requirements)
		if len(pins) == 0 {
			findings = append(findings, pythonCompatibilityFinding(requirement,
				"needs an exact registry requirement in [dependency-groups] with all requested extras and an identical normalized marker or no marker"))
		}
		for _, pin := range pins {
			version, _ := pin.ExactRegistryVersion()
			checks = append(checks, pythonfacts.VersionCheck{Specifier: requirement.Version, Version: version})
			declarations = append(declarations, requirement)
		}
	}
	return checks, declarations, findings
}

func pythonLibraryVersionFindings(python string, checks []pythonfacts.VersionCheck, declarations []repository.PythonRequirement) []policy.Finding {
	findings := []policy.Finding{}
	for offset := 0; offset < len(checks); offset += 4096 {
		batch := checks[offset:min(offset+4096, len(checks))]
		matches, err := pythonfacts.MatchVersions(python, batch)
		if err != nil {
			return append(findings, pythonCompatibilityFinding(declarations[offset], err.Error()))
		}
		for index, match := range matches {
			if match.Error != "" || !match.Matches {
				message := fmt.Sprintf("exact development version %q does not satisfy compatibility constraint %q", match.Version, match.Specifier)
				if match.Error != "" {
					message = match.Error
				}
				findings = append(findings, pythonCompatibilityFinding(declarations[offset+index], message))
			}
		}
	}
	return findings
}

func pythonCompatibilityFinding(requirement repository.PythonRequirement, message string) policy.Finding {
	return policy.Finding{
		Check: "supplyChain.pythonCompatibility", Path: requirement.Location.Path,
		Line: requirement.Location.Line, Subject: requirement.Name,
		Message: fmt.Sprintf("Python library dependency %q: %s", requirement.Raw, message),
	}
}
