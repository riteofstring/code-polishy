package testing

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type Request struct {
	Full         bool
	Recommended  bool
	Supplemental bool
	Modules      []string
	Suites       []string
	Changed      repository.Selection
}

type Plan struct {
	Suites          []policy.TestSuite
	ChangedModules  []string
	ImpactedModules []string
	SelectionBase   string
	Level           string
	Reasons         []string
}

type Advice struct {
	ChangedModules  []string
	ImpactedModules []string
	Focused         []policy.TestSuite
	Recommended     []policy.TestSuite
	Full            []policy.TestSuite
	AllSupplemental []policy.TestSuite
	Supplemental    []policy.TestSuite
	Suggested       string
	Reasons         []string
}

const (
	MergeLevelDocumentation = "documentation"
	MergeLevelRecommended   = "recommended"
	MergeLevelFull          = "full"
)

type MergeDecision struct {
	Level   string
	Reasons []string
}

func BuildPlan(repo repository.Repository, request Request) (Plan, error) {
	if plan, selected := documentationPlan(repo, request); selected {
		return plan, nil
	}
	if request.Supplemental {
		return Plan{
			Suites: selectByRunOn(repo.Config.Tests.Suites, "supplemental"), Level: "supplemental",
			Reasons: []string{"direct supplemental selection"},
		}, nil
	}
	if request.Full {
		return Plan{
			Suites: selectByRunOn(repo.Config.Tests.Suites, "full"), Level: "full",
			Reasons: []string{"direct full-profile selection"},
		}, nil
	}
	if len(request.Suites) > 0 {
		plan, err := explicitSuitePlan(repo.Config.Tests.Suites, request.Suites)
		if err != nil {
			return Plan{}, err
		}
		plan.Level = "focused"
		plan.Reasons = []string{"direct named-suite selection"}
		return plan, nil
	}
	direct := directModules(repo, request)
	direct = uniqueStrings(direct)
	if err := validateModuleNames(repo.Config, direct); err != nil {
		return Plan{}, err
	}
	impacted := append([]string{}, direct...)
	if len(request.Modules) == 0 {
		impacted = reverseClosure(repo.Config, direct)
	}
	changedPaths := analysisPaths(request.Changed)
	profile := "focused"
	reason := "direct focused selection"
	if request.Recommended {
		profile = "recommended"
		reason = "direct recommended-profile selection"
	}
	if len(request.Modules) > 0 {
		reason = "direct module-focused selection"
	}
	selected := suitesForProfile(repo.Config.Tests.Suites, profile, impacted, changedPaths)
	return Plan{
		Suites: uniqueSuites(selected), ChangedModules: direct, ImpactedModules: impacted,
		SelectionBase: request.Changed.Base, Level: profile, Reasons: []string{reason},
	}, nil
}

func documentationPlan(repo repository.Repository, request Request) (Plan, bool) {
	if request.Full || request.Recommended || request.Supplemental || len(request.Modules) > 0 || len(request.Suites) > 0 {
		return Plan{}, false
	}
	classification := repo.ClassifyDocumentationCandidate(request.Changed)
	if !classification.Ordinary {
		return Plan{}, false
	}
	return Plan{
		Level: MergeLevelDocumentation, SelectionBase: request.Changed.Base, Reasons: []string{classification.Reason},
	}, true
}

func BuildAdvice(repo repository.Repository, selection repository.Selection) (Advice, error) {
	focused, err := BuildPlan(repo, Request{Changed: selection})
	if err != nil {
		return Advice{}, err
	}
	recommended, err := BuildPlan(repo, Request{Recommended: true, Changed: selection})
	if err != nil {
		return Advice{}, err
	}
	full, err := BuildPlan(repo, Request{Full: true})
	if err != nil {
		return Advice{}, err
	}
	allSupplemental, err := BuildPlan(repo, Request{Supplemental: true})
	if err != nil {
		return Advice{}, err
	}
	changedPaths := analysisPaths(selection)
	supplemental := suitesForProfile(repo.Config.Tests.Suites, "supplemental", recommended.ImpactedModules, changedPaths)
	suggested, reasons := recommendScope(repo.Config, selection, recommended)
	return Advice{
		ChangedModules: recommended.ChangedModules, ImpactedModules: recommended.ImpactedModules,
		Focused: focused.Suites, Recommended: recommended.Suites, Full: full.Suites,
		AllSupplemental: allSupplemental.Suites, Supplemental: uniqueSuites(supplemental),
		Suggested: suggested, Reasons: reasons,
	}, nil
}

