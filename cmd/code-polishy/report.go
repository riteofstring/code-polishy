package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func printFindings(stdout, stderr io.Writer, findings []policy.Finding) {
	for _, finding := range findings {
		relation := finding.SelectionRelation
		if relation == "" {
			relation = policy.SelectionGlobal
		}
		output := stderr
		label := "FAIL"
		if finding.Severity == policy.FindingWarning {
			output = stdout
			label = "WARN"
		} else if finding.Severity == policy.FindingInformation {
			output = stdout
			label = "INFO"
		}
		fmt.Fprintf(output, "%s %-34s %s [%s] relation=%s\n     %s\n", label, finding.Check, findingLocation(finding), finding.Subject, relation, finding.Message)
		if finding.Remediation.Summary != "" {
			fmt.Fprintln(output, "     remedy:", finding.Remediation.Summary)
		}
		if finding.Remediation.NextCommand != nil {
			fmt.Fprintln(output, "     next:", strings.Join(finding.Remediation.NextCommand.Argv, " "))
		}
	}
}

func boundedReportItems[T any](items []T, remaining *int) []T {
	count := len(items)
	if count > *remaining {
		count = *remaining
	}
	*remaining -= count
	return items[:count]
}

func findingLocation(finding policy.Finding) string {
	if finding.Line <= 0 {
		return finding.Path
	}
	location := fmt.Sprintf("%s:%d", finding.Path, finding.Line)
	if finding.Column > 0 {
		location += fmt.Sprintf(":%d", finding.Column)
	}
	return location
}

func printSuppressedFindings(output io.Writer, suppressed []policy.Suppressed) {
	for _, finding := range suppressed {
		fmt.Fprintf(output, "WAIVED %-32s %s [%s] by %s\n", finding.Finding.Check, findingLocation(finding.Finding), finding.Finding.Subject, finding.Exception.ID)
	}
}

func printVulnerabilityAssessments(output io.Writer, assessed []policy.AssessedVulnerability) {
	for _, assessment := range assessed {
		observedSeverity := "unknown"
		if assessment.Finding.Vulnerability != nil {
			observedSeverity = policy.NormalizeVulnerabilitySeverity(assessment.Finding.Vulnerability.Severity)
		}
		fmt.Fprintf(
			output,
			"VULN-ACCEPTANCE %-23s %s [%s] by %s (observed %s, ceiling %s, %s/%s, approved by %s, expires %s)\n",
			assessment.Finding.Check, findingLocation(assessment.Finding), assessment.Finding.Subject, assessment.Assessment.ID,
			observedSeverity, assessment.Assessment.Severity, assessment.Assessment.Status, assessment.Assessment.Basis, assessment.Assessment.ApprovedBy,
			assessment.Assessment.Expires.Format("2006-01-02"),
		)
	}
}

func printReleaseAgeAssessments(output io.Writer, assessed []policy.AssessedReleaseAge) {
	for _, assessment := range assessed {
		fmt.Fprintf(output, "AGE-EXCEPTION %-25s %s [%s] by %s (%s)\n", assessment.Finding.Check, findingLocation(assessment.Finding), assessment.Finding.Subject, assessment.Assessment.ID, assessment.Assessment.Category)
	}
}

func printReportTables(output io.Writer, tables []engine.Table) {
	for _, table := range tables {
		printTable(output, table)
	}
}

func printReportNotes(output io.Writer, notes []string, verbose bool) {
	if !verbose {
		return
	}
	for _, note := range notes {
		fmt.Fprintln(output, "NOTE", note)
	}
}

func printReportCompletion(stdout, stderr io.Writer, report engine.Report) {
	errors, warnings, information := report.Summary.Errors, report.Summary.Warnings, report.Summary.Information
	if report.Protocol == "" {
		errors, warnings, information = reportFindingTotals(report.Findings)
	}
	if report.Summary.Status == "review-required" {
		fmt.Fprintf(stdout, "REVIEW REQUIRED errors=%d warnings=%d information=%d\n", errors, warnings, information)
	} else if errors > 0 || report.GateRunPolicy != nil && report.GateRunPolicy.Status == "failed" {
		fmt.Fprintf(stderr, "FAILED errors=%d warnings=%d information=%d\n", errors, warnings, information)
	} else if warnings > 0 {
		fmt.Fprintf(stdout, "PASS with %s; errors=0 warnings=%d information=%d\n", findingCount(warnings, "warning"), warnings, information)
	} else if information > 0 {
		fmt.Fprintf(stdout, "PASS with %s; errors=0 warnings=0 information=%d\n", findingCount(information, "informational finding"), information)
	} else {
		fmt.Fprintln(stdout, "PASS errors=0 warnings=0 information=0")
	}
}

