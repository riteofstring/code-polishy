package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
)

func TestPrintReportLabelsNonBlockingAdvisory(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{Advisories: []policy.Advisory{{Check: "portability.machinePath", Path: "app.js", Subject: "3", Message: "machine path"}}}
	printReportTo(stdout, stderr, report)
	if !strings.Contains(stdout.String(), "WARN portability.machinePath") || !strings.Contains(stdout.String(), "PASS policy completed") {
		t.Fatalf("stdout = %q", stdout.String())
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

func TestParseTestRequestRejectsSupplementalWithFull(t *testing.T) {
	t.Parallel()
	if _, err := parseTestRequest(&engine.Engine{}, []string{"--supplemental", "--all"}); err == nil {
		t.Fatal("expected selector conflict")
	}
}

func TestParseBaseOption(t *testing.T) {
	t.Parallel()
	base, err := parseBaseOption("test-plan", []string{"--base", "origin/main"})
	if err != nil || base != "origin/main" {
		t.Fatalf("base=%q err=%v", base, err)
	}
}

func TestCommandHandlersExposeTestLevels(t *testing.T) {
	t.Parallel()
	if _, exists := commandHandlers()["test-levels"]; !exists {
		t.Fatal("test-levels handler is missing")
	}
}

func TestCommandHandlersExposeMergeGate(t *testing.T) {
	t.Parallel()
	if _, exists := commandHandlers()["merge-gate"]; !exists {
		t.Fatal("merge-gate handler is missing")
	}
}

func TestCommandHandlersExposeDependencyReview(t *testing.T) {
	t.Parallel()
	if _, exists := commandHandlers()["dependency-review"]; !exists {
		t.Fatal("dependency-review handler is missing")
	}
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
}

func TestPrintReportSummarizesMergePolicyForHumans(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	report := engine.Report{MergePolicy: &engine.MergePolicy{
		Level: "recommended", Base: "origin/main", Reasons: []string{"content-only change"},
	}}
	printReportTo(stdout, stderr, report)
	wantPrefix := "MERGE GATE: RECOMMENDED against origin/main\n"
	if !strings.HasPrefix(stdout.String(), wantPrefix) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "MERGE POLICY REASON") {
		t.Fatalf("concise output leaked verbose receipt: %q", stdout.String())
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
	wantPrefix := "MERGE POLICY LEVEL: RECOMMENDED\nMERGE POLICY BASE: origin/main\nMERGE POLICY REASON: content-only change\n"
	if !strings.HasPrefix(stdout.String(), wantPrefix) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestConciseReportHidesRoutineNotes(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	printReportTo(stdout, stderr, engine.Report{Notes: []string{"inventory: 257 governed files"}})
	if strings.Contains(stdout.String(), "NOTE") || !strings.Contains(stdout.String(), "PASS policy completed") {
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
	wantPrefix := "CHANGE BOUNDARY BASE: 0123456789012345678901234567890123456789\n" +
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

func TestPrintTableUsesStableASCIIColumns(t *testing.T) {
	t.Parallel()
	output := &bytes.Buffer{}
	printTable(output, engine.Table{
		Title:   "TEST LEVELS",
		Columns: []string{"LEVEL", "AUTH"},
		Rows:    [][]string{{"focused", "routine"}, {"full *", "choice"}},
	})
	want := "" +
		"TEST LEVELS\n" +
		"+---------+---------+\n" +
		"| LEVEL   | AUTH    |\n" +
		"+---------+---------+\n" +
		"| focused | routine |\n" +
		"| full *  | choice  |\n" +
		"+---------+---------+\n"
	if output.String() != want {
		t.Fatalf("table = %q, want %q", output.String(), want)
	}
}

// installedRelease writes the smallest tree that is a Code Polishy release: the
// engine binary a launcher would run, and the manifest recording it.
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
	manifest := release.Manifest{
		ManifestVersion: release.ManifestVersion, CodePolishyVersion: "9.9.9",
		SourceRevision: revision, Host: host,
		Features: []string{"javascript-bundle"},
		Tools: release.Tools{
			Go: "1.26.6", Govulncheck: "1.3.0", Node: "24.18.0", OSVScanner: "2.4.0",
			PNPM: "11.13.0", Ruff: "0.16.0", Shellcheck: "0.11.0", Staticcheck: "0.7.0",
		},
		Entries: []release.Entry{{Path: release.BinaryPath, SHA256: hex.EncodeToString(content[:])}},
	}
	manifest.EntryCount = len(manifest.Entries)
	manifest.ContentDigest = release.EntriesDigest(manifest.Entries)
	// A release is named by the release its own record describes, so a test
	// release records the digest a real one would.
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
	if err := manifest.Satisfies(written); err != nil {
		t.Fatalf("the written lock does not select the release that wrote it: %v", err)
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
