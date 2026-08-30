package main

import (
	"fmt"
	"io"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func printFindings(output io.Writer, findings []policy.Finding) {
	for _, finding := range findings {
		fmt.Fprintf(output, "FAIL %-34s %s [%s]\n     %s\n", finding.Check, finding.Path, finding.Subject, finding.Message)
	}
}

func printSuppressedFindings(output io.Writer, suppressed []policy.Suppressed) {
	for _, finding := range suppressed {
		fmt.Fprintf(output, "WAIVED %-32s %s [%s] by %s\n", finding.Finding.Check, finding.Finding.Path, finding.Finding.Subject, finding.Exception.ID)
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
			assessment.Finding.Check, assessment.Finding.Path, assessment.Finding.Subject, assessment.Assessment.ID,
			observedSeverity, assessment.Assessment.Severity, assessment.Assessment.Status, assessment.Assessment.Basis, assessment.Assessment.ApprovedBy,
			assessment.Assessment.Expires.Format("2006-01-02"),
		)
	}
}

func printReleaseAgeAssessments(output io.Writer, assessed []policy.AssessedReleaseAge) {
	for _, assessment := range assessed {
		fmt.Fprintf(output, "AGE-EXCEPTION %-25s %s [%s] by %s (%s)\n", assessment.Finding.Check, assessment.Finding.Path, assessment.Finding.Subject, assessment.Assessment.ID, assessment.Assessment.Category)
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
