package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonExternalProject(repo repository.Repository, project repository.PythonProject, facts map[string]pythonSourceFact, runtime pythonfacts.RuntimeCheckProject) (pythonResolution, error) {
	declarations := pythonExternalDeclarations(repo, project.Manifest)
	result := pythonResolution{external: []sourcegraph.ExternalComposition{}, findings: pythonUndeclaredExternalImports(project, facts, declarations)}
	if len(declarations) == 0 {
		return result, nil
	}
	requests := pythonExternalRuntimeRequests(declarations)
	distributions := []string{}
	for _, declaration := range declarations {
		distributions = append(distributions, declaration.Distribution)
	}
	dependencies, dependencyError := repo.PythonPluginDependencies(project, distributions)
	for index, declaration := range declarations {
		edge, message := pythonExternalDeclaration(repo, project, facts, declaration, requests[index].ID, runtime, dependencies, dependencyError)
		if message != "" {
			result.findings = append(result.findings, pythonExternalFinding(declaration, message))
			continue
		}
		result.external = append(result.external, edge)
		fact := facts[declaration.Consumer.Importer]
		if fact.ExternalImports == nil {
			fact.ExternalImports = map[pythonfacts.SourceLocation]bool{}
		}
		fact.ExternalImports[pythonfacts.SourceLocation{Line: edge.Line, Column: edge.Column}] = true
		facts[declaration.Consumer.Importer] = fact
	}
	encoded, _ := json.Marshal(dependencies)
	digest := sha256.Sum256(append([]byte(runtime.Identity+"\x00"), encoded...))
	result.identity = hex.EncodeToString(digest[:])
	return result, nil
}

func pythonExternalDeclarations(repo repository.Repository, project string) []policy.PythonExternalPluginImport {
	result := []policy.PythonExternalPluginImport{}
	for _, declaration := range repo.Config.Scope.PythonExternalPluginImports {
		if declaration.Project == project {
			result = append(result, declaration)
		}
	}
	return result
}

func pythonExternalRuntimeRequests(declarations []policy.PythonExternalPluginImport) []pythonfacts.RuntimeCheckInput {
	requests := make([]pythonfacts.RuntimeCheckInput, 0, len(declarations))
	for index, declaration := range declarations {
		encoded, _ := json.Marshal(declaration)
		digest := sha256.Sum256(encoded)
		consumer, _ := json.Marshal(declaration.Consumer)
		requests = append(requests, pythonfacts.RuntimeCheckInput{ID: fmt.Sprintf("external:%04d:%s", index, hex.EncodeToString(digest[:])), Consumer: consumer, Check: pythonfacts.RuntimeCheck{Kind: declaration.Check.Kind, Protocol: declaration.Check.Protocol, Site: pythonfacts.SourceLocation{Line: declaration.Check.Site.Line, Column: declaration.Check.Site.Column}}})
	}
	return requests
}

func pythonExternalDeclaration(repo repository.Repository, project repository.PythonProject, facts map[string]pythonSourceFact, declaration policy.PythonExternalPluginImport, id string, runtime pythonfacts.RuntimeCheckProject, dependencies repository.PythonPluginDependencyFacts, dependencyError error) (sourcegraph.ExternalComposition, string) {
	for _, problem := range runtime.Problems {
		if problem.ID == id {
			return sourcegraph.ExternalComposition{}, problem.Message
		}
	}
	position := slices.IndexFunc(runtime.Evidence, func(value pythonfacts.RuntimeCheckEvidence) bool { return value.ID == id })
	if position < 0 {
		return sourcegraph.ExternalComposition{}, "runtime-check evidence is missing"
	}
	evidence := runtime.Evidence[position]
	if dependencyError != nil {
		return sourcegraph.ExternalComposition{}, dependencyError.Error()
	}
	dependencyPosition := slices.IndexFunc(dependencies.Dependencies, func(value repository.PythonPluginDependency) bool {
		return value.Distribution == declaration.Distribution
	})
	if dependencyPosition < 0 {
		return sourcegraph.ExternalComposition{}, "direct dependency evidence is missing"
	}
	dependency := dependencies.Dependencies[dependencyPosition]
	if dependency.Error != "" {
		return sourcegraph.ExternalComposition{}, dependency.Error
	}
	imported, message := pythonExternalObjectInput(repo, project, facts[declaration.Consumer.Importer], declaration, evidence)
	if message != "" {
		return sourcegraph.ExternalComposition{}, message
	}
	return pythonExternalComposition(declaration, evidence, dependencies, dependency, imported), ""
}

func pythonExternalObjectInput(repo repository.Repository, project repository.PythonProject, fact pythonSourceFact, declaration policy.PythonExternalPluginImport, evidence pythonfacts.RuntimeCheckEvidence) (pythonfacts.ObjectImport, string) {
	if declaration.InputGrammar != "python-module-object/v1" {
		return pythonfacts.ObjectImport{}, "input grammar is unsupported"
	}
	if _, err := sourceModuleOwner(repo, declaration.Consumer.Importer); err != nil {
		return pythonfacts.ObjectImport{}, err.Error()
	}
	position := slices.IndexFunc(fact.ObjectImports, func(value pythonfacts.ObjectImport) bool {
		return value.Site == evidence.LoaderSite && value.EndSite == evidence.LoaderEndSite
	})
	if position < 0 {
		return pythonfacts.ObjectImport{}, "loader has no independent object-import evidence"
	}
	imported := fact.ObjectImports[position]
	if imported.Error != "" {
		return pythonfacts.ObjectImport{}, imported.Error
	}
	if message := pythonExternalRegistryMessage(imported, declaration); message != "" {
		return pythonfacts.ObjectImport{}, message
	}
	if imported.Guard != nil {
		return imported, pythonExternalGuardMessage(project, declaration.Namespace, *imported.Guard)
	}
	return imported, pythonExternalTargetsMessage(project, declaration.Namespace, imported.Targets)
}

