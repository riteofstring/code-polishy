package architecture

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const pythonImportBudget = 15 * time.Minute

type pythonSelectionPlan struct {
	findings         []policy.Finding
	owners           map[string]string
	projects         map[string]repository.PythonProject
	sourcesByProject map[string][]string
}

func PythonGraphCommands(repo repository.Repository, selected []string) []policy.Command {
	allFiles, err := repo.AllFiles()
	if err != nil {
		return nil
	}
	plan := pythonSelection(repo, selected, allFiles)
	commands := []policy.Command{}
	for _, manifest := range pythonPlanManifests(plan) {
		command, commandErr := pythonGraphCommand(repo, plan.projects[manifest], plan.sourcesByProject[manifest])
		if commandErr == nil {
			commands = append(commands, command)
		}
	}
	return commands
}

func pythonFindings(ctx context.Context, repo repository.Repository, selected, allFiles []string, commandRunner runner.Runner) []policy.Finding {
	plan := pythonSelection(repo, selected, allFiles)
	findings := append([]policy.Finding{}, plan.findings...)
	for _, manifest := range pythonPlanManifests(plan) {
		project := plan.projects[manifest]
		sources := plan.sourcesByProject[manifest]
		command, err := pythonGraphCommand(repo, project, sources)
		if err != nil {
			findings = append(findings, pythonCoverageForSources(sources, "the Python project cannot produce a contained Ruff graph command: "+err.Error())...)
			continue
		}
		findings = append(findings, pythonProjectFindings(ctx, repo, project, sources, plan.owners, allFiles, command, commandRunner)...)
	}
	return findings
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
		if len(repo.ModuleNames(source)) != 1 {
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
	for _, manifest := range pythonPlanManifests(pythonSelectionPlan{projects: projects, sourcesByProject: selectedByProject}) {
		_, found := projects[manifest]
		if !found {
			for _, source := range selectedByProject[manifest] {
				findings = append(findings, pythonImportCoverage(source, "the Python project inventory returned an unknown project"))
			}
		}
	}
	return pythonSelectionPlan{
		findings: findings, owners: owners, projects: projects, sourcesByProject: selectedByProject,
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

func pythonInventoryFindings(inventory repository.PythonProjectInventory, sources []string) (map[string]bool, []policy.Finding) {
	invalid := map[string]bool{}
	findings := []policy.Finding{}
	for _, problem := range inventory.Problems {
		for _, source := range sources {
			if invalid[source] || !pythonProblemAffectsSource(problem, source) {
				continue
			}
			invalid[source] = true
			findings = append(findings, pythonImportCoverage(source, "the Python project inventory cannot cover this source: "+problem.Message))
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

func pythonProjectFindings(
	ctx context.Context,
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	owners map[string]string,
	allFiles []string,
	command policy.Command,
	commandRunner runner.Runner,
) []policy.Finding {
	structured, ok := commandRunner.(runner.StructuredRunner)
	if !ok {
		return []policy.Finding{{
			Check: "policy.tool", Path: project.Manifest, Subject: "ruff",
			Message: "the architecture runner cannot capture the bounded Ruff graph output",
		}}
	}
	bounded, cancel := context.WithTimeout(ctx, pythonImportBudget)
	defer cancel()
	_, output, runErr := structured.RunStructured(bounded, repo.Root, command)
	if runErr != nil {
		return []policy.Finding{{Check: "policy.tool", Path: project.Manifest, Subject: "ruff", Message: runErr.Error()}}
	}
	graph, err := parsePythonGraph(output.Stdout)
	if err != nil {
		return pythonCoverageForSources(sources, "the policy-owned Ruff graph output is malformed: "+err.Error())
	}
	return pythonGraphFindings(repo, project, sources, owners, allFiles, graph)
}

func pythonGraphCommand(repo repository.Repository, project repository.PythonProject, sources []string) (policy.Command, error) {
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		relative, err := pythonProjectPath(project, source)
		if err != nil {
			return policy.Command{}, err
		}
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	roots, err := pythonSourceRoots(project)
	if err != nil {
		return policy.Command{}, err
	}
	arguments := []string{
		repo.PolicyTool("ruff"), "analyze", "graph", "--quiet", "--isolated",
		"--detect-string-imports", "--min-dots", "0", "--type-checking-imports",
		"--config", pythonSourceRootConfig(roots), "--",
	}
	arguments = append(arguments, paths...)
	return policy.Command{
		Name:              "policy-ruff-import-graph-" + pythonGraphName(project.Root),
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

func pythonSourceRoots(project repository.PythonProject) ([]string, error) {
	roots := []string{"."}
	if project.SourceRoot == "" {
		return roots, nil
	}
	relative, err := pythonProjectPath(project, project.SourceRoot)
	if err != nil {
		return nil, err
	}
	if relative != "." {
		roots = append(roots, relative)
	}
	return roots, nil
}

func pythonSourceRootConfig(roots []string) string {
	values := make([]string, 0, len(roots))
	for _, root := range roots {
		values = append(values, strconv.Quote(root))
	}
	return "src = [" + strings.Join(values, ", ") + "]"
}
