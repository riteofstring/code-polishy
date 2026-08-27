package policymodule

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type Resolution struct {
	Active     []policy.ActivePolicyModule
	Commands   []policy.Command
	Findings   []policy.Finding
	LintScopes []policy.JavaScriptLintScope
}

func Resolve(repo repository.Repository, files []string) Resolution {
	packages, packageFindings := discoverNodePackages(repo, files)
	activations := discover(repo, files, packages)
	activations = appendEnabledOverrides(activations, repo.Config.PolicyModules.Overrides)
	activations = applyDisabledOverrides(activations, repo.Config.PolicyModules.Overrides)
	activations = uniqueActivations(activations)

	resolution := Resolution{Active: activations, Findings: packageFindings}
	for _, activation := range activations {
		switch activation.Name {
		case "ruff":
			applyRuff(repo, files, activation, &resolution)
		case "react":
			applyReact(packages, activation, &resolution)
		case "electron":
			applyElectron(packages, activation, &resolution)
		case "osv":
			applyOSV(repo, files, activation, &resolution)
		}
	}
	resolution.Commands = uniqueCommands(resolution.Commands)
	resolution.Findings = uniqueFindings(resolution.Findings)
	return resolution
}

func Apply(config *policy.Config, resolution Resolution) {
	config.ActivePolicyModules = append([]policy.ActivePolicyModule{}, resolution.Active...)
	config.JavaScriptLintScopes = append([]policy.JavaScriptLintScope{}, resolution.LintScopes...)
	config.Checks = append(config.Checks, resolution.Commands...)
}

func Notes(active []policy.ActivePolicyModule) []string {
	notes := make([]string, 0, len(active))
	for _, item := range active {
		notes = append(notes, fmt.Sprintf("conditional policy module: %s at %s (%s)", item.Name, item.Root, item.Evidence))
	}
	return notes
}

func discover(repo repository.Repository, files []string, packages []nodePackage) []policy.ActivePolicyModule {
	active := []policy.ActivePolicyModule{}
	for _, root := range pythonRoots(repo, files) {
		active = append(active, activation("ruff", root, "Python source"))
	}
	for _, pkg := range packages {
		if pkg.hasDependency("react") {
			active = append(active, activation("react", pkg.Root, pkg.Path+" declares react"))
		}
		if pkg.hasDependency("electron") {
			active = append(active, activation("electron", pkg.Root, pkg.Path+" declares electron"))
		}
	}
	if hasDependencyGraph(repo, files) {
		active = append(active, activation("osv", ".", "supported dependency manifest or lockfile"))
	}
	return active
}

func activation(name, root, evidence string) policy.ActivePolicyModule {
	if root == "" {
		root = "."
	}
	return policy.ActivePolicyModule{Name: name, Root: root, Evidence: evidence}
}

func appendEnabledOverrides(active []policy.ActivePolicyModule, overrides []policy.PolicyModuleOverride) []policy.ActivePolicyModule {
	for _, override := range overrides {
		if override.Mode == "enabled" {
			active = append(active, activation(override.Name, override.Root, "explicitly enabled"))
		}
	}
	return active
}

func applyDisabledOverrides(active []policy.ActivePolicyModule, overrides []policy.PolicyModuleOverride) []policy.ActivePolicyModule {
	result := []policy.ActivePolicyModule{}
	for _, item := range active {
		disabled := false
		for _, override := range overrides {
			if override.Mode == "disabled" && override.Name == item.Name && override.Root == item.Root {
				disabled = true
				break
			}
		}
		if !disabled {
			result = append(result, item)
		}
	}
	return result
}

func uniqueActivations(active []policy.ActivePolicyModule) []policy.ActivePolicyModule {
	seen := map[string]bool{}
	result := []policy.ActivePolicyModule{}
	for _, item := range active {
		key := item.Name + "\x00" + item.Root
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Name+"\x00"+result[left].Root < result[right].Name+"\x00"+result[right].Root
	})
	return result
}

func uniqueCommands(commands []policy.Command) []policy.Command {
	seen := map[string]bool{}
	result := []policy.Command{}
	for _, command := range commands {
		if !seen[command.Name] {
			seen[command.Name] = true
			result = append(result, command)
		}
	}
	return result
}

func uniqueFindings(findings []policy.Finding) []policy.Finding {
	seen := map[string]bool{}
	result := []policy.Finding{}
	for _, item := range findings {
		key := item.Check + "\x00" + item.Path + "\x00" + item.Subject + "\x00" + item.Message
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	return result
}

func modulesFor(repo repository.Repository, files []string, root, language string) []string {
	modules := []string{}
	for _, path := range files {
		if repo.Language(path) == language && insideRoot(path, root) {
			modules = append(modules, repo.ModuleNames(path)...)
		}
	}
	sort.Strings(modules)
	return slices.Compact(modules)
}

func insideRoot(path, root string) bool {
	return root == "." || path == root || strings.HasPrefix(path, root+"/")
}

func rootedPatterns(root string, patterns ...string) []string {
	if root == "." {
		return append([]string{}, patterns...)
	}
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		result = append(result, filepath.ToSlash(filepath.Join(root, filepath.FromSlash(pattern))))
	}
	return result
}

func finding(name, root, subject, message string) policy.Finding {
	path := policy.ConfigFilename
	if root != "." {
		path = root
	}
	return policy.Finding{Check: "policy.conditionalModule", Path: path, Subject: name + ":" + subject, Message: message}
}

func safeName(value string) string {
	if value == "." || value == "" {
		return "root"
	}
	return "x" + hex.EncodeToString([]byte(value))
}
