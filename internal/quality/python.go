package quality

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const pythonQualityBudget = 15 * time.Minute

const pythonStructuredOutputMaximumBytes = 4 << 20

const pythonStructuredDiagnosticMaximum = 10000

const pythonStructuredMessageMaximumBytes = 4096

const pythonRuffBaselineSelection = "B,C4,E,F,I,PIE,RUF,SIM,UP"

const pythonRuffComplexitySelection = "C901"

type pythonQualityKind string

const (
	pythonRuffBaselineCoverageQualityKind pythonQualityKind = "ruff-baseline-coverage"
	pythonRuffBaselineQualityKind         pythonQualityKind = "ruff-baseline"
	pythonRuffComplexityQualityKind       pythonQualityKind = "ruff-complexity"
	pythonRuffTargetQualityKind           pythonQualityKind = "ruff-target"
	pythonVultureQualityKind              pythonQualityKind = "vulture"
	pythonTyQualityKind                   pythonQualityKind = "ty"
)

type pythonQualityCommand struct {
	kind    pythonQualityKind
	command policy.Command
}

type pythonQualityProject struct {
	project  repository.PythonProject
	sources  []string
	commands []pythonQualityCommand
}

type pythonQualityPlan struct {
	projects []pythonQualityProject
	findings []policy.Finding
}

type pythonRuffDiagnostic struct {
	Code    string
	Path    string
	Line    int
	Column  int
	Message string
}

type pythonRuffDiagnosticWire struct {
	Code     string `json:"code"`
	Filename string `json:"filename"`
	Location struct {
		Row    int `json:"row"`
		Column int `json:"column"`
	} `json:"location"`
	Message string `json:"message"`
}

type pythonTyDiagnostic struct {
	Rule    string
	Path    string
	Line    int
	Column  int
	Message string
}

type pythonTyDiagnosticWire struct {
	CheckName   string `json:"check_name"`
	Description string `json:"description"`
	Location    struct {
		Path  string `json:"path"`
		Line  int    `json:"line"`
		Lines struct {
			Begin int `json:"begin"`
		} `json:"lines"`
		Positions struct {
			Begin struct {
				Line   int `json:"line"`
				Column int `json:"column"`
			} `json:"begin"`
		} `json:"positions"`
	} `json:"location"`
}

func PythonQualityFindings(ctx context.Context, repo repository.Repository, selected []string, commandRunner runner.Runner) []policy.Finding {
	return pythonQualityFindingsForPlan(ctx, repo, pythonQualityPlanFor(repo, selected), commandRunner)
}

func pythonQualityFindingsForSelection(ctx context.Context, repo repository.Repository, selection repository.Selection, commandRunner runner.Runner, profile string) []policy.Finding {
	return pythonQualityFindingsForPlan(ctx, repo, buildPythonQualityPlan(repo, selection.Files, selection.All || profile == "gate"), commandRunner)
}

func pythonQualityFindingsForPlan(ctx context.Context, repo repository.Repository, plan pythonQualityPlan, commandRunner runner.Runner) []policy.Finding {
	findings := append([]policy.Finding{}, plan.findings...)
	structured, supportsStructuredOutput := commandRunner.(runner.StructuredRunner)
	for _, project := range plan.projects {
		findings = append(findings, pythonQualityProjectFindings(ctx, repo, project, structured, supportsStructuredOutput)...)
	}
	return uniquePythonQualityFindings(findings)
}

func pythonQualityProjectFindings(ctx context.Context, repo repository.Repository, project pythonQualityProject, structured runner.StructuredRunner, supportsStructuredOutput bool) []policy.Finding {
	findings := []policy.Finding{}
	for _, specification := range project.commands {
		findings = append(findings, pythonQualityCommandFindings(ctx, repo, project, specification, structured, supportsStructuredOutput)...)
	}
	return findings
}

func pythonQualityCommandFindings(ctx context.Context, repo repository.Repository, project pythonQualityProject, specification pythonQualityCommand, structured runner.StructuredRunner, supportsStructuredOutput bool) []policy.Finding {
	if !supportsStructuredOutput {
		if specification.kind == pythonVultureQualityKind {
			return pythonVultureCoverage(project.project.Files, "the quality runner cannot capture bounded structured tool output")
		}
		return []policy.Finding{pythonQualityToolFinding(project.project, specification.kind, "the quality runner cannot capture bounded structured tool output")}
	}
	bounded, cancel := context.WithTimeout(ctx, pythonQualityBudget)
	_, output, runErr := structured.RunStructured(bounded, repo.Root, specification.command)
	cancel()
	if specification.kind == pythonVultureQualityKind {
		recordPythonVultureCommandPhases(ctx, specification.command, commandRunnerPhaseRecorder(structured), output.Stdout)
	}
	if runErr != nil {
		if specification.kind == pythonVultureQualityKind {
			return pythonVultureCoverage(project.project.Files, "the policy-owned Vulture analysis failed: "+runErr.Error())
		}
		return []policy.Finding{pythonQualityToolFinding(project.project, specification.kind, runErr.Error())}
	}
	return pythonQualityOutputFindings(repo, project, specification.kind, output.Stdout)
}

