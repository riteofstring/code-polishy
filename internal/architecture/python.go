package architecture

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const pythonImportBudget = 15 * time.Minute

type pythonSelectionPlan struct {
	findings          []policy.Finding
	owners            map[string]string
	projects          map[string]repository.PythonProject
	sourcesByProject  map[string][]string
	selectedByProject map[string][]string
}

type pythonGraphCommandPair struct {
	complete policy.Command
	runtime  policy.Command
}

func (pair pythonGraphCommandPair) ordered() []policy.Command {
	return []policy.Command{pair.complete, pair.runtime}
}

func PythonGraphCommands(repo repository.Repository, selected []string) []policy.Command {
	allFiles, err := repo.AllFiles()
	if err != nil {
		return nil
	}
	plan := pythonSelection(repo, selected, allFiles)
	commands := []policy.Command{}
	for _, manifest := range pythonPlanManifests(plan) {
		pair, commandErr := pythonGraphCommands(repo, plan.projects[manifest], plan.sourcesByProject[manifest])
		if commandErr == nil {
			commands = append(commands, pair.ordered()...)
		}
	}
	return commands
}

func pythonSourceGraph(ctx context.Context, repo repository.Repository, selected, allFiles []string, commandRunner runner.Runner) sourceGraphPart {
	plan := pythonSelection(repo, selected, allFiles)
	part := sourceGraphPart{findings: append([]policy.Finding{}, plan.findings...), incomplete: len(plan.findings) > 0}
	externalCoverage := pythonExternalDeclarationCoverage(repo, selected, plan)
	part.findings = append(part.findings, externalCoverage...)
	part.incomplete = part.incomplete || len(externalCoverage) != 0
	for _, manifest := range pythonPlanManifests(plan) {
		project := plan.projects[manifest]
		sources := plan.sourcesByProject[manifest]
		selectedSources := plan.selectedByProject[manifest]
		commands, err := pythonGraphCommands(repo, project, sources)
		if err != nil {
			part.findings = append(part.findings, pythonCoverageForSources(selectedSources, "the Python project cannot produce a contained Ruff graph command: "+err.Error())...)
			part.incomplete = true
			continue
		}
		part = mergeSourceGraphParts(part, pythonProjectSourceGraph(ctx, repo, project, sources, selectedSources, plan.owners, allFiles, commands, commandRunner))
	}
	return part
}

func pythonSelection(repo repository.Repository, selected, allFiles []string) pythonSelectionPlan {
	sources := pythonSources(repo, selected)
	if len(sources) == 0 {
		return pythonSelectionPlan{}
	}
	inventory := repo.PythonProjectInventory(allFiles)
	problemSources, findings := pythonInventoryFindings(inventory, sources)
	projects := map[string]repository.PythonProject{}
	owners := map[string]string{}
	for _, project := range inventory.Projects {
		projects[project.Manifest] = project
		for _, source := range project.Files {
			owners[source] = project.Manifest
		}
	}
	selectedByProject := map[string][]string{}
	for _, source := range sources {
		if problemSources[source] {
			continue
		}
		if _, err := sourceModuleOwner(repo, source); err != nil {
			findings = append(findings, pythonImportCoverage(source, "Python source must belong to exactly one repository module"))
			continue
		}
		manifest, found := owners[source]
		if !found {
			findings = append(findings, pythonImportCoverage(source, "the Python project inventory did not assign this source to a project"))
			continue
		}
		selectedByProject[manifest] = append(selectedByProject[manifest], source)
	}
	sourcesByProject, contextFindings := pythonProjectSelection(repo, projects, selectedByProject)
	findings = append(findings, contextFindings...)
	return pythonSelectionPlan{
		findings: findings, owners: owners, projects: projects, sourcesByProject: sourcesByProject, selectedByProject: selectedByProject,
	}
}

func pythonPlanManifests(plan pythonSelectionPlan) []string {
	manifests := make([]string, 0, len(plan.sourcesByProject))
	for manifest := range plan.sourcesByProject {
		if _, found := plan.projects[manifest]; found {
			manifests = append(manifests, manifest)
		}
	}
	sort.Strings(manifests)
	return manifests
}

