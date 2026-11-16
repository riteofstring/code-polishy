package quality

import (
	"fmt"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonTypecheckCommands(repo repository.Repository, project repository.PythonProject, sources []string) ([]pythonQualityCommand, []policy.Finding) {
	groups := map[string][]string{}
	for _, source := range repo.NativeAnalysisFiles(sources, "typecheck", "") {
		environment := repo.PythonTypecheckEnvironment(source)
		groups[environment] = append(groups[environment], source)
	}
	commands := []pythonQualityCommand{}
	findings := []policy.Finding{}
	for _, environment := range pythonQualityProjectManifests(groups) {
		command, err := pythonTypecheckEnvironmentCommand(repo, project, groups[environment], environment)
		if err != nil {
			findings = append(findings, policy.Finding{
				Check: "quality.typecheckCoverage", Path: project.Manifest, Subject: "ty", Message: err.Error(),
			})
			continue
		}
		commands = append(commands, command)
	}
	return commands, findings
}

func pythonTypecheckEnvironmentCommand(repo repository.Repository, project repository.PythonProject, sources []string, environment string) (pythonQualityCommand, error) {
	paths, err := pythonQualityProjectPaths(project, sources)
	if err != nil {
		return pythonQualityCommand{}, err
	}
	searchPaths, err := pythonQualityProjectSearchPaths(project)
	if err != nil {
		return pythonQualityCommand{}, err
	}
	venv, err := pythonTypecheckEnvironmentPath(repo, project, environment)
	if err != nil {
		return pythonQualityCommand{}, err
	}
	suffix := pythonQualityProjectName(project.Root)
	if environment != "" {
		suffix = "environment-" + safeName(project.Root+"\x00"+environment)
	}
	return pythonQualityCommand{
		kind: pythonTyQualityKind, sources: slices.Clone(sources), environment: environment,
		command: pythonTyCommand(repo, project, suffix, pythonQualityModules(repo, sources), paths, searchPaths, venv),
	}, nil
}

func pythonTypecheckEnvironmentPath(repo repository.Repository, project repository.PythonProject, environment string) (string, error) {
	if environment == "" {
		if len(project.Requirements) == 0 {
			return "", nil
		}
		return pythonQualityVenv(repo, project)
	}
	files, err := repo.AllFiles()
	if err != nil {
		return "", err
	}
	target, err := repo.PythonTypecheckProject(environment, files)
	if err != nil {
		return "", err
	}
	if _, err := pythonQualityVenv(repo, target); err != nil {
		return "", fmt.Errorf("typechecking environment %s: %w", environment, err)
	}
	return repo.Resolve(target.Venv)
}

func pythonTypecheckEnvironmentEvidence(project pythonQualityProject, specification pythonQualityCommand) policy.Finding {
	locations := make([]policy.FindingLocation, 0, len(specification.sources))
	for _, path := range specification.sources {
		locations = append(locations, policy.FindingLocation{Path: path})
	}
	return policy.Finding{
		Check: "quality.typecheckEnvironment", Path: specification.environment, Subject: project.project.Manifest,
		Severity: policy.FindingInformation, Related: locations,
		Message:     fmt.Sprintf("ty analyzed %d files owned by %s using the separately locked environment %s; related locations list the analyzed files.", len(locations), project.project.Manifest, specification.environment),
		Remediation: policy.FindingRemediation{Summary: "Keep this environment aligned with the runtime that executes the listed files.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "check", "--all"}, Cwd: "."}},
	}
}

func PythonCheckNotes(repo repository.Repository, selection repository.Selection, profile string) []string {
	if selection.All || profile == "gate" {
		return nil
	}
	sources := pythonQualitySources(repo, repo.NativeAnalysisFiles(selection.Files, "dead-code", ""))
	if len(sources) == 0 {
		return nil
	}
	return []string{"Python dead-code analysis was not run for this focused selection; use check --all for complete project analysis. Unused-code candidates are warnings; unreachable control flow remains blocking."}
}
