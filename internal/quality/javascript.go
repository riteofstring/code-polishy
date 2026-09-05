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

const javascriptProvenanceBudget = 2 * time.Minute

const javascriptFormatBudget = 12 * time.Minute

const javascriptLintBudget = 20 * time.Minute

const javascriptTypeCheckBudget = 30 * time.Minute

const javascriptDeadCodeBudget = 30 * time.Minute

var javascriptFormatExtensions = map[string]bool{
	".cjs": true, ".css": true, ".cts": true, ".html": true, ".js": true,
	".json": true, ".jsonc": true, ".jsx": true, ".md": true, ".mjs": true,
	".markdown": true, ".mts": true, ".ts": true, ".tsx": true, ".yaml": true, ".yml": true,
}

var javascriptSourceExtensions = map[string]bool{
	".cjs": true, ".cts": true, ".js": true, ".jsx": true,
	".mjs": true, ".mts": true, ".ts": true, ".tsx": true,
}

var javascriptTypeCheckExtensions = map[string]bool{
	".cts": true, ".mts": true, ".ts": true, ".tsx": true,
}

var javascriptComplexityRules = map[string]bool{
	"complexity": true, "max-depth": true, "max-params": true,
}

func JavaScriptBundleStatus(repo repository.Repository, files []string) ([]policy.Finding, []string) {
	if !bearsJavaScript(repo, files) && len(markdownFormatFiles(repo, files)) == 0 && len(dataFiles(repo, files)) == 0 {
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

func JavaScriptFormatFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	return javascriptFormat(ctx, repo, files, false)
}

func JavaScriptFormatWrite(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	return javascriptFormat(ctx, repo, files, true)
}

func javascriptFormat(ctx context.Context, repo repository.Repository, files []string, write bool) []policy.Finding {
	governed, err := javascriptTarget(repo)
	if err != nil {
		return []policy.Finding{toolFinding("javascript-bundle", err.Error())}
	}
	if !governed {
		return markdownFormat(ctx, repo, files, write)
	}
	findings := javascriptFormatConfigFindings(repo, files)
	selected := javascriptFormatFiles(repo, files)
	if write {
		selected = editableJavaScriptFormatFiles(repo, selected)
	}
	return append(findings, runJavaScriptFormat(ctx, repo, selected, write)...)
}

func runJavaScriptFormat(ctx context.Context, repo repository.Repository, selected []string, write bool) []policy.Finding {
	if len(selected) == 0 {
		return nil
	}
	findings := []policy.Finding{}
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
		findings = append(findings, javascriptLintResultFindings(repo, result, group.checkComplexity)...)
	}
	return findings
}

type javascriptLintGroup struct {
	limits          javascript.LintLimits
	activation      javascript.LintActivation
	paths           []string
	checkComplexity bool
}

