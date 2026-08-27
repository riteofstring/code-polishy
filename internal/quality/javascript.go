package quality

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

// How long the sealed bundle has to report what it is. It reads only its own
// installed JSON, so an exchange that takes longer than this is a broken
// installation rather than slow work.
const javascriptProvenanceBudget = 2 * time.Minute

// How long the sealed bundle has to format one selection. It parses and prints
// each selected file once, so this bounds a whole-repository run.
const javascriptFormatBudget = 12 * time.Minute

// How long the sealed bundle has to lint every group of one selection. Groups
// share it, so a selection cannot buy more time by splitting into more of them.
const javascriptLintBudget = 20 * time.Minute

// How long the sealed bundle has to type check every project a selection
// reaches. Projects share it for the same reason lint groups do.
const javascriptTypeCheckBudget = 30 * time.Minute

// How long the sealed bundle has to analyze every package tree for dead code.
// Trees share it, so a repository cannot buy more time by having more of them.
const javascriptDeadCodeBudget = 30 * time.Minute

// The file types the sealed central formatting configuration governs. Code
// Policy owns this list: a target neither extends nor narrows it.
var javascriptFormatExtensions = map[string]bool{
	".cjs": true, ".css": true, ".cts": true, ".html": true, ".js": true,
	".json": true, ".jsonc": true, ".jsx": true, ".md": true, ".mjs": true,
	".mts": true, ".ts": true, ".tsx": true, ".yaml": true, ".yml": true,
}

// The source files the sealed linter and dead-code analyzer decide. It is the
// JavaScript and TypeScript the bundle parses on its own, not everything the
// formatter prints: a single-file component needs a compiler, and a compiler is
// target code.
var javascriptSourceExtensions = map[string]bool{
	".cjs": true, ".cts": true, ".js": true, ".jsx": true,
	".mjs": true, ".mts": true, ".ts": true, ".tsx": true,
}

// The source the sealed type checker must cover. It is TypeScript alone: a
// JavaScript file is checked only when the target's own project asks for it,
// so requiring coverage for one would fail every ordinary project.
var javascriptTypeCheckExtensions = map[string]bool{
	".cts": true, ".mts": true, ".ts": true, ".tsx": true,
}

// The budget rules, whose violations are complexity findings rather than
// ordinary lint findings.
var javascriptComplexityRules = map[string]bool{
	"complexity": true, "max-depth": true, "max-params": true,
}

// JavaScriptBundleStatus launches the sealed, policy-owned JavaScript tool
// bundle and reports exactly which bytes and analyzer versions answered.
//
// It launches only when the target actually bears JavaScript or TypeScript, so
// a target without either never requires the bundle. When the target does bear
// it, a missing, corrupted, or unanswering bundle is a finding rather than a
// fallback: no ambient runtime, user cache, or target-installed tool may stand
// in for it.
func JavaScriptBundleStatus(repo repository.Repository, files []string) ([]policy.Finding, []string) {
	if !bearsJavaScript(repo, files) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), javascriptProvenanceBudget)
	defer cancel()
	reported, err := javascript.Bundle{PolicyRoot: repo.PolicyRoot}.Provenance(ctx)
	if err != nil {
		return []policy.Finding{toolFinding("javascript-bundle", err.Error())}, nil
	}
	note := fmt.Sprintf("sealed JavaScript bundle %s: node %s, pnpm %s, %s",
		reported.BundleDigest, reported.Node, reported.Pnpm, analyzerVersions(reported.Tools))
	return nil, []string{note}
}

// JavaScriptFormatFindings reports every selected file the sealed bundle's
// central formatting configuration would rewrite, and every selected file it
// could not decide at all. It changes nothing.
func JavaScriptFormatFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	return javascriptFormat(ctx, repo, files, false)
}

// JavaScriptFormatWrite rewrites those files. Writing is a separate operation
// the bundle only performs when this Go caller asks for it.
func JavaScriptFormatWrite(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	return javascriptFormat(ctx, repo, files, true)
}

