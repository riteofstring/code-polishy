package quality

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/strictjson"
)

var goReachabilityTargets = []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64"}

type goReachabilityPackage struct {
	Name  string `json:"Name"`
	Path  string `json:"Path"`
	Funcs []struct {
		Name     string `json:"Name"`
		Position struct {
			File string `json:"File"`
			Line int    `json:"Line"`
			Col  int    `json:"Col"`
		} `json:"Position"`
		Generated bool `json:"Generated"`
		Marker    bool `json:"Marker"`
	} `json:"Funcs"`
}

type goReachabilityContext struct {
	module   repository.GoModule
	targets  []goBuildTarget
	reported []repository.GoBuildSource
}

type goReachabilityPlan struct {
	root     string
	modules  []repository.GoModule
	reported map[string]bool
	contexts []goReachabilityContext
}

type goModuleProbe struct {
	targets []goBuildTarget
	failed  bool
}

type goUnreachableFunction struct {
	path string
	line int
	name string
}

func goReachabilityFindings(ctx context.Context, repo repository.Repository, selection repository.Selection, commandRunner runner.Runner, profile string) []policy.Finding {
	plan, findings := planGoReachability(repo, selection, profile)
	reports := map[string]map[goUnreachableFunction]bool{}
	candidates := map[goUnreachableFunction]bool{}
	for _, planned := range plan.contexts {
		for index, command := range goContextReachabilityCommands(repo, planned) {
			_, output, err := runner.RunCaptured(ctx, commandRunner, repo.Root, command, runner.CaptureStructured)
			if err != nil {
				return append(findings, commandFinding("quality.deadCode", command.Name, err))
			}
			unreachable, err := goUnreachableFunctions(repo, plan, planned.module, output.Stdout)
			if err != nil {
				return append(findings, commandFinding("quality.deadCode", command.Name, err))
			}
			if err := withoutLinkedFunctions(repo, plan, planned.module, planned.targets[index], unreachable); err != nil {
				return append(findings, policy.Finding{Check: "quality.deadCodeCoverage", Path: planned.module.Manifest, Subject: planned.targets[index].name, Message: err.Error()})
			}
			reports[goReportKey(planned.module, planned.targets[index])] = unreachable
			maps.Copy(candidates, unreachable)
		}
	}
	for _, function := range sortedUnreachableFunctions(candidates) {
		if targets, dead := goFunctionDeadOnCompilingTargets(plan, reports, function); dead {
			findings = append(findings, policy.Finding{
				Check: "quality.deadCode", Path: function.path, Line: function.line, Subject: function.name,
				Message: fmt.Sprintf("%s is unreachable from every main package on %s", function.name, strings.Join(targets, ", ")),
			})
		}
	}
	return findings
}

func goReachabilityCommands(repo repository.Repository, selection repository.Selection, profile string) []policy.Command {
	commands := []policy.Command{}
	plan, _ := planGoReachability(repo, selection, profile)
	for _, planned := range plan.contexts {
		commands = append(commands, goContextReachabilityCommands(repo, planned)...)
	}
	return commands
}

func goContextReachabilityCommands(repo repository.Repository, planned goReachabilityContext) []policy.Command {
	module := planned.module
	paths := []string{}
	for _, reported := range planned.reported {
		for _, path := range reported.Paths {
			paths = append(paths, regexp.QuoteMeta(path))
		}
	}
	slices.Sort(paths)
	filter := "^(" + strings.Join(slices.Compact(paths), "|") + ")(/|$)"
	commands := make([]policy.Command, 0, len(planned.targets))
	for _, target := range planned.targets {
		commands = append(commands, policy.Command{
			Name: "deadcode-" + target.goos + "-" + target.goarch + "-" + safeName(module.Root),
			Argv: append([]string{repo.PolicyTool("deadcode"), "-json", "-filter", filter}, target.packages...),
			Cwd:  module.Root, TimeoutSeconds: 900, EnvironmentOverrides: target.environment(),
		})
	}
	return commands
}

