package engine

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/supplychain"
)

const ReportProtocol = "code-polishy-report/v1"

const ReportSchemaURL = "https://code-polishy.dev/schema/code-polishy-report.schema.json"

func advisoryFindings(advisories []policy.Advisory) []policy.Finding {
	findings := make([]policy.Finding, 0, len(advisories))
	for _, advisory := range advisories {
		findings = append(findings, policy.Finding{
			Check: advisory.Check, Path: advisory.Path, Subject: advisory.Subject, Message: advisory.Message,
			Severity: policy.FindingWarning,
		})
	}
	return findings
}

func (engine *Engine) withSelection(report Report, selection repository.Selection) Report {
	requested := selection.Requested
	requested.Operands = slices.Clone(requested.Operands)
	requested.Modules = slices.Clone(requested.Modules)
	requested.Expanded = slices.Clone(requested.Expanded)
	report.RequestedSelection = &requested
	report.AnalysisContext = engine.analysisContexts(selection, report.Findings)
	return engine.normalizeReport(report)
}

func (engine *Engine) normalizeReport(report Report) Report {
	report.Schema = ReportSchemaURL
	report.Protocol = ReportProtocol
	engine.enrichGeneratedReport(&report)
	if report.RequestedSelection != nil {
		report.RequestedSelection.Operands = reportArray(report.RequestedSelection.Operands)
		report.RequestedSelection.Expanded = reportArray(report.RequestedSelection.Expanded)
	}
	report.Findings = engine.normalizeFindings(report.Findings, report.RequestedSelection)
	sortFindings(report.Findings)
	report.Findings = compactFindings(report.Findings)
	for index := range report.Suppressed {
		report.Suppressed[index].Finding = engine.normalizeFinding(report.Suppressed[index].Finding, report.RequestedSelection)
	}
	for index := range report.Assessed {
		report.Assessed[index].Finding = engine.normalizeFinding(report.Assessed[index].Finding, report.RequestedSelection)
	}
	for index := range report.ReleaseAges {
		report.ReleaseAges[index].Finding = engine.normalizeFinding(report.ReleaseAges[index].Finding, report.RequestedSelection)
	}
	coalesceReportOutcomes(&report)
	report.AnalysisContext = reportArray(report.AnalysisContext)
	report.TestCommands = reportArray(report.TestCommands)
	report.TestDiagnostics = reportArray(report.TestDiagnostics)
	report.TestAggregations = reportArray(report.TestAggregations)
	report.Findings = reportArray(report.Findings)
	report.Suppressed = reportArray(report.Suppressed)
	report.Assessed = reportArray(report.Assessed)
	report.ReleaseAges = reportArray(report.ReleaseAges)
	report.GitEvidence = normalizeGitEvidence(report.GitEvidence)
	report.Tables = reportArray(report.Tables)
	report.Notes = reportArray(report.Notes)
	report.Summary = summarizeReport(report)
	return report
}

func normalizeGitEvidence(receipts []supplychain.GitEvidenceReceipt) []supplychain.GitEvidenceReceipt {
	ordered := slices.Clone(receipts)
	slices.SortFunc(ordered, func(left, right supplychain.GitEvidenceReceipt) int {
		if order := strings.Compare(left.Scope+"\x00"+left.Path+"\x00"+left.SHA256, right.Scope+"\x00"+right.Path+"\x00"+right.SHA256); order != 0 {
			return order
		}
		return right.RetrievedAt.Compare(left.RetrievedAt)
	})
	return slices.CompactFunc(ordered, func(left, right supplychain.GitEvidenceReceipt) bool {
		left.RetrievedAt, right.RetrievedAt = time.Time{}, time.Time{}
		return left == right
	})
}

