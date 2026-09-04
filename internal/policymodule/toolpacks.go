package policymodule

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func applyRuff(repo repository.Repository, active policy.ActivePolicyModule, inventory repository.PythonProjectInventory, resolution *Resolution) {
	project, found := pythonProjectAtRoot(inventory, active.Root)
	if !found || len(project.Files) == 0 {
		resolution.Findings = append(resolution.Findings, finding("ruff", active.Root, "python", "enabled Ruff policy did not find governed Python source at this root"))
		return
	}
	configurationFindings := pythonRuffConfigurationFindings(inventory, project)
	if len(configurationFindings) > 0 {
		resolution.Findings = append(resolution.Findings, configurationFindings...)
		return
	}
	ruffOptions, err := project.Ruff.CommandOptions(project.Root, project.SourceRoots)
	if err != nil {
		resolution.Findings = append(resolution.Findings, policy.Finding{
			Check: "policy.pythonRuffConfiguration", Path: project.Manifest, Subject: project.Manifest, Message: err.Error(),
		})
		return
	}
	ruff := repo.PolicyTool("ruff")
	if pin := repo.ToolPin("ruff"); pin == "" || !executableVersion(ruff, "ruff "+pin) {
		resolution.Findings = append(resolution.Findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "ruff", Message: "conditional Python policy requires the Ruff version tools/ruff-version.txt pins; run ./tools/install-policy-tools.sh"})
	}
	modules := modulesForPythonFiles(repo, project.Files)
	inputs := append(slices.Clone(project.Files), project.Manifest)
	suffix := safeName(active.Root)
	checkArguments := append([]string{ruff, "format"}, ruffOptions...)
	checkArguments = append(checkArguments, "--check", "--")
	writeArguments := append([]string{ruff, "format"}, ruffOptions...)
	writeArguments = append(writeArguments, "--")
	resolution.Commands = append(resolution.Commands,
		policy.Command{
			Name: "policy-ruff-format-" + suffix, Provides: []string{"format"},
			Argv: checkArguments, Cwd: active.Root, Paths: inputs, Modules: modules,
			RunOn: []string{"check", "gate"}, TimeoutSeconds: 300, Managed: true, PassFiles: true, PassFilePaths: project.Files,
		},
		policy.Command{
			Name: "policy-ruff-write-" + suffix, Provides: []string{"format"},
			Argv: writeArguments, Cwd: active.Root, Paths: inputs, Modules: modules,
			RunOn: []string{"format"}, TimeoutSeconds: 300, Managed: true, PassFiles: true, PassFilePaths: project.Files,
		},
	)
}

func pythonRuffConfigurationFindings(inventory repository.PythonProjectInventory, project repository.PythonProject) []policy.Finding {
	findings := []policy.Finding{}
	for _, problem := range inventory.Problems {
		if problem.Kind != repository.PythonRuffConfigurationProblem || problem.Subject != project.Manifest {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "policy.pythonRuffConfiguration", Path: problem.Path, Line: problem.Line,
			Subject: project.Manifest, Message: problem.Message,
		})
	}
	return findings
}

func applyTy(repo repository.Repository, active policy.ActivePolicyModule, inventory repository.PythonProjectInventory, resolution *Resolution) {
	pythonFiles := pythonProjectFiles(inventory, active.Root)
	if len(pythonFiles) == 0 {
		resolution.Findings = append(resolution.Findings, finding("ty", active.Root, "python", "enabled ty policy did not find governed Python source at this root"))
		return
	}
	ty := repo.PolicyTool("ty")
	if pin := repo.ToolPin("ty"); pin == "" || !tyExecutableVersion(ty, pin) {
		resolution.Findings = append(resolution.Findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "ty", Message: "conditional Python policy requires the ty version tools/ty-version.txt pins; run ./tools/install-policy-tools.sh"})
	}
}

func applyVulture(repo repository.Repository, active policy.ActivePolicyModule, inventory repository.PythonProjectInventory, resolution *Resolution) {
	pythonFiles := pythonProjectFiles(inventory, active.Root)
	if len(pythonFiles) == 0 {
		resolution.Findings = append(resolution.Findings, finding("vulture", active.Root, "python", "enabled Vulture policy did not find governed Python source at this root"))
		return
	}
	if !pythonRuntimeVersion(repo, repo.ToolPin("python")) {
		resolution.Findings = append(resolution.Findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "python", Message: "conditional Python dead-code policy requires the CPython version tools/python-version.txt pins; run ./tools/install-policy-tools.sh"})
	}
	if !vultureRuntimeVersion(repo, repo.ToolPin("vulture")) {
		resolution.Findings = append(resolution.Findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "vulture", Message: "conditional Python dead-code policy requires the Vulture version tools/vulture-version.txt pins; run ./tools/install-policy-tools.sh"})
	}
}

