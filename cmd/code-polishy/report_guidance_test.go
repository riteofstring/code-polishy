package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestReportConsolidatesGuidanceAcrossRulesAndKeepsEveryDiagnostic(t *testing.T) {
	t.Parallel()
	for _, rules := range [][]string{
		{"quality.gofmt", "quality.trailingWhitespace", "quality.finalNewline"},
		{"quality.lint", "quality.lint"},
		{"quality.typecheck", "quality.typecheck"},
		{"quality.complexity", "quality.complexity"},
		{"quality.deadCode", "quality.deadCode"},
		{"policy.exceptionExpired", "policy.vulnerabilityAssessmentExpired"},
	} {
		t.Run(rules[0], func(t *testing.T) {
			findings := []policy.Finding{}
			for index, rule := range rules {
				findings = append(findings, policy.Finding{
					Check: rule, Path: "src/app.go", Line: index + 10, Column: 2,
					Subject: fmt.Sprintf("subject-%d", index), Message: fmt.Sprintf("diagnostic-%d", index),
					SelectionRelation: policy.SelectionSelected,
					Remediation: policy.FindingRemediation{
						Summary:     "Repair the selected source.",
						NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "check", "--files", "src/app.go"}, Cwd: "."},
					},
				})
			}
			var output, diagnostics bytes.Buffer
			printFindings(&output, &diagnostics, findings)
			text := diagnostics.String()
			if output.Len() != 0 || strings.Count(text, "remedy:") != 1 || strings.Count(text, "next:") != 1 {
				t.Fatalf("guidance was repeated or misrouted: stdout=%s stderr=%s", output.String(), text)
			}
			for index := range rules {
				facts := fmt.Sprintf("src/app.go:%d:2 [subject-%d] relation=selected remedy=R1 next=C1\n     diagnostic-%d\n", index+10, index, index)
				if strings.Count(text, facts) != 1 {
					t.Fatalf("diagnostic lost its facts or shared guidance: %s", text)
				}
			}
			if !strings.Contains(text, "remedy: Repair the selected source.") || !strings.Contains(text, "next: 'code-polishy' 'check' '--files' 'src/app.go'") {
				t.Fatalf("missing repair instructions: %s", text)
			}
		})
	}
}

func TestReportSharesRemediesAndCommandsIndependently(t *testing.T) {
	t.Parallel()
	first := &policy.FindingCommand{Argv: []string{"code-polishy", "check", "--files", "first.go"}, Cwd: "."}
	second := &policy.FindingCommand{Argv: []string{"code-polishy", "check", "--files", "second.go"}, Cwd: "."}
	findings := []policy.Finding{
		{Check: "quality.lint", Path: "first.go", Message: "first diagnostic", Remediation: policy.FindingRemediation{Summary: "Repair imports.", NextCommand: first}},
		{Check: "quality.lint", Path: "second.go", Message: "second diagnostic", Remediation: policy.FindingRemediation{Summary: "Repair imports.", NextCommand: second}},
		{Check: "quality.typecheck", Path: "second.go", Message: "third diagnostic", Remediation: policy.FindingRemediation{Summary: "Repair types.", NextCommand: second}},
		{Check: "custom.check", Path: "third.go", Message: "diagnostic without guidance"},
	}
	var output, diagnostics bytes.Buffer
	printFindings(&output, &diagnostics, findings)
	text := diagnostics.String()
	for _, expected := range []string{
		"first.go [] relation=global remedy=R1 next=C1",
		"second.go [] relation=global remedy=R1 next=C2",
		"second.go [] relation=global remedy=R2 next=C2",
		"third.go [] relation=global\n     diagnostic without guidance",
		"REMEDY R1 (2 findings):\n     remedy: Repair imports.",
		"REMEDY R2 (1 finding):\n     remedy: Repair types.",
		"COMMAND C2 (2 findings, cwd .):",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q: %s", expected, text)
		}
	}
	if strings.Count(text, "remedy:") != 2 || strings.Count(text, "next:") != 2 || strings.Index(text, "first diagnostic") > strings.Index(text, "second diagnostic") {
		t.Fatalf("guidance or order changed: %s", text)
	}
}

