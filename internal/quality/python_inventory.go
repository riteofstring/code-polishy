package quality

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonQualityInventoryFindings(repo repository.Repository, sources []string, selectedManifests map[string]bool, inventory repository.PythonProjectInventory) ([]policy.Finding, map[string]bool, map[string]bool) {
	interested := pythonQualityInterestedProjects(repo, sources, selectedManifests, inventory.Assignments)
	invalidProjects := pythonQualityInvalidProjects(inventory)
	invalidSources := map[string]bool{}
	findings := []policy.Finding{}
	for _, problem := range inventory.Problems {
		relevant := pythonQualityMarkInvalidSources(problem, sources, invalidSources)
		if relevant || pythonQualityProblemAffectsInterestedProject(problem, inventory.Projects, interested) {
			findings = append(findings, repository.PythonInventoryFinding(problem))
		}
	}
	return findings, invalidSources, invalidProjects
}

func pythonQualityInterestedProjects(repo repository.Repository, sources []string, selectedManifests map[string]bool, assignments []repository.PythonProjectAssignment) map[string]bool {
	interested := map[string]bool{}
	for manifest := range selectedManifests {
		interested[manifest] = true
	}
	for _, reference := range repo.Config.Scope.PythonDynamicReferences {
		interested[reference.Project] = true
	}
	selected := pythonQualitySelectedSources(sources)
	for _, assignment := range assignments {
		if selected[assignment.Path] {
			interested[assignment.Manifest] = true
		}
	}
	return interested
}

func pythonQualityInvalidProjects(inventory repository.PythonProjectInventory) map[string]bool {
	invalid := map[string]bool{}
	for _, problem := range inventory.Problems {
		for _, project := range inventory.Projects {
			if pythonInventoryProblemAffectsProject(problem, project) {
				invalid[project.Manifest] = true
			}
		}
	}
	return invalid
}

func pythonQualityMarkInvalidSources(problem repository.PythonInventoryProblem, sources []string, invalid map[string]bool) bool {
	relevant := false
	for _, source := range sources {
		if pythonInventoryProblemAffects(problem, source) {
			invalid[source] = true
			relevant = true
		}
	}
	return relevant
}

func pythonQualityProblemAffectsInterestedProject(problem repository.PythonInventoryProblem, projects []repository.PythonProject, interested map[string]bool) bool {
	for _, project := range projects {
		if interested[project.Manifest] && pythonInventoryProblemAffectsProject(problem, project) {
			return true
		}
	}
	return false
}

func pythonInventoryProblemAffectsProject(problem repository.PythonInventoryProblem, project repository.PythonProject) bool {
	if problem.Path == project.Manifest || problem.Subject == project.Manifest {
		return true
	}
	for _, source := range project.Files {
		if pythonInventoryProblemAffects(problem, source) {
			return true
		}
	}
	return false
}

func pythonQualityProjectOwners(inventory repository.PythonProjectInventory) (map[string]repository.PythonProject, map[string]string) {
	projects := map[string]repository.PythonProject{}
	owners := map[string]string{}
	for _, project := range inventory.Projects {
		projects[project.Manifest] = project
		for _, source := range project.Files {
			owners[source] = project.Manifest
		}
	}
	return projects, owners
}

func pythonQualitySelectedProjects(sources []string, invalid map[string]bool, owners map[string]string) (map[string][]string, []policy.Finding) {
	selectedByProject := map[string][]string{}
	findings := []policy.Finding{}
	for _, source := range sources {
		if invalid[source] {
			continue
		}
		manifest, found := owners[source]
		if found {
			selectedByProject[manifest] = append(selectedByProject[manifest], source)
			continue
		}
		message := "the Python project inventory did not assign this source to a project"
		findings = append(findings, pythonQualityAllCoverage([]string{source}, message)...)
	}
	return selectedByProject, findings
}

func pythonQualityProjectManifests(selectedByProject map[string][]string) []string {
	manifests := make([]string, 0, len(selectedByProject))
	for manifest := range selectedByProject {
		manifests = append(manifests, manifest)
	}
	sort.Strings(manifests)
	return manifests
}

func pythonQualityPlannedProject(repo repository.Repository, manifest string, selected []string, projects map[string]repository.PythonProject) (pythonQualityProject, bool, []policy.Finding) {
	sources := append([]string{}, selected...)
	sort.Strings(sources)
	project, found := projects[manifest]
	if !found {
		message := "the Python project inventory returned an unknown project"
		return pythonQualityProject{}, false, pythonQualityAllCoverage(sources, message)
	}
	commands, typecheckProblem, err := pythonQualityProjectCommands(repo, project, sources)
	if err != nil {
		message := "the Python project cannot produce a contained quality command: " + err.Error()
		findings := pythonQualityNonDeadCodeCoverage(sources, message)
		findings = append(findings, pythonVultureCoverage(project.Files, message)...)
		return pythonQualityProject{}, false, findings
	}
	findings := []policy.Finding{}
	if typecheckProblem != "" {
		findings = append(findings, policy.Finding{
			Check: "quality.typecheckCoverage", Path: project.Manifest, Subject: "ty", Message: typecheckProblem,
		})
	}
	return pythonQualityProject{project: project, sources: sources, commands: commands}, true, findings
}

func pythonQualitySources(repo repository.Repository, selected []string) []string {
	seen := map[string]bool{}
	sources := []string{}
	for _, source := range selected {
		if repo.Language(source) != "python" || seen[source] {
			continue
		}
		seen[source] = true
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

func pythonInventoryProblemAffects(problem repository.PythonInventoryProblem, source string) bool {
	for _, candidate := range []string{problem.Path, problem.Subject} {
		if candidate == source {
			return true
		}
		if candidate == "." || candidate != "" && strings.HasPrefix(source, strings.TrimSuffix(candidate, "/")+"/") {
			return true
		}
		if filepath.Base(candidate) == "pyproject.toml" {
			directory := filepath.ToSlash(filepath.Dir(candidate))
			if directory == "." || strings.HasPrefix(source, directory+"/") {
				return true
			}
		}
	}
	return false
}
