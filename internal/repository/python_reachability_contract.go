package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

func (repo Repository) pythonReachabilityContract(project PythonProject, dependency PythonPluginDependency, consumer policy.PythonDynamicConsumer) (string, pythonfacts.ContractDefinitionEvidence, error) {
	snapshot, err := repo.ReadPythonDistributionSources(project, dependency)
	if err != nil {
		return "", pythonfacts.ContractDefinitionEvidence{}, err
	}
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return "", pythonfacts.ContractDefinitionEvidence{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	parsed, err := pythonfacts.AnalyzeProjectSources(ctx, python, snapshot.Sources)
	if err != nil {
		return "", pythonfacts.ContractDefinitionEvidence{}, err
	}
	layout := PythonProject{Root: snapshot.Root, SourceRoots: []string{snapshot.Root}}
	modules := make([]pythonfacts.TypeModule, 0, len(parsed.Sources))
	for _, source := range parsed.Sources {
		module, packageName := PythonModuleName(layout, source.Path)
		modules = append(modules, pythonfacts.TypeModule{Path: source.Path, Module: module, Package: packageName, SourceSHA256: source.SHA256, Facts: source.TypeFacts})
	}
	request := pythonfacts.ContractDefinitionRequest{ID: consumer.Kind + ":" + consumer.Qualified + ":" + consumer.Member, Kind: consumer.Kind, Qualified: consumer.Qualified, Member: consumer.Member}
	result, err := pythonfacts.ResolveContractDefinitions(ctx, python, modules, []pythonfacts.ContractDefinitionRequest{request})
	if err != nil {
		return "", pythonfacts.ContractDefinitionEvidence{}, err
	}
	if len(result.Problems) > 0 {
		return "", pythonfacts.ContractDefinitionEvidence{}, fmt.Errorf("dependency-owned contract cannot resolve: %s", result.Problems[0].Message)
	}
	if len(result.Evidence) != 1 {
		return "", pythonfacts.ContractDefinitionEvidence{}, fmt.Errorf("dependency-owned contract evidence is missing")
	}
	return snapshot.Identity, result.Evidence[0], nil
}