func commandRunnerPhaseRecorder(commandRunner runner.StructuredRunner) runner.CommandPhaseRecorder {
	recorder, _ := commandRunner.(runner.CommandPhaseRecorder)
	return recorder
}

type pythonVultureTiming struct {
	Name       string `json:"name"`
	DurationNS int64  `json:"duration_ns"`
}

func validatePythonVultureTimings(timings []pythonVultureTiming) error {
	allowed := map[string]bool{
		"request-validation": true, "source-parse": true, "request-model": true, "vulture-scan": true,
		"type-facts": true, "typed-dict": true, "framework-contracts": true, "static-contracts": true,
		"reachability": true, "diagnostics": true, "adapter-total": true,
	}
	seen := map[string]bool{}
	for _, timing := range timings {
		if !allowed[timing.Name] || seen[timing.Name] || timing.DurationNS < 0 || timing.DurationNS > int64(pythonQualityBudget) {
			return fmt.Errorf("output contains invalid Vulture timing evidence")
		}
		seen[timing.Name] = true
	}
	if !seen["adapter-total"] {
		return fmt.Errorf("output omits Vulture total timing evidence")
	}
	return nil
}

func recordPythonVultureCommandPhases(ctx context.Context, command policy.Command, recorder runner.CommandPhaseRecorder, data []byte) {
	if recorder == nil {
		return
	}
	response, err := parsePythonVultureResponse(data)
	if err != nil {
		return
	}
	phases := make([]runner.CommandPhase, 0, len(response.Timings))
	for _, timing := range response.Timings {
		phases = append(phases, runner.CommandPhase{Name: timing.Name, Duration: time.Duration(timing.DurationNS)})
	}
	recorder.RecordCommandPhases(ctx, command, phases)
}

func pythonQualityOutputFindings(repo repository.Repository, project pythonQualityProject, kind pythonQualityKind, output []byte) []policy.Finding {
	switch kind {
	case pythonRuffBaselineCoverageQualityKind:
		return pythonQualityRuffCoverageFindings(repo, project, output)
	case pythonRuffBaselineQualityKind:
		return pythonQualityRuffLintFindings(repo, project, output, true)
	case pythonRuffTargetQualityKind:
		return pythonQualityRuffLintFindings(repo, project, output, false)
	case pythonRuffComplexityQualityKind:
		return pythonQualityRuffComplexityFindings(repo, project, output)
	case pythonVultureQualityKind:
		return pythonVultureFindingsForSources(repo, project.project, project.sources, output)
	case pythonTyQualityKind:
		return pythonQualityTyFindings(repo, project, output)
	}
	return nil
}

func pythonQualityRuffCoverageFindings(repo repository.Repository, project pythonQualityProject, output []byte) []policy.Finding {
	covered, err := parsePythonRuffCoverage(repo, project.project, project.sources, output)
	if err != nil {
		return pythonQualityCoverage(project.sources, "quality.lintCoverage", "ruff", "the policy-owned Ruff coverage output cannot be used: "+err.Error())
	}
	findings := []policy.Finding{}
	for _, source := range project.sources {
		if !covered[source] {
			findings = append(findings, pythonQualityCoverage([]string{source}, "quality.lintCoverage", "ruff", "the isolated Ruff baseline did not analyze this governed Python source")...)
		}
	}
	return findings
}

func pythonQualityRuffLintFindings(repo repository.Repository, project pythonQualityProject, output []byte, baseline bool) []policy.Finding {
	diagnostics, err := parsePythonRuffDiagnostics(repo, project.project, project.sources, output)
	if err != nil {
		return pythonQualityCoverage(project.sources, "quality.lintCoverage", "ruff", "the policy-owned Ruff output cannot be used: "+err.Error())
	}
	if baseline && !pythonRuffBaselineDiagnostics(diagnostics) {
		return pythonQualityCoverage(project.sources, "quality.lintCoverage", "ruff", "the isolated Ruff baseline emitted a diagnostic outside "+pythonRuffBaselineSelection)
	}
	return pythonRuffDiagnosticFindings(pythonStyleFilteredDiagnostics(repo, diagnostics), "quality.lint")
}

func pythonQualityRuffComplexityFindings(repo repository.Repository, project pythonQualityProject, output []byte) []policy.Finding {
	diagnostics, err := parsePythonRuffDiagnostics(repo, project.project, project.sources, output)
	if err != nil {
		return pythonQualityCoverage(project.sources, "quality.complexityCoverage", "ruff", "the policy-owned Ruff complexity output cannot be used: "+err.Error())
	}
	if !pythonRuffComplexityDiagnostics(diagnostics) {
		return pythonQualityCoverage(project.sources, "quality.complexityCoverage", "ruff", "the isolated Ruff complexity pass emitted a diagnostic outside C901")
	}
	return pythonRuffDiagnosticFindings(diagnostics, "quality.complexity")
}

