package architecture

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func Check(ctx context.Context, repo repository.Repository, selected []string) []policy.Finding {
	allFiles, err := repo.AllFiles()
	if err != nil {
		return []policy.Finding{{Check: "architecture.inventory", Path: "repository", Subject: "files", Message: err.Error()}}
	}
	modules := repo.GoModules(allFiles)
	findings := []policy.Finding{}
	for _, source := range selected {
		findings = append(findings, checkGoFile(repo, source, allFiles, modules)...)
	}
	return append(findings, javascriptFindings(ctx, repo, selected, allFiles)...)
}

func checkGoFile(repo repository.Repository, source string, allFiles []string, modules []repository.GoModule) []policy.Finding {
	if repo.Language(source) != "go" {
		return nil
	}
	owners := repo.ModuleNames(source)
	if len(owners) != 1 {
		return nil
	}
	if _, found := repo.OwningGoModule(source, modules); !found {
		return nil
	}
	data, err := repo.Read(source)
	if err != nil {
		return nil
	}
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, source, data, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	findings := []policy.Finding{}
	for _, imported := range parsed.Imports {
		if finding, exists := importFinding(repo, source, owners[0], modules, imported.Path.Value, set.Position(imported.Pos()).Line, allFiles); exists {
			findings = append(findings, finding)
		}
	}
	return findings
}

func importFinding(repo repository.Repository, source, sourceOwner string, modules []repository.GoModule, quotedImport string, line int, allFiles []string) (policy.Finding, bool) {
	raw, err := strconv.Unquote(quotedImport)
	if err != nil {
		return policy.Finding{}, false
	}
	targetDirectory, internal := internalGoDirectory(modules, raw)
	if !internal {
		return policy.Finding{}, false
	}
	targetPath := representativeGoFile(targetDirectory, allFiles)
	targetOwners := repo.ModuleNames(targetPath)
	if targetPath == "" || len(targetOwners) != 1 || targetOwners[0] == sourceOwner {
		return policy.Finding{}, false
	}
	sourceModule := repo.Config.Modules[repo.Config.ModuleByName[sourceOwner]]
	if slices.Contains(sourceModule.DependsOn, targetOwners[0]) {
		return policy.Finding{}, false
	}
	return policy.Finding{
		Check:   "architecture.moduleDependency",
		Path:    source,
		Subject: targetOwners[0],
		Message: fmt.Sprintf("line %d module %q imports module %q without declaring dependsOn", line, sourceOwner, targetOwners[0]),
	}, true
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
		if repo.Language(path) != "go" {
			continue
		}
		directory := filepath.ToSlash(filepath.Dir(path))
		if ownersByDirectory[directory] == nil {
			ownersByDirectory[directory] = map[string]bool{}
		}
		for _, owner := range repo.ModuleNames(path) {
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

func representativeGoFile(directory string, files []string) string {
	directory = strings.TrimSuffix(directory, "/")
	for _, path := range files {
		if filepath.Ext(path) != ".go" {
			continue
		}
		if filepath.ToSlash(filepath.Dir(path)) == directory {
			return path
		}
	}
	return ""
}