func javascriptFormat(ctx context.Context, repo repository.Repository, files []string, write bool) []policy.Finding {
	governed, err := javascriptTarget(repo)
	if err != nil {
		return []policy.Finding{toolFinding("javascript-bundle", err.Error())}
	}
	if !governed {
		return nil
	}
	findings := javascriptFormatConfigFindings(repo, files)
	selected := javascriptFormatFiles(repo, files)
	if len(selected) == 0 {
		return findings
	}
	ctx, cancel := context.WithTimeout(ctx, javascriptFormatBudget)
	defer cancel()
	bundle := javascript.Bundle{PolicyRoot: repo.PolicyRoot}
	decide := bundle.Format
	if write {
		decide = bundle.FormatWrite
	}
	result, err := decide(ctx, repo.Root, selected)
	if err != nil {
		return append(findings, toolFinding("javascript-bundle", err.Error()))
	}
	for _, entry := range result.Unsupported {
		findings = append(findings, policy.Finding{
			Check: "quality.formatCoverage", Path: entry.Path, Subject: "prettier",
			Message: "the policy-owned formatter could not decide this file: " + entry.Reason,
		})
	}
	if write {
		return findings
	}
	for _, path := range result.Changed {
		findings = append(findings, policy.Finding{
			Check: "quality.format", Path: path, Subject: "prettier",
			Message: "the policy-owned formatter would rewrite this file; run code-polishy format",
		})
	}
	return findings
}

// JavaScriptLintFindings reports what the sealed bundle's central lint
// configuration finds in the selected JavaScript and TypeScript source.
//
// Go decides everything the run depends on: which files are linted, the exact
// budgets each one is held to, and which framework rules are active. The bundle
// supplies ESLint, the parsers, and the plug-ins, so a target installs, pins,
// and configures none of them.
func JavaScriptLintFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	governed, err := javascriptTarget(repo)
	if err != nil {
		return []policy.Finding{toolFinding("javascript-bundle", err.Error())}
	}
	if !governed {
		return nil
	}
	findings := javascriptLintConfigFindings(files)
	groups := javascriptLintGroups(repo, files)
	if len(groups) == 0 {
		return findings
	}
	ctx, cancel := context.WithTimeout(ctx, javascriptLintBudget)
	defer cancel()
	bundle := javascript.Bundle{PolicyRoot: repo.PolicyRoot}
	for _, group := range groups {
		result, err := bundle.Lint(ctx, repo.Root, group.paths, group.limits, group.activation)
		if err != nil {
			return append(findings, toolFinding("javascript-bundle", err.Error()))
		}
		findings = append(findings, javascriptLintResultFindings(result)...)
	}
	return findings
}

// One lint exchange over files that share exact budgets and activation. A
// selection is split into as few of these as its own facts require, so a test
// file is never held to a production budget and a package without React never
// has React rules run over it.
type javascriptLintGroup struct {
	limits     javascript.LintLimits
	activation javascript.LintActivation
	paths      []string
}

