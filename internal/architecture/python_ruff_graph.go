package architecture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type pythonRuffEdgeEvidence struct {
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Kind   string `json:"kind"`
}

type pythonDeclaredArchitecture struct {
	edges    map[string]map[string]pythonRuffEdgeEvidence
	external []sourcegraph.ExternalComposition
	findings []policy.Finding
}

var pythonQualifiedName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*$`)

func pythonRuffGraphSourcePart(
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	selectedSources []string,
	owners map[string]string,
	allFiles []string,
	graph pythonGraph,
	runtimeGraph pythonGraph,
) sourceGraphPart {
	dependencies, coverage, err := pythonGraphDependencies(repo, project, sources, graph)
	if err != nil {
		return sourceGraphPart{findings: pythonCoverageForSources(selectedSources, err.Error()), incomplete: true}
	}
	runtimeDependencies, runtimeCoverage, err := pythonGraphDependencies(repo, project, sources, runtimeGraph)
	if err != nil {
		return sourceGraphPart{findings: pythonCoverageForSources(selectedSources, err.Error()), incomplete: true}
	}
	pythonGraphMissingCoverage(sources, selectedSources, dependencies, coverage)
	pythonGraphMissingCoverage(sources, selectedSources, runtimeDependencies, runtimeCoverage)
	pythonRuffGraphRelationCoverage(dependencies, runtimeDependencies, runtimeCoverage)
	for source, message := range runtimeCoverage {
		if coverage[source] == "" {
			coverage[source] = message
		}
	}
	pythonGraphTargetCoverage(repo, project, owners, allFiles, dependencies, coverage)
	declared := pythonDeclaredGraph(repo, project, sources, dependencies, coverage)
	coverageFindings := pythonCoverageFindings(coverage)
	findings := append([]policy.Finding{}, coverageFindings...)
	findings = append(findings, declared.findings...)
	findings = append(findings, pythonModuleDependencyFindings(repo, selectedSources, dependencies, coverage)...)
	part := sourceGraphPart{findings: pythonSortedFindings(findings), external: declared.external}
	part.incomplete = len(coverageFindings) > 0 || slices.ContainsFunc(declared.findings, func(finding policy.Finding) bool {
		return finding.Severity != policy.FindingInformation
	})
	if part.incomplete {
		return part
	}
	part.nodes = pythonGraphNodes(repo, project, sources, dependencies)
	part.edges = pythonRuffGraphEdges(sources, dependencies, runtimeDependencies, declared.edges)
	input, inputErr := pythonRuffGraphInput(repo, project, part.nodes, graph, runtimeGraph, declared)
	if inputErr != nil {
		part.findings = append(part.findings, pythonImportCoverage(selectedSources[0], inputErr.Error()))
		part.incomplete = true
		return part
	}
	part.inputs = []sourcegraph.FactInput{input}
	return part
}

func pythonRuffGraphRelationCoverage(complete, runtime map[string]map[string]bool, coverage map[string]string) {
	for source, targets := range runtime {
		for target := range targets {
			if !complete[source][target] {
				coverage[source] = "the runtime Ruff graph contains an edge absent from the complete graph"
			}
		}
	}
}

func pythonDeclaredGraph(repo repository.Repository, project repository.PythonProject, sources []string, dependencies map[string]map[string]bool, coverage map[string]string) pythonDeclaredArchitecture {
	result := pythonDeclaredArchitecture{edges: map[string]map[string]pythonRuffEdgeEvidence{}, external: []sourcegraph.ExternalComposition{}}
	selected := pythonExpectedSources(sources)
	pythonDeclareComputedImports(repo, project, selected, dependencies, coverage, result.edges)
	result.findings = pythonDeclareRuntimeLoaders(repo, project, selected)
	result.external, result.findings = pythonDeclaredExternal(repo, project, selected, result.external, result.findings)
	return result
}

func pythonDeclareComputedImports(repo repository.Repository, project repository.PythonProject, selected map[string]bool, dependencies map[string]map[string]bool, coverage map[string]string, edges map[string]map[string]pythonRuffEdgeEvidence) {
	index := newPythonModuleIndex(project)
	for _, declaration := range repo.Config.Scope.PythonComputedImports {
		if declaration.Project != project.Manifest || !selected[declaration.Importer] {
			continue
		}
		message := pythonDeclaredSourceMessage(repo, project, declaration.Importer, declaration.Module, declaration.SourceSHA256, declaration.Line, declaration.Column)
		targets := []string{}
		if message == "" {
			targets, message = pythonDeclaredComputedTargets(repo, project, declaration)
		}
		if message == "" {
			message = pythonAddDeclaredTargets(index, declaration.Importer, declaration.Line, declaration.Column, targets, dependencies, edges)
		}
		if message != "" {
			coverage[declaration.Importer] = message
		}
	}
}

func pythonDeclareRuntimeLoaders(repo repository.Repository, project repository.PythonProject, selected map[string]bool) []policy.Finding {
	findings := []policy.Finding{}
	for _, declaration := range repo.Config.Scope.PythonRuntimeLoaders {
		if declaration.Project != project.Manifest || !selected[declaration.Consumer.Importer] {
			continue
		}
		message := pythonDeclaredSourceMessage(repo, project, declaration.Consumer.Importer, declaration.Consumer.Module, declaration.Consumer.SourceSHA256, declaration.Consumer.Site.Line, declaration.Consumer.Site.Column)
		if message == "" && declaration.InputGrammar != "ascii-module-object/v1" {
			message = "runtime loader input grammar is unsupported"
		}
		finding := policy.Finding{Check: "architecture.importCoverage", Path: declaration.Consumer.Importer, Line: declaration.Consumer.Site.Line, Column: declaration.Consumer.Site.Column, Subject: "runtime-loader", Message: message}
		if message == "" {
			finding.Severity = policy.FindingInformation
			finding.Message = "Operator-controlled runtime loader boundary: unknown targets are delegated and are not represented as local dependency edges. " + declaration.Reason
		}
		findings = append(findings, finding)
	}
	return findings
}

func pythonDeclaredSourceMessage(repo repository.Repository, project repository.PythonProject, source, module, expectedDigest string, line, column int) string {
	if _, err := sourceModuleOwner(repo, source); err != nil {
		return err.Error()
	}
	actualModule, _ := repository.PythonModuleName(project, source)
	if actualModule != module {
		return "declared Python module identity is stale"
	}
	data, err := repo.Read(source)
	if err != nil {
		return "declared Python source is unreadable: " + err.Error()
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return "declared Python source digest is stale"
	}
	if !pythonDeclaredLocation(data, line, column) {
		return "declared Python source location is stale"
	}
	return ""
}

func pythonDeclaredLocation(data []byte, line, column int) bool {
	if line < 1 || column < 1 {
		return false
	}
	lines := bytes.Split(data, []byte{'\n'})
	if line > len(lines) || !utf8.Valid(lines[line-1]) {
		return false
	}
	return column <= utf8.RuneCount(lines[line-1])+1
}

func pythonDeclaredComputedTargets(repo repository.Repository, project repository.PythonProject, declaration policy.PythonComputedImport) ([]string, string) {
	if declaration.Callee != "pkgutil.resolve_name" {
		return pythonComputedTargets(repo, project, declaration)
	}
	return pythonDeclaredObjectTargets(repo, declaration)
}

func pythonDeclaredObjectTargets(repo repository.Repository, declaration policy.PythonComputedImport) ([]string, string) {
	if len(declaration.Configuration) != 1 {
		return nil, "computed object import requires one governed configuration input"
	}
	values, message := pythonComputedObjectConfigurationTargets(repo, declaration.Configuration[0])
	if message != "" {
		return nil, message
	}
	modules := []string{}
	for _, value := range values {
		module, targetMessage := pythonDeclaredObjectModule(value, declaration.Namespace)
		if targetMessage != "" {
			return nil, targetMessage
		}
		modules = append(modules, module)
	}
	sort.Strings(modules)
	return slices.Compact(modules), ""
}

func pythonDeclaredObjectModule(value, namespace string) (string, string) {
	module, object, found := strings.Cut(value, ":")
	if !found || object == "" || strings.Contains(object, ":") {
		return "", "computed object import configuration contains an invalid module:object target"
	}
	if !pythonQualifiedName.MatchString(module) || !pythonQualifiedName.MatchString(object) {
		return "", "computed object import configuration contains an invalid module:object target"
	}
	if module != namespace && !strings.HasPrefix(module, namespace+".") {
		return "", fmt.Sprintf("computed Python import target %q escapes namespace %q", module, namespace)
	}
	return module, ""
}

func pythonAddDeclaredTargets(index pythonModuleIndex, source string, line, column int, targets []string, dependencies map[string]map[string]bool, evidence map[string]map[string]pythonRuffEdgeEvidence) string {
	if evidence[source] == nil {
		evidence[source] = map[string]pythonRuffEdgeEvidence{}
	}
	for _, target := range targets {
		paths := index.files[target]
		if len(paths) != 1 || index.ambiguous[target] {
			return fmt.Sprintf("line %d computed Python import target %q does not resolve to exactly one governed project module", line, target)
		}
		dependencies[source][paths[0]] = true
		evidence[source][paths[0]] = pythonRuffEdgeEvidence{Line: line, Column: column, Kind: "proven-dynamic"}
	}
	return ""
}

func pythonRuffGraphEdges(sources []string, dependencies, runtimeDependencies map[string]map[string]bool, declared map[string]map[string]pythonRuffEdgeEvidence) []sourcegraph.Edge {
	edges := []sourcegraph.Edge{}
	for _, source := range sources {
		targets := make([]string, 0, len(dependencies[source]))
		for target := range dependencies[source] {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			evidence := declared[source][target]
			if runtimeDependencies[source][target] && !strings.HasSuffix(source, ".pyi") {
				evidence = pythonRuffEdgeEvidence{Line: 1, Column: 1, Kind: "runtime"}
			} else if evidence.Line == 0 {
				evidence = pythonRuffEdgeEvidence{Line: 1, Column: 1, Kind: "type-only"}
			}
			edges = append(edges, pythonGraphEdge(source, target, evidence.Line, evidence.Column, evidence.Kind))
		}
	}
	return edges
}

func pythonComputedObjectConfigurationTargets(repo repository.Repository, input policy.PythonComputedImportInput) ([]string, string) {
	data, err := repo.Read(input.Path)
	if err != nil {
		return nil, fmt.Sprintf("computed import configuration %s is unreadable: %v", input.Path, err)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != input.SHA256 {
		return nil, fmt.Sprintf("computed import configuration %s changed and its declaration is stale", input.Path)
	}
	value, err := decodeComputedJSON(data)
	if err != nil {
		return nil, fmt.Sprintf("computed import configuration %s is malformed: %v", input.Path, err)
	}
	value, err = computedJSONPointer(value, input.JSONPointer)
	if err != nil {
		return nil, fmt.Sprintf("computed import configuration %s pointer %s is invalid: %v", input.Path, input.JSONPointer, err)
	}
	values, message := pythonComputedObjectValues(value)
	if message != "" {
		return nil, message
	}
	if len(values) == 0 || slices.Contains(values, "") {
		return nil, "computed object import configuration selects no targets"
	}
	sort.Strings(values)
	return values, ""
}

func pythonComputedObjectValues(value any) ([]string, string) {
	values := []string{}
	switch current := value.(type) {
	case string:
		values = append(values, current)
	case []any:
		for _, item := range current {
			text, ok := item.(string)
			if !ok {
				return nil, "computed object import configuration must contain only module:object strings"
			}
			values = append(values, text)
		}
	case map[string]any:
		for _, item := range current {
			text, ok := item.(string)
			if !ok {
				return nil, "computed object import configuration must contain only module:object strings"
			}
			values = append(values, text)
		}
	default:
		return nil, "computed object import configuration must select module:object strings"
	}
	return values, ""
}

func pythonRuffGraphInput(repo repository.Repository, project repository.PythonProject, nodes []sourcegraph.Node, graph, runtimeGraph pythonGraph, declared pythonDeclaredArchitecture) (sourcegraph.FactInput, error) {
	paths := make([]string, 0, len(nodes))
	partition := sha256.New()
	for _, node := range nodes {
		paths = append(paths, node.Path)
		data, err := repo.Read(node.Path)
		if err != nil {
			return sourcegraph.FactInput{}, fmt.Errorf("read %s for Ruff graph identity: %w", node.Path, err)
		}
		_, _ = partition.Write([]byte(node.Path + "\x00"))
		_, _ = partition.Write(data)
		_, _ = partition.Write([]byte{'\x00'})
	}
	sort.Strings(paths)
	graphData, _ := json.Marshal(struct {
		Complete pythonGraph `json:"complete"`
		Runtime  pythonGraph `json:"runtime"`
	}{graph, runtimeGraph})
	resolutionData, _ := json.Marshal(struct {
		Edges    map[string]map[string]pythonRuffEdgeEvidence
		External []sourcegraph.ExternalComposition
	}{declared.edges, declared.external})
	factsDigest := sha256.Sum256(graphData)
	resolutionDigest := sha256.Sum256(resolutionData)
	return sourcegraph.FactInput{
		Analyzer: "ruff", Protocol: pythonGraphProtocol, Project: project.Manifest, Root: project.Root, Paths: paths,
		FactsSHA256: hex.EncodeToString(factsDigest[:]), PartitionsSHA256: hex.EncodeToString(partition.Sum(nil)), ResolutionSHA256: hex.EncodeToString(resolutionDigest[:]),
	}, nil
}

func pythonDeclaredExternal(repo repository.Repository, project repository.PythonProject, selected map[string]bool, external []sourcegraph.ExternalComposition, findings []policy.Finding) ([]sourcegraph.ExternalComposition, []policy.Finding) {
	declarations := pythonSelectedExternalDeclarations(repo, project, selected)
	distributions := []string{}
	for _, declaration := range declarations {
		distributions = append(distributions, declaration.Distribution)
	}
	if len(distributions) == 0 {
		return external, findings
	}
	dependencies, dependencyError := repo.PythonPluginDependencies(project, distributions)
	for _, declaration := range declarations {
		edge, message := pythonDeclaredExternalComposition(repo, project, declaration, dependencies, dependencyError)
		if message != "" {
			findings = append(findings, pythonExternalFinding(declaration, message))
			continue
		}
		external = append(external, edge)
	}
	return external, findings
}

func pythonSelectedExternalDeclarations(repo repository.Repository, project repository.PythonProject, selected map[string]bool) []policy.PythonExternalPluginImport {
	declarations := []policy.PythonExternalPluginImport{}
	for _, declaration := range pythonExternalDeclarations(repo, project.Manifest) {
		if selected[declaration.Consumer.Importer] {
			declarations = append(declarations, declaration)
		}
	}
	return declarations
}

func pythonDeclaredExternalComposition(repo repository.Repository, project repository.PythonProject, declaration policy.PythonExternalPluginImport, dependencies repository.PythonPluginDependencyFacts, dependencyError error) (sourcegraph.ExternalComposition, string) {
	message := pythonDeclaredSourceMessage(repo, project, declaration.Consumer.Importer, declaration.Consumer.Module, declaration.Consumer.SourceSHA256, declaration.Consumer.Site.Line, declaration.Consumer.Site.Column)
	if message != "" {
		return sourcegraph.ExternalComposition{}, message
	}
	if declaration.InputGrammar != "python-module-object/v1" {
		return sourcegraph.ExternalComposition{}, "input grammar is unsupported"
	}
	dependency, message := pythonDeclaredExternalDependency(declaration.Distribution, dependencies, dependencyError)
	if message != "" {
		return sourcegraph.ExternalComposition{}, message
	}
	inputDigest, message := pythonExternalInputDigest(repo, declaration)
	if message != "" {
		return sourcegraph.ExternalComposition{}, message
	}
	checkData, _ := json.Marshal(declaration.Check)
	checkDigest := sha256.Sum256(checkData)
	return sourcegraph.ExternalComposition{
		Source: declaration.Consumer.Importer, SourceResolution: "file:" + declaration.Consumer.Importer, Line: declaration.Consumer.Site.Line, Column: declaration.Consumer.Site.Column,
		Dependency: sourcegraph.ExternalDependency{Project: dependencies.Project, Lock: dependencies.Lock, ManifestSHA256: dependencies.ManifestSHA256, LockSHA256: dependencies.LockSHA256, Distribution: dependency.Distribution, Version: dependency.Version, Kind: dependency.Kind, Source: dependency.Source, Namespace: declaration.Namespace},
		Contract:   sourcegraph.ExternalContract{InputGrammar: declaration.InputGrammar, CheckKind: declaration.Check.Kind, Protocol: declaration.Check.Protocol, RuntimeType: declaration.Check.Protocol, CheckLine: declaration.Check.Site.Line, CheckColumn: declaration.Check.Site.Column, SourceSHA256: declaration.Consumer.SourceSHA256, RuntimeSHA256: hex.EncodeToString(checkDigest[:]), InputSHA256: inputDigest},
	}, ""
}

func pythonDeclaredExternalDependency(distribution string, dependencies repository.PythonPluginDependencyFacts, dependencyError error) (repository.PythonPluginDependency, string) {
	if dependencyError != nil {
		return repository.PythonPluginDependency{}, dependencyError.Error()
	}
	position := slices.IndexFunc(dependencies.Dependencies, func(value repository.PythonPluginDependency) bool {
		return value.Distribution == distribution
	})
	if position < 0 {
		return repository.PythonPluginDependency{}, "direct dependency evidence is missing"
	}
	dependency := dependencies.Dependencies[position]
	return dependency, dependency.Error
}

func pythonExternalInputDigest(repo repository.Repository, declaration policy.PythonExternalPluginImport) (string, string) {
	if declaration.Configuration == nil {
		digest := sha256.Sum256([]byte(declaration.Consumer.Argument + "\x00" + declaration.Namespace))
		return hex.EncodeToString(digest[:]), ""
	}
	data, err := repo.Read(declaration.Configuration.Path)
	if err != nil {
		return "", "governed registry input is unreadable: " + err.Error()
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != declaration.Configuration.SHA256 {
		return "", "governed registry input digest is stale"
	}
	identity, _ := json.Marshal(declaration.Configuration)
	inputDigest := sha256.Sum256(identity)
	return hex.EncodeToString(inputDigest[:]), ""
}
