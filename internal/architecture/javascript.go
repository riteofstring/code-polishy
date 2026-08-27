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

// How long the sealed bundle has to read the imports of one selection. It
// parses each selected file once and resolves the specifiers it wrote, so this
// bounds a whole-repository run.
const javascriptImportBudget = 20 * time.Minute

// The source the sealed bundle reads on its own. It is the JavaScript and
// TypeScript the bundle parses without a compiler: a single-file component
// needs one, and a compiler is target code no policy operation executes.
var javascriptSourceExtensions = map[string]bool{
	".cjs": true, ".cts": true, ".js": true, ".jsx": true,
	".mjs": true, ".mts": true, ".ts": true, ".tsx": true,
}

// javascriptFindings enforces declared direction over JavaScript and TypeScript
// source: which module a file may reach, and which dependency the package that
// owns it may reach.
//
// Go owns both decisions exactly as it does for Go source: which module owns
// each file, whether the owning module declared a dependency on the module it
// reaches, and whether the owning package declared the external package it
// reaches. The sealed bundle owns only the facts -- that one file imports
// another, and which package a specifier names -- because which file a written
// specifier names is TypeScript's answer rather than a path rewrite Go could
// approximate.
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

// javascriptFactFindings decides every reported import: the module boundary it
// crosses, and the dependency it reaches outside the repository.
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

// The selected source whose imports decide module direction, and a finding for
// every governed file of that language the bundle cannot read. A file no module
// owns, or one more than one module owns, is already reported as missing module
// coverage, so its imports are not read under an ambiguous owner.
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

// One import edge, decided the same way a Go import is. An import that names
// nothing inside the repository, or something the repository does not govern,
// crosses no declared module boundary: what a package outside the repository
// may be reached by is decided against the manifest that owns the file.
func javascriptImportFinding(repo repository.Repository, governed map[string]bool, fact javascript.ImportFact) (policy.Finding, bool) {
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

// javascriptPackageFinding decides one import of an external package. A package
// the owning manifest declares nowhere is reached only because a neighbour
// happens to provide it, and one it declares for development alone must not be
// reached by source that ships.
//
// An undeclared dependency is decided from what the target installed: an import
// resolving into an installed tree names a package that really exists, while a
// specifier naming nothing may be a project path alias this reader does not
// apply. A declared name is exact either way, because the name a manifest
// writes is the name source imports even when the declaration is an alias.
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

// installedPackage reports whether one resolved file belongs to an installed
// dependency tree rather than to the repository's own source.
func installedPackage(resolved string) bool {
	return resolved != "" && slices.Contains(strings.Split(resolved, "/"), "node_modules")
}

func importCoverageFinding(path, message string) policy.Finding {
	return policy.Finding{Check: "architecture.importCoverage", Path: path, Subject: "typescript", Message: message}
}

// The manifest a Node package declares its dependencies in. The nearest one
// above a file owns it, exactly as the ecosystem resolves one.
const nodeManifestName = "package.json"

// nodePackages answers which package owns a file and what that package
// declared. A manifest is read once and reported once, so a package with ten
// imports has one unreadable manifest rather than ten.
type nodePackages struct {
	repo     repository.Repository
	owners   map[string]bool
	read     map[string]nodePackage
	coverage []policy.Finding
}

// nodePackage is what one manifest declares. Runtime holds every dependency any
// file of the package may reach; development holds the ones only a file that
// never ships may.
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

// owning names the package one file belongs to: the nearest manifest above it.
// A file with none belongs to no package, so no manifest declares what it may
// import.
func (packages *nodePackages) owning(path string) (nodePackage, bool) {
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

// declared reads one manifest, once. A manifest that could not be read is
// missing coverage: declarations that were never read are not declarations that
// admitted the import.
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