func javascriptLintGroups(repo repository.Repository, files []string) []javascriptLintGroup {
	grouped := map[string]*javascriptLintGroup{}
	for _, path := range files {
		if !javascriptSourceExtensions[strings.ToLower(filepath.Ext(path))] || repo.IsGenerated(path) {
			continue
		}
		limits := javascriptLintLimits(repo, repo.IsTest(path))
		activation := javascriptLintActivation(repo, path)
		key := fmt.Sprintf("%+v|%+v", limits, activation)
		group, exists := grouped[key]
		if !exists {
			group = &javascriptLintGroup{limits: limits, activation: activation}
			grouped[key] = group
		}
		group.paths = append(group.paths, path)
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	groups := make([]javascriptLintGroup, 0, len(keys))
	for _, key := range keys {
		sort.Strings(grouped[key].paths)
		groups = append(groups, *grouped[key])
	}
	return groups
}

// The shared thresholds in ESLint's form. A budget is the complexity a file
// must not reach, so the rule's allowed maximum is one less; depth and
// parameter limits are already allowed maximums.
func javascriptLintLimits(repo repository.Repository, test bool) javascript.LintLimits {
	quality := repo.Config.Quality
	if test {
		return javascript.LintLimits{
			Complexity: javascriptLimit(quality.Complexity.TypeScriptTest, policy.MaxTypeScriptTestComplexity) - 1,
			Depth:      javascriptLimit(quality.MaxTestDepth, policy.MaxTypeScriptTestDepth),
			Parameters: javascriptLimit(quality.MaxTestParams, policy.MaxTypeScriptTestParams),
		}
	}
	return javascript.LintLimits{
		Complexity: javascriptLimit(quality.Complexity.TypeScript, policy.MaxTypeScriptComplexity) - 1,
		Depth:      javascriptLimit(quality.MaxDepth, policy.MaxTypeScriptDepth),
		Parameters: javascriptLimit(quality.MaxParams, policy.MaxTypeScriptParams),
	}
}

func javascriptLimit(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// Which framework rules a file is linted with, decided by the conditional
// policy modules already resolved for the repository. The nearest declaring
// root wins, so a React package in a monorepo does not activate React rules
// over an unrelated sibling.
func javascriptLintActivation(repo repository.Repository, path string) javascript.LintActivation {
	activation := javascript.LintActivation{}
	nearest := ""
	for _, scope := range repo.Config.JavaScriptLintScopes {
		if !javascriptScopeOwns(scope.Root, path) || (nearest != "" && len(scope.Root) <= len(nearest)) {
			continue
		}
		nearest = scope.Root
		activation = javascript.LintActivation{ReactHooks: scope.ReactHooks, JSXAccessibility: scope.JSXAccessibility}
	}
	return activation
}

func javascriptScopeOwns(root, path string) bool {
	return root == "." || strings.HasPrefix(path, root+"/")
}

func javascriptLintResultFindings(result javascript.LintResult) []policy.Finding {
	findings := []policy.Finding{}
	for _, entry := range result.Unsupported {
		findings = append(findings, policy.Finding{
			Check: "quality.lintCoverage", Path: entry.Path, Subject: "eslint",
			Message: "the policy-owned linter could not decide this file: " + entry.Reason,
		})
	}
	for _, directive := range result.Directives {
		findings = append(findings, policy.Finding{
			Check: "quality.lintDirective", Path: directive.Path, Subject: "eslint",
			Message: fmt.Sprintf("line %d declares an inline ESLint directive, which the sealed configuration never honors; remove it or declare a Code Polishy exception", directive.Line),
		})
	}
	for _, violation := range result.Findings {
		check := "quality.lint"
		if javascriptComplexityRules[violation.Rule] {
			check = "quality.complexity"
		}
		findings = append(findings, policy.Finding{
			Check: check, Path: violation.Path, Subject: violation.Rule,
			Message: fmt.Sprintf("line %d, column %d: %s", violation.Line, violation.Column, violation.Message),
		})
	}
	return findings
}

// JavaScriptTypeCheckFindings reports what the TypeScript compiler finds in the
// projects that govern the selected source.
//
// The sealed bundle supplies the compiler, so a target pins and installs none.
// The target still owns its projects, because the language level, libraries,
// and strictness of a codebase are facts about that codebase. Go decides which
// project governs which file and requires that a governed file is actually
// covered by one: a file no project reaches is missing coverage, not clean.
func JavaScriptTypeCheckFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	inventory, err := repo.AllFiles()
	if err != nil {
		return []policy.Finding{toolFinding("javascript-bundle", err.Error())}
	}
	if !bearsJavaScript(repo, inventory) {
		return nil
	}
	projects, ungoverned := javascriptTypeCheckProjects(repo, files, inventory)
	findings := []policy.Finding{}
	for _, path := range ungoverned {
		findings = append(findings, javascriptCoverageFinding(path, "no contained TypeScript project governs this file"))
	}
	if len(projects) == 0 {
		return findings
	}
	ctx, cancel := context.WithTimeout(ctx, javascriptTypeCheckBudget)
	defer cancel()
	bundle := javascript.Bundle{PolicyRoot: repo.PolicyRoot}
	for _, project := range projects {
		result, err := bundle.TypeCheck(ctx, repo.Root, project.project, project.paths)
		if err != nil {
			return append(findings, toolFinding("javascript-bundle", err.Error()))
		}
		findings = append(findings, javascriptTypeCheckResultFindings(project, result)...)
	}
	return findings
}

// One typecheck exchange: the project and the selected files it must cover. A
// file is checked by the nearest project above it, so a monorepo package is
// checked under its own settings rather than an unrelated sibling's.
type javascriptTypeCheckProject struct {
	project string
	paths   []string
}

func javascriptTypeCheckProjects(repo repository.Repository, files, inventory []string) ([]javascriptTypeCheckProject, []string) {
	configurations := javascriptProjectConfigurations(inventory)
	grouped := map[string][]string{}
	ungoverned := []string{}
	for _, path := range javascriptTypeCheckFiles(repo, files) {
		project, governed := javascriptNearestProject(configurations, path)
		if !governed {
			ungoverned = append(ungoverned, path)
			continue
		}
		grouped[project] = append(grouped[project], path)
	}
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)
	projects := make([]javascriptTypeCheckProject, 0, len(names))
	for _, name := range names {
		projects = append(projects, javascriptTypeCheckProject{project: name, paths: grouped[name]})
	}
	return projects, ungoverned
}

