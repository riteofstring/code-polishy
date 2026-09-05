package engine

import (
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const (
	DefaultFindingDisplayLimit = 20
	MaximumFindingDisplayLimit = 100
)

func PrepareReportDisplay(report Report, options DisplayOptions) Report {
	options = normalizedDisplayOptions(options)
	displayed := filteredReport(report, options.Filters)
	report.Display = &ReportDisplay{
		Filters: options.Filters, GroupBy: options.GroupBy, Limit: options.Limit,
		Total: reportOccurrenceCount(report), Displayed: reportOccurrenceCount(displayed),
	}
	report.Display.Omitted = report.Display.Total - report.Display.Displayed
	return report
}

func ReportView(report Report, options DisplayOptions) Report {
	options = normalizedDisplayOptions(options)
	view := filteredReport(report, options.Filters)
	orderReportFindings(view.Findings, options.GroupBy)
	view.Display = &ReportDisplay{
		Filters: options.Filters, GroupBy: options.GroupBy, Limit: options.Limit,
		Total: reportOccurrenceCount(report), Displayed: reportOccurrenceCount(view),
	}
	view.Display.Omitted = view.Display.Total - view.Display.Displayed
	return view
}

func normalizedDisplayOptions(options DisplayOptions) DisplayOptions {
	if options.Limit <= 0 {
		options.Limit = DefaultFindingDisplayLimit
	}
	if options.Limit > MaximumFindingDisplayLimit {
		options.Limit = MaximumFindingDisplayLimit
	}
	options.Filters.Rules = reportSortedUniqueStrings(options.Filters.Rules)
	options.Filters.Modules = reportSortedUniqueStrings(options.Filters.Modules)
	options.Filters.Paths = reportSortedUniqueStrings(options.Filters.Paths)
	sort.Slice(options.Filters.Relations, func(left, right int) bool {
		return options.Filters.Relations[left] < options.Filters.Relations[right]
	})
	options.Filters.Relations = slices.Compact(options.Filters.Relations)
	return options
}

func filteredReport(report Report, filters ReportFilters) Report {
	report.Findings = filterFindings(report.Findings, filters)
	report.Suppressed = slices.DeleteFunc(slices.Clone(report.Suppressed), func(item policy.Suppressed) bool {
		return !findingMatchesFilters(item.Finding, filters)
	})
	report.Assessed = slices.DeleteFunc(slices.Clone(report.Assessed), func(item policy.AssessedVulnerability) bool {
		return !findingMatchesFilters(item.Finding, filters)
	})
	report.ReleaseAges = slices.DeleteFunc(slices.Clone(report.ReleaseAges), func(item policy.AssessedReleaseAge) bool {
		return !findingMatchesFilters(item.Finding, filters)
	})
	return report
}

func filterFindings(findings []policy.Finding, filters ReportFilters) []policy.Finding {
	return slices.DeleteFunc(slices.Clone(findings), func(finding policy.Finding) bool {
		return !findingMatchesFilters(finding, filters)
	})
}

func findingMatchesFilters(finding policy.Finding, filters ReportFilters) bool {
	return matchesExactFilter(finding.Check, filters.Rules) && matchesExactFilter(finding.Module, filters.Modules) &&
		matchesPathFilter(finding.Path, filters.Paths) && matchesRelationFilter(finding.SelectionRelation, filters.Relations)
}

func matchesExactFilter(value string, filters []string) bool {
	return len(filters) == 0 || slices.Contains(filters, value)
}

func matchesPathFilter(value string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	return slices.ContainsFunc(filters, func(filter string) bool {
		return value == filter || policy.Match(value, filter)
	})
}

func matchesRelationFilter(value policy.SelectionRelation, filters []policy.SelectionRelation) bool {
	return len(filters) == 0 || slices.Contains(filters, value)
}

func orderReportFindings(findings []policy.Finding, groupBy string) {
	sort.SliceStable(findings, func(left, right int) bool {
		leftKey := reportGroupKey(findings[left], groupBy)
		rightKey := reportGroupKey(findings[right], groupBy)
		if leftKey == rightKey {
			return findingKey(findings[left]) < findingKey(findings[right])
		}
		return leftKey < rightKey
	})
}

func reportGroupKey(finding policy.Finding, groupBy string) string {
	switch groupBy {
	case "rule":
		return finding.Check
	case "module":
		return finding.Module
	case "path":
		return finding.Path
	case "relation":
		return string(finding.SelectionRelation)
	default:
		return ""
	}
}

func reportOccurrenceCount(report Report) int {
	return len(report.Findings) + len(report.Suppressed) + len(report.Assessed) + len(report.ReleaseAges)
}

func reportSortedUniqueStrings(values []string) []string {
	values = slices.Clone(values)
	for index := range values {
		values[index] = strings.TrimSpace(values[index])
	}
	sort.Strings(values)
	return slices.Compact(values)
}
