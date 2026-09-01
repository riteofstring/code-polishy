package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func printFindings(output io.Writer, findings []policy.Finding) {
	for _, finding := range findings {
		fmt.Fprintf(output, "FAIL %-34s %s [%s]\n     %s\n", finding.Check, findingLocation(finding), finding.Subject, finding.Message)
	}
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

func printAdvisories(output io.Writer, advisories []policy.Advisory) {
	for _, advisory := range advisories {
		fmt.Fprintf(output, "WARN %-34s %s [%s]\n     %s\n", advisory.Check, advisory.Path, advisory.Subject, advisory.Message)
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
	if len(report.Findings) == 0 && (report.GateRunPolicy == nil || report.GateRunPolicy.Status == "passed") {
		fmt.Fprintln(stdout, "PASS policy completed without findings")
		return
	}
	if len(report.Findings) > 0 {
		fmt.Fprintf(stderr, "FAILED with %d finding(s)\n", len(report.Findings))
	}
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