func applyOSV(repo repository.Repository, files []string, active policy.ActivePolicyModule, resolution *Resolution) {
	if !hasDependencyGraphAt(repo, files, active.Root) {
		resolution.Findings = append(resolution.Findings, finding("osv", active.Root, "dependencies", "enabled OSV policy did not find a supported dependency graph at this root"))
		return
	}
	osv := repo.PolicyTool("osv-scanner")
	if pin := repo.ToolPin("osv-scanner"); pin == "" || !executableVersion(osv, "osv-scanner version: "+pin) {
		resolution.Findings = append(resolution.Findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "osv-scanner", Message: "conditional dependency policy requires the OSV-Scanner version tools/osv-scanner-version.txt pins; run ./tools/install-policy-tools.sh"})
	}
}

func pythonRoots(inventory repository.PythonProjectInventory) []string {
	roots := map[string]bool{}
	for _, project := range inventory.Projects {
		if len(project.Files) > 0 {
			roots[project.Root] = true
		}
	}
	result := make([]string, 0, len(roots))
	for root := range roots {
		result = append(result, root)
	}
	sort.Strings(result)
	return result
}

func pythonProjectFiles(inventory repository.PythonProjectInventory, root string) []string {
	project, found := pythonProjectAtRoot(inventory, root)
	if !found {
		return nil
	}
	return append([]string{}, project.Files...)
}

func pythonProjectAtRoot(inventory repository.PythonProjectInventory, root string) (repository.PythonProject, bool) {
	for _, project := range inventory.Projects {
		if project.Root == root {
			return project, true
		}
	}
	return repository.PythonProject{}, false
}

func modulesForPythonFiles(repo repository.Repository, files []string) []string {
	modules := []string{}
	for _, path := range files {
		modules = append(modules, repo.ModuleNames(path)...)
	}
	sort.Strings(modules)
	return slices.Compact(modules)
}

func executableVersion(path string, acceptedLines ...string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return false
	}
	output, err := runner.ToolOutput(path, "--version")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if slices.Contains(acceptedLines, strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func tyExecutableVersion(path, pin string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return false
	}
	output, err := runner.ToolOutput(path, "--version")
	if err != nil {
		return false
	}
	fields := strings.Fields(string(output))
	return len(fields) >= 2 && fields[0] == "ty" && fields[1] == pin
}

func pythonRuntimeVersion(repo repository.Repository, pin string) bool {
	version, build, found := strings.Cut(pin, "+")
	return found && version != "" && pythonBuildTag(build) &&
		pythonRuntimeOutput(repo, "import sys; print(\".\".join(str(value) for value in sys.version_info[:3]))") == version
}

func pythonBuildTag(value string) bool {
	if len(value) != 8 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func vultureRuntimeVersion(repo repository.Repository, pin string) bool {
	return pin != "" && pythonRuntimeOutput(repo, "import importlib.metadata; print(importlib.metadata.version(\"vulture\"))") == pin
}

func pythonRuntimeOutput(repo repository.Repository, source string) string {
	command := repo.PythonCommand(source)
	path := command[0]
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return ""
	}
	_, output, err := (runner.OSRunner{}).RunStructured(context.Background(), repo.Root, policy.Command{
		Name: "policy-python-runtime-probe", Argv: command, Cwd: ".", TimeoutSeconds: 30,
		Managed: true, SealedEnvironment: true,
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output.Stdout))
}

func hasDependencyGraph(repo repository.Repository, files []string) bool {
	return hasDependencyGraphAt(repo, files, ".")
}

func hasDependencyGraphAt(repo repository.Repository, files []string, root string) bool {
	for _, path := range files {
		if insideRoot(path, root) && dependencyInput(repo, path) {
			return true
		}
	}
	return false
}

func dependencyInput(repo repository.Repository, path string) bool {
	name := filepath.Base(path)
	if name == "pyproject.toml" {
		data, err := repo.Read(path)
		return err == nil && (strings.Contains(string(data), "[project]") || strings.Contains(string(data), "[dependency-groups]"))
	}
	if strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
		return true
	}
	if strings.HasSuffix(strings.ToLower(name), ".lock") || strings.Contains(strings.ToLower(name), "-lock.") {
		return true
	}
	switch name {
	case "go.mod", "go.sum", "go.work", "package.json", "pnpm-lock.yaml", "bun.lockb", "uv.lock",
		"Cargo.toml", "Pipfile", "pom.xml", "build.gradle", "build.gradle.kts", "Gemfile", "composer.json", "Package.swift", "Package.resolved", "pubspec.yaml":
		return true
	default:
		return false
	}
}