func BuildMergeDecision(repo repository.Repository, selection repository.Selection) (MergeDecision, error) {
	classification := repo.ClassifyDocumentationCandidate(selection)
	if classification.Ordinary {
		return MergeDecision{Level: MergeLevelDocumentation, Reasons: []string{classification.Reason}}, nil
	}
	advice, err := BuildAdvice(repo, selection)
	if err != nil {
		return MergeDecision{}, err
	}
	mergeGate := repo.Config.Verification.MergeGate
	if mergeGate == nil {
		return MergeDecision{
			Level: MergeLevelFull, Reasons: []string{"adaptive merge verification is not configured"},
		}, nil
	}
	if selection.All {
		return MergeDecision{Level: MergeLevelFull, Reasons: append([]string{}, advice.Reasons...)}, nil
	}
	allowed := map[string]bool{}
	for _, module := range mergeGate.RecommendedModules {
		allowed[module] = true
	}
	changedPaths := selection.Candidate.Paths()
	for _, path := range changedPaths {
		modules := repo.ModuleNames(path)
		if len(modules) != 1 {
			return MergeDecision{
				Level:   MergeLevelFull,
				Reasons: []string{fmt.Sprintf("changed path %q belongs to %d modules; recommended merge verification requires exactly one", path, len(modules))},
			}, nil
		}
		if !allowed[modules[0]] {
			return MergeDecision{
				Level:   MergeLevelFull,
				Reasons: []string{fmt.Sprintf("changed module %q is not configured for recommended merge verification", modules[0])},
			}, nil
		}
	}
	if advice.Suggested == MergeLevelFull {
		return MergeDecision{Level: MergeLevelFull, Reasons: append([]string{}, advice.Reasons...)}, nil
	}
	reasons := []string{}
	if len(changedPaths) > 0 {
		reasons = append(reasons, "every changed path belongs to exactly one module configured for recommended merge verification")
	}
	reasons = append(reasons, advice.Reasons...)
	return MergeDecision{Level: MergeLevelRecommended, Reasons: reasons}, nil
}

func explicitSuitePlan(suites []policy.TestSuite, names []string) (Plan, error) {
	selected := []policy.TestSuite{}
	for _, name := range names {
		index := slices.IndexFunc(suites, func(suite policy.TestSuite) bool { return suite.Name == name })
		if index < 0 {
			return Plan{}, fmt.Errorf("unknown test suite %q", name)
		}
		selected = append(selected, suites[index])
	}
	return Plan{Suites: uniqueSuites(selected)}, nil
}

func directModules(repo repository.Repository, request Request) []string {
	if len(request.Modules) > 0 {
		return append([]string{}, request.Modules...)
	}
	direct := []string{}
	for _, path := range analysisPaths(request.Changed) {
		direct = append(direct, repo.ModuleNames(path)...)
	}
	return direct
}

func validateModuleNames(config policy.Config, names []string) error {
	for _, name := range names {
		if _, exists := config.ModuleByName[name]; !exists {
			return fmt.Errorf("unknown module %q", name)
		}
	}
	return nil
}

func suitesForProfile(suites []policy.TestSuite, profile string, impacted, changedPaths []string) []policy.TestSuite {
	selected := []policy.TestSuite{}
	for _, suite := range suites {
		if !slices.Contains(suite.RunOn, profile) {
			continue
		}
		moduleMatch := suite.Scope == "module" && intersects(suite.Modules, impacted)
		repositoryMatch := suite.Scope == "repository" && pathsMatch(changedPaths, suite.Paths)
		if moduleMatch || repositoryMatch {
			selected = append(selected, suite)
		}
	}
	return selected
}