// The selected source the type checker must cover.
func javascriptTypeCheckFiles(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		if javascriptTypeCheckExtensions[strings.ToLower(filepath.Ext(path))] && !repo.IsGenerated(path) {
			selected = append(selected, path)
		}
	}
	sort.Strings(selected)
	return selected
}

// The one project each directory declares. A directory declaring tsconfig.json
// declares that; a directory declaring exactly one other tsconfig*.json
// declares that instead. Anything more ambiguous declares nothing, so the
// search continues upward rather than guessing which project a file belongs to.
func javascriptProjectConfigurations(inventory []string) map[string]string {
	declared := map[string][]string{}
	for _, path := range inventory {
		name := filepath.Base(path)
		if !strings.HasPrefix(name, "tsconfig") || !strings.HasSuffix(name, ".json") {
			continue
		}
		directory := filepath.ToSlash(filepath.Dir(path))
		declared[directory] = append(declared[directory], path)
	}
	configurations := map[string]string{}
	for directory, candidates := range declared {
		preferred := javascriptDirectoryFile(directory, "tsconfig.json")
		switch {
		case slices.Contains(candidates, preferred):
			configurations[directory] = preferred
		case len(candidates) == 1:
			configurations[directory] = candidates[0]
		}
	}
	return configurations
}

func javascriptDirectoryFile(directory, name string) string {
	if directory == "." {
		return name
	}
	return directory + "/" + name
}

// The nearest project above a file, which is the project whose settings that
// file is actually compiled under.
func javascriptNearestProject(configurations map[string]string, path string) (string, bool) {
	directory := filepath.ToSlash(filepath.Dir(path))
	for {
		if project, governs := configurations[directory]; governs {
			return project, true
		}
		if directory == "." {
			return "", false
		}
		directory = filepath.ToSlash(filepath.Dir(directory))
	}
}

func javascriptTypeCheckResultFindings(project javascriptTypeCheckProject, result javascript.TypeCheckResult) []policy.Finding {
	findings := []policy.Finding{}
	for _, entry := range result.Unsupported {
		findings = append(findings, javascriptCoverageFinding(entry.Path,
			"the policy-owned type checker could not analyze this project: "+entry.Reason))
	}
	for _, path := range project.paths {
		if !slices.Contains(result.Covered, path) {
			findings = append(findings, javascriptCoverageFinding(path,
				"the TypeScript project "+project.project+" does not check this file"))
		}
	}
	for _, diagnostic := range result.Diagnostics {
		findings = append(findings, policy.Finding{
			Check: "quality.typecheck", Path: diagnostic.Path, Subject: fmt.Sprintf("TS%d", diagnostic.Code),
			Message: fmt.Sprintf("line %d, column %d: %s", diagnostic.Line, diagnostic.Column, diagnostic.Message),
		})
	}
	return findings
}

func javascriptCoverageFinding(path, message string) policy.Finding {
	return policy.Finding{Check: "quality.typecheckCoverage", Path: path, Subject: "typescript", Message: message}
}

