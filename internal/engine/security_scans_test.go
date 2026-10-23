package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRecursiveOSVScansDetectChangedIncludedRequirements(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"direct", "nested-ignored"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			included := "pinned.txt"
			if name == "nested-ignored" {
				included = "hidden/pinned.data"
				writeEngineFile(t, root, "requirements.txt", "-r includes/base.in\n", 0o600)
				writeEngineFile(t, root, "includes/base.in", "-r ../hidden/pinned.data\n", 0o600)
			} else {
				writeEngineFile(t, root, "requirements.txt", "-r pinned.txt\n", 0o600)
			}
			writeEngineFile(t, root, ".gitignore", ".code-polishy-reports/\nhidden/\n", 0o600)
			writeEngineFile(t, root, included, "requests==2.32.4\n", 0o600)
			initializeEngineGitRepository(t, root)
			repo := repository.Repository{Root: root, PolicyRoot: enginePolicyRoot(t)}
			command := policy.Command{
				Name: "osv-scan-root", Cwd: ".", TimeoutSeconds: 30,
				Argv:                 []string{repo.PolicyTool("osv-scanner"), "scan", "source", "--all-vulns", "--format", "json", "--verbosity", "error", "--recursive", "--experimental-exclude", "g:**/.code-polishy-reports", "--offline", "."},
				EnvironmentOverrides: []string{"OSV_SCANNER_LOCAL_DB_CACHE_DIRECTORY=" + requirementsAdvisoryDatabase(t)},
				ReportProtocol:       policy.OSVVulnerabilityReportProtocol,
			}
			policyEngine := &Engine{Repository: repo, Runner: runner.OSRunner{Stdout: io.Discard, Stderr: io.Discard}}
			first := newSecurityScanRunner(policyEngine, []policy.Command{command})
			if result, output, err := runner.RunCaptured(t.Context(), first, root, command, runner.CaptureStructured); err != nil || result.ExitStatus != 0 {
				t.Fatalf("initial clean scan: %+v %s %v", result, output.Stdout, err)
			}
			writeEngineFile(t, root, included, "requests==2.31.0\n", 0o600)
			for _, commandRunner := range []runner.Runner{policyEngine.Runner, newSecurityScanRunner(policyEngine, []policy.Command{command})} {
				result, output, err := runner.RunCaptured(t.Context(), commandRunner, root, command, runner.CaptureStructured)
				if err == nil || result.ExitStatus != 1 || !strings.Contains(string(output.Stdout), "GHSA-included-requirement") {
					t.Fatalf("changed included requirement concealed a vulnerability: %+v %s %v", result, output.Stdout, err)
				}
			}
		})
	}
}

func requirementsAdvisoryDatabase(t *testing.T) string {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	entry, err := archive.Create("GHSA-included-requirement.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.WriteString(entry, `{"schema_version":"1.7.0","id":"GHSA-included-requirement","modified":"2026-01-01T00:00:00Z","published":"2026-01-01T00:00:00Z","summary":"Offline requirements fixture","database_specific":{"severity":"HIGH"},"affected":[{"package":{"ecosystem":"PyPI","name":"requests"},"ranges":[{"type":"ECOSYSTEM","events":[{"introduced":"0"},{"fixed":"2.32.0"}]}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeEngineFile(t, root, "osv-scanner/PyPI/all.zip", data.String(), 0o600)
	return root
}

type changingSecurityRunner struct {
	commands    []string
	advisory    bool
	unavailable bool
	mutate      bool
}

func (scanner *changingSecurityRunner) Execute(_ context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	scanner.commands = append(scanner.commands, invocation.Command.Name)
	if scanner.mutate && invocation.Command.Name == "offline-supply" {
		err := os.WriteFile(filepath.Join(invocation.Root, "content/data.json"), []byte(`{"provider":"changed source"}`), 0o600)
		return runner.Result{}, runner.Output{}, err
	}
	audit := strings.HasPrefix(invocation.Command.Name, "pnpm-audit-")
	if invocation.Command.ReportProtocol != policy.OSVVulnerabilityReportProtocol && !audit {
		return runner.Result{ExitStatus: 0}, runner.Output{}, nil
	}
	if scanner.unavailable {
		return runner.Result{ExitStatus: 2, FailureCategory: runner.FailureCommandExit}, runner.Output{Stderr: []byte("advisory service unavailable")}, errors.New("advisory service unavailable")
	}
	if audit {
		advisories := `[]`
		if scanner.advisory {
			advisories = `[{"id":"GHSA-new-advisory","package":"example","severity":"critical","versions":["1.2.3"]}]`
		}
		return runner.Result{}, runner.Output{Stdout: []byte(`{"protocolVersion":3,"operation":"audit","result":{"advisories":` + advisories + `,"unsupported":[]}}`)}, nil
	}
	if scanner.advisory {
		data := `{"results":[{"source":{"path":"content/data.json"},"packages":[{"package":{"name":"example","version":"1.2.3","ecosystem":"npm"},"groups":[{"ids":["GHSA-new-advisory"],"max_severity":"critical"}]}]}]}`
		return runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}, runner.Output{Stdout: []byte(data)}, errors.New("vulnerabilities reported")
	}
	return runner.Result{ExitStatus: 0}, runner.Output{Stdout: []byte(`{"results":[]}`)}, nil
}

