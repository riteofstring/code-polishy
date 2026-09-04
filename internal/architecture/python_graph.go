package architecture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type pythonGraph map[string][]string

func parsePythonGraph(data []byte) (pythonGraph, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := pythonGraphOpen(decoder); err != nil {
		return nil, err
	}
	graph, err := pythonGraphEntries(decoder)
	if err != nil {
		return nil, err
	}
	if err := pythonGraphClose(decoder); err != nil {
		return nil, err
	}
	if err := pythonGraphNoTrailingJSON(decoder); err != nil {
		return nil, err
	}
	return graph, nil
}

func pythonGraphOpen(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	open, ok := token.(json.Delim)
	if !ok || open != '{' {
		return fmt.Errorf("the graph must be a JSON object")
	}
	return nil
}

func pythonGraphEntries(decoder *json.Decoder) (pythonGraph, error) {
	graph := pythonGraph{}
	for decoder.More() {
		if err := pythonGraphEntry(decoder, graph); err != nil {
			return nil, err
		}
	}
	return graph, nil
}

func pythonGraphEntry(decoder *json.Decoder, graph pythonGraph) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	source, ok := token.(string)
	if !ok || source == "" {
		return fmt.Errorf("the graph has an empty or non-string source path")
	}
	if _, duplicate := graph[source]; duplicate {
		return fmt.Errorf("the graph names source path %q more than once", source)
	}
	targets, err := pythonGraphEntryTargets(decoder, source)
	if err != nil {
		return err
	}
	graph[source] = targets
	return nil
}

func pythonGraphEntryTargets(decoder *json.Decoder, source string) ([]string, error) {
	var targets []string
	if err := decoder.Decode(&targets); err != nil || targets == nil {
		return nil, fmt.Errorf("the graph targets for %q are not an array of paths", source)
	}
	for _, target := range targets {
		if target == "" {
			return nil, fmt.Errorf("the graph targets for %q include an empty path", source)
		}
	}
	return targets, nil
}

func pythonGraphClose(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	close, ok := token.(json.Delim)
	if !ok || close != '}' {
		return fmt.Errorf("the graph object is not closed")
	}
	return nil
}

func pythonGraphNoTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err == nil {
		return fmt.Errorf("the graph has trailing JSON data")
	} else {
		return err
	}
}

func pythonGraphFindings(
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	owners map[string]string,
	allFiles []string,
	graph pythonGraph,
) []policy.Finding {
	dependencies, coverage, err := pythonGraphDependencies(repo, project, sources, graph)
	if err != nil {
		return pythonCoverageForSources(sources, err.Error())
	}
	pythonGraphMissingCoverage(sources, dependencies, coverage)
	pythonGraphTargetCoverage(repo, project, owners, allFiles, dependencies, coverage)
	pythonGraphImportCoverage(repo, project, sources, dependencies, coverage)
	findings := pythonCoverageFindings(coverage)
	findings = append(findings, pythonModuleDependencyFindings(repo, sources, dependencies, coverage)...)
	return pythonSortedFindings(findings)
}

func pythonGraphDependencies(
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	graph pythonGraph,
) (map[string]map[string]bool, map[string]string, error) {
	expected := pythonExpectedSources(sources)
	dependencies := map[string]map[string]bool{}
	coverage := map[string]string{}
	for rawSource, rawTargets := range graph {
		source, err := pythonGraphPath(repo, project, rawSource)
		if err != nil || !expected[source] {
			return nil, nil, fmt.Errorf("the Ruff graph includes an unselected or escaping source path")
		}
		if _, duplicate := dependencies[source]; duplicate {
			return nil, nil, fmt.Errorf("the Ruff graph identifies one source through multiple paths")
		}
		targets, message := pythonGraphTargets(repo, project, rawTargets)
		if message != "" {
			coverage[source] = message
		}
		dependencies[source] = targets
	}
	return dependencies, coverage, nil
}

func pythonExpectedSources(sources []string) map[string]bool {
	expected := make(map[string]bool, len(sources))
	for _, source := range sources {
		expected[source] = true
	}
	return expected
}

func pythonGraphTargets(repo repository.Repository, project repository.PythonProject, rawTargets []string) (map[string]bool, string) {
	targets := map[string]bool{}
	message := ""
	for _, rawTarget := range rawTargets {
		target, err := pythonGraphPath(repo, project, rawTarget)
		if err != nil {
			message = "the Ruff graph resolves an import outside the repository"
			continue
		}
		targets[target] = true
	}
	return targets, message
}

func pythonGraphMissingCoverage(sources []string, dependencies map[string]map[string]bool, coverage map[string]string) {
	for _, source := range sources {
		if _, found := dependencies[source]; !found {
			coverage[source] = "the Ruff graph omitted this selected Python source"
		}
	}
}