func recommendScope(config policy.Config, selection repository.Selection, plan Plan) (string, []string) {
	changedPaths := analysisPaths(selection)
	if len(changedPaths) == 0 {
		return "recommended", []string{"no governed changes were detected, so no broader test run is warranted"}
	}
	if selection.All {
		return "full", []string{"impact expanded to the whole repository because this is a new repository or the change includes a deletion or policy, dependency, workflow, or container input"}
	}
	if len(changedPaths) >= 20 {
		return "full", []string{fmt.Sprintf("the change spans %d governed paths", len(changedPaths))}
	}
	moduleCount := len(config.Modules)
	if moduleCount >= 3 && len(plan.ImpactedModules)*3 >= moduleCount*2 {
		return "full", []string{fmt.Sprintf("the change impacts %d of %d modules through the dependency graph", len(plan.ImpactedModules), moduleCount)}
	}
	if len(plan.ChangedModules) >= 3 {
		return "full", []string{fmt.Sprintf("the change directly crosses %d module boundaries", len(plan.ChangedModules))}
	}
	return "recommended", []string{fmt.Sprintf("the change is limited to %d governed paths and impacts %d of %d modules", len(changedPaths), len(plan.ImpactedModules), moduleCount)}
}

func analysisPaths(selection repository.Selection) []string {
	return uniqueStrings(append(append([]string{}, selection.Files...), selection.Candidate.Deleted...))
}

func Run(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, plan Plan, reporter *ExecutionReporter) []policy.Finding {
	return run(ctx, repo, commandRunner, plan, reporter, false)
}

func RunUntilFailure(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, plan Plan, reporter *ExecutionReporter) []policy.Finding {
	return run(ctx, repo, commandRunner, plan, reporter, true)
}

func run(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, plan Plan, reporter *ExecutionReporter, stopAfterFailure bool) []policy.Finding {
	findings := []policy.Finding{}
	for index, suite := range plan.Suites {
		command := policy.Command{
			Name: suite.Name, Argv: suite.Argv, Cwd: suite.Cwd,
			Paths: suite.Paths, Modules: suite.Modules, Environment: suite.Environment,
			ExclusiveResources: suite.ExclusiveResources, TimeoutSeconds: suite.TimeoutSeconds,
		}
		runContext := ctx
		if reporter != nil {
			reporter.CommandWaiting(index)
			runContext = runner.WithResourceWaitObserver(ctx, func(elapsed time.Duration) {
				reporter.CommandResourceWaiting(index, elapsed)
			})
		}
		result, resourceWaitKnown, err := runSuite(runContext, repo.Root, commandRunner, command)
		if reporter != nil {
			status := executionStatusPassed
			if err != nil {
				status = executionStatusFailed
			}
			reporter.CommandFinished(index, ExecutionResult{
				Status: status, ExecutionDuration: result.ExecutionDuration,
				ResourceWait: result.ResourceWait, ResourceWaitKnown: resourceWaitKnown,
			})
		}
		if err != nil {
			findings = append(findings, policy.Finding{Check: "test." + suite.Kind, Path: "repository", Subject: suite.Name, Message: err.Error()})
			if stopAfterFailure {
				return findings
			}
		}
	}
	return findings
}

type resultRunner interface {
	RunWithResult(context.Context, string, policy.Command) (runner.Result, error)
}

func runSuite(ctx context.Context, root string, commandRunner runner.Runner, command policy.Command) (runner.Result, bool, error) {
	if observed, ok := commandRunner.(resultRunner); ok {
		result, err := observed.RunWithResult(ctx, root, command)
		return result, true, err
	}
	started := time.Now()
	err := commandRunner.Run(ctx, root, command)
	return runner.Result{ExecutionDuration: time.Since(started)}, false, err
}