func goReportKey(module repository.GoModule, target goBuildTarget) string {
	return module.Root + "\x00" + target.name
}

func goFunctionDeadOnCompilingTargets(plan goReachabilityPlan, reports map[string]map[goUnreachableFunction]bool, function goUnreachableFunction) ([]string, bool) {
	targets := []string{}
	for _, planned := range plan.contexts {
		for _, target := range planned.targets {
			if !target.compiled[function.path] {
				continue
			}
			if !reports[goReportKey(planned.module, target)][function] {
				return nil, false
			}
			targets = append(targets, target.name)
		}
	}
	slices.Sort(targets)
	targets = slices.Compact(targets)
	return targets, len(targets) > 0
}

func sortedUnreachableFunctions(functions map[goUnreachableFunction]bool) []goUnreachableFunction {
	sorted := slices.Collect(maps.Keys(functions))
	slices.SortFunc(sorted, func(left, right goUnreachableFunction) int {
		return cmp.Or(strings.Compare(left.path, right.path), cmp.Compare(left.line, right.line), strings.Compare(left.name, right.name))
	})
	return sorted
}

func goUnreachableFunctions(repo repository.Repository, plan goReachabilityPlan, module repository.GoModule, data []byte) (map[goUnreachableFunction]bool, error) {
	var packages []goReachabilityPackage
	if err := strictjson.Decode(data, &packages); err != nil {
		return nil, fmt.Errorf("decode deadcode report: %w", err)
	}
	unreachable := map[goUnreachableFunction]bool{}
	for _, reported := range packages {
		for _, function := range reported.Funcs {
			if function.Position.Line <= 0 || function.Name == "" {
				return nil, fmt.Errorf("deadcode reported an incomplete function in module %q", module.Root)
			}
			path, owned := goReportedFunctionPath(repo, plan, module, function.Position.File)
			if !owned || function.Generated || function.Marker || repo.IsGenerated(path) || !goNativeDeadCodePath(repo, path) {
				continue
			}
			unreachable[goUnreachableFunction{path: path, line: function.Position.Line, name: function.Name}] = true
		}
	}
	return unreachable, nil
}

func goReportedFunctionPath(repo repository.Repository, plan goReachabilityPlan, module repository.GoModule, reported string) (string, bool) {
	relative := filepath.Join(filepath.FromSlash(module.Root), reported)
	if filepath.IsAbs(reported) {
		var err error
		if relative, err = filepath.Rel(plan.root, reported); err != nil {
			return "", false
		}
	}
	if !filepath.IsLocal(relative) {
		return "", false
	}
	path := filepath.ToSlash(relative)
	owner, found := repo.OwningGoModule(path, plan.modules)
	return path, found && plan.reported[owner.Root]
}

func planGoReachability(repo repository.Repository, selection repository.Selection, profile string) (goReachabilityPlan, []policy.Finding) {
	if !selection.All && profile != "gate" {
		return goReachabilityPlan{}, nil
	}
	allFiles, err := repo.AllFiles()
	if err != nil {
		return goReachabilityPlan{}, nil
	}
	callerFiles, err := repo.RawFiles()
	if err != nil {
		return goReachabilityPlan{}, nil
	}
	root, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return goReachabilityPlan{}, []policy.Finding{{Check: "quality.deadCodeCoverage", Subject: "repository", Message: err.Error()}}
	}
	plan := goReachabilityPlan{root: root, modules: repo.GoModules(callerFiles), reported: map[string]bool{}}
	builds := map[string]repository.GoBuild{}
	for _, module := range plan.modules {
		builds[module.Root] = repo.GoBuild(callerFiles, module, plan.modules)
	}
	selected := goSelectedModules(repo, selection, allFiles, plan.modules)
	probes := map[string]goModuleProbe{}
	findings := []policy.Finding{}
	for _, module := range sortedGoModules(selected) {
		findings = append(findings, probeGoModule(repo, module, builds[module.Root], probes)...)
		plan.reported[module.Root] = !probes[module.Root].failed && len(probes[module.Root].targets) > 0
	}
	for _, module := range sortedGoModules(goModulesByRoot(plan.modules)) {
		if slices.ContainsFunc(builds[module.Root].Sources, func(source repository.GoBuildSource) bool { return plan.reported[source.Root] }) {
			findings = append(findings, probeGoModule(repo, module, builds[module.Root], probes)...)
		}
	}
	return withGoReachabilityContexts(plan, probes, builds), findings
}

