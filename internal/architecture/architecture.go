package architecture

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func Check(ctx context.Context, repo repository.Repository, selected []string) []policy.Finding {
	return CheckWithRunner(ctx, repo, selected, runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries})
}

func CheckWithRunner(ctx context.Context, repo repository.Repository, selected []string, commandRunner runner.Runner) []policy.Finding {
	return AnalyzeWithRunner(ctx, repo, selected, commandRunner).Findings
}

type Analysis struct {
	Findings    []policy.Finding
	Graph       *sourcegraph.Graph
	TestImports map[string][]string
}

func AnalyzeWithRunner(ctx context.Context, repo repository.Repository, selected []string, commandRunner runner.Runner) Analysis {
	allFiles, err := repo.AllFiles()
	if err != nil {
		return Analysis{Findings: []policy.Finding{{Check: "architecture.inventory", Path: "repository", Subject: "files", Message: err.Error()}}}
	}
	goInventory := repo.InspectGoModules(allFiles)
	part := mergeSourceGraphParts(
		goSourceGraph(repo, selected, allFiles, goInventory),
		javascriptSourceGraph(ctx, repo, selected, allFiles),
		pythonSourceGraph(ctx, repo, selected, allFiles, commandRunner),
	)
	graph, findings := completeSourceGraph(part, selected)
	return Analysis{Findings: findings, Graph: graph, TestImports: part.testImports}
}

func CoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, module := range repo.Config.Modules {
		matched := false
		for _, path := range files {
			if policy.MatchesAny(path, module.Paths) {
				matched = true
				break
			}
		}
		if !matched {
			findings = append(findings, policy.Finding{Check: "policy.moduleCoverage", Path: policy.ConfigFilename, Subject: module.Name, Message: "module paths do not match any governed file"})
		}
	}
	findings = append(findings, goPackageOwnerFindings(repo, files)...)
	return findings
}

func goPackageOwnerFindings(repo repository.Repository, files []string) []policy.Finding {
	ownersByDirectory := map[string]map[string]bool{}
	for _, path := range files {
		if repo.Language(path) != "go" || repo.IsTest(path) {
			continue
		}
		directory := filepath.ToSlash(filepath.Dir(path))
		if ownersByDirectory[directory] == nil {
			ownersByDirectory[directory] = map[string]bool{}
		}
		for _, owner := range repo.OwnerModuleNames(path) {
			ownersByDirectory[directory][owner] = true
		}
	}
	findings := []policy.Finding{}
	for directory, owners := range ownersByDirectory {
		if len(owners) > 1 {
			findings = append(findings, policy.Finding{Check: "policy.moduleCoverage", Path: directory, Subject: "go-package", Message: "one Go package directory cannot be split across architecture modules"})
		}
	}
	return findings
}

func internalGoDirectory(modules []repository.GoModule, imported string) (string, bool) {
	matched := repository.GoModule{}
	for _, module := range modules {
		if module.Name != "" && (imported == module.Name || strings.HasPrefix(imported, module.Name+"/")) && len(module.Name) > len(matched.Name) {
			matched = module
		}
	}
	if matched.Name == "" {
		return "", false
	}
	suffix := strings.TrimPrefix(strings.TrimPrefix(imported, matched.Name), "/")
	if suffix == "" {
		return matched.Root, true
	}
	if matched.Root == "." {
		return filepath.ToSlash(suffix), true
	}
	return filepath.ToSlash(filepath.Join(matched.Root, suffix)), true
}