func pythonExternalGuardMessage(project repository.PythonProject, namespace string, guard pythonfacts.ObjectInputGuard) string {
	if guard.Namespace != namespace {
		return "runtime input guard names a different dependency namespace"
	}
	if guard.StandardLibrary || pythonLocalModule(newPythonModuleIndex(project), namespace) {
		return "runtime input guard admits a local or standard-library namespace"
	}
	return ""
}

func pythonExternalTargetsMessage(project repository.PythonProject, namespace string, targets []pythonfacts.ObjectImportTarget) string {
	index := newPythonModuleIndex(project)
	for _, target := range targets {
		if target.StandardLibrary || pythonLocalModule(index, target.Module) {
			return "external plug-in target is a local or standard-library module"
		}
		if target.Module != namespace && !strings.HasPrefix(target.Module, namespace+".") {
			return "loaded module is outside the admitted dependency namespace"
		}
	}
	return ""
}

func pythonExternalRegistryMessage(imported pythonfacts.ObjectImport, declaration policy.PythonExternalPluginImport) string {
	if imported.Registry == nil {
		if declaration.Configuration != nil {
			return "registry declaration does not match the loader input"
		}
		return ""
	}
	input := declaration.Configuration
	if input == nil || input.Path != imported.Registry.Path || input.JSONPointer != imported.Registry.JSONPointer || input.SHA256 != imported.Registry.SHA256 {
		return "governed registry input, selector, or digest is missing or stale"
	}
	return ""
}

func pythonExternalComposition(declaration policy.PythonExternalPluginImport, runtime pythonfacts.RuntimeCheckEvidence, facts repository.PythonPluginDependencyFacts, dependency repository.PythonPluginDependency, imported pythonfacts.ObjectImport) sourcegraph.ExternalComposition {
	encoded, _ := json.Marshal(imported)
	inputDigest := sha256.Sum256(encoded)
	return sourcegraph.ExternalComposition{Source: declaration.Consumer.Importer, SourceResolution: "file:" + declaration.Consumer.Importer, Line: runtime.LoaderSite.Line, Column: runtime.LoaderSite.Column,
		Dependency: sourcegraph.ExternalDependency{Project: facts.Project, Lock: facts.Lock, ManifestSHA256: facts.ManifestSHA256, LockSHA256: facts.LockSHA256, Distribution: dependency.Distribution, Version: dependency.Version, Kind: dependency.Kind, Source: dependency.Source, Namespace: declaration.Namespace},
		Contract:   sourcegraph.ExternalContract{InputGrammar: declaration.InputGrammar, CheckKind: runtime.Kind, Protocol: runtime.Contract, RuntimeType: runtime.RuntimeType, CheckLine: runtime.CheckSite.Line, CheckColumn: runtime.CheckSite.Column, SourceSHA256: declaration.Consumer.SourceSHA256, RuntimeSHA256: runtime.Identity, InputSHA256: hex.EncodeToString(inputDigest[:])}}
}

func pythonExternalFinding(declaration policy.PythonExternalPluginImport, message string) policy.Finding {
	return policy.Finding{Check: "policy.pythonExternalPluginImport", Path: declaration.Consumer.Importer, Line: declaration.Consumer.Site.Line, Column: declaration.Consumer.Site.Column, Subject: declaration.Distribution, Message: message,
		Remediation: policy.FindingRemediation{Summary: "Verify the direct dependency, namespace, loader input, and rejecting runtime check before updating this exact external plug-in declaration.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "architecture", "--files", declaration.Consumer.Importer}, Cwd: "."}}}
}

func pythonUndeclaredExternalImports(project repository.PythonProject, facts map[string]pythonSourceFact, declarations []policy.PythonExternalPluginImport) []policy.Finding {
	index := newPythonModuleIndex(project)
	findings := []policy.Finding{}
	for source, fact := range facts {
		for _, imported := range fact.ObjectImports {
			declared := slices.ContainsFunc(declarations, func(value policy.PythonExternalPluginImport) bool {
				return value.Consumer.Importer == source && value.Consumer.Site.Line == imported.Site.Line && value.Consumer.Site.Column == imported.Site.Column
			})
			if declared || imported.Error != "" || imported.Guard == nil && !slices.ContainsFunc(imported.Targets, func(target pythonfacts.ObjectImportTarget) bool {
				return !target.StandardLibrary && !pythonLocalModule(index, target.Module)
			}) {
				continue
			}
			findings = append(findings, policy.Finding{Check: "policy.pythonExternalPluginImport", Path: source, Line: imported.Site.Line, Column: imported.Site.Column, Subject: "undeclared-loader", Message: "external object loader requires an exact direct dependency, namespace, input grammar, and runtime-check declaration"})
		}
	}
	return pythonSortedFindings(findings)
}