func pythonGraphTargetCoverage(
	repo repository.Repository,
	project repository.PythonProject,
	owners map[string]string,
	allFiles []string,
	dependencies map[string]map[string]bool,
	coverage map[string]string,
) {
	governed := pythonGovernedFiles(allFiles)
	for source, targets := range dependencies {
		for target := range targets {
			if message := pythonGraphTargetCoverageMessage(repo, project, owners, governed, target); message != "" {
				coverage[source] = message
			}
		}
	}
}

func pythonGovernedFiles(allFiles []string) map[string]bool {
	governed := make(map[string]bool, len(allFiles))
	for _, path := range allFiles {
		governed[path] = true
	}
	return governed
}

func pythonGraphTargetCoverageMessage(
	repo repository.Repository,
	project repository.PythonProject,
	owners map[string]string,
	governed map[string]bool,
	target string,
) string {
	if !governed[target] || repo.Language(target) != "python" {
		return "the Ruff graph resolves a local import to a file outside the governed Python source"
	}
	owner, found := owners[target]
	if !found {
		return "the Ruff graph resolves a local import to a Python file without a project owner"
	}
	if owner != project.Manifest {
		return "the Ruff graph resolves a local import outside the selected Python project"
	}
	if len(repo.ModuleNames(target)) != 1 {
		return "the Ruff graph resolves a local import to an ambiguously owned module"
	}
	return ""
}

func pythonGraphImportCoverage(
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	dependencies map[string]map[string]bool,
	coverage map[string]string,
) {
	index := newPythonModuleIndex(project)
	for _, source := range sources {
		if coverage[source] != "" {
			continue
		}
		if message := pythonSourceImportCoverage(repo, index, source, dependencies[source]); message != "" {
			coverage[source] = message
		}
	}
}

func pythonSourceImportCoverage(repo repository.Repository, index pythonModuleIndex, source string, dependencies map[string]bool) string {
	data, err := repo.Read(source)
	if err != nil {
		return "the selected Python source could not be read for import coverage: " + err.Error()
	}
	for _, reference := range pythonImportReferences(data) {
		if message := pythonUnprovenImport(index, source, reference, dependencies); message != "" {
			return message
		}
	}
	return ""
}

func pythonModuleDependencyFindings(
	repo repository.Repository,
	sources []string,
	dependencies map[string]map[string]bool,
	coverage map[string]string,
) []policy.Finding {
	findings := []policy.Finding{}
	for _, source := range sources {
		if coverage[source] != "" {
			continue
		}
		findings = append(findings, pythonSourceModuleDependencyFindings(repo, source, dependencies[source])...)
	}
	return findings
}

func pythonSourceModuleDependencyFindings(repo repository.Repository, source string, dependencies map[string]bool) []policy.Finding {
	if repo.IsTest(source) {
		return nil
	}
	findings := []policy.Finding{}
	sourceOwners := repo.ModuleNames(source)
	for target := range dependencies {
		targetOwners := repo.ModuleNames(target)
		if sourceOwners[0] == targetOwners[0] {
			continue
		}
		sourceModule := repo.Config.Modules[repo.Config.ModuleByName[sourceOwners[0]]]
		if slices.Contains(sourceModule.DependsOn, targetOwners[0]) {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "architecture.moduleDependency", Path: source, Subject: targetOwners[0],
			Message: fmt.Sprintf("module %q imports module %q without declaring dependsOn", sourceOwners[0], targetOwners[0]),
		})
	}
	return findings
}

func pythonSortedFindings(findings []policy.Finding) []policy.Finding {
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Check+"\x00"+findings[left].Path+"\x00"+findings[left].Subject+"\x00"+findings[left].Message <
			findings[right].Check+"\x00"+findings[right].Path+"\x00"+findings[right].Subject+"\x00"+findings[right].Message
	})
	return findings
}

func pythonGraphPath(repo repository.Repository, project repository.PythonProject, value string) (string, error) {
	path := value
	if !filepath.IsAbs(filepath.FromSlash(path)) {
		if project.Root != "." {
			path = project.Root + "/" + path
		}
	}
	return repo.NormalizePath(path)
}

func pythonCoverageForSources(sources []string, message string) []policy.Finding {
	findings := make([]policy.Finding, 0, len(sources))
	for _, source := range sources {
		findings = append(findings, pythonImportCoverage(source, message))
	}
	return findings
}

func pythonCoverageFindings(coverage map[string]string) []policy.Finding {
	sources := make([]string, 0, len(coverage))
	for source, message := range coverage {
		if message != "" {
			sources = append(sources, source)
		}
	}
	sort.Strings(sources)
	findings := make([]policy.Finding, 0, len(sources))
	for _, source := range sources {
		findings = append(findings, pythonImportCoverage(source, coverage[source]))
	}
	return findings
}

func pythonImportCoverage(path, message string) policy.Finding {
	return policy.Finding{Check: "architecture.importCoverage", Path: path, Subject: "python", Message: message}
}
