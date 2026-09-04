package architecture

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const javascriptImportBudget = 20 * time.Minute

var javascriptSourceExtensions = map[string]bool{
	".cjs": true, ".cts": true, ".js": true, ".jsx": true,
	".mjs": true, ".mts": true, ".ts": true, ".tsx": true,
}

func javascriptFindings(ctx context.Context, repo repository.Repository, selected, allFiles []string) []policy.Finding {
	readable, findings := javascriptImportSources(repo, selected)
	if len(readable) == 0 {
		return findings
	}
	ctx, cancel := context.WithTimeout(ctx, javascriptImportBudget)
	defer cancel()
	result, err := javascript.Bundle{PolicyRoot: repo.PolicyRoot}.Imports(ctx, repo.Root, readable)
	if err != nil {
		return append(findings, policy.Finding{
			Check: "policy.tool", Path: "repository", Subject: "javascript-bundle", Message: err.Error(),
		})
	}
	for _, entry := range result.Unsupported {
		findings = append(findings, importCoverageFinding(entry.Path,
			"the policy-owned import reader could not read this import: "+entry.Reason))
	}
	governed := make(map[string]bool, len(allFiles))
	for _, path := range allFiles {
		governed[path] = true
	}
	packages := newNodePackages(repo, allFiles)
	findings = append(findings, javascriptFactFindings(repo, packages, governed, result.Imports)...)
	return append(findings, packages.coverage...)
}

func javascriptFactFindings(repo repository.Repository, packages *nodePackages,
	governed map[string]bool, facts []javascript.ImportFact) []policy.Finding {
	findings := []policy.Finding{}
	for _, fact := range facts {
		if finding, exists := javascriptImportFinding(repo, governed, fact); exists {
			findings = append(findings, finding)
		}
		if finding, exists := javascriptPackageFinding(repo, packages, fact); exists {
			findings = append(findings, finding)
		}
	}
	return findings
}

func javascriptImportSources(repo repository.Repository, selected []string) ([]string, []policy.Finding) {
	readable := []string{}
	findings := []policy.Finding{}
	for _, path := range selected {
		if repo.Language(path) != "typescript" || len(repo.ModuleNames(path)) != 1 {
			continue
		}
		if !javascriptSourceExtensions[strings.ToLower(filepath.Ext(path))] {
			findings = append(findings, importCoverageFinding(path,
				"the policy-owned import reader does not read this file"))
			continue
		}
		readable = append(readable, path)
	}
	return readable, findings
}

func javascriptImportFinding(repo repository.Repository, governed map[string]bool, fact javascript.ImportFact) (policy.Finding, bool) {
	if repo.IsTest(fact.Path) {
		return policy.Finding{}, false
	}
	if fact.Resolved == "" || !governed[fact.Resolved] {
		return policy.Finding{}, false
	}
	owners := repo.ModuleNames(fact.Path)
	targets := repo.ModuleNames(fact.Resolved)
	if len(owners) != 1 || len(targets) != 1 || owners[0] == targets[0] {
		return policy.Finding{}, false
	}
	sourceModule := repo.Config.Modules[repo.Config.ModuleByName[owners[0]]]
	if slices.Contains(sourceModule.DependsOn, targets[0]) {
		return policy.Finding{}, false
	}
	return policy.Finding{
		Check:   "architecture.moduleDependency",
		Path:    fact.Path,
		Subject: targets[0],
		Message: fmt.Sprintf("line %d module %q imports module %q without declaring dependsOn", fact.Line, owners[0], targets[0]),
	}, true
}

func javascriptPackageFinding(repo repository.Repository, packages *nodePackages,
	fact javascript.ImportFact) (policy.Finding, bool) {
	if fact.Package == "" {
		return policy.Finding{}, false
	}
	owner, owned := packages.owning(fact.Path)
	if !owned || fact.Package == owner.name || owner.runtime[fact.Package] {
		return policy.Finding{}, false
	}
	message := ""
	switch {
	case owner.development[fact.Package]:
		if repo.IsDevelopment(fact.Path) {
			return policy.Finding{}, false
		}
		message = fmt.Sprintf("line %d package %q imports development dependency %q from source that ships",
			fact.Line, owner.root, fact.Package)
	case installedPackage(fact.Resolved):
		message = fmt.Sprintf("line %d package %q imports %q without declaring it as a dependency",
			fact.Line, owner.root, fact.Package)
	default:
		return policy.Finding{}, false
	}
	return policy.Finding{
		Check: "architecture.packageDependency", Path: fact.Path, Subject: fact.Package, Message: message,
	}, true
}

func installedPackage(resolved string) bool {
	return resolved != "" && slices.Contains(strings.Split(resolved, "/"), "node_modules")
}

func importCoverageFinding(path, message string) policy.Finding {
	return policy.Finding{Check: "architecture.importCoverage", Path: path, Subject: "typescript", Message: message}
}

const nodeManifestName = "package.json"

type nodePackages struct {
	repo     repository.Repository
	owners   map[string]bool
	read     map[string]nodePackage
	coverage []policy.Finding
}

type nodePackage struct {
	root        string
	name        string
	runtime     map[string]bool
	development map[string]bool
	readable    bool
}

func newNodePackages(repo repository.Repository, allFiles []string) *nodePackages {
	owners := map[string]bool{}
	for _, path := range allFiles {
		if filepath.Base(path) == nodeManifestName {
			owners[filepath.ToSlash(filepath.Dir(path))] = true
		}
	}
	return &nodePackages{repo: repo, owners: owners, read: map[string]nodePackage{}}
}

func (packages *nodePackages) owning(path string) (nodePackage, bool) {
	path = packages.repo.JavaScriptContextPath(path)
	directory := filepath.ToSlash(filepath.Dir(path))
	for {
		if packages.owners[directory] {
			return packages.declared(directory)
		}
		if directory == "." {
			return nodePackage{}, false
		}
		directory = filepath.ToSlash(filepath.Dir(directory))
	}
}

func (packages *nodePackages) declared(directory string) (nodePackage, bool) {
	if cached, found := packages.read[directory]; found {
		return cached, cached.readable
	}
	declared, err := readNodePackage(packages.repo, directory)
	packages.read[directory] = declared
	if err != nil {
		packages.coverage = append(packages.coverage, policy.Finding{
			Check: "architecture.importCoverage", Path: nodeManifestPath(directory), Subject: nodeManifestName,
			Message: "the owning package manifest could not be read: " + err.Error(),
		})
	}
	return declared, declared.readable
}

func readNodePackage(repo repository.Repository, directory string) (nodePackage, error) {
	data, err := repo.Read(nodeManifestPath(directory))
	if err != nil {
		return nodePackage{root: directory}, err
	}
	var payload struct {
		Name        string            `json:"name"`
		Runtime     map[string]string `json:"dependencies"`
		Development map[string]string `json:"devDependencies"`
		Optional    map[string]string `json:"optionalDependencies"`
		Peers       map[string]string `json:"peerDependencies"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nodePackage{root: directory}, err
	}
	return nodePackage{
		root: directory, name: payload.Name, readable: true,
		runtime:     declaredPackages(payload.Runtime, payload.Optional, payload.Peers),
		development: declaredPackages(payload.Development),
	}, nil
}

func declaredPackages(declarations ...map[string]string) map[string]bool {
	names := map[string]bool{}
	for _, declared := range declarations {
		for name := range declared {
			names[name] = true
		}
	}
	return names
}

func nodeManifestPath(directory string) string {
	if directory == "." {
		return nodeManifestName
	}
	return directory + "/" + nodeManifestName
}