func pythonQualityTyFindings(repo repository.Repository, project pythonQualityProject, output []byte) []policy.Finding {
	diagnostics, err := parsePythonTyDiagnostics(repo, project.project, project.sources, output)
	if err != nil {
		return pythonQualityCoverage(project.sources, "quality.typecheckCoverage", "ty", "the policy-owned ty output cannot be used: "+err.Error())
	}
	findings := make([]policy.Finding, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		findings = append(findings, policy.Finding{
			Check: "quality.typecheck", Path: diagnostic.Path, Line: diagnostic.Line, Column: diagnostic.Column,
			Subject: pythonTySubject(diagnostic), Message: pythonDiagnosticMessage(diagnostic.Line, diagnostic.Column, diagnostic.Message),
		})
	}
	return findings
}

func pythonRuffDiagnosticFindings(diagnostics []pythonRuffDiagnostic, check string) []policy.Finding {
	findings := make([]policy.Finding, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		findings = append(findings, policy.Finding{
			Check: check, Path: diagnostic.Path, Line: diagnostic.Line, Column: diagnostic.Column,
			Subject: diagnostic.Code, Message: pythonDiagnosticMessage(diagnostic.Line, diagnostic.Column, diagnostic.Message),
		})
	}
	return findings
}

func pythonQualityCommands(repo repository.Repository, selected []string) []policy.Command {
	return pythonQualityPlanCommands(pythonQualityPlanFor(repo, selected))
}

func pythonQualityCommandsForSelection(repo repository.Repository, selection repository.Selection, profile string) []policy.Command {
	return pythonQualityPlanCommands(buildPythonQualityPlan(repo, selection.Files, selection.All || profile == "gate"))
}

func pythonQualityPlanCommands(plan pythonQualityPlan) []policy.Command {
	commands := []policy.Command{}
	for _, project := range plan.projects {
		for _, specification := range project.commands {
			commands = append(commands, specification.command)
		}
	}
	return commands
}

func pythonQualityPlanFor(repo repository.Repository, selected []string) pythonQualityPlan {
	return buildPythonQualityPlan(repo, selected, true)
}

func buildPythonQualityPlan(repo repository.Repository, selected []string, includeVulture bool) pythonQualityPlan {
	sources := pythonQualitySources(repo, selected)
	selectedManifests := pythonQualitySelectedManifests(repo, selected)
	if len(sources) == 0 && len(selectedManifests) == 0 {
		return pythonQualityPlan{}
	}
	allFiles, err := repo.AllFiles()
	if err != nil {
		message := "the Python project inventory is unavailable: " + err.Error()
		findings := pythonQualityAllCoverage(sources, message)
		findings = append(findings, pythonQualityDynamicReferenceInventoryFindings(repo, selectedManifests, message)...)
		findings = append(findings, pythonQualityExternalAttributeInventoryFindings(repo, selectedManifests, message)...)
		return pythonQualityPlan{findings: pythonQualityProfileFindings(findings, includeVulture)}
	}
	inventory := repo.PythonProjectInventory(allFiles)
	findings, invalid, invalidProjects := pythonQualityInventoryFindings(sources, selectedManifests, inventory)
	projects, owners := pythonQualityProjectOwners(inventory)
	selectedByProject, ownershipFindings := pythonQualitySelectedProjects(sources, invalid, owners)
	findings = append(findings, ownershipFindings...)
	referenceFindings, referenceOnly := pythonQualityDynamicReferenceProjects(repo, projects, selectedByProject, selectedManifests, invalidProjects)
	findings = append(findings, referenceFindings...)
	planned := []pythonQualityProject{}
	for _, manifest := range pythonQualityProjectManifests(selectedByProject) {
		project, found, projectFindings := pythonQualityPlannedProjectForProfile(repo, manifest, selectedByProject[manifest], projects, includeVulture)
		findings = append(findings, projectFindings...)
		if found {
			planned = append(planned, project)
		}
	}
	if includeVulture {
		planned, findings = pythonQualityReferenceOnlyProjects(repo, projects, selectedByProject, referenceOnly, planned, findings)
	}
	sort.Slice(planned, func(left, right int) bool {
		return planned[left].project.Manifest < planned[right].project.Manifest
	})
	return pythonQualityPlan{projects: planned, findings: pythonQualityProfileFindings(findings, includeVulture)}
}

func pythonQualityProfileFindings(findings []policy.Finding, includeVulture bool) []policy.Finding {
	if includeVulture {
		return findings
	}
	return slices.DeleteFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.deadCodeCoverage"
	})
}