func javascriptLintGroups(repo repository.Repository, files []string) []javascriptLintGroup {
	grouped := map[string]*javascriptLintGroup{}
	for _, path := range files {
		if !javascriptSourceExtensions[strings.ToLower(filepath.Ext(path))] || repo.IsData(path) {
			continue
		}
		limits := javascriptLintLimits(repo, repo.IsTest(path))
		activation := javascriptLintActivation(repo, path)
		checkComplexity := !repo.IsGenerated(path)
		key := fmt.Sprintf("%+v|%+v|%t", limits, activation, checkComplexity)
		group, exists := grouped[key]
		if !exists {
			group = &javascriptLintGroup{limits: limits, activation: activation, checkComplexity: checkComplexity}
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

func javascriptLintActivation(repo repository.Repository, path string) javascript.LintActivation {
	path = repo.JavaScriptContextPath(path)
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

func javascriptLintResultFindings(repo repository.Repository, result javascript.LintResult, complexity ...bool) []policy.Finding {
	checkComplexity := len(complexity) == 0 || complexity[0]
	findings := []policy.Finding{}
	findings = append(findings, javascriptLintCommentFindings(repo, result)...)
	return append(findings, javascriptLintViolationFindings(result.Findings, checkComplexity)...)
}

func javascriptLintCommentFindings(repo repository.Repository, result javascript.LintResult) []policy.Finding {
	if repo.Config.Quality.CommentsAllowed() {
		return nil
	}
	findings := javascriptLintCommentCoverageFindings(repo, result.Unsupported)
	return append(findings, javascriptLintProseCommentFindings(repo, result.Comments)...)
}

func javascriptLintCommentCoverageFindings(repo repository.Repository, unsupported []javascript.Unsupported) []policy.Finding {
	findings := []policy.Finding{}
	for _, entry := range unsupported {
		if repo.IsGenerated(entry.Path) {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "policy.sourceCommentCoverage", Path: entry.Path, Subject: "eslint",
			Message: "the policy-owned source-comment scanner could not decide this file: " + entry.Reason,
		})
	}
	return findings
}

func javascriptLintProseCommentFindings(repo repository.Repository, comments []javascript.LintComment) []policy.Finding {
	findings := []policy.Finding{}
	for _, comment := range comments {
		if repo.IsGenerated(comment.Path) || javascriptSourceCommentAllowed(repo, comment) {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "policy.sourceComment", Path: comment.Path,
			Subject: fmt.Sprintf("%d:%d", comment.Line, comment.Column),
			Message: "prose comments and docstrings are forbidden; move durable context to a design document or use an allowed machine directive",
		})
	}
	return findings
}

func javascriptLintViolationFindings(violations []javascript.LintViolation, checkComplexity bool) []policy.Finding {
	findings := []policy.Finding{}
	for _, violation := range violations {
		finding, include := javascriptLintViolationFinding(violation, checkComplexity)
		if include {
			findings = append(findings, finding)
		}
	}
	return findings
}

func javascriptLintViolationFinding(violation javascript.LintViolation, checkComplexity bool) (policy.Finding, bool) {
	isComplexity := javascriptComplexityRules[violation.Rule]
	if !checkComplexity && isComplexity {
		return policy.Finding{}, false
	}
	check := "quality.lint"
	if isComplexity {
		check = "quality.complexity"
	}
	return policy.Finding{
		Check: check, Path: violation.Path, Subject: violation.Rule,
		Message: fmt.Sprintf("line %d, column %d: %s", violation.Line, violation.Column, violation.Message),
	}, true
}

func javascriptSourceCommentAllowed(repo repository.Repository, comment javascript.LintComment) bool {
	return javascriptShebangAllowed(comment) ||
		javascriptTypeScriptReferenceAllowed(comment) ||
		javascriptVitestEnvironmentAllowed(repo, comment)
}

func javascriptShebangAllowed(comment javascript.LintComment) bool {
	return comment.Complete && comment.Kind == "Shebang" && comment.ByteZero && comment.BeforeCode && comment.Preamble &&
		comment.Line == 1 && comment.Column == 1 && strings.HasPrefix(comment.Raw, "#!") &&
		strings.TrimSpace(strings.TrimPrefix(comment.Raw, "#!")) != ""
}

func javascriptTypeScriptReferenceAllowed(comment javascript.LintComment) bool {
	return comment.Complete && javascriptTypeCheckExtensions[strings.ToLower(filepath.Ext(comment.Path))] &&
		comment.Kind == "Line" && comment.BeforeCode && javascriptTripleSlashReference(comment.Raw)
}

func javascriptTripleSlashReference(raw string) bool {
	const prefix = "/// <reference "
	const suffix = " />"
	if !strings.HasPrefix(raw, prefix) || !strings.HasSuffix(raw, suffix) {
		return false
	}
	attribute := strings.TrimSuffix(strings.TrimPrefix(raw, prefix), suffix)
	return javascriptTripleSlashAttribute(attribute, "path") ||
		javascriptTripleSlashAttribute(attribute, "types") ||
		javascriptTripleSlashAttribute(attribute, "lib") ||
		attribute == `no-default-lib="true"`
}

func javascriptTripleSlashAttribute(attribute, name string) bool {
	prefix := name + `="`
	if !strings.HasPrefix(attribute, prefix) || !strings.HasSuffix(attribute, `"`) {
		return false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(attribute, prefix), `"`)
	return value != "" && !strings.ContainsAny(value, "\"\r\n")
}

func javascriptVitestEnvironmentAllowed(repo repository.Repository, comment javascript.LintComment) bool {
	return comment.Complete && repo.IsTest(comment.Path) && comment.BeforeCode && comment.Preamble &&
		((comment.Kind == "Line" && comment.Raw == "// @vitest-environment jsdom") ||
			(comment.Kind == "Block" && comment.Raw == "/* @vitest-environment jsdom */"))
}

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
		result, err := bundle.TypeCheckInherited(ctx, repo.Root, project.project, project.paths, project.inherited)
		if err != nil {
			return append(findings, toolFinding("javascript-bundle", err.Error()))
		}
		findings = append(findings, javascriptTypeCheckResultFindings(project, result)...)
	}
	return findings
}

