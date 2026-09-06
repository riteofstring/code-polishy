package architecture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	pathpkg "path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type pythonGraph map[string][]string

const (
	pythonGraphProtocol       = "ruff-graph-facts/v1"
	pythonGraphRuffVersion    = "0.16.0"
	pythonGraphMaximumBytes   = 8 << 20
	pythonGraphMaximumSources = 4096
	pythonGraphMaximumEdges   = 262144
	pythonGraphMaximumPath    = 4096
)

type pythonGraphFacts struct {
	Protocol    string
	RuffVersion string
	Graph       pythonGraph
}

func adaptPythonGraph(data []byte) (pythonGraphFacts, error) {
	if len(data) == 0 || len(data) > pythonGraphMaximumBytes || !utf8.Valid(data) {
		return pythonGraphFacts{}, fmt.Errorf("the Ruff graph has an invalid byte size or encoding")
	}
	graph, err := parsePythonGraph(data)
	if err != nil {
		return pythonGraphFacts{}, err
	}
	return pythonGraphFacts{Protocol: pythonGraphProtocol, RuffVersion: pythonGraphRuffVersion, Graph: graph}, nil
}

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
	edges := 0
	for decoder.More() {
		if len(graph) >= pythonGraphMaximumSources {
			return nil, fmt.Errorf("the Ruff graph exceeds its source limit")
		}
		count, err := pythonGraphEntry(decoder, graph)
		if err != nil {
			return nil, err
		}
		edges += count
		if edges > pythonGraphMaximumEdges {
			return nil, fmt.Errorf("the Ruff graph exceeds its edge limit")
		}
	}
	return graph, nil
}

func pythonGraphEntry(decoder *json.Decoder, graph pythonGraph) (int, error) {
	token, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	rawSource, ok := token.(string)
	if !ok {
		return 0, fmt.Errorf("the graph has a non-string source path")
	}
	source, err := pythonGraphNormalizePath(rawSource)
	if err != nil {
		return 0, fmt.Errorf("the graph has an invalid source path: %w", err)
	}
	if _, duplicate := graph[source]; duplicate {
		return 0, fmt.Errorf("the graph names source path %q more than once", source)
	}
	targets, err := pythonGraphEntryTargets(decoder, source)
	if err != nil {
		return 0, err
	}
	graph[source] = targets
	return len(targets), nil
}

func pythonGraphEntryTargets(decoder *json.Decoder, source string) ([]string, error) {
	var targets []string
	if err := decoder.Decode(&targets); err != nil || targets == nil {
		return nil, fmt.Errorf("the graph targets for %q are not an array of paths", source)
	}
	seen := map[string]bool{}
	for index, rawTarget := range targets {
		target, err := pythonGraphNormalizePath(rawTarget)
		if err != nil {
			return nil, fmt.Errorf("the graph targets for %q include an invalid path: %w", source, err)
		}
		if seen[target] {
			return nil, fmt.Errorf("the graph targets for %q include path %q more than once", source, target)
		}
		seen[target] = true
		targets[index] = target
	}
	return targets, nil
}

func pythonGraphNormalizePath(value string) (string, error) {
	if value == "" || len(value) > pythonGraphMaximumPath || strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("path is empty or exceeds its lexical boundary")
	}
	normalized := pathpkg.Clean(strings.ReplaceAll(value, "\\", "/"))
	if normalized == "." || normalized == "" {
		return "", fmt.Errorf("path does not name a file")
	}
	return normalized, nil
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
	sourceFacts map[string]pythonSourceFact,
) []policy.Finding {
	return pythonGraphSourcePart(repo, project, sources, sources, owners, allFiles, graph, sourceFacts).findings
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

func pythonGraphMissingCoverage(sources, selected []string, dependencies map[string]map[string]bool, coverage map[string]string) {
	selectedSet := pythonExpectedSources(selected)
	contextMissing := []string{}
	for _, source := range sources {
		if _, found := dependencies[source]; !found {
			if selectedSet[source] {
				coverage[source] = "the Ruff graph omitted this selected Python source"
			} else {
				contextMissing = append(contextMissing, source)
			}
		}
	}
	if len(contextMissing) > 0 {
		path := sources[0]
		if len(selected) > 0 {
			path = selected[0]
		}
		if coverage[path] == "" {
			coverage[path] = fmt.Sprintf("the Ruff graph omitted %d contextual Python source(s), beginning with %s", len(contextMissing), contextMissing[0])
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
	if _, err := sourceModuleOwner(repo, target); err != nil {
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
	sourceFacts map[string]pythonSourceFact,
) {
	index := newPythonModuleIndex(project)
	pythonRuntimeTargetDependencies(index, sources, dependencies, sourceFacts)
	pythonProvenDynamicDependencies(index, sources, dependencies, sourceFacts)
	pythonObjectImportCoverage(repo, project, sources, dependencies, coverage, sourceFacts)
	pythonComputedImportCoverage(repo, project, sources, dependencies, coverage, sourceFacts)
	for _, source := range sources {
		if coverage[source] != "" {
			continue
		}
		if message := pythonSourceImportCoverage(index, source, dependencies[source], sourceFacts[source]); message != "" {
			coverage[source] = message
		}
	}
}

func pythonRuntimeTargetDependencies(index pythonModuleIndex, sources []string, dependencies map[string]map[string]bool, sourceFacts map[string]pythonSourceFact) {
	for _, source := range sources {
		for _, target := range sourceFacts[source].RuntimeTargets {
			for _, path := range index.files[target.Module] {
				if dependencies[source] != nil {
					dependencies[source][path] = true
				}
			}
		}
	}
}

func pythonProvenDynamicDependencies(index pythonModuleIndex, sources []string, dependencies map[string]map[string]bool, sourceFacts map[string]pythonSourceFact) {
	for _, source := range sources {
		for _, reference := range sourceFacts[source].Imports {
			if reference.Kind != "proven-dynamic" {
				continue
			}
			for _, path := range pythonReferenceTargets(index, source, reference, nil) {
				if dependencies[source] != nil {
					dependencies[source][path] = true
				}
			}
		}
	}
}

func pythonSourceImportCoverage(index pythonModuleIndex, source string, dependencies map[string]bool, facts pythonSourceFact) string {
	if facts.SHA256 == "" {
		return "the Python facts adapter omitted this selected source"
	}
	for _, reference := range facts.Imports {
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
	sourceOwners := repo.OwnerModuleNames(source)
	for target := range dependencies {
		targetOwners := repo.OwnerModuleNames(target)
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