func pythonQualityReferenceOnlyProjects(repo repository.Repository, projects map[string]repository.PythonProject, selectedByProject, referenceOnly map[string][]string, planned []pythonQualityProject, findings []policy.Finding) ([]pythonQualityProject, []policy.Finding) {
	for _, manifest := range pythonQualityProjectManifests(referenceOnly) {
		if _, selected := selectedByProject[manifest]; selected {
			continue
		}
		project, found := projects[manifest]
		if !found {
			continue
		}
		command, commandErr := pythonVultureCommandForSources(repo, project, nil)
		if commandErr != nil {
			findings = append(findings, pythonVultureCoverage(project.Files, "the Python project cannot produce a contained Vulture command: "+commandErr.Error())...)
			continue
		}
		planned = append(planned, pythonQualityProject{
			project: project, commands: []pythonQualityCommand{{kind: pythonVultureQualityKind, command: command}},
		})
	}
	return planned, findings
}

func pythonQualityAllCoverage(sources []string, message string) []policy.Finding {
	findings := pythonQualityNonDeadCodeCoverage(sources, message)
	return append(findings, pythonVultureCoverage(sources, message)...)
}

func pythonQualityNonDeadCodeCoverage(sources []string, message string) []policy.Finding {
	findings := pythonQualityCoverage(sources, "quality.lintCoverage", "ruff", message)
	findings = append(findings, pythonQualityCoverage(sources, "quality.complexityCoverage", "ruff", message)...)
	return append(findings, pythonQualityCoverage(sources, "quality.typecheckCoverage", "ty", message)...)
}

func pythonQualityProjectCommandsForProfile(repo repository.Repository, project repository.PythonProject, sources []string, includeVulture bool) ([]pythonQualityCommand, string, error) {
	paths, err := pythonQualityProjectPaths(project, sources)
	if err != nil {
		return nil, "", err
	}
	searchPaths, err := pythonQualityProjectSearchPaths(project)
	if err != nil {
		return nil, "", err
	}
	ruffOptions, err := project.Ruff.CommandOptions(project.Root, project.SourceRoots)
	if err != nil {
		return nil, "", err
	}
	modules := pythonQualityModules(repo, sources)
	suffix := pythonQualityProjectName(project.Root)
	commands := []pythonQualityCommand{
		{kind: pythonRuffBaselineCoverageQualityKind, command: pythonRuffBaselineCoverageCommand(repo, project, suffix, modules, paths, ruffOptions)},
		{kind: pythonRuffBaselineQualityKind, command: pythonRuffBaselineCommand(repo, project, suffix, modules, paths, ruffOptions)},
		{kind: pythonRuffComplexityQualityKind, command: pythonRuffComplexityCommand(repo, project, suffix, modules, paths, ruffOptions)},
		{kind: pythonRuffTargetQualityKind, command: pythonRuffTargetCommand(repo, project, suffix, modules, paths, ruffOptions)},
	}
	if includeVulture {
		vulture, vultureErr := pythonVultureCommandForSources(repo, project, sources)
		if vultureErr != nil {
			return nil, "", vultureErr
		}
		commands = append(commands, pythonQualityCommand{kind: pythonVultureQualityKind, command: vulture})
	}
	if len(project.Requirements) == 0 {
		commands = append(commands, pythonQualityCommand{kind: pythonTyQualityKind, command: pythonTyCommand(repo, project, suffix, modules, paths, searchPaths, "")})
		return commands, "", nil
	}
	venv, venvErr := pythonQualityVenv(repo, project)
	if venvErr != nil {
		return commands, venvErr.Error(), nil
	}
	commands = append(commands, pythonQualityCommand{kind: pythonTyQualityKind, command: pythonTyCommand(repo, project, suffix, modules, paths, searchPaths, venv)})
	return commands, "", nil
}

func pythonRuffBaselineCoverageCommand(repo repository.Repository, project repository.PythonProject, suffix string, modules, paths, ruffOptions []string) policy.Command {
	arguments := []string{
		repo.PolicyTool("ruff"), "check",
	}
	arguments = append(arguments, ruffOptions...)
	arguments = append(arguments, "--no-fix", "--isolated", "--select", pythonRuffBaselineSelection,
		"--no-respect-gitignore", "--no-force-exclude", "--show-files", "--exit-zero", "--")
	arguments = append(arguments, paths...)
	return policy.Command{
		Name: "policy-ruff-baseline-coverage-" + suffix, Provides: []string{"lint"}, Argv: arguments,
		Cwd: project.Root, Modules: modules, RunOn: []string{"check", "gate"}, TimeoutSeconds: int(pythonQualityBudget.Seconds()),
		Managed: true, SealedEnvironment: true,
	}
}

