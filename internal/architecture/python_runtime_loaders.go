package architecture

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonRuntimeLoaderProject(ctx context.Context, repo repository.Repository, python string, project repository.PythonProject, modules []pythonfacts.TypeModule, facts map[string]pythonSourceFact) (pythonResolution, error) {
	result := pythonResolution{}
	for _, declaration := range repo.Config.Scope.PythonRuntimeLoaders {
		if declaration.Project != project.Manifest {
			continue
		}
		source, err := repo.Read(declaration.Consumer.Importer)
		if err != nil {
			return result, err
		}
		consumer, _ := json.Marshal(declaration.Consumer)
		checked, err := pythonfacts.ResolveRuntimeLoader(ctx, python, modules, pythonfacts.RuntimeLoaderInput{Consumer: consumer, Source: string(source), InputGrammar: declaration.InputGrammar, Check: pythonfacts.RuntimeCheck{Kind: declaration.Check.Kind, Protocol: declaration.Check.Protocol, Site: pythonfacts.SourceLocation{Line: declaration.Check.Site.Line, Column: declaration.Check.Site.Column}}})
		if err != nil {
			return result, err
		}
		result.identity += checked.Identity
		finding := policy.Finding{Check: "architecture.importCoverage", Path: declaration.Consumer.Importer, Line: declaration.Consumer.Site.Line, Column: declaration.Consumer.Site.Column, Subject: "runtime-loader"}
		if checked.Error != "" {
			finding.Message = checked.Error
			result.findings = append(result.findings, finding)
			continue
		}
		if message := pythonRuntimeLoaderTargets(project, facts, checked.Targets); message != "" {
			finding.Message = message
			result.findings = append(result.findings, finding)
			continue
		}
		fact := facts[declaration.Consumer.Importer]
		if fact.RuntimeLoaders == nil {
			fact.RuntimeLoaders = map[pythonfacts.SourceLocation]bool{}
		}
		fact.RuntimeLoaders[pythonfacts.SourceLocation{Line: declaration.Consumer.Site.Line, Column: declaration.Consumer.Site.Column}] = true
		facts[declaration.Consumer.Importer] = fact
		finding.Severity = policy.FindingInformation
		finding.Message = "Operator-controlled runtime loader boundary: unknown targets are delegated, not statically verified; import executes before the protocol check. " + declaration.Reason
		result.findings = append(result.findings, finding)
	}
	return result, nil
}

func pythonRuntimeLoaderTargets(project repository.PythonProject, facts map[string]pythonSourceFact, targets []pythonfacts.RuntimeLoaderTarget) string {
	index := newPythonModuleIndex(project)
	for _, target := range targets {
		if !pythonLocalModule(index, target.Module) {
			continue
		}
		if len(index.files[target.Module]) != 1 || pythonAmbiguousImportModule(index, target.Module) {
			return fmt.Sprintf("known runtime loader target %q is missing or ambiguous", target.Module)
		}
		fact := facts[target.Path]
		fact.RuntimeTargets = append(fact.RuntimeTargets, pythonImportReference{Module: target.Module, Line: target.Site.Line, Column: target.Site.Column, Kind: "proven-dynamic", Literal: true})
		fact.Imports = append(fact.Imports, fact.RuntimeTargets[len(fact.RuntimeTargets)-1])
		facts[target.Path] = fact
	}
	return ""
}