func securityTestEngine(t *testing.T) (*Engine, *changingSecurityRunner, *time.Time) {
	t.Helper()
	root := contentRepository(t, nil)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	path := filepath.Join(root, policy.ConfigFilename)
	configuration, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, policy.ConfigFilename, string(configuration)+"\n", 0o600)
	commitEngineCandidate(t, root, "change policy")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	policyEngine.Repository.Config.ActivePolicyModules = []policy.ActivePolicyModule{{Name: "osv", Root: "."}}
	scanner := &changingSecurityRunner{}
	policyEngine.Runner = scanner
	now := time.Now().UTC()
	policyEngine.securityClock = func() time.Time { return now }
	return policyEngine, scanner, &now
}

func TestRecursiveScansRunAcrossPreinstallAndPostinstallWithoutSkippingProviders(t *testing.T) {
	t.Parallel()
	policyEngine, scanner, _ := securityTestEngine(t)
	preinstall, err := policyEngine.DependencyReview(t.Context(), "main")
	if err != nil || HasFindings(preinstall) || preinstall.Security == nil || preinstall.Security.Status != "partial" {
		t.Fatalf("pre-install review: %+v %v", preinstall, err)
	}
	if slices.Contains(scanner.commands, "offline-supply") {
		t.Fatal("pre-install review ran repository code")
	}
	postinstall, err := policyEngine.SupplyChain(t.Context(), false)
	if err != nil || HasFindings(postinstall) || postinstall.Security.Scans[0].Status != "scanned" || countCommand(scanner.commands, "osv-scan-root") != 2 || !slices.Contains(scanner.commands, "offline-supply") {
		t.Fatalf("post-install scans or providers were skipped: %+v commands=%v error=%v", postinstall, scanner.commands, err)
	}
	data, err := os.ReadFile(filepath.Join(policyEngine.Repository.Root, postinstall.Security.Scans[0].ReportPath))
	if err != nil || !strings.Contains(string(data), `\"results\"`) {
		t.Fatalf("raw scanner report was not retained: %s %v", data, err)
	}
	if _, err := policyEngine.FinalizeReport("supply-chain", postinstall); err != nil {
		t.Fatal(err)
	}
}

