package policymodule

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func applyRuff(repo repository.Repository, files []string, active policy.ActivePolicyModule, resolution *Resolution) {
	pythonFiles := filesFor(repo, files, active.Root, "python")
	if len(pythonFiles) == 0 {
		resolution.Findings = append(resolution.Findings, finding("ruff", active.Root, "python", "enabled Ruff policy did not find governed Python source at this root"))
		return
	}
	ruff := repo.PolicyTool("ruff")
	if pin := repo.ToolPin("ruff"); pin == "" || !executableVersion(ruff, "ruff "+pin) {
		resolution.Findings = append(resolution.Findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "ruff", Message: "conditional Python policy requires the Ruff version tools/ruff-version.txt pins; run ./tools/install-policy-tools.sh"})
	}
	paths := rootedPatterns(active.Root, "**/*.py", "**/*.pyi")
	modules := modulesFor(repo, files, active.Root, "python")
	suffix := safeName(active.Root)
	complexity := repo.Config.Quality.Complexity.Python
	if complexity == 0 {
		complexity = policy.MaxPythonComplexity
	}
	resolution.Commands = append(resolution.Commands,
		policy.Command{
			Name: "policy-ruff-format-" + suffix, Provides: []string{"format"},
			Argv: []string{ruff, "format", "--check", "--"}, Cwd: ".", Paths: paths, Modules: modules,
			RunOn: []string{"check", "gate"}, TimeoutSeconds: 300, Managed: true, PassFiles: true,
		},
		policy.Command{
			Name: "policy-ruff-lint-" + suffix, Provides: []string{"lint", "dead-code"},
			Argv: []string{ruff, "check", "--no-fix", "--"}, Cwd: ".", Paths: paths, Modules: modules,
			RunOn: []string{"check", "gate"}, TimeoutSeconds: 300, Managed: true, PassFiles: true,
		},
		policy.Command{
			Name: "policy-ruff-complexity-" + suffix, Provides: []string{"complexity"},
			Argv: []string{
				ruff, "check", "--no-fix", "--isolated", "--select", "C901", "--ignore-noqa",
				"--config", "lint.mccabe.max-complexity = " + strconv.Itoa(complexity-1), "--",
			},
			Cwd: ".", Paths: paths, Modules: modules,
			RunOn: []string{"check", "gate"}, TimeoutSeconds: 300, Managed: true, PassFiles: true,
		},
		policy.Command{
			Name: "policy-ruff-write-" + suffix, Provides: []string{"format"},
			Argv: []string{ruff, "format", "--"}, Cwd: ".", Paths: paths, Modules: modules,
			RunOn: []string{"format"}, TimeoutSeconds: 300, Managed: true, PassFiles: true,
		},
	)
}

func applyTy(repo repository.Repository, files []string, active policy.ActivePolicyModule, resolution *Resolution) {
	pythonFiles := filesFor(repo, files, active.Root, "python")
	if len(pythonFiles) == 0 {
		resolution.Findings = append(resolution.Findings, finding("ty", active.Root, "python", "enabled ty policy did not find governed Python source at this root"))
		return
	}
	ty := repo.PolicyTool("ty")
	if pin := repo.ToolPin("ty"); pin == "" || !tyExecutableVersion(ty, pin) {
		resolution.Findings = append(resolution.Findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "ty", Message: "conditional Python policy requires the ty version tools/ty-version.txt pins; run ./tools/install-policy-tools.sh"})
	}
	paths := rootedPatterns(active.Root, "**/*.py", "**/*.pyi")
	modules := modulesFor(repo, files, active.Root, "python")
	suffix := safeName(active.Root)
	resolution.Commands = append(resolution.Commands, policy.Command{
		Name: "policy-ty-typecheck-" + suffix, Provides: []string{"typecheck"},
		Argv: []string{ty, "check", "--config-file", filepath.Join(repo.PolicyRoot, "tools", "ty.toml"), "--project", ".", "--"},
		Cwd:  active.Root, Paths: paths, Modules: modules,
		RunOn: []string{"check", "gate"}, TimeoutSeconds: 300, Managed: true, PassFiles: true,
	})
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

func pythonRoots(repo repository.Repository, files []string) []string {
	fileSet := map[string]bool{}
	for _, path := range files {
		fileSet[path] = true
	}
	roots := map[string]bool{}
	for _, path := range files {
		if repo.Language(path) != "python" {
			continue
		}
		roots[nearestPythonRoot(repo, path, fileSet)] = true
	}
	result := make([]string, 0, len(roots))
	for root := range roots {
		result = append(result, root)
	}
	sort.Strings(result)
	return result
}

func tyRoots(repo repository.Repository, files []string) []string {
	fileSet := map[string]bool{}
	for _, path := range files {
		fileSet[path] = true
	}
	roots := map[string]bool{}
	for _, path := range files {
		if repo.Language(path) != "python" {
			continue
		}
		roots[nearestTyRoot(path, fileSet)] = true
	}
	result := make([]string, 0, len(roots))
	for root := range roots {
		result = append(result, root)
	}
	sort.Strings(result)
	return result
}

func nearestPythonRoot(repo repository.Repository, path string, files map[string]bool) string {
	directory := filepath.ToSlash(filepath.Dir(path))
	for {
		for _, name := range []string{"ruff.toml", ".ruff.toml"} {
			candidate := rootFile(directory, name)
			if files[candidate] {
				return directory
			}
		}
		pyproject := rootFile(directory, "pyproject.toml")
		if files[pyproject] {
			data, err := repo.Read(pyproject)
			if err == nil && strings.Contains(string(data), "[tool.ruff") {
				return directory
			}
		}
		if directory == "." {
			return "."
		}
		directory = filepath.ToSlash(filepath.Dir(directory))
	}
}

func nearestTyRoot(path string, files map[string]bool) string {
	directory := filepath.ToSlash(filepath.Dir(path))
	for {
		for _, name := range []string{"ty.toml", "pyproject.toml"} {
			if files[rootFile(directory, name)] {
				return directory
			}
		}
		if directory == "." {
			return "."
		}
		directory = filepath.ToSlash(filepath.Dir(directory))
	}
}

func rootFile(root, name string) string {
	if root == "." {
		return name
	}
	return root + "/" + name
}

func filesFor(repo repository.Repository, files []string, root, language string) []string {
	result := []string{}
	for _, path := range files {
		if insideRoot(path, root) && repo.Language(path) == language && !repo.IsGenerated(path) {
			result = append(result, path)
		}
	}
	return result
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
