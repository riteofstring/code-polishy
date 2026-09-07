package architecture

import (
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

func javascriptImportFinding(repo repository.Repository, governed map[string]bool, fact javascript.ImportFact) (policy.Finding, bool) {
	return importModuleFinding(repo, governed, fact.Path, fact.Resolved, fact.Line)
}

func importModuleFinding(repo repository.Repository, governed map[string]bool, source, resolved string, line int) (policy.Finding, bool) {
	if repo.IsTest(source) {
		return policy.Finding{}, false
	}
	if resolved == "" || !governed[resolved] {
		return policy.Finding{}, false
	}
	owners := repo.OwnerModuleNames(source)
	targets := repo.OwnerModuleNames(resolved)
	if len(owners) != 1 || len(targets) != 1 || owners[0] == targets[0] {
		return policy.Finding{}, false
	}
	sourceModule := repo.Config.Modules[repo.Config.ModuleByName[owners[0]]]
	if slices.Contains(sourceModule.DependsOn, targets[0]) {
		return policy.Finding{}, false
	}
	return policy.Finding{
		Check:   "architecture.moduleDependency",
		Path:    source,
		Subject: targets[0],
		Message: fmt.Sprintf("line %d module %q imports module %q without declaring dependsOn", line, owners[0], targets[0]),
	}, true
}

func javascriptPackageFinding(repo repository.Repository, packages *nodePackages,
	fact javascript.ImportFact) (policy.Finding, bool) {
	return nodePackageFinding(repo, packages, fact.Path, fact.Resolved, fact.Package, fact.Line)
}

func nodePackageFinding(repo repository.Repository, packages *nodePackages, source, resolved, name string, line int) (policy.Finding, bool) {
	if name == "" {
		return policy.Finding{}, false
	}
	owner, owned := packages.owning(source)
	if !owned || name == owner.name || owner.runtime[name] {
		return policy.Finding{}, false
	}
	message := ""
	switch {
	case owner.development[name]:
		if repo.IsDevelopment(source) {
			return policy.Finding{}, false
		}
		message = fmt.Sprintf("line %d package %q imports development dependency %q from source that ships",
			line, owner.root, name)
	case installedPackage(resolved):
		message = fmt.Sprintf("line %d package %q imports %q without declaring it as a dependency",
			line, owner.root, name)
	default:
		return policy.Finding{}, false
	}
	return policy.Finding{
		Check: "architecture.packageDependency", Path: source, Subject: name, Message: message,
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