// JavaScriptDeadCodeFindings reports the governed source no entry point reaches
// and the exported symbols nothing uses.
//
// Reachability is a property of a whole package tree rather than of one file,
// so the analysis always covers a tree: deleting the last import of a module
// makes an untouched file dead. It runs whenever the selection contains
// something that can change that answer, and is skipped entirely otherwise.
//
// Go owns every fact the analysis depends on: which package owns which governed
// file, which of those files are entry points, and which governed source went
// unanalyzed. The bundle supplies Knip, so a target installs, pins, and
// configures none of it.
func JavaScriptDeadCodeFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	inventory, err := repo.AllFiles()
	if err != nil {
		return []policy.Finding{toolFinding("javascript-bundle", err.Error())}
	}
	if !bearsJavaScript(repo, inventory) {
		return nil
	}
	findings := javascriptDeadCodeConfigFindings(files)
	if !javascriptDeadCodeSelected(files) {
		return findings
	}
	analyses, uncovered := javascriptDeadCodeAnalyses(repo, inventory)
	for _, entry := range uncovered {
		findings = append(findings, javascriptDeadCodeCoverageFinding(entry.path, entry.reason))
	}
	if len(analyses) == 0 {
		return findings
	}
	ctx, cancel := context.WithTimeout(ctx, javascriptDeadCodeBudget)
	defer cancel()
	bundle := javascript.Bundle{PolicyRoot: repo.PolicyRoot}
	for _, analysis := range analyses {
		result, err := bundle.DeadCode(ctx, repo.Root, analysis.directory, analysis.workspaces)
		if err != nil {
			return append(findings, toolFinding("javascript-bundle", err.Error()))
		}
		findings = append(findings, javascriptDeadCodeResultFindings(analysis, result)...)
	}
	return findings
}

// One dead-code exchange: the package tree it analyzes, and every package in
// that tree with the governed files and entry points Go decided for it.
type javascriptDeadCodeAnalysis struct {
	directory  string
	workspaces []javascript.DeadCodeWorkspace
}

// One governed file the analysis did not decide, with the reason.
type javascriptUncoveredFile struct {
	path   string
	reason string
}

// Whether the selection contains anything the dead-code answer depends on. The
// analysis is never narrowed to the selection, because a whole tree decides
// what is reachable; it is only skipped when nothing it reads changed.
func javascriptDeadCodeSelected(files []string) bool {
	for _, path := range files {
		name := filepath.Base(path)
		switch {
		case javascriptSourceExtensions[strings.ToLower(filepath.Ext(path))],
			name == "package.json", name == "pnpm-workspace.yaml", name == policy.ConfigFilename,
			strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json"):
			return true
		}
	}
	return false
}

func javascriptDeadCodeAnalyses(repo repository.Repository, inventory []string) ([]javascriptDeadCodeAnalysis, []javascriptUncoveredFile) {
	packages := javascriptPackageRoots(inventory)
	grouped := map[string]*javascript.DeadCodeWorkspace{}
	uncovered := []javascriptUncoveredFile{}
	for _, path := range inventory {
		if repo.Language(path) != "typescript" || repo.IsGenerated(path) {
			continue
		}
		if !javascriptSourceExtensions[strings.ToLower(filepath.Ext(path))] {
			uncovered = append(uncovered, javascriptUncoveredFile{path,
				"the policy-owned dead-code analyzer does not analyze this file"})
			continue
		}
		owner, owned := javascriptOwningPackage(packages, path)
		if !owned {
			uncovered = append(uncovered, javascriptUncoveredFile{path,
				"no package.json governs this file, so no package declares what it belongs to"})
			continue
		}
		workspace, exists := grouped[owner]
		if !exists {
			workspace = &javascript.DeadCodeWorkspace{Root: owner, Entry: []string{}, Project: []string{}}
			grouped[owner] = workspace
		}
		workspace.Project = append(workspace.Project, path)
		if javascriptEntryPoint(repo, owner, path) {
			workspace.Entry = append(workspace.Entry, path)
		}
	}
	return javascriptDeadCodeTrees(packages, grouped), uncovered
}

// Every directory that declares a package.
func javascriptPackageRoots(inventory []string) map[string]bool {
	roots := map[string]bool{}
	for _, path := range inventory {
		if filepath.Base(path) == "package.json" {
			roots[filepath.ToSlash(filepath.Dir(path))] = true
		}
	}
	return roots
}