func goSelectedModules(repo repository.Repository, selection repository.Selection, allFiles []string, modules []repository.GoModule) map[string]repository.GoModule {
	paths, _ := nativeGoPackageFiles(repo, languageFiles(repo, selection.Files, "go"), allFiles, "dead-code")
	selected := map[string]repository.GoModule{}
	for _, path := range paths {
		if module, found := repo.OwningGoModule(path, modules); found && module.Name != "" {
			selected[module.Root] = module
		}
	}
	return selected
}

func probeGoModule(repo repository.Repository, module repository.GoModule, build repository.GoBuild, probes map[string]goModuleProbe) []policy.Finding {
	if _, probed := probes[module.Root]; probed {
		return nil
	}
	plans, err := goModuleBuildTargets(repo, module, build)
	if err != nil {
		probes[module.Root] = goModuleProbe{failed: true}
		return []policy.Finding{{Check: "quality.deadCodeCoverage", Path: module.Manifest, Subject: "build-targets", Message: err.Error()}}
	}
	coverage, targets := goModuleTargetCoverage(module, plans)
	probes[module.Root] = goModuleProbe{targets: targets, failed: len(coverage) > 0}
	return coverage
}

func withGoReachabilityContexts(plan goReachabilityPlan, probes map[string]goModuleProbe, builds map[string]repository.GoBuild) goReachabilityPlan {
	for root, probe := range probes {
		if probe.failed {
			for _, source := range builds[root].Sources {
				plan.reported[source.Root] = false
			}
		}
	}
	for _, module := range sortedGoModules(goModulesByRoot(plan.modules)) {
		probe, probed := probes[module.Root]
		if !probed || probe.failed || len(probe.targets) == 0 {
			continue
		}
		reported := []repository.GoBuildSource{}
		for _, source := range builds[module.Root].Sources {
			if plan.reported[source.Root] {
				reported = append(reported, source)
			}
		}
		if len(reported) > 0 {
			plan.contexts = append(plan.contexts, goReachabilityContext{module: module, targets: probe.targets, reported: reported})
		}
	}
	return plan
}

func goModulesByRoot(modules []repository.GoModule) map[string]repository.GoModule {
	byRoot := map[string]repository.GoModule{}
	for _, module := range modules {
		byRoot[module.Root] = module
	}
	return byRoot
}

func goModuleTargetCoverage(module repository.GoModule, plans []goTargetPlan) ([]policy.Finding, []goBuildTarget) {
	findings := []policy.Finding{}
	targets := []goBuildTarget{}
	for _, plan := range plans {
		if plan.problem != "" {
			findings = append(findings, policy.Finding{
				Check: "quality.deadCodeCoverage", Path: module.Manifest, Subject: plan.target.name,
				Message: "whole-program dead-code analysis cannot load this target's programs: " + plan.problem,
			})
		}
		if plan.analyzed {
			targets = append(targets, plan.target)
		}
	}
	return findings, targets
}

func sortedGoModules(modules map[string]repository.GoModule) []repository.GoModule {
	sorted := slices.Collect(maps.Values(modules))
	slices.SortFunc(sorted, func(left, right repository.GoModule) int { return strings.Compare(left.Root, right.Root) })
	return sorted
}
