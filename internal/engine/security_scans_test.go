package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

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
	if invocation.Command.ReportProtocol != policy.OSVVulnerabilityReportProtocol {
		return runner.Result{ExitStatus: 0}, runner.Output{}, nil
	}
	if scanner.unavailable {
		return runner.Result{ExitStatus: 2, FailureCategory: runner.FailureCommandExit}, runner.Output{Stderr: []byte("advisory service unavailable")}, errors.New("advisory service unavailable")
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

func TestSecurityScansReuseAcrossPreinstallAndPostinstallWithoutSkippingProviders(t *testing.T) {
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
	if err != nil || HasFindings(postinstall) || postinstall.Security.Scans[0].Status != "reused" || countCommand(scanner.commands, "osv-scan-root") != 1 || !slices.Contains(scanner.commands, "offline-supply") {
		t.Fatalf("post-install coverage or reuse: %+v commands=%v error=%v", postinstall, scanner.commands, err)
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
	executed := len(scanner.commands)
	second, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, second, err)
	if second.GateRunPolicy.Status != "already-passed" || len(scanner.commands) != executed {
		t.Fatalf("fresh gate reran work: %+v %v", second, err)
	}
	*now = now.Add(25 * time.Hour)
	scanner.advisory = true
	refreshed, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || !HasFindings(refreshed) || refreshed.GateRunPolicy.Status != "failed" || countCommand(scanner.commands, "full") != 1 || countCommand(scanner.commands, "osv-scan-root") != 2 {
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
	executed = len(scanner.commands)
	reused, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, reused, err)
	if len(scanner.commands) != executed {
		t.Fatalf("refreshed pass was not reusable: %+v %v", reused, err)
	}
}

func requireSuccessfulSecurityReport(t *testing.T, report Report, err error) {
	t.Helper()
	if err != nil || HasFindings(report) {
		t.Fatalf("security verification failed: %+v %v", report, err)
	}
}

func TestSecurityScanInputChangesInvalidateEvidence(t *testing.T) {
	t.Parallel()
	policyEngine, scanner, _ := securityTestEngine(t)
	for _, change := range []func(){
		func() {},
		func() { policyEngine.Repository.Config.SupplyChain.AuditLevel = "moderate" },
		func() { writeEngineFile(t, policyEngine.Repository.Root, "other/bom.json", `{"components":[]}`, 0o600) },
	} {
		change()
		report, err := policyEngine.SupplyChain(t.Context(), false)
		if err != nil || HasFindings(report) {
			t.Fatalf("scan after input change: %+v %v", report, err)
		}
	}
	if countCommand(scanner.commands, "osv-scan-root") != 3 {
		t.Fatalf("changed security inputs reused old results: %v", scanner.commands)
	}
}

func TestSecurityScansInvalidateWhenTrackedSBOMIgnoreRulesChange(t *testing.T) {
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
			scanner.advisory = true
			unchanged, err := policyEngine.SupplyChain(t.Context(), false)
			requireSuccessfulSecurityReport(t, unchanged, err)
			if countCommand(scanner.commands, "osv-scan-root") != 1 {
				t.Fatal("unchanged inputs did not reuse the recent clean scan")
			}
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
			if !found || countCommand(scanner.commands, "osv-scan-root") != 2 {
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

func TestPackageScanReuseCannotSatisfyAnUnavailableImageScan(t *testing.T) {
	t.Parallel()
	policyEngine, scanner, _ := securityTestEngine(t)
	policyEngine.Repository.Config.SupplyChain.ArtifactSecurity.Targets = []policy.ArtifactTarget{
		{Name: "application", Mode: "command", Producer: &policy.ArtifactProducer{Argv: []string{"build-image"}, Cwd: ".", Manifest: "image.json"}},
	}
	preinstall, err := policyEngine.DependencyReview(t.Context(), "main")
	requireSuccessfulSecurityReport(t, preinstall, err)
	for range 2 {
		report, err := policyEngine.SupplyChain(t.Context(), false)
		if err != nil || !dependencyReviewHasFinding(report, "artifactSecurity.operation") || report.Security.Status != "failed" || report.Security.Scans[0].Status != "reused" {
			t.Fatalf("package evidence bypassed an unavailable image scan: %+v %v", report, err)
		}
	}
	if countCommand(scanner.commands, "osv-scan-root") != 1 || countCommand(scanner.commands, "artifact-security-docker-preflight") != 2 {
		t.Fatalf("deferred image verification was skipped or package scan repeated: %v", scanner.commands)
	}
}

func TestInvalidOrFutureSecurityScanEvidenceIsReplacedByARealScan(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"invalid-report", "incomplete-report", "future", "expired"} {
		t.Run(mode, func(t *testing.T) {
			policyEngine, scanner, now := securityTestEngine(t)
			first, err := policyEngine.SupplyChain(t.Context(), false)
			if err != nil || HasFindings(first) {
				t.Fatalf("initial scan: %+v %v", first, err)
			}
			path := first.Security.Scans[0].ReportPath
			var record securityScanRecord
			if _, err := readSecurityEvidence(policyEngine.Repository, path, &record); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "invalid-report":
				record.Stdout = `{}`
			case "incomplete-report":
				record.Stdout = `{"results":[{"source":{"path":"uv.lock"},"packages":[{"package":{"name":"aiohttp"}}]}]}`
			case "future":
				record.ScannedAt = now.Add(time.Hour)
				record.LastSucceededAt = record.ScannedAt
			case "expired":
				*now = now.Add(policy.SecurityEvidenceLifetime)
			}
			if err := writeSecurityEvidence(policyEngine.Repository, path, record); err != nil {
				t.Fatal(err)
			}
			scanner.advisory = true
			next, err := policyEngine.SupplyChain(t.Context(), false)
			if err != nil || !HasFindings(next) || countCommand(scanner.commands, "osv-scan-root") != 2 || next.Security.Scans[0].Status != "scanned" {
				t.Fatalf("invalid evidence hid current vulnerability: %+v commands=%v error=%v", next, scanner.commands, err)
			}
		})
	}
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