// The package that owns a file, which is the nearest one above it.
func javascriptOwningPackage(packages map[string]bool, path string) (string, bool) {
	directory := filepath.ToSlash(filepath.Dir(path))
	for {
		if packages[directory] {
			return directory, true
		}
		if directory == "." {
			return "", false
		}
		directory = filepath.ToSlash(filepath.Dir(directory))
	}
}

// One analysis per independent package tree. The outermost package above a
// package decides which tree it belongs to, so a workspace monorepo is analyzed
// whole -- an export a sibling package uses is used -- and two unrelated
// package trees are analyzed separately rather than pretending to be one.
func javascriptDeadCodeTrees(packages map[string]bool, grouped map[string]*javascript.DeadCodeWorkspace) []javascriptDeadCodeAnalysis {
	trees := map[string][]javascript.DeadCodeWorkspace{}
	for root, workspace := range grouped {
		sort.Strings(workspace.Entry)
		sort.Strings(workspace.Project)
		directory := javascriptOutermostPackage(packages, root)
		trees[directory] = append(trees[directory], *workspace)
	}
	directories := make([]string, 0, len(trees))
	for directory := range trees {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	analyses := make([]javascriptDeadCodeAnalysis, 0, len(directories))
	for _, directory := range directories {
		workspaces := trees[directory]
		sort.Slice(workspaces, func(left, right int) bool {
			return workspaces[left].Root < workspaces[right].Root
		})
		analyses = append(analyses, javascriptDeadCodeAnalysis{directory: directory, workspaces: workspaces})
	}
	return analyses
}

func javascriptOutermostPackage(packages map[string]bool, root string) string {
	outermost := root
	for directory := root; directory != "."; {
		directory = filepath.ToSlash(filepath.Dir(directory))
		if packages[directory] {
			outermost = directory
		}
	}
	return outermost
}

// The conventional names of a module something loads rather than imports.
var javascriptEntryNames = map[string]bool{"cli": true, "index": true, "main": true}

// Which governed files are entry points. Code Polishy owns this: a test file is
// loaded by its runner, a module at a conventional package entry name is loaded
// by whatever consumes the package, a configuration module beside a package
// manifest is loaded by the tool it configures, and a target declares the rest
// as facts about itself. None of it is learned from a framework configuration
// file, because learning it that way means executing that file.
func javascriptEntryPoint(repo repository.Repository, root, path string) bool {
	if repo.IsTest(path) || policy.MatchesAny(path, repo.Config.Scope.EntryPoints) {
		return true
	}
	directory := filepath.ToSlash(filepath.Dir(path))
	name := filepath.Base(path)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if directory == root {
		return javascriptEntryNames[stem] || strings.HasSuffix(stem, ".config")
	}
	return directory == javascriptDirectoryFile(root, "src") && javascriptEntryNames[stem]
}

func javascriptDeadCodeResultFindings(analysis javascriptDeadCodeAnalysis, result javascript.DeadCodeResult) []policy.Finding {
	findings := []policy.Finding{}
	decided := map[string]bool{}
	for _, path := range result.Covered {
		decided[path] = true
	}
	for _, entry := range result.Unsupported {
		decided[entry.Path] = true
		findings = append(findings, javascriptDeadCodeCoverageFinding(entry.Path,
			"the policy-owned dead-code analyzer could not analyze this file: "+entry.Reason))
	}
	for _, workspace := range analysis.workspaces {
		for _, path := range workspace.Project {
			if !decided[path] {
				findings = append(findings, javascriptDeadCodeCoverageFinding(path,
					"the policy-owned dead-code analyzer did not analyze this file"))
			}
		}
	}
	for _, path := range result.UnusedFiles {
		findings = append(findings, policy.Finding{
			Check: "quality.deadCode", Path: path, Subject: "knip",
			Message: "no entry point reaches this file; delete it or declare it in scope.entryPoints",
		})
	}
	for _, unused := range result.UnusedExports {
		findings = append(findings, policy.Finding{
			Check: "quality.deadCode", Path: unused.Path, Subject: "knip",
			Message: javascriptUnusedExportMessage(unused),
		})
	}
	return findings
}

// What each analyzer issue type calls the symbol it reported.
var javascriptUnusedExportNouns = map[string]string{
	"classMembers": "class member", "enumMembers": "enum member",
	"exports": "export", "nsExports": "namespace export",
	"nsTypes": "namespace exported type", "types": "exported type",
}

func javascriptUnusedExportMessage(unused javascript.UnusedExport) string {
	position := fmt.Sprintf("line %d, column %d: ", unused.Line, unused.Column)
	if unused.Kind == "duplicates" {
		return position + "the same symbol is exported more than once, as " + unused.Symbol
	}
	noun, named := javascriptUnusedExportNouns[unused.Kind]
	if !named {
		noun = unused.Kind
	}
	return position + "the " + noun + " " + unused.Symbol + " is exported but never used"
}

func javascriptDeadCodeCoverageFinding(path, message string) policy.Finding {
	return policy.Finding{Check: "quality.deadCodeCoverage", Path: path, Subject: "knip", Message: message}
}

// A target that ships dead-code configuration believes it decides what is
// reachable. Code Polishy owns the sealed configuration and never reads such a
// file, so it is reported rather than silently ignored.
func javascriptDeadCodeConfigFindings(files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range files {
		name := filepath.Base(path)
		if !strings.HasPrefix(name, "knip.") && !strings.HasPrefix(name, ".knip.") {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "quality.deadCodeConfiguration", Path: path, Subject: "knip",
			Message: "Code Polishy owns the sealed dead-code configuration; a target dead-code configuration is never read",
		})
	}
	return findings
}