func pythonSources(repo repository.Repository, selected []string) []string {
	seen := map[string]bool{}
	sources := []string{}
	candidates := append([]string{}, selected...)
	for _, declaration := range repo.Config.Scope.PythonRuntimeLoaders {
		if pythonComputedInputSelected(repo, selected, policy.PythonComputedImport{Project: declaration.Project}) {
			candidates = append(candidates, declaration.Consumer.Importer)
		}
	}
	for _, declaration := range repo.Config.Scope.PythonComputedImports {
		if pythonComputedInputSelected(repo, selected, declaration) {
			candidates = append(candidates, declaration.Importer)
		}
	}
	for _, declaration := range repo.Config.Scope.PythonExternalPluginImports {
		if pythonExternalInputSelected(repo, selected, declaration) {
			candidates = append(candidates, declaration.Consumer.Importer)
		}
	}
	for _, source := range candidates {
		if repo.Language(source) != "python" || seen[source] {
			continue
		}
		seen[source] = true
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

func pythonComputedInputSelected(repo repository.Repository, selected []string, declaration policy.PythonComputedImport) bool {
	configPath := policy.ConfigFilename
	if repo.Config.ConfigPath != "" {
		if normalized, err := repo.NormalizePath(repo.Config.ConfigPath); err == nil {
			configPath = normalized
		}
	}
	if slices.Contains(selected, configPath) || slices.Contains(selected, declaration.Project) {
		return true
	}
	for _, input := range declaration.Configuration {
		if slices.Contains(selected, input.Path) {
			return true
		}
	}
	return false
}

func pythonInventoryFindings(inventory repository.PythonProjectInventory, sources []string) (map[string]bool, []policy.Finding) {
	invalid := map[string]bool{}
	findings := []policy.Finding{}
	for _, problem := range inventory.Problems {
		relevant := false
		for _, source := range sources {
			if pythonProblemAffectsSource(problem, source) {
				invalid[source] = true
				relevant = true
			}
		}
		if relevant {
			findings = append(findings, repository.PythonInventoryFinding(problem))
		}
	}
	return invalid, findings
}

func pythonProblemAffectsSource(problem repository.PythonInventoryProblem, source string) bool {
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

func pythonProjectSourceGraph(
	ctx context.Context,
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	selectedSources []string,
	owners map[string]string,
	allFiles []string,
	commands pythonGraphCommandPair,
	commandRunner runner.Runner,
) sourceGraphPart {
	structured, ok := commandRunner.(runner.StructuredRunner)
	if !ok {
		return sourceGraphPart{findings: []policy.Finding{{
			Check: "policy.tool", Path: project.Manifest, Subject: "ruff",
			Message: "the architecture runner cannot capture the bounded Ruff graph output",
		}}, incomplete: true}
	}
	bounded, cancel := context.WithTimeout(ctx, pythonImportBudget)
	defer cancel()
	graphs := make([]pythonGraph, 0, 2)
	for _, command := range commands.ordered() {
		_, output, runErr := structured.RunStructured(bounded, repo.Root, command)
		if runErr != nil {
			return sourceGraphPart{findings: []policy.Finding{{Check: "policy.tool", Path: project.Manifest, Subject: "ruff", Message: runErr.Error()}}, incomplete: true}
		}
		if len(output.Stderr) != 0 {
			return sourceGraphPart{findings: pythonCoverageForSources(selectedSources, "the policy-owned Ruff graph emitted diagnostics"), incomplete: true}
		}
		facts, err := adaptPythonGraph(output.Stdout)
		if err != nil {
			return sourceGraphPart{findings: pythonCoverageForSources(selectedSources, "the policy-owned Ruff graph output is malformed: "+err.Error()), incomplete: true}
		}
		graphs = append(graphs, facts.Graph)
	}
	return pythonRuffGraphSourcePart(repo, project, sources, selectedSources, owners, allFiles, graphs[0], graphs[1])
}

func pythonGraphCommands(repo repository.Repository, project repository.PythonProject, sources []string) (pythonGraphCommandPair, error) {
	complete, err := pythonGraphCommand(repo, project, sources, true)
	if err != nil {
		return pythonGraphCommandPair{}, err
	}
	runtime, err := pythonGraphCommand(repo, project, sources, false)
	return pythonGraphCommandPair{complete: complete, runtime: runtime}, err
}

func pythonGraphCommand(repo repository.Repository, project repository.PythonProject, sources []string, typeChecking bool) (policy.Command, error) {
	if version := repo.ToolPin("ruff"); version != pythonGraphRuffVersion {
		return policy.Command{}, fmt.Errorf("%s requires Ruff %s, found %q", pythonGraphProtocol, pythonGraphRuffVersion, version)
	}
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		relative, err := pythonProjectPath(project, source)
		if err != nil {
			return policy.Command{}, err
		}
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	ruffOptions, err := project.Ruff.CommandOptions(project.Root, project.SourceRoots)
	if err != nil {
		return policy.Command{}, err
	}
	arguments := []string{
		repo.PolicyTool("ruff"), "analyze", "graph", "--quiet", "--isolated",
	}
	arguments = append(arguments, ruffOptions...)
	mode := "runtime"
	flag := "--no-type-checking-imports"
	if typeChecking {
		mode = "complete"
		flag = "--type-checking-imports"
	}
	arguments = append(arguments, flag, "--")
	arguments = append(arguments, paths...)
	return policy.Command{
		Name:              "ruff-graph-facts-v2-ruff-" + pythonGraphRuffVersion + "-" + pythonGraphName(project.Root) + "-" + mode,
		Argv:              arguments,
		Cwd:               project.Root,
		Paths:             paths,
		TimeoutSeconds:    int(pythonImportBudget.Seconds()),
		SealedEnvironment: true,
	}, nil
}

func pythonGraphName(root string) string {
	if root == "." {
		return "root"
	}
	return strings.NewReplacer("/", "-", "\\", "-", ".", "-").Replace(root)
}

func pythonProjectPath(project repository.PythonProject, source string) (string, error) {
	root := filepath.FromSlash(project.Root)
	relative, err := filepath.Rel(root, filepath.FromSlash(source))
	if err != nil {
		return "", err
	}
	relative = filepath.ToSlash(relative)
	if relative == ".." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("source %q is outside project root %q", source, project.Root)
	}
	return relative, nil
}

func pythonProjectSelection(repo repository.Repository, projects map[string]repository.PythonProject, selectedByProject map[string][]string) (map[string][]string, []policy.Finding) {
	findings := []policy.Finding{}
	sourcesByProject := map[string][]string{}
	for _, manifest := range pythonPlanManifests(pythonSelectionPlan{projects: projects, sourcesByProject: selectedByProject}) {
		_, found := projects[manifest]
		if !found {
			for _, source := range selectedByProject[manifest] {
				findings = append(findings, pythonImportCoverage(source, "the Python project inventory returned an unknown project"))
			}
			continue
		}
		for _, source := range selectedByProject[manifest] {
			if _, err := sourceModuleOwner(repo, source); err != nil {
				findings = append(findings, pythonImportCoverage(source, "Python source must belong to exactly one repository module"))
				continue
			}
			sourcesByProject[manifest] = append(sourcesByProject[manifest], source)
		}
		sort.Strings(sourcesByProject[manifest])
	}
	return sourcesByProject, findings
}