func TestReportGuidanceStaysWithItsOutputStream(t *testing.T) {
	t.Parallel()
	findings := []policy.Finding{}
	for _, severity := range []policy.FindingSeverity{policy.FindingError, policy.FindingWarning, policy.FindingInformation} {
		findings = append(findings, policy.Finding{
			Check: "quality.lint", Path: "src/app.go", Severity: severity,
			Remediation: policy.FindingRemediation{Summary: "Repair imports.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "doctor"}, Cwd: "."}},
		})
	}
	var output, diagnostics bytes.Buffer
	printFindings(&output, &diagnostics, findings)
	for _, text := range []string{output.String(), diagnostics.String()} {
		if strings.Count(text, "remedy: Repair imports.") != 1 || strings.Count(text, "next: 'code-polishy' 'doctor'") != 1 {
			t.Fatalf("stream is missing its own guidance: %s", text)
		}
	}
	if !strings.Contains(diagnostics.String(), "FAIL quality.lint") || strings.Contains(diagnostics.String(), "WARN") ||
		strings.Contains(output.String(), "FAIL") || !strings.Contains(output.String(), "WARN quality.lint") || !strings.Contains(output.String(), "INFO quality.lint") ||
		!strings.Contains(output.String(), "REMEDY R2 (2 findings)") || !strings.Contains(diagnostics.String(), "REMEDY R1 (1 finding)") {
		t.Fatalf("streams or group identities were conflated: stdout=%s stderr=%s", output.String(), diagnostics.String())
	}
}

func TestReportCommandGroupsPreserveArgumentBoundariesAndWorkingDirectories(t *testing.T) {
	t.Parallel()
	commands := []policy.FindingCommand{
		{Argv: []string{"tool", "a b"}, Cwd: "frontend"},
		{Argv: []string{"tool", "a b"}, Cwd: "frontend"},
		{Argv: []string{"tool", "a", "b"}, Cwd: "frontend"},
		{Argv: []string{"tool", "a b"}, Cwd: "backend"},
		{Argv: []string{"tool", "a'b"}, Cwd: "frontend"},
	}
	findings := []policy.Finding{}
	for _, command := range commands {
		findings = append(findings, policy.Finding{Check: "custom.check", Remediation: policy.FindingRemediation{NextCommand: &command}})
	}
	var output bytes.Buffer
	printFindings(&output, &output, findings)
	text := output.String()
	for _, expected := range []string{
		"COMMAND C1 (2 findings, cwd frontend):\n     next: 'tool' 'a b'",
		"COMMAND C2 (1 finding, cwd frontend):\n     next: 'tool' 'a' 'b'",
		"COMMAND C3 (1 finding, cwd backend):\n     next: 'tool' 'a b'",
		"COMMAND C4 (1 finding, cwd frontend):\n     next: 'tool' 'a'\\''b'",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q: %s", expected, text)
		}
	}
	if strings.Count(text, "next:") != 4 {
		t.Fatalf("wrong command count: %s", text)
	}
}

func TestReportGuidanceUsesOnlyVisibleFindingsAndPreservesMachineData(t *testing.T) {
	t.Parallel()
	report := engine.Report{Protocol: engine.ReportProtocol, Summary: engine.ReportSummary{Status: "failed", Errors: 4}}
	for index, path := range []string{"hidden.go", "src/app.go", "src/app.go", "src/app.go"} {
		report.Findings = append(report.Findings, policy.Finding{
			Check: "quality.lint", Path: path, Line: index, Message: fmt.Sprintf("diagnostic-%d", index),
			Remediation: policy.FindingRemediation{Summary: "Repair " + path, NextCommand: &policy.FindingCommand{Argv: []string{"check", path}, Cwd: "."}},
		})
	}
	before, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	view := engine.ReportView(report, engine.DisplayOptions{Filters: engine.ReportFilters{Paths: []string{"src/app.go"}}, GroupBy: "rule", Limit: 2})
	var output, diagnostics bytes.Buffer
	printReportTo(&output, &diagnostics, view)
	text := output.String() + diagnostics.String()
	for _, expected := range []string{"FAILED errors=4", "OMITTED 1 finding(s)", "FILTERED 1 occurrence(s)", "REMEDY R1 (2 findings)", "COMMAND C1 (2 findings"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q: %s", expected, text)
		}
	}
	if strings.Contains(text, "hidden.go") || strings.Contains(text, "diagnostic-0") || strings.Count(text, "diagnostic-") != 2 || strings.Count(text, "FAIL quality.lint") != 2 {
		t.Fatalf("hidden findings entered the guidance groups: %s", text)
	}
	after, err := json.Marshal(report)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("terminal grouping changed machine report data: %v", err)
	}
}
