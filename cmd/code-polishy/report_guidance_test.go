package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
			if !strings.Contains(text, "remedy: Repair the selected source.") || !strings.Contains(text, "next: "+hostReportCommand("'./code-polishyw' 'check' '--files' 'src/app.go'", "& '.\\code-polishyw.ps1' 'check' '--files' 'src/app.go'")) {
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
			Remediation: policy.FindingRemediation{
				Summary: "Repair imports.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "doctor"}, Cwd: "."},
				Generation: &policy.GenerationRemediation{
					Producer: "contracts", Inputs: []string{"source/schema.json"},
					Generate: policy.GenerationCommand{Argv: []string{"node", "generate.js"}, Cwd: "frontend"},
					Verify:   policy.GenerationCommand{Argv: []string{"node", "verify.js"}, Cwd: "frontend"},
				},
			},
		})
	}
	var output, diagnostics bytes.Buffer
	printFindings(&output, &diagnostics, findings)
	for _, stream := range []struct{ text, commandRole string }{{output.String(), "optional"}, {diagnostics.String(), "next"}} {
		if strings.Count(stream.text, "remedy: Repair imports.") != 1 || strings.Count(stream.text, stream.commandRole+": "+hostReportCommand("'./code-polishyw' 'doctor'", "& '.\\code-polishyw.ps1' 'doctor'")) != 1 {
			t.Fatalf("stream is missing its own guidance: %s", stream.text)
		}
	}
	if !strings.Contains(diagnostics.String(), "FAIL quality.lint") || strings.Contains(diagnostics.String(), "WARN") ||
		strings.Contains(output.String(), "FAIL") || !strings.Contains(output.String(), "WARN quality.lint") || !strings.Contains(output.String(), "INFO quality.lint") ||
		!strings.Contains(output.String(), "REMEDY R2 (2 findings)") || !strings.Contains(diagnostics.String(), "REMEDY R1 (1 finding)") {
		t.Fatalf("streams or group identities were conflated: stdout=%s stderr=%s", output.String(), diagnostics.String())
	}
	if strings.Contains(output.String(), "next=") || strings.Contains(output.String(), "next:") || strings.Contains(diagnostics.String(), "optional") {
		t.Fatalf("optional investigation was conflated with required recovery: stdout=%s stderr=%s", output.String(), diagnostics.String())
	}
	for _, expected := range []string{"optional=C2", "COMMAND C2 (2 findings, cwd ., optional investigation)", "GENERATION G2 (2 findings, optional investigation)", "source/schema.json", "generate (cwd frontend)", "verify (cwd frontend)"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("optional guidance lost %q: %s", expected, output.String())
		}
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
		"COMMAND C1 (2 findings, cwd frontend):\n     next: " + hostReportCommand("'tool' 'a b'", "& 'tool' 'a b'"),
		"COMMAND C2 (1 finding, cwd frontend):\n     next: " + hostReportCommand("'tool' 'a' 'b'", "& 'tool' 'a' 'b'"),
		"COMMAND C3 (1 finding, cwd backend):\n     next: " + hostReportCommand("'tool' 'a b'", "& 'tool' 'a b'"),
		"COMMAND C4 (1 finding, cwd frontend):\n     next: " + hostReportCommand("'tool' 'a'\\''b'", "& 'tool' 'a''b'"),
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q: %s", expected, text)
		}
	}
	if strings.Count(text, "next:") != 4 {
		t.Fatalf("wrong command count: %s", text)
	}
}

func hostReportCommand(posix, powershell string) string {
	if runtime.GOOS == "windows" {
		return powershell
	}
	return posix
}

func TestReportRemediationCommandUsesPlatformInvocationAndLiteralArguments(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		argv  []string
		posix string
		power string
	}{
		{"recovery", []string{"code-polishy", "check", "--files", "src/app.go"}, "'./code-polishyw' 'check' '--files' 'src/app.go'", "& '.\\code-polishyw.ps1' 'check' '--files' 'src/app.go'"},
		{"literal arguments", []string{"tools/it's a tool", "a'b", "", "$HOME; `echo hi`", `C:\src\file name.go`, `a"b`},
			"'tools/it'\\''s a tool' 'a'\\''b' '' '$HOME; `echo hi`' 'C:\\src\\file name.go' 'a\"b'",
			"& 'tools/it''s a tool' 'a''b' '' '$HOME; `echo hi`' 'C:\\src\\file name.go' 'a\"b'"},
		{"Unicode delimiters", []string{"tools/‘’‚‛", "src/a’; Write-Output injected #.go", "‘’‚‛"},
			"'tools/‘’‚‛' 'src/a’; Write-Output injected #.go' '‘’‚‛'",
			"& 'tools/‘‘’’‚‚‛‛' 'src/a’’; Write-Output injected #.go' '‘‘’’‚‚‛‛'"},
		{"empty", nil, "", ""},
	} {
		for _, goos := range []string{"linux", "darwin", "windows"} {
			t.Run(test.name+"/"+goos, func(t *testing.T) {
				want := test.posix
				if goos == "windows" {
					want = test.power
				}
				if got := platformRemediationCommand(test.argv, goos); got != want {
					t.Fatalf("command = %q, want %q", got, want)
				}
				if goos == runtime.GOOS && displayRemediationCommand(test.argv) != want {
					t.Fatalf("host command = %q, want %q", displayRemediationCommand(test.argv), want)
				}
			})
		}
	}
}