func TestUnchangedGateRefreshesSecurityWithoutRepeatingTestsAndRecovers(t *testing.T) {
	t.Parallel()
	policyEngine, scanner, now := securityTestEngine(t)
	first, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, first, err)
	second, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, second, err)
	if second.GateRunPolicy.Status != "security-refreshed" || countCommand(scanner.commands, "full") != 1 || countCommand(scanner.commands, "osv-scan-root") != 2 {
		t.Fatalf("gate reuse skipped recursive security or repeated tests: %+v commands=%v %v", second, scanner.commands, err)
	}
	scanner.advisory = true
	refreshed, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || !HasFindings(refreshed) || refreshed.GateRunPolicy.Status != "failed" || countCommand(scanner.commands, "full") != 1 || countCommand(scanner.commands, "osv-scan-root") != 3 {
		t.Fatalf("new advisory was hidden or tests repeated: %+v commands=%v error=%v", refreshed, scanner.commands, err)
	}
	*now = now.Add(25 * time.Hour)
	scanner.unavailable = true
	failed, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || !HasFindings(failed) || failed.Security.Status != "failed" {
		t.Fatalf("failed refresh disappeared: %+v %v", failed, err)
	}
	scanner.unavailable, scanner.advisory = false, false
	recovered, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, recovered, err)
	if recovered.Security.Status != "current" || countCommand(scanner.commands, "full") != 1 {
		t.Fatalf("security-only recovery failed: %+v commands=%v error=%v", recovered, scanner.commands, err)
	}
	reused, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, reused, err)
	if countCommand(scanner.commands, "full") != 1 || countCommand(scanner.commands, "osv-scan-root") != 6 {
		t.Fatalf("refreshed pass skipped recursive security or repeated tests: %+v commands=%v %v", reused, scanner.commands, err)
	}
}

func requireSuccessfulSecurityReport(t *testing.T, report Report, err error) {
	t.Helper()
	if err != nil || HasFindings(report) {
		t.Fatalf("security verification failed: %+v %v", report, err)
	}
}

func TestRecursiveScansRunWhenTrackedSBOMIgnoreRulesChange(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, ignore      string
		untracked, remove bool
	}{
		{"root edited", ".gitignore", false, false},
		{"root removed", ".gitignore", false, true},
		{"nested edited", "content/.gitignore", false, false},
		{"nested removed", "content/.gitignore", false, true},
		{"ignored ignore file edited", "content/.gitignore", true, false},
		{"ignored ignore file removed", "content/.gitignore", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			policyEngine, scanner, _ := securityTestEngine(t)
			root := policyEngine.Repository.Root
			sbom := filepath.Join(filepath.Dir(test.ignore), "inventory.cdx.json")
			writeEngineFile(t, root, sbom, `{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"components":[{"type":"library","name":"example","version":"1.2.3","purl":"pkg:npm/example@1.2.3"}]}`, 0o600)
			gitEngineOutput(t, root, "add", "--force", "--", sbom)
			remaining := ".code-polishy-reports/\n"
			if test.untracked {
				remaining += ".gitignore\n"
			}
			writeEngineFile(t, root, test.ignore, remaining+"inventory.cdx.json\n", 0o600)
			if !test.untracked {
				gitEngineOutput(t, root, "add", "--force", "--", test.ignore)
			}
			first, err := policyEngine.SupplyChain(t.Context(), false)
			requireSuccessfulSecurityReport(t, first, err)
			unchanged, err := policyEngine.SupplyChain(t.Context(), false)
			requireSuccessfulSecurityReport(t, unchanged, err)
			if countCommand(scanner.commands, "osv-scan-root") != 2 {
				t.Fatal("unchanged recursive scan reused incomplete input evidence")
			}
			scanner.advisory = true
			if test.remove {
				if err := os.Remove(filepath.Join(root, test.ignore)); err != nil {
					t.Fatal(err)
				}
			} else {
				writeEngineFile(t, root, test.ignore, remaining, 0o600)
			}
			included, err := policyEngine.SupplyChain(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			found := slices.ContainsFunc(included.Findings, func(finding policy.Finding) bool {
				return finding.Vulnerability != nil && finding.Vulnerability.Advisory == "GHSA-new-advisory"
			})
			if !found || countCommand(scanner.commands, "osv-scan-root") != 3 {
				t.Fatalf("ignore change reused a clean scan and concealed the included vulnerability: %+v commands=%v", included, scanner.commands)
			}
		})
	}
}