func pythonQualityProjectPaths(project repository.PythonProject, sources []string) ([]string, error) {
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		path, err := pythonQualityProjectPath(project, source)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func pythonQualityProjectSearchPaths(project repository.PythonProject) ([]string, error) {
	paths := []string{}
	for _, root := range project.SourceRoots {
		relative, err := pythonQualityProjectPath(project, root)
		if err != nil {
			return nil, err
		}
		if relative != "." {
			paths = append(paths, relative)
		}
	}
	return paths, nil
}

func pythonQualityProjectPath(project repository.PythonProject, source string) (string, error) {
	root := filepath.FromSlash(project.Root)
	relative, err := filepath.Rel(root, filepath.FromSlash(source))
	if err != nil {
		return "", err
	}
	relative = filepath.ToSlash(relative)
	if relative == ".." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("source %q is outside project root %q", source, project.Root)
	}
	return relative, nil
}

func pythonQualityModules(repo repository.Repository, sources []string) []string {
	modules := []string{}
	for _, source := range sources {
		modules = append(modules, repo.OwnerModuleNames(source)...)
	}
	sort.Strings(modules)
	return slices.Compact(modules)
}

func pythonQualityProjectName(root string) string {
	if root == "" || root == "." {
		return "root"
	}
	return "x" + hex.EncodeToString([]byte(root))
}

func pythonRuffBaselineCommand(repo repository.Repository, project repository.PythonProject, suffix string, modules, paths, ruffOptions []string) policy.Command {
	arguments := []string{
		repo.PolicyTool("ruff"), "check",
	}
	arguments = append(arguments, ruffOptions...)
	arguments = append(arguments, "--no-fix", "--isolated", "--select", pythonRuffBaselineSelection, "--ignore-noqa",
		"--no-respect-gitignore", "--no-force-exclude", "--output-format", "json", "--exit-zero", "--")
	arguments = append(arguments, paths...)
	return policy.Command{
		Name: "policy-ruff-baseline-" + suffix, Provides: []string{"lint"}, Argv: arguments,
		Cwd: project.Root, Modules: modules, RunOn: []string{"check", "gate"}, TimeoutSeconds: int(pythonQualityBudget.Seconds()),
		Managed: true, SealedEnvironment: true,
	}
}

func pythonRuffComplexityCommand(repo repository.Repository, project repository.PythonProject, suffix string, modules, paths, ruffOptions []string) policy.Command {
	complexity := repo.Config.Quality.Complexity.Python
	if complexity == 0 {
		complexity = policy.MaxPythonComplexity
	}
	arguments := []string{
		repo.PolicyTool("ruff"), "check",
	}
	arguments = append(arguments, ruffOptions...)
	arguments = append(arguments, "--no-fix", "--isolated", "--select", pythonRuffComplexitySelection, "--ignore-noqa",
		"--config", "lint.mccabe.max-complexity = "+strconv.Itoa(complexity-1),
		"--no-respect-gitignore", "--no-force-exclude", "--output-format", "json", "--exit-zero", "--")
	arguments = append(arguments, paths...)
	return policy.Command{
		Name: "policy-ruff-complexity-" + suffix, Provides: []string{"complexity"}, Argv: arguments,
		Cwd: project.Root, Modules: modules, RunOn: []string{"check", "gate"}, TimeoutSeconds: int(pythonQualityBudget.Seconds()),
		Managed: true, SealedEnvironment: true,
	}
}

func pythonRuffTargetCommand(repo repository.Repository, project repository.PythonProject, suffix string, modules, paths, ruffOptions []string) policy.Command {
	arguments := []string{
		repo.PolicyTool("ruff"), "check",
	}
	arguments = append(arguments, ruffOptions...)
	arguments = append(arguments, "--no-fix", "--no-respect-gitignore", "--no-force-exclude",
		"--output-format", "json", "--exit-zero", "--")
	arguments = append(arguments, paths...)
	return policy.Command{
		Name: "policy-ruff-target-" + suffix, Provides: []string{"lint"}, Argv: arguments,
		Cwd: project.Root, Modules: modules, RunOn: []string{"check", "gate"}, TimeoutSeconds: int(pythonQualityBudget.Seconds()),
		Managed: true, SealedEnvironment: true,
	}
}

func pythonTyCommand(repo repository.Repository, project repository.PythonProject, suffix string, modules, paths, searchPaths []string, venv string) policy.Command {
	arguments := []string{
		repo.PolicyTool("ty"), "check", "--config-file", filepath.Join(repo.PolicyRoot, "tools", "ty.toml"), "--project", ".",
		"--output-format", "gitlab", "--exit-zero", "--no-progress", "--color", "never",
		"--no-respect-ignore-files", "--no-force-exclude", "--include-scripts",
	}
	if venv != "" {
		arguments = append(arguments, "--python", venv)
	}
	for _, searchPath := range searchPaths {
		arguments = append(arguments, "--extra-search-path", searchPath)
	}
	arguments = append(arguments, "--")
	arguments = append(arguments, paths...)
	return policy.Command{
		Name: "policy-ty-typecheck-" + suffix, Provides: []string{"typecheck"}, Argv: arguments,
		Cwd: project.Root, Modules: modules, RunOn: []string{"check", "gate"}, TimeoutSeconds: int(pythonQualityBudget.Seconds()),
		Managed: true, SealedEnvironment: true,
	}
}