type javascriptTypeCheckProject struct {
	project   string
	paths     []string
	inherited []string
}

func javascriptTypeCheckProjects(repo repository.Repository, files, inventory []string) ([]javascriptTypeCheckProject, []string) {
	configurations := javascriptProjectConfigurations(inventory)
	grouped := map[string][]string{}
	ungoverned := []string{}
	for _, path := range javascriptTypeCheckFiles(repo, files) {
		contextPath := repo.JavaScriptContextPath(path)
		project, governed := javascriptNearestProject(configurations, contextPath)
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
		inherited := []string{}
		for _, path := range grouped[name] {
			if _, found := repo.GeneratedJavaScriptOwner(path); found {
				inherited = append(inherited, path)
			}
		}
		projects = append(projects, javascriptTypeCheckProject{project: name, paths: grouped[name], inherited: inherited})
	}
	return projects, ungoverned
}

func javascriptTypeCheckFiles(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		if javascriptTypeCheckExtensions[strings.ToLower(filepath.Ext(path))] {
			selected = append(selected, path)
		}
	}
	sort.Strings(selected)
	return selected
}

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

type javascriptDeadCodeAnalysis struct {
	directory  string
	workspaces []javascript.DeadCodeWorkspace
}

type javascriptUncoveredFile struct {
	path   string
	reason string
}

func javascriptDeadCodeSelected(files []string) bool {
	for _, path := range files {
		name := filepath.Base(path)
		switch {
		case javascriptDeadCodeExtension(path),
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
		if repo.Language(path) != "typescript" {
			continue
		}
		if !javascriptDeadCodeExtension(path) {
			uncovered = append(uncovered, javascriptUncoveredFile{path,
				"the policy-owned dead-code analyzer does not analyze this file"})
			continue
		}
		owner, owned := javascriptOwningPackage(packages, repo.JavaScriptContextPath(path))
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
		if _, inherited := repo.GeneratedJavaScriptOwner(path); inherited {
			workspace.Inherited = append(workspace.Inherited, path)
		}
		if javascriptEntryPoint(repo, owner, path) {
			workspace.Entry = append(workspace.Entry, path)
		}
	}
	return javascriptDeadCodeTrees(packages, grouped), uncovered
}

func javascriptDeadCodeExtension(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return javascriptSourceExtensions[extension] || extension == ".astro"
}

func javascriptPackageRoots(inventory []string) map[string]bool {
	roots := map[string]bool{}
	for _, path := range inventory {
		if filepath.Base(path) == "package.json" {
			roots[filepath.ToSlash(filepath.Dir(path))] = true
		}
	}
	return roots
}

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

func javascriptDeadCodeTrees(packages map[string]bool, grouped map[string]*javascript.DeadCodeWorkspace) []javascriptDeadCodeAnalysis {
	trees := map[string][]javascript.DeadCodeWorkspace{}
	for root, workspace := range grouped {
		sort.Strings(workspace.Entry)
		sort.Strings(workspace.Project)
		sort.Strings(workspace.Inherited)
		directory := javascriptDeadCodeDirectory(packages, root, workspace.Project)
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

func javascriptDeadCodeDirectory(packages map[string]bool, root string, paths []string) string {
	directory := javascriptOutermostPackage(packages, root)
	for _, path := range paths {
		for !javascriptScopeOwns(directory, path) {
			if directory == "." {
				return directory
			}
			directory = filepath.ToSlash(filepath.Dir(directory))
		}
	}
	return directory
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

var javascriptEntryNames = map[string]bool{"cli": true, "index": true, "main": true}

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

func javascriptFormatFiles(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		name := strings.ToLower(filepath.Base(path))
		if !javascriptFormatExtensions[strings.ToLower(filepath.Ext(path))] || repo.IsData(path) {
			continue
		}
		if repo.IsGenerated(path) && !repo.IsExecutableSource(path) {
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

func editableJavaScriptFormatFiles(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		if !repo.IsGenerated(path) && !repo.IsData(path) {
			selected = append(selected, path)
		}
	}
	return selected
}

func markdownFormatFiles(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		if markdownPath(path) && !repo.IsGenerated(path) && !repo.IsData(path) {
			selected = append(selected, path)
		}
	}
	return uniqueMarkdownPaths(selected)
}

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

func analyzerVersions(tools map[string]string) string {
	described := make([]string, 0, len(tools))
	for name, version := range tools {
		described = append(described, name+" "+version)
	}
	sort.Strings(described)
	return strings.Join(described, ", ")
}