func TestSecurityRefreshRejectsCandidateMutationAndRecoversAfterRestoration(t *testing.T) {
	t.Parallel()
	policyEngine, scanner, now := securityTestEngine(t)
	path := filepath.Join(policyEngine.Repository.Root, "content/data.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, initial, err)
	*now = now.Add(25 * time.Hour)
	scanner.mutate = true
	changed, err := policyEngine.MergeGate(t.Context(), "main")
	if err == nil || !HasFindings(changed) || changed.Security.Status != "unavailable" || changed.GateRunPolicy.Status != "failed" {
		t.Fatalf("refresh accepted modified source using historical tests: %+v %v", changed, err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	scanner.mutate = false
	restored, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, restored, err)
	if restored.Security.Status != "current" || countCommand(scanner.commands, "full") != 1 || countCommand(scanner.commands, "offline-supply") != 3 {
		t.Fatalf("restored candidate could not refresh security independently: %+v commands=%v", restored, scanner.commands)
	}
}

func TestRecursiveScanCannotSatisfyAnUnavailableImageScan(t *testing.T) {
	t.Parallel()
	policyEngine, scanner, _ := securityTestEngine(t)
	policyEngine.Repository.Config.SupplyChain.ArtifactSecurity.Targets = []policy.ArtifactTarget{
		{Name: "application", Mode: "command", Producer: &policy.ArtifactProducer{Argv: []string{"build-image"}, Cwd: ".", Manifest: "image.json"}},
	}
	preinstall, err := policyEngine.DependencyReview(t.Context(), "main")
	requireSuccessfulSecurityReport(t, preinstall, err)
	for range 2 {
		report, err := policyEngine.SupplyChain(t.Context(), false)
		if err != nil || !dependencyReviewHasFinding(report, "artifactSecurity.operation") || report.Security.Status != "failed" || report.Security.Scans[0].Status != "scanned" {
			t.Fatalf("package evidence bypassed an unavailable image scan: %+v %v", report, err)
		}
	}
	if countCommand(scanner.commands, "osv-scan-root") != 3 || countCommand(scanner.commands, "artifact-security-docker-preflight") != 2 {
		t.Fatalf("deferred image verification or recursive scan was skipped: %v", scanner.commands)
	}
}

func TestInvalidOrFutureSecurityScanEvidenceIsReplacedByARealScan(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"invalid-report", "incomplete-report", "future", "expired"} {
		t.Run(mode, func(t *testing.T) {
			root, scanner, now := t.TempDir(), &changingSecurityRunner{}, time.Now().UTC()
			writeEngineFile(t, root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n", 0o600)
			command := auditSecurityCommand(t, root, ".")
			first := auditSecurityScanRunner(t, root, scanner, &now, command)
			runAuditSecurityScan(t, first, command)
			path := first.scans[0].ReportPath
			var record securityScanRecord
			if _, err := readSecurityEvidence(first.repo, path, &record); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "invalid-report":
				record.Stdout = `{}`
			case "incomplete-report":
				record.Stdout = `{"protocolVersion":3,"operation":"audit"}`
			case "future":
				record.ScannedAt = now.Add(time.Hour)
				record.LastSucceededAt = record.ScannedAt
			case "expired":
				now = now.Add(policy.SecurityEvidenceLifetime)
			}
			if err := writeSecurityEvidence(first.repo, path, record); err != nil {
				t.Fatal(err)
			}
			scanner.advisory = true
			next := auditSecurityScanRunner(t, root, scanner, &now, command)
			result := runAuditSecurityScan(t, next, command)
			if len(result.Advisories) != 1 || countCommand(scanner.commands, command.Name) != 2 || next.scans[0].Status != "scanned" {
				t.Fatalf("invalid evidence hid current vulnerability: %+v commands=%v", result, scanner.commands)
			}
		})
	}
}

func TestPNPMAuditsReuseOnlyTheirDeclaredLockfile(t *testing.T) {
	t.Parallel()
	root, scanner, now := t.TempDir(), &changingSecurityRunner{}, time.Now().UTC()
	commands := []policy.Command{auditSecurityCommand(t, root, "one"), auditSecurityCommand(t, root, "two")}
	for _, command := range commands {
		writeEngineFile(t, root, command.Paths[0], "lockfileVersion: '9.0'\n", 0o600)
	}
	for range 2 {
		scans := auditSecurityScanRunner(t, root, scanner, &now, commands...)
		for _, command := range commands {
			if result := runAuditSecurityScan(t, scans, command); len(result.Advisories) != 0 {
				t.Fatalf("initial audit was not clean: %+v", result)
			}
		}
	}
	if len(scanner.commands) != 2 {
		t.Fatalf("unchanged explicit inputs did not reuse audits: %v", scanner.commands)
	}
	writeEngineFile(t, root, commands[1].Paths[0], "lockfileVersion: '9.0'\npackages: {}\n", 0o600)
	scanner.advisory = true
	scans := auditSecurityScanRunner(t, root, scanner, &now, commands...)
	for index, command := range commands {
		if result := runAuditSecurityScan(t, scans, command); len(result.Advisories) != index {
			t.Fatalf("audit reused another project's input identity: %+v commands=%v", result, scanner.commands)
		}
	}
	if len(scanner.commands) != 3 {
		t.Fatalf("lock change did not rerun exactly its audit: %v", scanner.commands)
	}
}

func TestPNPMAuditCacheBindsTheScannedSnapshot(t *testing.T) {
	t.Parallel()
	for _, directory := range []string{".", "packages/web"} {
		t.Run(directory, func(t *testing.T) {
			root, now := t.TempDir(), time.Now().UTC()
			command := auditSecurityCommand(t, root, directory)
			vulnerable := "lockfileVersion: '9.0'\npackages:\n  example@1.2.3: {}\n"
			clean := "lockfileVersion: '9.0'\npackages:\n  example@2.0.0: {}\n"
			writeEngineFile(t, root, command.Paths[0], vulnerable, 0o600)
			scanner := &lockReadingAuditRunner{root: root, path: command.Paths[0], original: vulnerable, replacement: clean, restore: directory != "."}
			first := auditSecurityScanRunner(t, root, scanner, &now, command)
			if result := runAuditSecurityScan(t, first, command); len(result.Advisories) != 1 {
				t.Fatalf("audit scanned replacement bytes under the vulnerable lock's identity: %+v", result)
			}
			for _, step := range []struct {
				lock     string
				findings int
				status   string
			}{{vulnerable, 1, "reused"}, {clean, 0, "scanned"}, {vulnerable, 1, "reused"}} {
				writeEngineFile(t, root, command.Paths[0], step.lock, 0o600)
				scans := auditSecurityScanRunner(t, root, scanner, &now, command)
				result := runAuditSecurityScan(t, scans, command)
				if len(result.Advisories) != step.findings || scans.scans[0].Status != step.status {
					t.Fatalf("restoring a lock reused evidence for different bytes: %+v %+v", result, scans.scans)
				}
			}
			if len(scanner.scanner.commands) != 2 {
				t.Fatalf("unchanged snapshots did not reuse their own results: %v", scanner.scanner.commands)
			}
			for _, snapshot := range scanner.reads {
				if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
					t.Fatalf("audit snapshot was not removed: %s %v", snapshot, err)
				}
			}
		})
	}
}