func pythonQualityVenv(repo repository.Repository, project repository.PythonProject) (string, error) {
	resolved, err := pythonQualityVenvDirectory(repo, project)
	if err != nil {
		return "", err
	}
	if !pythonQualityVenvHasConfiguration(resolved) {
		return "", fmt.Errorf("project environment %s does not contain pyvenv.cfg", project.Venv)
	}
	if !pythonQualityVenvHasInterpreter(resolved) {
		return "", fmt.Errorf("project environment %s has no runnable Python interpreter", project.Venv)
	}
	return pythonQualityProjectPath(project, project.Venv)
}

func pythonQualityVenvDirectory(repo repository.Repository, project repository.PythonProject) (string, error) {
	if project.Venv == "" {
		return "", fmt.Errorf("project dependencies require the contained environment %s", pythonQualityExpectedVenv(project))
	}
	resolved, err := repo.Resolve(project.Venv)
	if err != nil {
		return "", fmt.Errorf("project environment %s is unavailable: %w", project.Venv, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("project environment %s is not a directory", project.Venv)
	}
	return resolved, nil
}

func pythonQualityExpectedVenv(project repository.PythonProject) string {
	if project.Root == "." {
		return ".venv"
	}
	return project.Root + "/.venv"
}

func pythonQualityVenvHasConfiguration(root string) bool {
	info, err := os.Stat(filepath.Join(root, "pyvenv.cfg"))
	return err == nil && info.Mode().IsRegular()
}

func pythonQualityVenvHasInterpreter(root string) bool {
	for _, candidate := range []string{"bin/python", "bin/python3", "Scripts/python.exe", "Scripts/python"} {
		if pythonQualityVenvInterpreterIsRunnable(root, candidate) {
			return true
		}
	}
	return false
}

func pythonQualityVenvInterpreterIsRunnable(root, candidate string) bool {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(candidate)))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return info.Mode()&0o111 != 0 || strings.HasSuffix(strings.ToLower(candidate), ".exe")
}