func ExecutionPlanForDirectTest(plan Plan) ExecutionPlan {
	commands := make([]ExecutionCommand, 0, len(plan.Suites))
	for _, suite := range plan.Suites {
		commands = append(commands, ExecutionCommand{
			Name: suite.Name, Kind: suite.Kind, Scope: suite.Scope, Cost: suite.Cost,
			Argv: append([]string{}, suite.Argv...), Cwd: suite.Cwd,
			ExclusiveResources: append([]string{}, suite.ExclusiveResources...), TimeoutSeconds: suite.TimeoutSeconds,
		})
	}
	trustedBase := plan.SelectionBase
	if trustedBase == "" {
		trustedBase = "not-applicable"
	}
	return ExecutionPlan{
		Stage: "direct", Level: plan.Level, TrustedBase: trustedBase, Candidate: "working-tree",
		Reasons: append([]string{}, plan.Reasons...), ImpactedModules: append([]string{}, plan.ImpactedModules...), Commands: commands,
	}
}

func NewDirectExecutionReporter(output io.Writer, plan Plan, verbose bool) *ExecutionReporter {
	return newExecutionReporter(output, ExecutionPlanForDirectTest(plan), verbose)
}

func CoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	config := repo.Config
	findings := focusedCoverageFindings(config)
	findings = append(findings, fullRepositoryCoverageFindings(config.Tests.Suites)...)
	findings = append(findings, capabilityCoverageFindings(config)...)
	findings = append(findings, conditionalModuleCoverageFindings(config)...)
	findings = append(findings, requiredKindFindings(config)...)
	findings = append(findings, requiredSupplementalKindFindings(config)...)
	findings = append(findings, gherkinCoverageFindings(config.Tests.Suites, files)...)
	return findings
}

func Notes(_ repository.Repository, files []string) []string {
	notes := []string{}
	if hasGherkinFile(files) {
		notes = append(notes, "conditional executable-specification policy: governed .feature files")
	}
	return notes
}

func focusedCoverageFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, module := range config.Modules {
		if !moduleHasFocusedSuite(config.Tests.Suites, module.Name) {
			findings = append(findings, policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: module.Name, Message: "module has no focused boundary test suite"})
		}
	}
	return findings
}

func moduleHasFocusedSuite(suites []policy.TestSuite, module string) bool {
	for _, suite := range suites {
		if suite.Scope == "module" && slices.Contains(suite.RunOn, "focused") && slices.Contains(suite.Modules, module) {
			return true
		}
	}
	return false
}

func fullRepositoryCoverageFindings(suites []policy.TestSuite) []policy.Finding {
	for _, suite := range suites {
		if suite.Scope == "repository" && slices.Contains(suite.RunOn, "full") {
			return nil
		}
	}
	return []policy.Finding{{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: "full", Message: "at least one repository-scoped full suite is required"}}
}

func capabilityCoverageFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, capability := range config.Project.Capabilities {
		kinds, expected := capabilityRequirement(capability)
		if len(kinds) > 0 && !hasFullRepositoryKind(config.Tests.Suites, kinds...) {
			findings = append(findings, capabilityFinding(capability, expected))
		}
	}
	return findings
}

func conditionalModuleCoverageFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, module := range config.ActivePolicyModules {
		var kinds []string
		var expected string
		switch module.Name {
		case "react":
			kinds, expected = []string{"component", "browser", "e2e"}, "component, browser, or e2e"
		case "electron":
			kinds, expected = []string{"electron", "browser", "e2e"}, "Electron, browser, or e2e"
		default:
			continue
		}
		if !hasFullRepositoryKind(config.Tests.Suites, kinds...) {
			subject := module.Name + ":" + module.Root
			message := fmt.Sprintf("detected %s policy at %s requires a full repository-scoped %s suite", module.Name, module.Root, expected)
			findings = append(findings, policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: subject, Message: message})
		}
	}
	return findings
}

func hasFullRepositoryKind(suites []policy.TestSuite, kinds ...string) bool {
	for _, suite := range suites {
		if suite.Scope == "repository" && slices.Contains(kinds, suite.Kind) && slices.Contains(suite.RunOn, "full") {
			return true
		}
	}
	return false
}