func TestPNPMAuditSnapshotFailureRetainsExpiryAndAllowsRecovery(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"unavailable", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			root, now := t.TempDir(), time.Now().UTC()
			command := auditSecurityCommand(t, root, "packages/web")
			lock := "lockfileVersion: '9.0'\npackages: {}\n"
			writeEngineFile(t, root, command.Paths[0], lock, 0o600)
			scanner := &lockReadingAuditRunner{root: root, path: command.Paths[0], replacement: lock}
			first := auditSecurityScanRunner(t, root, scanner, &now, command)
			runAuditSecurityScan(t, first, command)
			previous := first.scans[0]
			now = now.Add(25 * time.Hour)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failure == "canceled" {
				scanner.cancel = cancel
			} else {
				scanner.scanner.unavailable = true
			}
			failed := auditSecurityScanRunner(t, root, scanner, &now, command)
			_, _, _ = runner.RunCaptured(ctx, failed, root, command, runner.CaptureStructured)
			if len(failed.scans) != 1 || failed.scans[0].Status != "unavailable" || !failed.scans[0].LastSucceededAt.Equal(previous.LastSucceededAt) || !failed.scans[0].RefreshAfter.Equal(previous.RefreshAfter) {
				t.Fatalf("failed audit extended successful evidence: %+v", failed.scans)
			}
			scanner.cancel, scanner.scanner.unavailable = nil, false
			recovered := auditSecurityScanRunner(t, root, scanner, &now, command)
			runAuditSecurityScan(t, recovered, command)
			if len(scanner.scanner.commands) != 3 || recovered.scans[0].Status != "scanned" || !recovered.scans[0].LastSucceededAt.Equal(now) {
				t.Fatalf("audit could not recover after a failed snapshot scan: %+v", recovered.scans)
			}
			for _, snapshot := range scanner.reads {
				if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
					t.Fatalf("audit snapshot was not removed: %s %v", snapshot, err)
				}
			}
		})
	}
}