func findingCount(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

func printReportSummaryGroups(output io.Writer, summary engine.ReportSummary) {
	printSummaryGroup(output, "RULE", summary.ByRule)
	printSummaryGroup(output, "MODULE", summary.ByModule)
	relations := make(map[string]int, len(summary.ByRelation))
	for relation, count := range summary.ByRelation {
		relations[string(relation)] = count
	}
	printSummaryGroup(output, "RELATION", relations)
}

func printSummaryGroup(output io.Writer, label string, counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for key, count := range counts {
		if key != "" && count > 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	const limit = 20
	displayed := len(keys)
	if displayed > limit {
		displayed = limit
	}
	for _, key := range keys[:displayed] {
		fmt.Fprintf(output, "COUNT %s %s: %d\n", label, key, counts[key])
	}
	if omitted := len(keys) - displayed; omitted > 0 {
		fmt.Fprintf(output, "COUNT %s OMITTED: %d group(s)\n", label, omitted)
	}
}

func reportFindingTotals(findings []policy.Finding) (int, int, int) {
	errors, warnings, information := 0, 0, 0
	for _, finding := range findings {
		switch finding.Severity {
		case policy.FindingWarning:
			warnings++
		case policy.FindingInformation:
			information++
		default:
			errors++
		}
	}
	return errors, warnings, information
}

func printBehaviorReview(output io.Writer, review *engine.BehaviorReviewStatus, verbose bool) {
	if review == nil {
		return
	}
	fmt.Fprintf(output, "BEHAVIOR REVIEW: %s (%s)\n", behaviorReviewStatusLabel(review.State), behaviorReviewScope(review))
	fmt.Fprintf(output, "FINAL STATE: %s (%s)\n", finalStateStatusLabel(review.FinalStateState), finalStateScope(review))
	if review.FinalStateState == engine.BehaviorReviewFailed {
		for _, finding := range review.FinalStateFindings {
			fmt.Fprintf(output, "%s:%d %s: %s\n", finding.Path, finding.Line, strings.ReplaceAll(finding.Kind, "-", " "), finding.Summary)
		}
	}
	if !verbose {
		return
	}
	fmt.Fprintln(output, "BEHAVIOR REVIEW BOUNDARY:", strings.ToUpper(string(review.RequiredBoundary)))
	for _, feature := range review.SelectedFeatures {
		fmt.Fprintln(output, "BEHAVIOR REVIEW FEATURE:", feature.Name)
		for _, reason := range feature.Reasons {
			fmt.Fprintf(output, "BEHAVIOR REVIEW REASON: %s (%s)\n", feature.Name, reason)
		}
	}
	if review.FullCandidate {
		fmt.Fprintln(output, "BEHAVIOR REVIEW FULL CANDIDATE: yes")
	}
	if review.ReceiptPath != "" {
		fmt.Fprintln(output, "BEHAVIOR REVIEW RECEIPT:", review.ReceiptPath)
	}
}

func finalStateStatusLabel(state engine.BehaviorReviewState) string {
	if state == engine.BehaviorReviewNotRun || state == engine.BehaviorReviewRequired {
		return "NOT RUN"
	}
	return strings.ToUpper(string(state))
}

func finalStateScope(review *engine.BehaviorReviewStatus) string {
	if review.FinalStateState == engine.BehaviorReviewNotRun && review.State == engine.BehaviorReviewNotRun {
		return "optional"
	}
	if review.FinalStateState == engine.BehaviorReviewNotRun {
		return "required"
	}
	return behaviorReviewScope(review)
}

func behaviorReviewStatusLabel(state engine.BehaviorReviewState) string {
	if state == engine.BehaviorReviewNotRun {
		return "NOT RUN"
	}
	return strings.ToUpper(string(state))
}

func behaviorReviewScope(review *engine.BehaviorReviewStatus) string {
	if review.State == engine.BehaviorReviewNotRun {
		return "optional"
	}
	scope := make([]string, 0, len(review.SelectedFeatures)+1)
	if review.FullCandidate {
		scope = append(scope, "all changes")
	}
	for _, feature := range review.SelectedFeatures {
		scope = append(scope, feature.Name)
	}
	return strings.Join(scope, ", ")
}