func capabilityRequirement(capability string) ([]string, string) {
	requirements := map[string]struct {
		kinds    []string
		expected string
	}{
		"backend":  {[]string{"integration", "contract", "e2e"}, "integration, contract, or e2e"},
		"frontend": {[]string{"component", "browser", "e2e"}, "component, browser, or e2e"},
		"ui":       {[]string{"browser", "e2e"}, "browser or e2e"},
		"visual":   {[]string{"visual"}, "visual"},
		"content":  {[]string{"content", "contract"}, "content or contract"},
	}
	requirement := requirements[capability]
	return requirement.kinds, requirement.expected
}

func requiredKindFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, kind := range uniqueStrings(config.Tests.RequiredKinds) {
		if !hasFullKind(config.Tests.Suites, kind) {
			findings = append(findings, policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: kind, Message: fmt.Sprintf("required test kind %q has no full suite", kind)})
		}
	}
	return findings
}

func requiredSupplementalKindFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, kind := range uniqueStrings(config.Tests.RequiredSupplementalKinds) {
		if !hasSupplementalKind(config.Tests.Suites, kind) {
			message := fmt.Sprintf("required supplemental test kind %q has no supplemental suite", kind)
			findings = append(findings, policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: kind, Message: message})
		}
	}
	return findings
}

func gherkinCoverageFindings(suites []policy.TestSuite, files []string) []policy.Finding {
	if !hasGherkinFile(files) {
		return nil
	}
	findings := []policy.Finding{}
	if !hasFullRepositoryKind(suites, "acceptance", "gherkin") {
		findings = append(findings, policy.Finding{Check: "policy.testStrength", Path: policy.ConfigFilename, Subject: "gherkin:acceptance", Message: "governed .feature files must generate and execute a full repository acceptance suite"})
	}
	return findings
}

func hasGherkinFile(files []string) bool {
	for _, path := range files {
		if strings.EqualFold(filepath.Ext(path), ".feature") {
			return true
		}
	}
	return false
}

func hasSupplementalKind(suites []policy.TestSuite, kinds ...string) bool {
	for _, suite := range suites {
		if slices.Contains(kinds, suite.Kind) && slices.Contains(suite.RunOn, "supplemental") {
			return true
		}
	}
	return false
}

func capabilityFinding(capability, expected string) policy.Finding {
	return policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: capability, Message: fmt.Sprintf("project capability %q requires a full %s suite", capability, expected)}
}

func hasFullKind(suites []policy.TestSuite, kinds ...string) bool {
	for _, suite := range suites {
		if slices.Contains(kinds, suite.Kind) && slices.Contains(suite.RunOn, "full") {
			return true
		}
	}
	return false
}

func reverseClosure(config policy.Config, roots []string) []string {
	selected := map[string]bool{}
	queue := append([]string{}, roots...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if selected[name] {
			continue
		}
		selected[name] = true
		for _, module := range config.Modules {
			if slices.Contains(module.DependsOn, name) {
				queue = append(queue, module.Name)
			}
		}
	}
	result := make([]string, 0, len(selected))
	for name := range selected {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func pathsMatch(paths, patterns []string) bool {
	if len(patterns) == 0 {
		return len(paths) > 0
	}
	for _, path := range paths {
		if policy.MatchesAny(path, patterns) {
			return true
		}
	}
	return false
}

func intersects(left, right []string) bool {
	for _, value := range left {
		if slices.Contains(right, value) {
			return true
		}
	}
	return false
}

func selectByRunOn(suites []policy.TestSuite, profile string) []policy.TestSuite {
	selected := []policy.TestSuite{}
	for _, suite := range suites {
		if slices.Contains(suite.RunOn, profile) {
			selected = append(selected, suite)
		}
	}
	return selected
}

func uniqueSuites(suites []policy.TestSuite) []policy.TestSuite {
	result := []policy.TestSuite{}
	seen := map[string]bool{}
	for _, suite := range suites {
		if !seen[suite.Name] {
			seen[suite.Name] = true
			result = append(result, suite)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