func TestGateRecordsPlannedAuditAcrossSnapshotExecutionAndReuse(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"scanned", "reused"} {
		t.Run(status, func(t *testing.T) {
			root, now := t.TempDir(), time.Now().UTC()
			command := auditSecurityCommand(t, root, "packages/web")
			lock := "lockfileVersion: '9.0'\npackages: {}\n"
			writeEngineFile(t, root, command.Paths[0], lock, 0o600)
			scanner := &lockReadingAuditRunner{root: root, path: command.Paths[0], replacement: lock}
			if status == "reused" {
				runAuditSecurityScan(t, auditSecurityScanRunner(t, root, scanner, &now, command), command)
			}
			scans := auditSecurityScanRunner(t, root, scanner, &now, command)
			planned := MergeGateExecutionCommand{Root: root, Category: gaterun.SupplyChain, Scope: "repository", Cost: "quick", Command: command}
			identity, err := gaterun.NewIdentity(gaterun.IdentityInput{
				Gate: gaterun.MergeGate, RequestedBase: "main", ExactBase: strings.Repeat("a", 40), Candidate: strings.Repeat("b", 40), PolicyLevel: "recommended",
				Release: gaterun.ReleaseIdentity{Version: "test", Digest: strings.Repeat("a", 64)}, ConfigurationSHA256: strings.Repeat("b", 64),
				Platform: gaterun.Platform{OS: "test", Arch: "test"}, Commands: []gaterun.CommandSpec{gateRunCommandSpec(planned, nil)},
				Environment: []gaterun.EnvironmentInput{}, AmbientEnvironment: []gaterun.EnvironmentInput{}, BehaviorReview: testGateRunBehaviorReview(),
			})
			if err != nil {
				t.Fatal(err)
			}
			run, err := gaterun.Start(gaterun.StartOptions{RepositoryRoot: root, Identity: identity})
			if err != nil {
				t.Fatal(err)
			}
			boundary := &gateArtifactRunner{delegate: scans, run: run, expected: []MergeGateExecutionCommand{planned}, progress: io.Discard}
			result, output, err := runner.RunCaptured(t.Context(), boundary, root, command, runner.CaptureStructured)
			if err != nil || result.ExitStatus != 0 {
				t.Fatalf("gate rejected the declared audit: %+v %v", result, err)
			}
			if _, err := javascript.ParseAuditOutput(output.Stdout); err != nil {
				t.Fatal(err)
			}
			if _, err := run.Finalize(gaterun.FinalizeOptions{Status: gaterun.RunPassed, BehaviorReview: testGateRunBehaviorReview()}); err != nil {
				t.Fatal(err)
			}
			report, err := gaterun.LoadReport(root, identity)
			if err != nil || len(report.Commands) != 1 || report.Commands[0].Name != command.Name || report.Commands[0].Status != gaterun.Passed {
				t.Fatalf("gate lost the planned audit evidence: %+v %v", report.Commands, err)
			}
			if scans.scans[0].Status != status || len(scanner.scanner.commands) != 1 || scanner.reads[0] == filepath.Join(root, command.Paths[0]) {
				t.Fatalf("gate lost snapshot isolation or reuse: %+v %v", scans.scans, scanner.reads)
			}
		})
	}
}

type lockReadingAuditRunner struct {
	scanner               changingSecurityRunner
	root, path            string
	original, replacement string
	restore               bool
	reads                 []string
	cancel                context.CancelFunc
}