func parsePythonRuffDiagnostics(repo repository.Repository, project repository.PythonProject, sources []string, data []byte) ([]pythonRuffDiagnostic, error) {
	decoded, err := decodePythonRuffDiagnostics(data)
	if err != nil {
		return nil, err
	}
	selected := pythonQualitySelectedSources(sources)
	diagnostics := make([]pythonRuffDiagnostic, 0, len(decoded))
	for _, entry := range decoded {
		diagnostic, entryErr := pythonRuffDiagnosticFromWire(repo, project, selected, entry)
		if entryErr != nil {
			return nil, entryErr
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics, nil
}

func decodePythonRuffDiagnostics(data []byte) ([]pythonRuffDiagnosticWire, error) {
	if len(data) > pythonStructuredOutputMaximumBytes {
		return nil, fmt.Errorf("output exceeds %d bytes", pythonStructuredOutputMaximumBytes)
	}
	decoded := []pythonRuffDiagnosticWire{}
	if err := decodePythonStructuredJSON(data, &decoded); err != nil {
		return nil, err
	}
	if len(decoded) > pythonStructuredDiagnosticMaximum {
		return nil, fmt.Errorf("output contains more than %d diagnostics", pythonStructuredDiagnosticMaximum)
	}
	return decoded, nil
}

func pythonRuffDiagnosticFromWire(repo repository.Repository, project repository.PythonProject, selected map[string]bool, entry pythonRuffDiagnosticWire) (pythonRuffDiagnostic, error) {
	if entry.Code == "" || len(entry.Code) > 128 {
		return pythonRuffDiagnostic{}, fmt.Errorf("diagnostic has an invalid rule")
	}
	if entry.Location.Row < 1 || entry.Location.Column < 1 {
		return pythonRuffDiagnostic{}, fmt.Errorf("diagnostic %q has an invalid location", entry.Code)
	}
	message := strings.TrimSpace(entry.Message)
	if message == "" || len(message) > pythonStructuredMessageMaximumBytes {
		return pythonRuffDiagnostic{}, fmt.Errorf("diagnostic %q has an invalid message", entry.Code)
	}
	path, err := pythonQualityResultPath(repo, project, entry.Filename)
	if err != nil {
		return pythonRuffDiagnostic{}, fmt.Errorf("diagnostic %q has an invalid path: %w", entry.Code, err)
	}
	if !selected[path] {
		return pythonRuffDiagnostic{}, fmt.Errorf("diagnostic %q names an unselected file %s", entry.Code, path)
	}
	return pythonRuffDiagnostic{Code: entry.Code, Path: path, Line: entry.Location.Row, Column: entry.Location.Column, Message: message}, nil
}

func parsePythonRuffCoverage(repo repository.Repository, project repository.PythonProject, sources []string, data []byte) (map[string]bool, error) {
	if len(data) > pythonStructuredOutputMaximumBytes {
		return nil, fmt.Errorf("output exceeds %d bytes", pythonStructuredOutputMaximumBytes)
	}
	selected := pythonQualitySelectedSources(sources)
	covered := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		path, err := pythonQualityResultPath(repo, project, value)
		if err != nil {
			return nil, fmt.Errorf("covered path is invalid: %w", err)
		}
		if !selected[path] {
			return nil, fmt.Errorf("covered path names an unselected file %s", path)
		}
		if covered[path] {
			return nil, fmt.Errorf("covered path %s appears more than once", path)
		}
		covered[path] = true
	}
	return covered, nil
}

func parsePythonTyDiagnostics(repo repository.Repository, project repository.PythonProject, sources []string, data []byte) ([]pythonTyDiagnostic, error) {
	decoded, err := decodePythonTyDiagnostics(data)
	if err != nil {
		return nil, err
	}
	selected := pythonQualitySelectedSources(sources)
	diagnostics := make([]pythonTyDiagnostic, 0, len(decoded))
	for _, entry := range decoded {
		diagnostic, entryErr := pythonTyDiagnosticFromWire(repo, project, selected, entry)
		if entryErr != nil {
			return nil, entryErr
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics, nil
}

func decodePythonTyDiagnostics(data []byte) ([]pythonTyDiagnosticWire, error) {
	if len(data) > pythonStructuredOutputMaximumBytes {
		return nil, fmt.Errorf("output exceeds %d bytes", pythonStructuredOutputMaximumBytes)
	}
	decoded := []pythonTyDiagnosticWire{}
	if err := decodePythonStructuredJSON(data, &decoded); err != nil {
		return nil, err
	}
	if len(decoded) > pythonStructuredDiagnosticMaximum {
		return nil, fmt.Errorf("output contains more than %d diagnostics", pythonStructuredDiagnosticMaximum)
	}
	return decoded, nil
}

func pythonTyDiagnosticFromWire(repo repository.Repository, project repository.PythonProject, selected map[string]bool, entry pythonTyDiagnosticWire) (pythonTyDiagnostic, error) {
	rule := strings.TrimSpace(entry.CheckName)
	if rule == "" || len(rule) > 128 {
		return pythonTyDiagnostic{}, fmt.Errorf("diagnostic has an invalid rule")
	}
	message := strings.TrimSpace(entry.Description)
	if message == "" || len(message) > pythonStructuredMessageMaximumBytes {
		return pythonTyDiagnostic{}, fmt.Errorf("diagnostic %q has an invalid message", rule)
	}
	line, column, err := pythonTyDiagnosticLocation(entry, rule)
	if err != nil {
		return pythonTyDiagnostic{}, err
	}
	path, err := pythonQualityResultPath(repo, project, entry.Location.Path)
	if err != nil {
		return pythonTyDiagnostic{}, fmt.Errorf("diagnostic %q has an invalid path: %w", rule, err)
	}
	if !selected[path] {
		return pythonTyDiagnostic{}, fmt.Errorf("diagnostic %q names an unselected file %s", rule, path)
	}
	return pythonTyDiagnostic{Rule: rule, Path: path, Line: line, Column: column, Message: message}, nil
}

func pythonTyDiagnosticLocation(entry pythonTyDiagnosticWire, rule string) (int, int, error) {
	if !pythonTyDiagnosticLocationIsValid(entry) {
		return 0, 0, fmt.Errorf("diagnostic %q has an invalid location", rule)
	}
	line := entry.Location.Lines.Begin
	if entry.Location.Line > 0 {
		line = entry.Location.Line
	}
	position := entry.Location.Positions.Begin
	if position.Line > 0 {
		return position.Line, position.Column, nil
	}
	return line, 0, nil
}

func pythonTyDiagnosticLocationIsValid(entry pythonTyDiagnosticWire) bool {
	if entry.Location.Line < 0 || entry.Location.Lines.Begin < 0 {
		return false
	}
	return pythonTyDiagnosticPositionIsValid(entry.Location.Positions.Begin.Line, entry.Location.Positions.Begin.Column)
}

func pythonTyDiagnosticPositionIsValid(line, column int) bool {
	if line < 0 || column < 0 {
		return false
	}
	return line != 0 || column == 0
}

func decodePythonStructuredJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("malformed JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON has trailing data")
		}
		return fmt.Errorf("malformed trailing JSON: %w", err)
	}
	return nil
}