func reportArray[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

func (engine *Engine) normalizeFindings(findings []policy.Finding, requested *repository.RequestedSelection) []policy.Finding {
	for index := range findings {
		findings[index] = engine.normalizeFinding(findings[index], requested)
	}
	return findings
}

func (engine *Engine) normalizeFinding(finding policy.Finding, requested *repository.RequestedSelection) policy.Finding {
	if finding.Module == "" && finding.Path != "" && finding.Path != "repository" && finding.Path != policy.ConfigFilename {
		owners := engine.Repository.OwnerModuleNames(finding.Path)
		if len(owners) == 1 {
			finding.Module = owners[0]
		}
	}
	if requested != nil {
		finding.SelectionRelation = findingRelation(finding, *requested)
	}
	return policy.NormalizeFinding(finding)
}

func findingRelation(finding policy.Finding, requested repository.RequestedSelection) policy.SelectionRelation {
	if finding.Scope.Kind == "repository" || finding.Path == "" || finding.Path == "repository" || finding.Path == policy.ConfigFilename {
		return policy.SelectionGlobal
	}
	if slices.Contains(requested.Expanded, finding.Path) || finding.Module != "" && slices.Contains(requested.Modules, finding.Module) {
		return policy.SelectionSelected
	}
	if len(finding.SelectionEvidence) > 0 {
		return policy.SelectionRelated
	}
	return policy.SelectionContext
}

func summarizeReport(report Report) ReportSummary {
	summary := ReportSummary{
		Status: "passed", ByRelation: map[policy.SelectionRelation]int{}, ByRule: map[string]int{}, ByModule: map[string]int{},
	}
	for _, finding := range report.Findings {
		summaryFinding(&summary, finding)
	}
	for _, suppressed := range report.Suppressed {
		summary.Suppressed++
		summaryOutcome(&summary, suppressed.Finding)
	}
	for _, assessed := range report.Assessed {
		summary.Reviewed++
		summaryOutcome(&summary, assessed.Finding)
	}
	for _, assessed := range report.ReleaseAges {
		summary.Reviewed++
		summaryOutcome(&summary, assessed.Finding)
	}
	if summary.Errors > 0 || report.GateRunPolicy != nil && report.GateRunPolicy.Status == "failed" {
		summary.Status = "failed"
	} else if report.BehaviorReview != nil && report.BehaviorReview.State == BehaviorReviewRequired {
		summary.Status = "review-required"
	}
	return summary
}

func summaryFinding(summary *ReportSummary, finding policy.Finding) {
	if finding.Status == policy.FindingReviewed {
		summary.Reviewed++
	}
	switch finding.Severity {
	case policy.FindingWarning:
		summary.Warnings++
	case policy.FindingInformation:
		summary.Information++
	default:
		summary.Errors++
	}
	summaryOutcome(summary, finding)
}

func summaryOutcome(summary *ReportSummary, finding policy.Finding) {
	summary.ByRelation[finding.SelectionRelation]++
	summary.ByRule[finding.Check]++
	if finding.Module != "" {
		summary.ByModule[finding.Module]++
	}
}

func (engine *Engine) analysisContexts(selection repository.Selection, findings []policy.Finding) []AnalysisContext {
	if selection.Requested.Mode == "all" {
		return nil
	}
	allFiles, err := engine.Repository.AllFiles()
	if err != nil {
		return nil
	}
	selected := make(map[string]bool, len(selection.Requested.Expanded))
	for _, path := range selection.Requested.Expanded {
		selected[path] = true
	}
	contexts := pythonAnalysisContexts(engine.Repository, allFiles, selected)
	contexts = append(contexts, javascriptAnalysisContexts(allFiles, selected)...)
	contexts = appendFindingContext(contexts, findings, selected)
	sort.Slice(contexts, func(left, right int) bool {
		return contexts[left].Analyzer+"\x00"+contexts[left].Root < contexts[right].Analyzer+"\x00"+contexts[right].Root
	})
	return contexts
}

func pythonAnalysisContexts(repo repository.Repository, allFiles []string, selected map[string]bool) []AnalysisContext {
	contexts := []AnalysisContext{}
	inventory := repo.PythonProjectInventory(allFiles)
	for _, project := range inventory.Projects {
		if !pathsIntersect(project.Files, selected) {
			continue
		}
		paths := unselectedPaths(project.Files, selected)
		if len(paths) > 0 {
			contexts = append(contexts, AnalysisContext{
				Analyzer: "python-project", Root: project.Root, Reason: "project-wide Python analysis", Paths: paths,
			})
		}
	}
	return contexts
}

func javascriptAnalysisContexts(allFiles []string, selected map[string]bool) []AnalysisContext {
	roots := javascriptContextRoots(allFiles)
	groups := map[string][]string{}
	for path := range selected {
		if !javascriptContextSource(path) {
			continue
		}
		root := nearestContextRoot(path, roots)
		if root != "" {
			groups[root] = nil
		}
	}
	for _, path := range allFiles {
		root := nearestContextRoot(path, roots)
		if _, found := groups[root]; found && javascriptContextSource(path) && !selected[path] {
			groups[root] = append(groups[root], path)
		}
	}
	contexts := make([]AnalysisContext, 0, len(groups))
	for root, paths := range groups {
		if len(paths) > 0 {
			contexts = append(contexts, AnalysisContext{
				Analyzer: "javascript-package", Root: root, Reason: "package-wide JavaScript analysis", Paths: paths,
			})
		}
	}
	return contexts
}

func javascriptContextRoots(allFiles []string) []string {
	roots := []string{}
	for _, path := range allFiles {
		if filepath.Base(path) == "package.json" {
			roots = append(roots, filepath.ToSlash(filepath.Dir(path)))
		}
	}
	sort.Slice(roots, func(left, right int) bool { return len(roots[left]) > len(roots[right]) })
	return roots
}

func appendFindingContext(contexts []AnalysisContext, findings []policy.Finding, selected map[string]bool) []AnalysisContext {
	covered := map[string]bool{}
	for _, context := range contexts {
		for _, path := range context.Paths {
			covered[path] = true
		}
	}
	paths := []string{}
	for _, finding := range findings {
		if finding.Path != "" && finding.Path != "repository" && finding.Path != policy.ConfigFilename && !selected[finding.Path] && !covered[finding.Path] {
			paths = append(paths, finding.Path)
		}
	}
	paths = uniqueReportPaths(paths)
	if len(paths) > 0 {
		contexts = append(contexts, AnalysisContext{
			Analyzer: "policy-context", Root: ".", Reason: "sound policy evaluation required unselected context", Paths: paths,
		})
	}
	return contexts
}

func pathsIntersect(paths []string, selected map[string]bool) bool {
	return slices.ContainsFunc(paths, func(path string) bool { return selected[path] })
}

func unselectedPaths(paths []string, selected map[string]bool) []string {
	result := []string{}
	for _, path := range paths {
		if !selected[path] {
			result = append(result, path)
		}
	}
	return uniqueReportPaths(result)
}

func nearestContextRoot(path string, roots []string) string {
	for _, root := range roots {
		if root == "." || strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/") {
			return root
		}
	}
	return ""
}

func javascriptContextSource(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cjs", ".cts", ".js", ".jsx", ".mjs", ".mts", ".ts", ".tsx":
		return true
	default:
		return false
	}
}

func uniqueReportPaths(paths []string) []string {
	sort.Strings(paths)
	return slices.Compact(paths)
}