func TestReportRemediationCommandBoundsIncludePlatformInvocation(t *testing.T) {
	t.Parallel()
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, size := range []int{4092, 4093, 4095} {
			command := platformRemediationCommand([]string{strings.Repeat("x", size)}, goos)
			length := size + 2
			if goos == "windows" {
				length += 2
			}
			if (length > 4096) != strings.Contains(command, "complete report") || len(command) > 4096 {
				t.Errorf("%s: %d-byte executable produced invalid bound: %s", goos, size, command)
			}
		}
	}
}

func TestWindowsReportRecoveryCommandExecutesInPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native PowerShell execution runs on Windows")
	}
	t.Parallel()
	script := filepath.Join(t.TempDir(), "capture it's arguments.ps1")
	if err := os.WriteFile(script, []byte("$ErrorActionPreference = 'Stop'\nConvertTo-Json -InputObject @($args) -Compress\n"), 0600); err != nil {
		t.Fatal(err)
	}
	arguments := []string{"check", "--files", "src/it's a file.go", "", "$env:HOME", "`literal`", "a; Write-Error unexpected", `a"b`, `C:\source\`}
	for _, delimiter := range "'‘’‚‛" {
		arguments = append(arguments, "src/a"+string(delimiter)+"; Write-Output injected #.go")
	}
	finding := policy.Finding{Check: "quality.lint", Remediation: policy.FindingRemediation{
		NextCommand: &policy.FindingCommand{Argv: append([]string{script}, arguments...), Cwd: "."},
	}}
	var report bytes.Buffer
	printFindings(&report, &report, []policy.Finding{finding})
	_, command, found := strings.Cut(report.String(), "     next: ")
	if !found {
		t.Fatalf("report omitted recovery command: %s", report.String())
	}
	output, err := exec.Command("pwsh", "-NoProfile", "-NonInteractive", "-Command", strings.TrimSuffix(command, "\n")).CombinedOutput()
	if err != nil {
		t.Fatalf("printed command failed: %v\n%s", err, output)
	}
	var received []string
	if err := json.Unmarshal(bytes.TrimSpace(output), &received); err != nil || !slices.Equal(received, arguments) {
		t.Fatalf("arguments = %q, want %q; decode error: %v; output: %s", received, arguments, err, output)
	}
}

func BenchmarkReportGuidanceAll(b *testing.B) {
	for _, count := range []int{5000, 20000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			report := engine.Report{
				Protocol: engine.ReportProtocol, Summary: engine.ReportSummary{Status: "failed", Errors: count},
				Display: &engine.ReportDisplay{All: true},
			}
			for index := range count {
				path := fmt.Sprintf("src/file-%d.go", index)
				report.Findings = append(report.Findings, policy.Finding{
					Check: "quality.lint", Path: path, Message: "Repair imports.",
					Remediation: policy.FindingRemediation{Summary: "Repair imports.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "check", "--files", path}, Cwd: "."}},
				})
			}
			b.ReportAllocs()
			for b.Loop() {
				printReportTo(io.Discard, io.Discard, report)
			}
		})
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

func optionalAdvisoryReport() engine.Report {
	report := engine.Report{
		Protocol: engine.ReportProtocol, ReportPath: ".code-polishy-reports/check/example/report.json",
		Summary: engine.ReportSummary{
			Status: "passed", Warnings: 37, ByRule: map[string]int{"quality.deadCode": 37},
			ByModule: map[string]int{"application": 37}, ByRelation: map[policy.SelectionRelation]int{policy.SelectionSelected: 37},
		},
	}
	for index := range 37 {
		report.Findings = append(report.Findings, policy.NormalizeFinding(policy.Finding{
			Check: "quality.deadCode", Path: fmt.Sprintf("src/worker_%02d.py", index), Line: 3, Subject: "unused",
			Message: fmt.Sprintf("unused candidate %02d", index), Severity: policy.FindingWarning,
			Fields: map[string]string{"analysisOutcome": "advisory-candidate"},
			Remediation: policy.FindingRemediation{
				Summary: "Optional investigation guidance.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "check", "--all"}, Cwd: "."},
				Generation: &policy.GenerationRemediation{
					Producer: "worker", Inputs: []string{"source/schema.json"},
					Generate: policy.GenerationCommand{Argv: []string{"generate"}, Cwd: "."},
					Verify:   policy.GenerationCommand{Argv: []string{"verify"}, Cwd: "."},
				},
			},
		}))
	}
	return report
}

func TestOptionalAdvisoryReportIsCompactAndPreservesMachineEvidence(t *testing.T) {
	t.Parallel()
	report := optionalAdvisoryReport()
	before, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	printReportTo(&output, &diagnostics, report)
	if diagnostics.Len() != 0 || engine.HasFindings(report) || strings.Count(output.String(), "\n") != 2 ||
		!strings.Contains(output.String(), "PASS — 37 advisory warnings; no action required.") || !strings.Contains(output.String(), report.ReportPath) {
		t.Fatalf("advisories are not a compact passing summary: stdout=%s stderr=%s", output.String(), diagnostics.String())
	}
	after, err := json.Marshal(report)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("terminal compaction changed the machine report: %v", err)
	}
	sarif, err := engine.SARIF(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		for _, document := range [][]byte{after, sarif} {
			if !bytes.Contains(document, []byte(finding.Message)) || !bytes.Contains(document, []byte(finding.Path)) || !bytes.Contains(document, []byte("Optional investigation guidance.")) {
				t.Fatalf("machine evidence lost advisory %q", finding.Path)
			}
		}
	}
}

func TestOptionalAdvisoryDetailsRequireExplicitInspection(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		verbose bool
		display engine.DisplayOptions
		count   int
	}{
		"verbose":  {verbose: true, display: engine.DisplayOptions{Limit: 3}, count: 3},
		"all":      {display: engine.DisplayOptions{All: true}, count: 37},
		"filtered": {display: engine.DisplayOptions{Filters: engine.ReportFilters{Paths: []string{"src/worker_36.py"}}}, count: 1},
		"grouped":  {display: engine.DisplayOptions{GroupBy: "module", Limit: 2}, count: 2},
	} {
		t.Run(name, func(t *testing.T) {
			report := engine.ReportView(optionalAdvisoryReport(), test.display)
			var output, diagnostics bytes.Buffer
			printReportWithMode(&output, &diagnostics, report, test.verbose)
			if diagnostics.Len() != 0 || strings.Count(output.String(), "WARN quality.deadCode") != test.count ||
				!strings.Contains(output.String(), "Optional investigation guidance.") || !strings.Contains(output.String(), "optional:") ||
				!strings.Contains(output.String(), "producer: worker") || !strings.Contains(output.String(), "COUNT RULE quality.deadCode: 37") {
				t.Fatalf("explicit inspection lost advisory details: stdout=%s stderr=%s", output.String(), diagnostics.String())
			}
		})
	}
}

func TestOptionalAdvisoriesDoNotCrowdOutBlockersOrHideOtherWarnings(t *testing.T) {
	t.Parallel()
	report := optionalAdvisoryReport()
	report.Findings = append(report.Findings,
		policy.Finding{Check: "supplyChain.gitSourceCoverage", Path: "uv.lock", Severity: policy.FindingWarning, Message: "coverage is unverified"},
		policy.Finding{Check: "quality.typecheckEnvironment", Path: "worker/pyproject.toml", Severity: policy.FindingInformation, Message: "runtime evidence"},
		policy.Finding{
			Check: "quality.lint", Path: "broken.py", Severity: policy.FindingError, Message: "syntax is invalid",
			Fields:      map[string]string{"analysisOutcome": "advisory-candidate"},
			Remediation: policy.FindingRemediation{Summary: "Repair the syntax.", NextCommand: &policy.FindingCommand{Argv: []string{"check"}, Cwd: "."}},
		},
	)
	report.Summary.Status, report.Summary.Errors, report.Summary.Warnings, report.Summary.Information = "failed", 1, 38, 1
	for _, limit := range []int{1, 20} {
		report.Display = &engine.ReportDisplay{Limit: limit}
		var output, diagnostics bytes.Buffer
		printReportTo(&output, &diagnostics, report)
		if !strings.Contains(diagnostics.String(), "FAIL quality.lint") || !strings.Contains(diagnostics.String(), "next:") ||
			!strings.Contains(output.String(), "37 advisory warnings; no action required for these advisories.") ||
			strings.Contains(output.String(), "unused candidate") || strings.Contains(output.String(), "PASS") {
			t.Fatalf("advisories obscured the failure: stdout=%s stderr=%s", output.String(), diagnostics.String())
		}
		if strings.Contains(output.String(), "OMITTED") || !strings.Contains(output.String(), "NONBLOCKING: supplyChain.gitSourceCoverage: coverage is unverified") || !strings.Contains(output.String(), "quality.typecheckEnvironment: runtime evidence") {
			t.Fatalf("nonblocking summary lost evidence or consumed the blocker budget: %s", output.String())
		}
	}
}