func pythonQualityResultPath(repo repository.Repository, project repository.PythonProject, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("path is empty")
	}
	candidate := value
	if !filepath.IsAbs(filepath.FromSlash(candidate)) && project.Root != "." {
		candidate = project.Root + "/" + candidate
	}
	path, err := repo.NormalizePath(candidate)
	if err != nil && filepath.IsAbs(filepath.FromSlash(value)) {
		path, err = pythonQualityPhysicalResultPath(repo, project, value)
	}
	if err != nil {
		return "", err
	}
	if project.Root != "." && path != project.Root && !strings.HasPrefix(path, project.Root+"/") {
		return "", fmt.Errorf("path %s escapes project root %s", path, project.Root)
	}
	return path, nil
}

func pythonQualityPhysicalResultPath(repo repository.Repository, project repository.PythonProject, value string) (string, error) {
	physicalPath, err := filepath.EvalSymlinks(filepath.FromSlash(value))
	if err != nil {
		return "", err
	}
	physicalManifest, err := repo.Resolve(project.Manifest)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(filepath.Dir(physicalManifest), physicalPath)
	if err != nil {
		return "", err
	}
	relative = filepath.ToSlash(filepath.Clean(relative))
	if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("path escapes project root %s", project.Root)
	}
	if project.Root != "." {
		relative = project.Root + "/" + relative
	}
	return repo.NormalizePath(relative)
}

func pythonQualitySelectedSources(sources []string) map[string]bool {
	selected := map[string]bool{}
	for _, source := range sources {
		selected[source] = true
	}
	return selected
}

func pythonRuffBaselineDiagnostics(diagnostics []pythonRuffDiagnostic) bool {
	for _, diagnostic := range diagnostics {
		if pythonRuffBaselineDiagnostic(diagnostic.Code) {
			continue
		}
		return false
	}
	return true
}

func pythonRuffBaselineDiagnostic(code string) bool {
	for _, prefix := range []string{"B", "C4", "E", "F", "I", "PIE", "RUF", "SIM", "UP"} {
		if pythonRuffDiagnosticFamily(code, prefix) {
			return true
		}
	}
	return false
}

func pythonRuffDiagnosticFamily(code, prefix string) bool {
	if !strings.HasPrefix(code, prefix) || len(code) == len(prefix) {
		return false
	}
	for _, character := range code[len(prefix):] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func pythonRuffComplexityDiagnostics(diagnostics []pythonRuffDiagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != pythonRuffComplexitySelection {
			return false
		}
	}
	return true
}

func pythonTySubject(diagnostic pythonTyDiagnostic) string {
	identity := strings.Join([]string{
		diagnostic.Rule, diagnostic.Path, strconv.Itoa(diagnostic.Line), strconv.Itoa(diagnostic.Column), diagnostic.Message,
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return diagnostic.Rule + ":" + hex.EncodeToString(digest[:])
}

func pythonDiagnosticMessage(line, column int, message string) string {
	if line == 0 {
		return message
	}
	if column > 0 {
		return fmt.Sprintf("line %d, column %d: %s", line, column, message)
	}
	return fmt.Sprintf("line %d: %s", line, message)
}

func pythonQualityCoverage(sources []string, check, subject, message string) []policy.Finding {
	findings := make([]policy.Finding, 0, len(sources))
	for _, source := range sources {
		findings = append(findings, policy.Finding{Check: check, Path: source, Subject: subject, Message: message})
	}
	return findings
}

func pythonQualityToolFinding(project repository.PythonProject, kind pythonQualityKind, message string) policy.Finding {
	tool := "ruff"
	if kind == pythonVultureQualityKind {
		tool = "vulture"
	}
	if kind == pythonTyQualityKind {
		tool = "ty"
	}
	return policy.Finding{Check: "policy.tool", Path: project.Manifest, Subject: tool, Message: message}
}

func uniquePythonQualityFindings(findings []policy.Finding) []policy.Finding {
	seen := map[string]bool{}
	unique := []policy.Finding{}
	for _, finding := range findings {
		key := strings.Join([]string{
			finding.Check, finding.Path, strconv.Itoa(finding.Line), strconv.Itoa(finding.Column), finding.Subject, finding.Message,
		}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, finding)
	}
	sort.Slice(unique, func(left, right int) bool {
		leftFinding := unique[left]
		rightFinding := unique[right]
		return strings.Join([]string{
			leftFinding.Check, leftFinding.Path, strconv.Itoa(leftFinding.Line), strconv.Itoa(leftFinding.Column), leftFinding.Subject, leftFinding.Message,
		}, "\x00") < strings.Join([]string{
			rightFinding.Check, rightFinding.Path, strconv.Itoa(rightFinding.Line), strconv.Itoa(rightFinding.Column), rightFinding.Subject, rightFinding.Message,
		}, "\x00")
	})
	return unique
}
