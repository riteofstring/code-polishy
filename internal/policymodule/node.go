package policymodule

import (
	"encoding/json"
	"path/filepath"
	"sort"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type nodePackage struct {
	Path            string
	Root            string
	Dependencies    map[string]string
	DevDependencies map[string]string
	Optional        map[string]string
	Peers           map[string]string
}

func (pkg nodePackage) hasDependency(name string) bool {
	for _, declared := range []map[string]string{pkg.Dependencies, pkg.DevDependencies, pkg.Optional, pkg.Peers} {
		if _, exists := declared[name]; exists {
			return true
		}
	}
	return false
}

func discoverNodePackages(repo repository.Repository, files []string) ([]nodePackage, []policy.Finding) {
	packages := []nodePackage{}
	findings := []policy.Finding{}
	for _, path := range files {
		if filepath.Base(path) != "package.json" {
			continue
		}
		data, err := repo.Read(path)
		if err != nil {
			findings = append(findings, finding("node", filepath.ToSlash(filepath.Dir(path)), path, err.Error()))
			continue
		}
		var payload struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
			Optional        map[string]string `json:"optionalDependencies"`
			Peers           map[string]string `json:"peerDependencies"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}
		packages = append(packages, nodePackage{
			Path: path, Root: filepath.ToSlash(filepath.Dir(path)),
			Dependencies: payload.Dependencies, DevDependencies: payload.DevDependencies,
			Optional: payload.Optional, Peers: payload.Peers,
		})
	}
	sort.Slice(packages, func(left, right int) bool {
		return packages[left].Root < packages[right].Root
	})
	return packages, findings
}

func packageAt(packages []nodePackage, root string) (nodePackage, bool) {
	for _, pkg := range packages {
		if pkg.Root == root {
			return pkg, true
		}
	}
	return nodePackage{}, false
}

func applyReact(packages []nodePackage, active policy.ActivePolicyModule, resolution *Resolution) {
	pkg, exists := packageAt(packages, active.Root)
	if !exists || !pkg.hasDependency("react") {
		resolution.Findings = append(resolution.Findings, finding("react", active.Root, "react", "enabled React policy requires a package.json that declares react"))
		return
	}
	resolution.LintScopes = append(resolution.LintScopes, policy.JavaScriptLintScope{
		Root: active.Root, ReactHooks: true, JSXAccessibility: pkg.hasDependency("react-dom"),
	})
}

func applyElectron(packages []nodePackage, active policy.ActivePolicyModule, resolution *Resolution) {
	pkg, exists := packageAt(packages, active.Root)
	if !exists || !pkg.hasDependency("electron") {
		resolution.Findings = append(resolution.Findings, finding("electron", active.Root, "electron", "enabled Electron policy requires a package.json that declares electron"))
	}
}