func (scanner *lockReadingAuditRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if len(scanner.scanner.commands) == 0 {
		if err := os.WriteFile(filepath.Join(scanner.root, scanner.path), []byte(scanner.replacement), 0o600); err != nil {
			return runner.Result{}, runner.Output{}, err
		}
	}
	var request struct{ Root, Directory string }
	if err := json.Unmarshal([]byte(invocation.Command.Argv[len(invocation.Command.Argv)-1]), &request); err != nil {
		return runner.Result{}, runner.Output{}, err
	}
	path := filepath.Join(request.Root, request.Directory, "pnpm-lock.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return runner.Result{}, runner.Output{}, err
	}
	scanner.reads = append(scanner.reads, path)
	if scanner.restore {
		if err := os.WriteFile(filepath.Join(scanner.root, scanner.path), []byte(scanner.original), 0o600); err != nil {
			return runner.Result{}, runner.Output{}, err
		}
	}
	scanner.scanner.advisory = strings.Contains(string(data), "example@1.2.3:")
	if scanner.cancel != nil {
		scanner.cancel()
	}
	return scanner.scanner.Execute(ctx, invocation)
}

func TestPNPMAuditsWithoutCompleteIdentityAlwaysExecute(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"undeclared-input", "unsealed-environment", "development-bundle"} {
		t.Run(mode, func(t *testing.T) {
			root, scanner, now := t.TempDir(), &changingSecurityRunner{}, time.Now().UTC()
			writeEngineFile(t, root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n", 0o600)
			command := auditSecurityCommand(t, root, ".")
			if mode == "undeclared-input" {
				command.Paths = nil
			} else if mode == "unsealed-environment" {
				command.SealedEnvironment = false
			}
			for attempt := range 2 {
				scanner.advisory = attempt == 1
				scans := auditSecurityScanRunner(t, root, scanner, &now, command)
				if mode == "development-bundle" {
					delete(scans.tools, command.Argv[0])
				}
				if result := runAuditSecurityScan(t, scans, command); len(result.Advisories) != attempt {
					t.Fatalf("incomplete identity reused a clean audit: %+v", result)
				}
			}
			if len(scanner.commands) != 2 {
				t.Fatalf("ineligible audit was skipped: %v", scanner.commands)
			}
		})
	}
}

func auditSecurityCommand(t *testing.T, root, directory string) policy.Command {
	t.Helper()
	invocation, err := (javascript.Bundle{PolicyRoot: enginePolicyRoot(t)}).AuditCommand(root, directory)
	if err != nil {
		t.Fatal(err)
	}
	return policy.Command{
		Name: "pnpm-audit-" + strings.ReplaceAll(directory, "/", "-"), Argv: invocation.Argv, Cwd: invocation.Cwd,
		SealedEnvironment: invocation.SealedEnvironment, TimeoutSeconds: invocation.TimeoutSeconds,
		Paths: []string{filepath.ToSlash(filepath.Join(directory, "pnpm-lock.yaml"))},
	}
}

func auditSecurityScanRunner(t *testing.T, root string, scanner runner.Runner, now *time.Time, commands ...policy.Command) *securityScanRunner {
	t.Helper()
	policyEngine := &Engine{Repository: repository.Repository{Root: root, PolicyRoot: enginePolicyRoot(t)}, Runner: scanner, securityClock: func() time.Time { return *now }}
	scans := newSecurityScanRunner(policyEngine, commands)
	for _, command := range commands {
		scans.tools[command.Argv[0]] = "release:fixture"
	}
	return scans
}

func runAuditSecurityScan(t *testing.T, scans *securityScanRunner, command policy.Command) javascript.AuditResult {
	t.Helper()
	_, output, err := runner.RunWithReport(t.Context(), scans, scans.repo.Root, command, func(_ runner.Result, output runner.Output, runErr error) bool {
		result, err := javascript.ParseAuditOutput(output.Stdout)
		return runErr == nil && err == nil && len(result.Unsupported) == 0
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := javascript.ParseAuditOutput(output.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func countCommand(commands []string, name string) int {
	count := 0
	for _, command := range commands {
		if command == name {
			count++
		}
	}
	return count
}