// A target that ships lint configuration believes it decides which rules run.
// Code Polishy owns the sealed central configuration and never reads such a
// file, so it is reported rather than silently ignored.
func javascriptLintConfigFindings(files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range files {
		name := filepath.Base(path)
		if !strings.HasPrefix(name, "eslint.config.") && !strings.HasPrefix(name, ".eslintrc") {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "quality.lintConfiguration", Path: path, Subject: "eslint",
			Message: "Code Polishy owns the sealed lint configuration; a target lint configuration is never read",
		})
	}
	return findings
}

// A target that ships formatter configuration believes it decides formatting.
// Code Polishy owns the sealed central configuration and never reads such a
// file, so it is reported rather than silently ignored.
func javascriptFormatConfigFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range files {
		if !javascriptFormatConfigName(filepath.Base(path)) {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "quality.formatConfiguration", Path: path, Subject: "prettier",
			Message: "Code Polishy owns the sealed formatting configuration; a target formatter configuration is never read",
		})
	}
	return findings
}

func javascriptFormatConfigName(name string) bool {
	return name == ".prettierrc" || strings.HasPrefix(name, ".prettierrc.") ||
		strings.HasPrefix(name, "prettier.config.") || name == ".prettierignore"
}

// The governed files the sealed formatter decides. Generated files are excluded
// for the same reason every other quality check excludes them: nobody edits
// them. A lockfile is excluded because its package manager owns its exact
// bytes, and reformatting one would rewrite a record rather than source.
func javascriptFormatFiles(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		name := strings.ToLower(filepath.Base(path))
		if !javascriptFormatExtensions[strings.ToLower(filepath.Ext(path))] || repo.IsGenerated(path) {
			continue
		}
		if strings.HasSuffix(name, ".lock") || strings.Contains(name, "-lock.") {
			continue
		}
		selected = append(selected, path)
	}
	sort.Strings(selected)
	return selected
}

// Whether the target bears JavaScript or TypeScript at all. It is decided over
// the whole repository rather than one selection, so a change that touches only
// Markdown in a JavaScript target is still formatted, and no selection in a
// target without JavaScript ever launches the bundle.
func javascriptTarget(repo repository.Repository) (bool, error) {
	inventory, err := repo.AllFiles()
	if err != nil {
		return false, err
	}
	return bearsJavaScript(repo, inventory), nil
}

func bearsJavaScript(repo repository.Repository, files []string) bool {
	for _, path := range files {
		if repo.Language(path) == "typescript" {
			return true
		}
	}
	return false
}

// Every analyzer version the bundle answered with, so a report attributes its
// JavaScript findings to exact tools.
func analyzerVersions(tools map[string]string) string {
	described := make([]string, 0, len(tools))
	for name, version := range tools {
		described = append(described, name+" "+version)
	}
	sort.Strings(described)
	return strings.Join(described, ", ")
}
