package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/finalstate"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
)

func TestPrintReportLabelsNonBlockingAdvisory(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{Findings: []policy.Finding{{Check: "portability.machinePath", Path: "app.js", Subject: "3", Message: "machine path", Severity: policy.FindingWarning}}}
	printReportTo(stdout, stderr, report)
	if !strings.Contains(stdout.String(), "WARN portability.machinePath") || !strings.Contains(stdout.String(), "PASS with 1 warning") || strings.Contains(stdout.String(), "without findings") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHumanReportBoundsAllFindingOutcomesTogether(t *testing.T) {
	t.Parallel()
	report := engine.Report{
		Protocol: engine.ReportProtocol,
		Summary:  engine.ReportSummary{Status: "failed", Errors: 15},
		Display:  &engine.ReportDisplay{Limit: 20, Total: 30, Displayed: 30},
	}
	for index := 0; index < 15; index++ {
		finding := policy.Finding{Check: "quality.lint", Path: fmt.Sprintf("src/%02d.go", index), Subject: "unused", Message: "unused"}
		report.Findings = append(report.Findings, finding)
		report.Suppressed = append(report.Suppressed, policy.Suppressed{
			Finding: finding, Exception: policy.Exception{ID: fmt.Sprintf("exception-%02d", index)},
		})
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	printReportTo(stdout, stderr, report)
	if strings.Count(stderr.String(), "FAIL quality.lint") != 15 || strings.Count(stdout.String(), "WAIVED quality.lint") != 5 ||
		!strings.Contains(stdout.String(), "OMITTED 10 finding(s)") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestReportOutputOptionsAreSeparateFromEvaluationSelection(t *testing.T) {
	options, remaining, err := parseReportOutputOptions("check", []string{
		"--files", "src", "--format", "json", "--filter-relation=context", "--filter-rule", "quality.lint", "--group-by", "module", "--display-limit", "12",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.format != "json" || options.display.GroupBy != "module" || options.display.Limit != 12 || !slices.Equal(remaining, []string{"--files", "src"}) ||
		!slices.Equal(options.display.Filters.Rules, []string{"quality.lint"}) || !slices.Equal(options.display.Filters.Relations, []policy.SelectionRelation{policy.SelectionContext}) {
		t.Fatalf("options = %+v remaining = %v", options, remaining)
	}
	if _, _, err := parseReportOutputOptions("check", []string{"--format="}); err == nil {
		t.Fatal("empty inline format was accepted")
	}
}

func TestPrintReportShowsTestQualityReminderBeforeOrdinaryDetails(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{
		MergePolicy:         &engine.MergePolicy{Level: "recommended", Base: "origin/main"},
		TestQualityReminder: engine.NewTestQualityReminder([]string{"cmd/code-polishy/main_test.go", "internal/engine/engine_test.go"}),
	}
	printReportTo(stdout, stderr, report)
	output := stdout.String()
	lower := strings.ToLower(output)
	for _, fact := range []string{"test quality reminder", "changed test files: 2", "tautological", "change-detector", "merge gate"} {
		if !strings.Contains(lower, fact) {
			t.Fatalf("test reminder omitted %q: %q", fact, output)
		}
	}
	if strings.Index(lower, "test quality reminder") > strings.Index(lower, "merge gate") {
		t.Fatalf("test reminder did not precede ordinary report details: %q", output)
	}
	if !strings.Contains(output, "PASS errors=0 warnings=0 information=0") || engine.HasFindings(report) {
		t.Fatalf("reminder changed successful report behavior: stdout=%q report=%+v", output, report)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestPrintReportLabelsReleaseAgeAssessmentSeparately(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{ReleaseAges: []policy.AssessedReleaseAge{{
		Finding:    policy.Finding{Check: "supplyChain.releaseAge", Path: "pnpm-lock.yaml", Subject: "electron@43.3.0"},
		Assessment: policy.ReleaseAgeAssessment{ID: "electron-supported", Category: "supported-release"},
	}}}
	printReportTo(stdout, stderr, report)
	if !strings.Contains(stdout.String(), "AGE-EXCEPTION") || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestPrintReportKeepsApprovedVulnerabilityVisible(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{Assessed: []policy.AssessedVulnerability{{
		Finding: policy.Finding{
			Check: "supplyChain.osvVulnerability", Path: "pnpm-lock.yaml", Subject: "CVE-2026-1000:example@1.2.3",
			Vulnerability: &policy.VulnerabilityIdentity{Severity: "high"},
		},
		Assessment: policy.VulnerabilityAssessment{
			ID: "high-not-affected", Severity: "high", Status: "not-affected", Basis: "unreachable",
			ApprovedBy: "security", Expires: policy.Date{Time: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		},
	}}}
	printReportTo(stdout, stderr, report)
	if !strings.Contains(stdout.String(), "VULN-ACCEPTANCE") || !strings.Contains(stdout.String(), "high-not-affected") ||
		!strings.Contains(stdout.String(), "observed high, ceiling high") || !strings.Contains(stdout.String(), "not-affected/unreachable") ||
		!strings.Contains(stdout.String(), "approved by security") || !strings.Contains(stdout.String(), "expires 2026-09-01") || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestParseSelectionDefaultsToChanges(t *testing.T) {
	t.Parallel()
	mode, files, err := parseSelection(nil)
	if err != nil || mode != "changes" || len(files) != 0 {
		t.Fatalf("mode=%q files=%v err=%v", mode, files, err)
	}
}

func TestParseSelectionConsumesExplicitFiles(t *testing.T) {
	t.Parallel()
	mode, files, err := parseSelection([]string{"--files", "a.go", "b.go"})
	if err != nil || mode != "files" || !slices.Equal(files, []string{"a.go", "b.go"}) {
		t.Fatalf("mode=%q files=%v err=%v", mode, files, err)
	}
}

func TestParseSelectionConsumesDirectoriesAndModulesAsEvaluationOperands(t *testing.T) {
	t.Parallel()
	mode, paths, err := parseSelection([]string{"--files", "internal/engine", "cmd/code-polishy/main.go"})
	if err != nil || mode != "files" || !slices.Equal(paths, []string{"internal/engine", "cmd/code-polishy/main.go"}) {
		t.Fatalf("mode=%q paths=%v err=%v", mode, paths, err)
	}
	mode, modules, err := parseSelection([]string{"--module=engine", "--module", "cli"})
	if err != nil || mode != "modules" || !slices.Equal(modules, []string{"engine", "cli"}) {
		t.Fatalf("mode=%q modules=%v err=%v", mode, modules, err)
	}
}

func TestParseSelectionRejectsUnknownOption(t *testing.T) {
	t.Parallel()
	if _, _, err := parseSelection([]string{"--quick"}); err == nil {
		t.Fatal("expected unknown-option error")
	}
}

func TestParseSelectionRejectsMixedModes(t *testing.T) {
	t.Parallel()
	if _, _, err := parseSelection([]string{"--staged", "--all"}); err == nil {
		t.Fatal("expected mixed-selection error")
	}
	if _, _, err := parseSelection([]string{"--files", "src", "--module", "domain"}); err == nil {
		t.Fatal("expected file-and-module selection error")
	}
	if _, _, err := parseSelection([]string{"--module"}); err == nil {
		t.Fatal("expected missing-module error")
	}
}

func TestParseDesignContextOptionsSupportsStandardSelectors(t *testing.T) {
	t.Parallel()
	files, err := parseDesignContextOptions([]string{"--files", "src/value.go", "src/entry.go"})
	if err != nil || files.mode != "files" || !slices.Equal(files.files, []string{"src/value.go", "src/entry.go"}) || len(files.modules) != 0 {
		t.Fatalf("file options = %+v, err = %v", files, err)
	}
	modules, err := parseDesignContextOptions([]string{"--module=domain", "--module", "api"})
	if err != nil || modules.mode != "changes" || !slices.Equal(modules.modules, []string{"domain", "api"}) || len(modules.files) != 0 {
		t.Fatalf("module options = %+v, err = %v", modules, err)
	}
	defaults, err := parseDesignContextOptions(nil)
	if err != nil || defaults.mode != "changes" || len(defaults.files) != 0 || len(defaults.modules) != 0 {
		t.Fatalf("default options = %+v, err = %v", defaults, err)
	}
	for _, arguments := range [][]string{
		{"--module", "domain", "--all"},
		{"--files", "src/value.go", "--module", "domain"},
		{"--files"},
		{"--module"},
		{"--unknown"},
	} {
		if _, err := parseDesignContextOptions(arguments); err == nil {
			t.Fatalf("accepted invalid options %v", arguments)
		}
	}
}

func TestHandleDesignContextSurfacesRequestAndMappingFailures(t *testing.T) {
	t.Parallel()
	if _, err := handleDesignContext(t.Context(), &engine.Engine{}, []string{"--files"}); err == nil {
		t.Fatal("missing file selection was accepted")
	}
	unknownModule := &engine.Engine{}
	unknownModule.Repository.Root = t.TempDir()
	if _, err := handleDesignContext(t.Context(), unknownModule, []string{"--module", "absent"}); err == nil {
		t.Fatal("unknown module was accepted")
	}
	staleMapping := &engine.Engine{}
	staleMapping.Repository.Root = t.TempDir()
	staleMapping.Repository.Config = policy.Config{
		Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}},
		Documentation: policy.Documentation{Design: []policy.DesignDocument{{
			Path: "docs/design/missing.md", Module: "application",
		}}},
	}
	result, err := handleDesignContext(t.Context(), staleMapping, []string{"--module", "application"})
	if err != nil {
		t.Fatal(err)
	}
	if result.quiet || len(result.report.Findings) != 1 ||
		result.report.Findings[0].Check != "policy.designDocumentation" ||
		result.report.Findings[0].Subject != "docs/design/missing.md" {
		t.Fatalf("result = %+v", result)
	}
}

func TestParseChangeBoundaryOptionsSeparatesNewArtifactPaths(t *testing.T) {
	t.Parallel()
	options, err := parseChangeBoundaryOptions([]string{
		"--base", "0123456789012345678901234567890123456789",
		"--module", "repository",
		"--allow-path", "docs/plans/existing.md",
		"--allow-new-path", "docs/plans/new.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.base != "0123456789012345678901234567890123456789" ||
		!slices.Equal(options.modules, []string{"repository"}) ||
		!slices.Equal(options.paths, []string{"docs/plans/existing.md"}) ||
		!slices.Equal(options.newPaths, []string{"docs/plans/new.md"}) {
		t.Fatalf("options = %+v", options)
	}
}

func TestFirstCommandIndexSkipsGlobalValues(t *testing.T) {
	t.Parallel()
	arguments := []string{"--verbose", "--repo-root", "/tmp/example", "--config=policy.json", "doctor", "--strict"}
	if got := firstCommandIndex(arguments); got != 4 {
		t.Fatalf("index = %d", got)
	}
}

func TestParseGlobalInvocationAcceptsVerboseMode(t *testing.T) {
	t.Parallel()
	parsed, status := parseGlobalInvocation([]string{"--verbose", "doctor", "--strict"})
	if status != 0 || !parsed.verbose || parsed.command != "doctor" {
		t.Fatalf("invocation=%+v status=%d", parsed, status)
	}
}

func TestFindPolicyRootWalksUpFromBuiltBinary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, contents := range map[string]string{"VERSION": "0.1.0\n", "schema/code-polishy.schema.json": "{}\n"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	start := filepath.Join(root, ".tools", "bin")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}
	if found, ok := findPolicyRoot(start); !ok || found != root {
		t.Fatalf("root=%q found=%v", found, ok)
	}
}

func TestPrintVersionReadsPinnedPolicyRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("0.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := printVersion(invocation{policyRoot: root}); status != 0 {
		t.Fatalf("status = %d", status)
	}
}

func TestParseTestRequestAcceptsRepeatedModules(t *testing.T) {
	t.Parallel()
	request, err := parseTestRequest(&engine.Engine{}, []string{"--module", "domain", "--module", "api"})
	if err != nil || !slices.Equal(request.Modules, []string{"domain", "api"}) {
		t.Fatalf("request=%+v err=%v", request, err)
	}
}

func TestParseTestRequestRejectsMixedSelectors(t *testing.T) {
	t.Parallel()
	_, err := parseTestRequest(&engine.Engine{}, []string{"--changed", "--module", "domain"})
	if err == nil {
		t.Fatal("expected selector conflict")
	}
}

func TestParseTestRequestRejectsBaseWithoutChangedOrRecommended(t *testing.T) {
	t.Parallel()
	_, err := parseTestRequest(&engine.Engine{}, []string{"--base", "main"})
	if err == nil {
		t.Fatal("expected --base scope error")
	}
}

func TestParseTestRequestAcceptsBaseWithChanged(t *testing.T) {
	t.Parallel()
	options, err := parseTestOptions([]string{"--changed", "--base", "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTestOptions(options); err != nil {
		t.Fatalf("changed base was rejected: %v", err)
	}
}

func TestParseTestRequestRejectsRecommendedWithAnotherSelector(t *testing.T) {
	t.Parallel()
	_, err := parseTestRequest(&engine.Engine{}, []string{"--recommended", "--all"})
	if err == nil {
		t.Fatal("expected selector conflict")
	}
}

func TestParseTestRequestAcceptsSupplementalWithoutGitSelection(t *testing.T) {
	t.Parallel()
	request, err := parseTestRequest(&engine.Engine{}, []string{"--supplemental"})
	if err != nil || !request.Supplemental {
		t.Fatalf("request=%+v err=%v", request, err)
	}
}

func TestParseTestOptionsLeavesSupplementalUnselectedForChangedTests(t *testing.T) {
	t.Parallel()
	options, err := parseTestOptions([]string{"--changed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTestOptions(options); err != nil {
		t.Fatal(err)
	}
	if options.request.Supplemental {
		t.Fatalf("changed options selected supplemental: %+v", options)
	}
}

func TestParseTestRequestRejectsSupplementalWithFull(t *testing.T) {
	t.Parallel()
	if _, err := parseTestRequest(&engine.Engine{}, []string{"--supplemental", "--all"}); err == nil {
		t.Fatal("expected selector conflict")
	}
}

func TestParseTestOptionsAllowsResumeOnlyForSupplemental(t *testing.T) {
	t.Parallel()
	options, err := parseTestOptions([]string{"--supplemental", "--resume"})
	if err != nil || validateTestOptions(options) != nil || !options.request.Resume {
		t.Fatalf("options = %+v, error = %v", options, err)
	}
	options, err = parseTestOptions([]string{"--resume"})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTestOptions(options); err == nil {
		t.Fatal("test --resume without --supplemental was accepted")
	}
}

func TestParseTestReceiptOptionsRequiresOneCompleteMode(t *testing.T) {
	t.Parallel()
	output := filepath.Join(t.TempDir(), "receipts.json")
	options, err := parseTestReceiptOptions([]string{"export", "--output", output})
	if err != nil || options.mode != "export" || options.output != output {
		t.Fatalf("export options = %+v, error = %v", options, err)
	}
	options, err = parseTestReceiptOptions([]string{"import", "--source", output, "--sha256", strings.Repeat("a", 64)})
	if err != nil || options.mode != "import" || options.source != output {
		t.Fatalf("import options = %+v, error = %v", options, err)
	}
	for _, arguments := range [][]string{{}, {"export"}, {"import", "--source", output}, {"unknown"}} {
		if _, err := parseTestReceiptOptions(arguments); err == nil {
			t.Fatalf("accepted incomplete options %v", arguments)
		}
	}
}

func TestParseBaseOption(t *testing.T) {
	t.Parallel()
	base, err := parseBaseOption("test-plan", []string{"--base", "origin/main"})
	if err != nil || base != "origin/main" {
		t.Fatalf("base=%q err=%v", base, err)
	}
}

func TestDependencyReviewRequiresBase(t *testing.T) {
	t.Parallel()
	if _, err := parseRequiredBaseOption("dependency-review", nil); err == nil {
		t.Fatal("dependency-review accepted a missing base")
	}
}

func TestCheckNameRejectsBroadSelectionOptions(t *testing.T) {
	t.Parallel()
	if _, err := handleCheck(t.Context(), &engine.Engine{}, []string{"--name", "content-check", "--all"}); err == nil {
		t.Fatal("check --name accepted a broad selection option")
	}
}

func TestMergeGateRequiresBaseAndRejectsDownscoping(t *testing.T) {
	t.Parallel()
	if _, err := parseRequiredBaseOption("merge-gate", nil); err == nil {
		t.Fatal("expected a missing-base error")
	}
	if base, err := parseRequiredBaseOption("merge-gate", []string{"--base", "origin/main"}); err != nil || base != "origin/main" {
		t.Fatalf("base=%q err=%v", base, err)
	}
	if _, err := parseRequiredBaseOption("merge-gate", []string{"--base", "origin/main", "--files", "content/a.json"}); err == nil {
		t.Fatal("expected downscoping option to be rejected")
	}
	if _, err := parseRequiredBaseOption("merge-gate", []string{"--base", "main", "--base", "HEAD"}); err == nil {
		t.Fatal("expected duplicate base to be rejected")
	}
	options, err := parseMergeGateOptions([]string{"--base", "origin/main", "--resume"})
	if err != nil || options.base != "origin/main" || !options.resume {
		t.Fatalf("options=%+v err=%v", options, err)
	}
	if _, err := parseMergeGateOptions([]string{"--base", "origin/main", "--resume", "--resume"}); err == nil {
		t.Fatal("expected duplicate resume to be rejected")
	}
	if _, err := parseMergeGateOptions([]string{"--base", "origin/main", "--files", "content/a.json"}); err == nil {
		t.Fatal("expected merge-gate downscoping to be rejected")
	}
}

func TestCheckpointGateRequiresBaseAndRejectsDownscoping(t *testing.T) {
	t.Parallel()
	if _, err := parseRequiredBaseOption("checkpoint-gate", nil); err == nil {
		t.Fatal("expected a missing-base error")
	}
	if base, err := parseRequiredBaseOption("checkpoint-gate", []string{"--base", "HEAD~1"}); err != nil || base != "HEAD~1" {
		t.Fatalf("base=%q err=%v", base, err)
	}
	if _, err := parseRequiredBaseOption("checkpoint-gate", []string{"--base", "HEAD~1", "--all"}); err == nil {
		t.Fatal("expected downscoping option to be rejected")
	}
}

func TestPrintReportSummarizesAcceptedCheckpointForHumans(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{CheckpointPolicy: &engine.CheckpointPolicy{
		Scope: "changed", Base: "HEAD~1", Candidate: strings.Repeat("a", 40),
		ReceiptPath: ".code-polishy-reports/checkpoint-gate/receipt.json",
	}}
	printReportTo(stdout, stderr, report)
	want := "PASS errors=0 warnings=0 information=0\nCHECKPOINT GATE: CHANGED against HEAD~1\nCHECKPOINT ACCEPTED: " + strings.Repeat("a", 40) + "\n"
	if !strings.HasPrefix(stdout.String(), want) || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestPrintReportShowsGateArtifactAndBothTestReminderScopes(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{
		GateRunPolicy: &engine.GateRunPolicy{
			Status: "failed", ReportPath: ".code-polishy-reports/merge-gate/run/report.json", ReusedPhases: []string{"unit"},
		},
		TestQualityReminder: &engine.TestQualityReminder{
			ChangedTestPaths: []string{"tests/all_test.go"}, TaskBase: "checkpoint-sha", TaskChangedTestPaths: []string{"tests/task_test.go"},
		},
		Findings: []policy.Finding{{Check: "test.unit", Path: "repository", Subject: "unit", Message: "failed"}},
	}
	printReportTo(stdout, stderr, report)
	for _, want := range []string{
		"MERGE-TARGET CHANGED TEST FILES: 1", "LATEST TASK CHANGED TEST FILES: 1 since checkpoint-sha",
		"GATE RUN: FAILED", "GATE REPORT: .code-polishy-reports/merge-gate/run/report.json", "REUSED TEST PHASES: unit",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q: %q", want, stdout.String())
		}
	}
}

func TestPrintReportDoesNotAcceptFailedCheckpoint(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{
		CheckpointPolicy: &engine.CheckpointPolicy{Scope: "changed", Base: "HEAD~1", Candidate: strings.Repeat("a", 40)},
		Findings:         []policy.Finding{{Check: "policy.behaviorReview", Path: "receipt.json", Subject: "receipt", Message: "missing"}},
	}
	printReportTo(stdout, stderr, report)
	if strings.Contains(stdout.String(), "CHECKPOINT ACCEPTED") || !strings.HasPrefix(stdout.String(), "CHECKPOINT GATE: CHANGED against HEAD~1\n") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPrintReportSummarizesMergePolicyForHumans(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{MergePolicy: &engine.MergePolicy{
		Level: "recommended", Base: "origin/main", Reasons: []string{"content-only change"},
	}}
	printReportTo(stdout, stderr, report)
	wantPrefix := "PASS errors=0 warnings=0 information=0\nMERGE GATE: RECOMMENDED against origin/main\n"
	if !strings.HasPrefix(stdout.String(), wantPrefix) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "MERGE POLICY REASON") {
		t.Fatalf("concise output leaked verbose receipt: %q", stdout.String())
	}
}

func TestPrintReportSummarizesDocumentationMergePolicyForHumans(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{MergePolicy: &engine.MergePolicy{Level: "documentation", Base: "origin/main"}}
	printReportTo(stdout, stderr, report)
	wantPrefix := "PASS errors=0 warnings=0 information=0\nMERGE GATE: DOCUMENTATION against origin/main\n"
	if !strings.HasPrefix(stdout.String(), wantPrefix) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPrintReportRendersExactlyOneConciseBehaviorReviewStatusForBaseAwareReports(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		report  engine.Report
		want    string
		final   string
		finding string
	}{
		{
			name: "planning optional",
			report: engine.Report{
				MergePolicy: &engine.MergePolicy{Level: "recommended", Base: "origin/main"},
				BehaviorReview: &engine.BehaviorReviewStatus{
					State: engine.BehaviorReviewNotRun, FinalStateState: engine.BehaviorReviewNotRun, RequiredBoundary: engine.BehaviorReviewOnRequest,
					SelectedFeatures: []engine.BehaviorReviewFeatureSelection{}, SelectionDigest: strings.Repeat("a", 64),
				},
			},
			want:  "BEHAVIOR REVIEW: NOT RUN (optional)\n",
			final: "FINAL STATE: NOT RUN (optional)\n",
		},
		{
			name: "checkpoint required",
			report: engine.Report{
				CheckpointPolicy: &engine.CheckpointPolicy{Scope: "changed", Base: "TASK_BASE", Candidate: strings.Repeat("b", 40)},
				BehaviorReview: &engine.BehaviorReviewStatus{
					State: engine.BehaviorReviewRequired, FinalStateState: engine.BehaviorReviewNotRun, RequiredBoundary: engine.BehaviorReviewCheckpoint,
					SelectedFeatures: []engine.BehaviorReviewFeatureSelection{
						{Name: "authentication", Reasons: []string{"task-requested"}},
					},
					SelectionDigest: strings.Repeat("c", 64),
				},
			},
			want:  "BEHAVIOR REVIEW: REQUIRED (authentication)\n",
			final: "FINAL STATE: NOT RUN (required)\n",
		},
		{
			name: "merge passed",
			report: engine.Report{
				MergePolicy: &engine.MergePolicy{Level: "full", Base: "origin/main"},
				BehaviorReview: &engine.BehaviorReviewStatus{
					State: engine.BehaviorReviewPassed, FinalStateState: engine.BehaviorReviewPassed, RequiredBoundary: engine.BehaviorReviewMerge,
					SelectedFeatures: []engine.BehaviorReviewFeatureSelection{
						{Name: "checkout", Reasons: []string{"candidate-required"}},
					},
					SelectionDigest: strings.Repeat("d", 64), ReviewID: "review-123", ReceiptPath: ".code-polishy-reports/behavior-review/receipt.json",
				},
			},
			want:  "BEHAVIOR REVIEW: PASSED (checkout)\n",
			final: "FINAL STATE: PASSED (checkout)\n",
		},
		{
			name: "merge failed full candidate",
			report: engine.Report{
				MergePolicy: &engine.MergePolicy{Level: "full", Base: "origin/main"},
				BehaviorReview: &engine.BehaviorReviewStatus{
					State: engine.BehaviorReviewFailed, FinalStateState: engine.BehaviorReviewFailed, RequiredBoundary: engine.BehaviorReviewMerge,
					SelectedFeatures: []engine.BehaviorReviewFeatureSelection{}, FullCandidate: true, SelectionDigest: strings.Repeat("e", 64),
					FinalStateFindings: []finalstate.Finding{{Kind: finalstate.KindMetaNote, Path: "README.md", Line: 18, Summary: "Describes the editing task."}},
				},
			},
			want:    "BEHAVIOR REVIEW: FAILED (all changes)\n",
			final:   "FINAL STATE: FAILED (all changes)\n",
			finding: "README.md:18 meta note: Describes the editing task.\n",
		},
		{
			name: "behavior failed after clean final-state review",
			report: engine.Report{
				MergePolicy: &engine.MergePolicy{Level: "full", Base: "origin/main"},
				BehaviorReview: &engine.BehaviorReviewStatus{
					State: engine.BehaviorReviewFailed, FinalStateState: engine.BehaviorReviewPassed, RequiredBoundary: engine.BehaviorReviewMerge,
					SelectedFeatures: []engine.BehaviorReviewFeatureSelection{}, FullCandidate: true, SelectionDigest: strings.Repeat("f", 64),
				},
			},
			want:  "BEHAVIOR REVIEW: FAILED (all changes)\n",
			final: "FINAL STATE: PASSED (all changes)\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			printReportTo(stdout, stderr, testCase.report)
			if strings.Count(stdout.String(), "BEHAVIOR REVIEW:") != 1 || strings.Count(stdout.String(), "FINAL STATE:") != 1 ||
				!strings.Contains(stdout.String(), testCase.want) || !strings.Contains(stdout.String(), testCase.final) ||
				(testCase.finding != "" && !strings.Contains(stdout.String(), testCase.finding)) || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestVerboseBehaviorReviewReportAddsDetailsWithoutRepeatingStatus(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	printReportWithMode(stdout, stderr, engine.Report{BehaviorReview: &engine.BehaviorReviewStatus{
		State: engine.BehaviorReviewPassed, FinalStateState: engine.BehaviorReviewPassed, RequiredBoundary: engine.BehaviorReviewMerge,
		SelectedFeatures: []engine.BehaviorReviewFeatureSelection{{Name: "checkout", Reasons: []string{"candidate-required", "task-requested"}}},
		SelectionDigest:  strings.Repeat("f", 64), ReviewID: "review-123", ReceiptPath: ".code-polishy-reports/behavior-review/receipt.json",
	}}, true)
	output := stdout.String()
	for _, want := range []string{
		"BEHAVIOR REVIEW: PASSED (checkout)", "BEHAVIOR REVIEW BOUNDARY: MERGE", "BEHAVIOR REVIEW FEATURE: checkout",
		"BEHAVIOR REVIEW REASON: checkout (candidate-required)", "BEHAVIOR REVIEW RECEIPT: .code-polishy-reports/behavior-review/receipt.json",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout missing %q: %q", want, output)
		}
	}
	if strings.Count(output, "BEHAVIOR REVIEW:") != 1 || strings.Count(output, "FINAL STATE:") != 1 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", output, stderr.String())
	}
}

func TestVerboseReportPreservesDetailedMergePolicyReceipt(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{MergePolicy: &engine.MergePolicy{
		Level: "recommended", Base: "origin/main", Reasons: []string{"content-only change"},
	}}
	printReportWithMode(stdout, stderr, report, true)
	wantPrefix := "PASS errors=0 warnings=0 information=0\nMERGE POLICY LEVEL: RECOMMENDED\nMERGE POLICY BASE: origin/main\nMERGE POLICY REASON: content-only change\n"
	if !strings.HasPrefix(stdout.String(), wantPrefix) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestConciseReportHidesRoutineNotes(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	printReportTo(stdout, stderr, engine.Report{Notes: []string{"inventory: 257 governed files"}})
	if strings.Contains(stdout.String(), "NOTE") || !strings.Contains(stdout.String(), "PASS errors=0 warnings=0 information=0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPrintReportDisclosesTrustedChangeBoundary(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{ChangeBoundary: &engine.ChangeBoundary{
		Base: "0123456789012345678901234567890123456789", Modules: []string{"notes", "theme"},
		Paths: []string{"docs/plans/existing.md"}, NewPaths: []string{"docs/plans/new.md"},
	}}
	printReportTo(stdout, stderr, report)
	wantPrefix := "PASS errors=0 warnings=0 information=0\n" +
		"CHANGE BOUNDARY BASE: 0123456789012345678901234567890123456789\n" +
		"CHANGE BOUNDARY MODULES: notes, theme\n" +
		"CHANGE BOUNDARY EXACT PATHS: docs/plans/existing.md\n" +
		"CHANGE BOUNDARY NEW PATHS: docs/plans/new.md\n"
	if !strings.HasPrefix(stdout.String(), wantPrefix) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func installedRelease(t *testing.T, revision string) string {
	t.Helper()
	host, err := release.Host()
	if err != nil {
		t.Fatalf("resolve the host: %v", err)
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, filepath.FromSlash(release.BinaryPath))
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(binary), err)
	}
	if err := os.WriteFile(binary, []byte("engine"), 0o755); err != nil {
		t.Fatalf("write %s: %v", binary, err)
	}
	content := sha256.Sum256([]byte("engine"))
	catalog := `{"protocol":"release-capabilities/v1","capabilities":[{"name":"check","kind":"command","description":"Check selected source.","aliases":[],"paths":[],"modules":[],"enforcement":["explicit"],"workflows":["docs/agent-workflows.md"]}]}`
	catalogDigest := sha256.Sum256([]byte(catalog))
	workflow := "# Agent workflows\n"
	workflowDigest := sha256.Sum256([]byte(workflow))
	writeBehaviorReviewCLIFile(t, directory, release.CapabilityCatalogPath, catalog)
	writeBehaviorReviewCLIFile(t, directory, "docs/agent-workflows.md", workflow)
	manifest := release.Manifest{
		ManifestVersion: release.ManifestVersion, CodePolishyVersion: "9.9.9",
		SourceRevision: revision, Host: host,
		Features: []string{"javascript-bundle"},
		Tools: release.Tools{
			Go: "1.26.6", Govulncheck: "1.3.0", Node: "24.18.0", OSVScanner: "2.4.0",
			PNPM: "11.13.0", Packaging: "26.3", Python: "3.12.13+20260728", Ruff: "0.16.0", Shellcheck: "0.11.0", Staticcheck: "0.7.0",
			Ty: "0.0.65", Vulture: "2.16",
		},
		CapabilityCatalogSHA256: hex.EncodeToString(catalogDigest[:]),
		Entries: []release.Entry{
			{Path: release.BinaryPath, SHA256: hex.EncodeToString(content[:])},
			{Path: "docs/agent-workflows.md", SHA256: hex.EncodeToString(workflowDigest[:])},
			{Path: release.CapabilityCatalogPath, SHA256: hex.EncodeToString(catalogDigest[:])},
		},
	}
	manifest.EntryCount = len(manifest.Entries)
	manifest.ContentDigest = release.EntriesDigest(manifest.Entries)

	manifest.ReleaseDigest = manifest.Identity()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("render the manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, release.ManifestFilename), encoded, 0o644); err != nil {
		t.Fatalf("write the manifest: %v", err)
	}
	return directory
}

func TestLockWritesTheLockRequiringTheRunningRelease(t *testing.T) {
	repoRoot := t.TempDir()
	installed := installedRelease(t, strings.Repeat("a", 40))
	if status := handleLockMeta(invocation{repoRoot: repoRoot, policyRoot: installed}); status != 0 {
		t.Fatalf("status = %d", status)
	}
	written, present, err := release.ReadLock(repoRoot)
	if !present || err != nil {
		t.Fatalf("present=%v err=%v", present, err)
	}
	manifest, _, err := release.ReadManifest(installed)
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}
	if written.LockVersion != release.LockVersion ||
		written.CodePolishyVersion != manifest.CodePolishyVersion ||
		written.ReleaseDigest != manifest.ReleaseDigest ||
		!slices.Equal(written.Features, manifest.Features) {
		t.Fatalf("written lock = %+v, manifest = %+v", written, manifest)
	}
}

func TestLockRefusesToRecordASourceCheckout(t *testing.T) {
	repoRoot := t.TempDir()
	if status := handleLockMeta(invocation{repoRoot: repoRoot, policyRoot: t.TempDir()}); status != 2 {
		t.Fatalf("status = %d", status)
	}
	if _, present, _ := release.ReadLock(repoRoot); present {
		t.Fatalf("a source checkout wrote a target lock")
	}
	if status := handleLockMeta(invocation{
		repoRoot: repoRoot, policyRoot: installedRelease(t, strings.Repeat("a", 40)), arguments: []string{"--force"},
	}); status != 2 {
		t.Fatalf("lock accepted an option: status = %d", status)
	}
}

func TestInstalledReleaseCanVerifyAReleaseTreeWithoutATargetLock(t *testing.T) {
	repoRoot := t.TempDir()
	installed := installedRelease(t, strings.Repeat("a", 40))
	status := run([]string{
		"--repo-root", repoRoot,
		"--policy-root", installed,
		"release-manifest", "verify", "--root", installed,
	})
	if status != 0 {
		t.Fatalf("release manifest verification required an unrelated target lock: status=%d", status)
	}
}

func TestInstalledReleaseGovernsOnlyTheRepositoryThatLocksIt(t *testing.T) {
	repoRoot := t.TempDir()
	installed := installedRelease(t, strings.Repeat("a", 40))

	if status, governed := requireLockedRelease(invocation{
		repoRoot: repoRoot, policyRoot: t.TempDir(), command: "check",
	}); !governed || status != 0 {
		t.Fatalf("the source runner was refused: status=%d governed=%v", status, governed)
	}
	if status, governed := requireLockedRelease(invocation{
		repoRoot: repoRoot, policyRoot: installed, command: "check",
	}); governed || status != 2 {
		t.Fatalf("a repository with no lock ran an installed release: status=%d governed=%v", status, governed)
	}
	if status, governed := requireLockedRelease(invocation{
		repoRoot: repoRoot, policyRoot: installed, command: "lock",
	}); !governed || status != 0 {
		t.Fatalf("lock could not run before a lock exists: status=%d governed=%v", status, governed)
	}
	if status, governed := requireLockedRelease(invocation{
		repoRoot: repoRoot, policyRoot: installed, command: "install-bundle",
	}); !governed || status != 0 {
		t.Fatalf("the local release installer required a target lock: status=%d governed=%v", status, governed)
	}

	if status := handleLockMeta(invocation{repoRoot: repoRoot, policyRoot: installed}); status != 0 {
		t.Fatalf("write the lock: status = %d", status)
	}
	if status, governed := requireLockedRelease(invocation{
		repoRoot: repoRoot, policyRoot: installed, command: "check",
	}); !governed || status != 0 {
		t.Fatalf("the locked release was refused: status=%d governed=%v", status, governed)
	}
	if status, governed := requireLockedRelease(invocation{
		repoRoot: repoRoot, policyRoot: installedRelease(t, strings.Repeat("b", 40)), command: "check",
	}); governed || status != 2 {
		t.Fatalf("a release the lock does not name governed the repository: status=%d governed=%v", status, governed)
	}
}

func TestNativeTaskSessionParsesAuthorityWithoutAWorkflowScript(t *testing.T) {
	repoRoot := t.TempDir()
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	options, err := parseTaskSessionWorkflowOptions(repoRoot, []string{"--module", "application", "--", "./run.exe", "--flag"})
	if err != nil {
		t.Fatal(err)
	}
	if options.repoRoot != resolvedRepoRoot || !slices.Equal(options.modules, []string{"application"}) ||
		!slices.Equal(options.command, []string{"./run.exe", "--flag"}) {
		t.Fatalf("options = %+v", options)
	}
}

func TestWorkflowCommandRefusesAGlobalConfigOption(t *testing.T) {
	status := handleTaskSessionWorkflowMeta(invocation{
		repoRoot: t.TempDir(), policyRoot: t.TempDir(), configPath: "alternate.json",
		command: "task-session",
	})
	if status != 2 {
		t.Fatalf("status=%d", status)
	}
}

func TestNativeTaskSessionFreezesCanonicalScopeAndPreservesWorkerArgv(t *testing.T) {
	repoRoot := t.TempDir()
	outputParent := t.TempDir()
	resolvedOutputParent, err := filepath.EvalSymlinks(outputParent)
	if err != nil {
		t.Fatal(err)
	}
	options, err := parseTaskSessionWorkflowOptions(repoRoot, []string{
		"--allow-path=plan.md", "--module", "engine", "--allow-new-path", "result.json",
		"--output-dir", filepath.Join(outputParent, "artifacts"), "--config=.code-polishy.json", "--promote",
		"--", "./worker", "argument with spaces", "--module=worker-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if options.repoRoot != resolvedRepoRoot || options.config != ".code-polishy.json" || !options.promote ||
		options.outputDir != filepath.Join(resolvedOutputParent, "artifacts") ||
		!slices.Equal(options.modules, []string{"engine"}) || !slices.Equal(options.allowedPaths, []string{"plan.md"}) ||
		!slices.Equal(options.allowedNewPaths, []string{"result.json"}) ||
		!slices.Equal(options.command, []string{"./worker", "argument with spaces", "--module=worker-value"}) {
		t.Fatalf("options=%+v", options)
	}
}

func TestNativeTaskSessionFreezesAndValidatesExactArtifacts(t *testing.T) {
	output := t.TempDir()
	options, err := parseTaskSessionArtifactOptions([]string{
		"freeze", "--output-dir", output, "--config", ".code-polishy.json",
		"--module", "cli", "--allow-path=plan with spaces.md", "--allow-new-path", "new.md",
		"--", "./worker", "Unicode-λ", "",
	})
	if err != nil {
		t.Fatalf("parse freeze options: %v", err)
	}
	scopeDigest, commandDigest, err := freezeTaskSessionArtifacts(options)
	if err != nil {
		t.Fatalf("freeze artifacts: %v", err)
	}
	scope, err := os.ReadFile(filepath.Join(output, taskSessionScopeArtifact))
	if err != nil {
		t.Fatalf("read scope artifact: %v", err)
	}
	wantScope := nulDelimited([]string{"--module", "cli", "--allow-path", "plan with spaces.md", "--allow-new-path", "new.md"})
	if !slices.Equal(scope, wantScope) {
		t.Fatalf("scope artifact = %q, want %q", scope, wantScope)
	}
	command, err := os.ReadFile(filepath.Join(output, taskSessionCommandArtifact))
	if err != nil {
		t.Fatalf("read command artifact: %v", err)
	}
	wantCommand := nulDelimited([]string{"./worker", "Unicode-λ", ""})
	if !slices.Equal(command, wantCommand) {
		t.Fatalf("command artifact = %q, want %q", command, wantCommand)
	}
	if err := validateTaskSessionArtifacts(taskSessionArtifactOptions{
		operation: "validate", outputDir: output, config: ".code-polishy.json",
		scopeDigest: scopeDigest, commandDigest: commandDigest,
	}); err != nil {
		t.Fatalf("validate frozen artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(output, taskSessionCommandArtifact), append(command, 'x'), 0o600); err != nil {
		t.Fatalf("tamper with command artifact: %v", err)
	}
	if err := validateTaskSessionArtifacts(taskSessionArtifactOptions{
		operation: "validate", outputDir: output, config: ".code-polishy.json",
		scopeDigest: scopeDigest, commandDigest: commandDigest,
	}); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("tampered command validation error = %v", err)
	}
}

func TestNativeTaskSessionArtifactValidationRejectsChangedScopeAndWeakDigests(t *testing.T) {
	output := t.TempDir()
	options := taskSessionArtifactOptions{
		operation: "freeze", outputDir: output, config: "config.json",
		scope: []string{"--module", "cli"}, command: []string{"worker"},
	}
	scopeDigest, commandDigest, err := freezeTaskSessionArtifacts(options)
	if err != nil {
		t.Fatalf("freeze artifacts: %v", err)
	}
	if _, err := parseTaskSessionArtifactOptions([]string{
		"validate", "--output-dir", output, "--scope-digest", strings.Repeat("a", 40),
		"--command-digest", commandDigest,
	}); err == nil {
		t.Fatal("validate accepted a legacy short digest")
	}
	if err := validateTaskSessionArtifacts(taskSessionArtifactOptions{
		operation: "validate", outputDir: output, config: "changed.json",
		scopeDigest: scopeDigest, commandDigest: commandDigest,
	}); err == nil || !strings.Contains(err.Error(), "scope changed") {
		t.Fatalf("changed config validation error = %v", err)
	}
}

func TestNativeTaskSessionArtifactFreezeRollsBackAsOneMutation(t *testing.T) {
	output := t.TempDir()
	commandPath := filepath.Join(output, taskSessionCommandArtifact)
	if err := os.WriteFile(commandPath, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write existing command artifact: %v", err)
	}
	_, _, err := freezeTaskSessionArtifacts(taskSessionArtifactOptions{
		operation: "freeze", outputDir: output,
		scope: []string{"--module", "cli"}, command: []string{"worker"},
	})
	if err == nil {
		t.Fatal("freeze replaced an existing artifact")
	}
	if _, err := os.Lstat(filepath.Join(output, taskSessionScopeArtifact)); !os.IsNotExist(err) {
		t.Fatalf("failed freeze left a scope artifact: %v", err)
	}
	contents, err := os.ReadFile(commandPath)
	if err != nil || string(contents) != "existing" {
		t.Fatalf("existing command artifact changed: contents=%q err=%v", contents, err)
	}
}
