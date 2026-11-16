package repository

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func (repo Repository) PythonTypecheckEnvironment(path string) string {
	for _, environment := range repo.Config.Scope.PythonTypecheckEnvironments {
		if slices.Contains(environment.Paths, path) {
			return environment.EnvironmentProject
		}
	}
	return ""
}

func (repo Repository) PythonTypecheckProject(manifest string, files []string) (PythonProject, error) {
	inventory := repo.PythonProjectInventory(files)
	projects, _ := inventory.Owners()
	project, found := projects[manifest]
	if !found || !project.HasProjectTable {
		return PythonProject{}, fmt.Errorf("typechecking environment %s must name a governed Python project", manifest)
	}
	for _, problem := range inventory.Problems {
		if problem.Path == manifest || problem.Subject == manifest {
			return PythonProject{}, fmt.Errorf("typechecking environment %s is invalid: %s", manifest, problem.Message)
		}
	}
	lock := filepath.ToSlash(filepath.Join(project.Root, "uv.lock"))
	if !slices.Contains(files, lock) {
		return PythonProject{}, fmt.Errorf("typechecking environment %s requires its own governed %s", manifest, lock)
	}
	return project, nil
}

func (repo Repository) PythonTypecheckEnvironmentFindings(files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, environment := range repo.Config.Scope.PythonTypecheckEnvironments {
		if _, err := repo.PythonTypecheckProject(environment.EnvironmentProject, files); err != nil {
			findings = append(findings, pythonTypecheckEnvironmentFinding(environment.EnvironmentProject, err.Error()))
		}
		for _, path := range environment.Paths {
			if !slices.Contains(files, path) || repo.Language(path) != "python" {
				findings = append(findings, pythonTypecheckEnvironmentFinding(path, "typechecking environment path must name a current governed Python source"))
			}
		}
	}
	return findings
}

func pythonTypecheckEnvironmentFinding(subject, message string) policy.Finding {
	return policy.Finding{Check: "policy.pythonTypecheckEnvironment", Path: policy.ConfigFilename, Subject: subject, Message: message}
}
