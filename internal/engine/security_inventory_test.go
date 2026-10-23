package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/supplychain"
)

func TestOSVInventoriesReuseOnlyMatchingCapturedPackages(t *testing.T) {
	installInventoryRegistry(t)
	for _, kind := range []string{"uv", "cargo"} {
		t.Run(kind, func(t *testing.T) {
			engine, scanner, now, scope := inventorySecurityEngine(t, kind)
			first, scans := runInventorySecurity(t, engine, "release:fixture")
			if HasFindings(first) || scans[1].Status != "scanned" || scanner.scans != 1 {
				t.Fatalf("initial inventory scan: %+v %+v", first, scans)
			}
			*now = now.Add(time.Hour)
			second, scans := runInventorySecurity(t, engine, "release:fixture")
			if HasFindings(second) || scans[1].Status != "reused" || scanner.scans != 1 || scanner.recursive != 2 || !scans[1].LastSucceededAt.Equal(first.Security.Scans[1].LastSucceededAt) || !scans[1].RefreshAfter.Equal(first.Security.Scans[1].RefreshAfter) {
				t.Fatalf("reuse extended freshness or skipped recursive coverage: %+v %+v", second, scans)
			}
			writeEngineFile(t, engine.Repository.Root, scope, inventoryLock(kind, "2.0.0"), 0o600)
			_, scans = runInventorySecurity(t, engine, "release:fixture")
			if scans[1].Status != "scanned" || scanner.scans != 2 || scanner.versions[1] != "2.0.0" {
				t.Fatalf("changed packages reused old output: %+v %v", scans, scanner.versions)
			}
			_, scans = runInventorySecurity(t, engine, "release:updated")
			if scans[1].Status != "scanned" || scanner.scans != 3 {
				t.Fatalf("changed scanner release reused evidence: %+v", scans)
			}
		})
	}
}

func TestOSVInventoryReevaluatesCurrentAssessments(t *testing.T) {
	installInventoryRegistry(t)
	for _, kind := range []string{"uv", "cargo"} {
		t.Run(kind, func(t *testing.T) {
			engine, scanner, _, scope := inventorySecurityEngine(t, kind)
			scanner.advisory = true
			first, _ := runInventorySecurity(t, engine, "release:fixture")
			if len(first.Findings) != 1 || first.Findings[0].Vulnerability == nil {
				t.Fatalf("missing inventory vulnerability: %+v", first)
			}
			ecosystem := "pypi"
			if kind == "cargo" {
				ecosystem = "crates.io"
			}
			assessment := policy.VulnerabilityAssessment{ID: "inventory", Advisory: "GHSA-inventory", Ecosystem: ecosystem, Package: "inventory-package", AffectedVersion: "1.0.0", Scope: scope, Severity: "low", Expires: policy.Date{Time: time.Now().UTC().AddDate(0, 0, 1)}}
			engine.Repository.Config.SupplyChain.VulnerabilityAssessments = []policy.VulnerabilityAssessment{assessment}
			accepted, scans := runInventorySecurity(t, engine, "release:fixture")
			if HasFindings(accepted) || len(accepted.Assessed) != 1 || scans[1].Status != "reused" || scanner.scans != 1 {
				t.Fatalf("assessment edit failed to reuse raw evidence: %+v %+v", accepted, scans)
			}
			engine.Repository.Config.SupplyChain.VulnerabilityAssessments[0].Expires.Time = time.Now().UTC().AddDate(0, 0, -1)
			expired, scans := runInventorySecurity(t, engine, "release:fixture")
			if !HasFindings(expired) || scans[1].Status != "reused" || scanner.scans != 1 || len(expired.Assessed) != 0 {
				t.Fatalf("expired assessment retained acceptance: %+v %+v", expired, scans)
			}
		})
	}
}

func TestOSVInventorySnapshotSurvivesLiveLockChanges(t *testing.T) {
	installInventoryRegistry(t)
	for _, kind := range []string{"uv", "cargo"} {
		t.Run(kind, func(t *testing.T) {
			engine, scanner, _, scope := inventorySecurityEngine(t, kind)
			scanner.mutate = func() { writeEngineFile(t, engine.Repository.Root, scope, inventoryLock(kind, "2.0.0"), 0o600) }
			_, scans := runInventorySecurity(t, engine, "release:fixture")
			if scanner.versions[0] != "1.0.0" || scans[1].Status != "scanned" {
				t.Fatalf("scanner consumed the live replacement: %v %+v", scanner.versions, scans)
			}
			scanner.mutate = nil
			_, scans = runInventorySecurity(t, engine, "release:fixture")
			if scanner.versions[1] != "2.0.0" || scans[1].Status != "scanned" {
				t.Fatalf("replacement reused the wrong snapshot: %v %+v", scanner.versions, scans)
			}
			writeEngineFile(t, engine.Repository.Root, scope, inventoryLock(kind, "1.0.0"), 0o600)
			_, scans = runInventorySecurity(t, engine, "release:fixture")
			if scanner.scans != 2 || scans[1].Status != "reused" {
				t.Fatalf("restoring original inventory lost valid evidence: %+v", scans)
			}
		})
	}
}

func TestOSVInventoryEnvironmentChangesInvalidateEvidence(t *testing.T) {
	installInventoryRegistry(t)
	engine, scanner, _, _ := inventorySecurityEngine(t, "uv")
	engine.Repository.Config.SupplyChain.Environment = []string{"CODE_POLISHY_INVENTORY_TEST"}
	t.Setenv("CODE_POLISHY_INVENTORY_TEST", "first")
	runInventorySecurity(t, engine, "release:fixture")
	t.Setenv("CODE_POLISHY_INVENTORY_TEST", "second")
	_, scans := runInventorySecurity(t, engine, "release:fixture")
	if scanner.scans != 2 || scans[1].Status != "scanned" || !slices.Contains(scanner.environment, "CODE_POLISHY_INVENTORY_TEST=second") {
		t.Fatalf("effective scanner environment was not bound and consumed: %+v %v", scans, scanner.environment)
	}
}

func inventorySecurityEngine(t *testing.T, kind string) (*Engine, *inventorySecurityRunner, *time.Time, string) {
	t.Helper()
	root := t.TempDir()
	scope := "uv.lock"
	if kind == "cargo" {
		scope = "Cargo.lock"
	}
	writeEngineFile(t, root, scope, inventoryLock(kind, "1.0.0"), 0o600)
	writeEngineFile(t, root, "osv-scanner.toml", "invalid repository scanner configuration", 0o600)
	initializeEngineGitRepository(t, root)
	scanner := &inventorySecurityRunner{t: t, repositoryRoot: root}
	now := time.Now().UTC()
	repo := repository.Repository{Root: root, PolicyRoot: enginePolicyRoot(t), Config: policy.Config{ActivePolicyModules: []policy.ActivePolicyModule{{Name: "osv", Root: "."}}}}
	engine := &Engine{Repository: repo, Runner: scanner, securityClock: func() time.Time { return now }}
	return engine, scanner, &now, scope
}

func inventoryLock(kind, version string) string {
	if kind == "cargo" {
		return fmt.Sprintf("version = 4\n[[package]]\nname = \"inventory-package\"\nversion = %q\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = %q\n", version, strings.Repeat("a", 64))
	}
	return fmt.Sprintf("[[package]]\nname = \"inventory-package\"\nversion = %q\nsource = { registry = \"https://pypi.org/simple\" }\n", version)
}

func runInventorySecurity(t *testing.T, engine *Engine, tool string) (Report, []policy.SecurityScan) {
	t.Helper()
	commands, err := supplychain.OnlineCommands(engine.Repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	scans := newSecurityScanRunner(engine, commands)
	scans.tools[engine.Repository.PolicyTool("osv-scanner")] = tool
	findings := supplychain.OnlineWithCommands(t.Context(), engine.Repository, nil, commands, &inventoryInvocationRecorder{delegate: scans, scanner: engine.Runner.(*inventorySecurityRunner)})
	report := engine.finish(findings, nil)
	report.Security = scans.verification()
	if len(scans.scans) != 2 {
		t.Fatalf("missing recursive and inventory observations: %+v %+v", scans.scans, report)
	}
	return report, scans.scans
}

type inventoryInvocationRecorder struct {
	delegate runner.Runner
	scanner  *inventorySecurityRunner
}

func (recorder *inventoryInvocationRecorder) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Snapshot != nil {
		recorder.scanner.invocation = invocation
	}
	return recorder.delegate.Execute(ctx, invocation)
}

type inventorySecurityRunner struct {
	invocation                   runner.Invocation
	t                            *testing.T
	repositoryRoot               string
	scans, recursive             int
	advisory, fail, incomplete   bool
	mutate                       func()
	roots, versions, environment []string
}

func (scanner *inventorySecurityRunner) Execute(_ context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if strings.HasPrefix(invocation.Command.Name, "osv-scan-") {
		scanner.recursive++
		return runner.Result{}, runner.Output{Stdout: []byte(`{"results":[]}`)}, nil
	}
	scanner.scans++
	scanner.roots = append(scanner.roots, invocation.Root)
	scanner.environment = slices.Clone(invocation.Command.EnvironmentOverrides)
	if invocation.Root == scanner.repositoryRoot || !invocation.Command.SealedEnvironment {
		scanner.t.Fatal("inventory scan was not isolated")
	}
	config, err := os.ReadFile(filepath.Join(invocation.Root, "osv-scanner.toml"))
	if err != nil || len(config) != 0 {
		scanner.t.Fatalf("scanner inherited repository configuration: %s %v", config, err)
	}
	if scanner.mutate != nil {
		scanner.mutate()
	}
	if scanner.fail {
		return runner.Result{ExitStatus: 2, FailureCategory: runner.FailureCommandExit}, runner.Output{}, errors.New("scanner unavailable")
	}
	if scanner.incomplete {
		return runner.Result{}, runner.Output{Stdout: []byte(`{"results":[]}`)}, nil
	}
	index := slices.Index(invocation.Command.Argv, "--lockfile")
	path := strings.TrimPrefix(invocation.Command.Argv[index+1], "osv-scanner:")
	data, err := os.ReadFile(filepath.Join(invocation.Root, filepath.FromSlash(path)))
	if err != nil {
		scanner.t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		scanner.t.Fatal(err)
	}
	result := report["results"].([]any)[0].(map[string]any)
	result["source"].(map[string]any)["path"] = filepath.Join(invocation.Root, filepath.FromSlash(path))
	pkg := result["packages"].([]any)[0].(map[string]any)
	scanner.versions = append(scanner.versions, pkg["package"].(map[string]any)["version"].(string))
	if scanner.advisory {
		pkg["groups"] = []any{map[string]any{"ids": []string{"GHSA-inventory"}, "max_severity": "low"}}
	}
	report["provider_metadata"] = map[string]any{"retained": true}
	data, err = json.Marshal(report)
	if err != nil {
		scanner.t.Fatal(err)
	}
	if scanner.advisory {
		return runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}, runner.Output{Stdout: data}, errors.New("vulnerabilities found")
	}
	return runner.Result{}, runner.Output{Stdout: data}, nil
}

type inventoryRegistry struct{}

func (inventoryRegistry) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "pypi.org" || request.URL.Path != "/pypi/inventory-package/json" {
		return nil, fmt.Errorf("unexpected registry request: %s", request.URL.Redacted())
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"releases":{"1.0.0":[{"upload_time_iso_8601":"2020-01-01T00:00:00Z"}],"2.0.0":[{"upload_time_iso_8601":"2020-01-01T00:00:00Z"}]}}`))}, nil
}

func installInventoryRegistry(t *testing.T) {
	t.Helper()
	prior := http.DefaultTransport
	http.DefaultTransport = inventoryRegistry{}
	t.Cleanup(func() { http.DefaultTransport = prior })
}

func TestOSVInventoryOptionsBindReuseAndOfflineDatabasesStayUncached(t *testing.T) {
	installInventoryRegistry(t)
	engine, scanner, _, _ := inventorySecurityEngine(t, "uv")
	runInventorySecurity(t, engine, "release:fixture")
	invocation := scanner.invocation
	invocation.Command.Argv = append(slices.Clone(invocation.Command.Argv), "--verbosity", "warn")
	for _, status := range []string{"scanned", "reused"} {
		scans := newSecurityScanRunner(engine, []policy.Command{invocation.Command})
		scans.tools[invocation.Command.Argv[0]] = "release:fixture"
		if _, _, err := scans.Execute(t.Context(), invocation); err != nil || scans.scans[0].Status != status || scanner.scans != 2 {
			t.Fatalf("changed options did not establish independent evidence: %+v %v", scans.scans, err)
		}
	}
	invocation.Command.Argv = append(invocation.Command.Argv, "--offline")
	for range 2 {
		scans := newSecurityScanRunner(engine, []policy.Command{invocation.Command})
		scans.tools[invocation.Command.Argv[0]] = "release:fixture"
		if _, _, err := scans.Execute(t.Context(), invocation); err != nil || scans.scans[0].Status != "scanned" {
			t.Fatalf("offline scan reused an unidentified database: %+v %v", scans.scans, err)
		}
	}
	if scanner.scans != 4 {
		t.Fatalf("offline scan was skipped: %d executions", scanner.scans)
	}
}

func TestGateAlwaysReevaluatesProvidersWithoutOSVAndRecovers(t *testing.T) {
	t.Parallel()
	engine := gateWarningCandidate(t, "recommended")
	provider := &failingEngineRunner{}
	engine.Runner = provider
	first, err := engine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, first, err)
	provider.failure = "offline-supply"
	failed, err := engine.MergeGate(t.Context(), "main")
	if err != nil || !HasFindings(failed) || failed.GateRunPolicy.Status != "failed" || countCommand(provider.commands, "focused") != 1 || countCommand(provider.commands, "offline-supply") != 2 {
		t.Fatalf("old acceptance concealed a failing provider or repeated tests: %+v %v %v", failed, provider.commands, err)
	}
	provider.failure = ""
	recovered, err := engine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, recovered, err)
	if recovered.GateRunPolicy.Status != "security-refreshed" || countCommand(provider.commands, "focused") != 1 || countCommand(provider.commands, "offline-supply") != 3 {
		t.Fatalf("provider recovery repeated source verification: %+v %v", recovered, provider.commands)
	}
}

func TestGateRecordsOSVSnapshotExecutionAndReuse(t *testing.T) {
	installInventoryRegistry(t)
	for _, status := range []string{"scanned", "reused"} {
		t.Run(status, func(t *testing.T) {
			engine, scanner, _, _ := inventorySecurityEngine(t, "uv")
			runInventorySecurity(t, engine, "release:fixture")
			invocation := scanner.invocation
			command := invocation.Command
			scans := newSecurityScanRunner(engine, []policy.Command{command})
			scans.tools[command.Argv[0]] = "release:fixture"
			executions := 1
			if status == "scanned" {
				scans.tools[command.Argv[0]] = "release:updated"
				executions++
			}
			planned := MergeGateExecutionCommand{Root: engine.Repository.Root, Category: gaterun.SupplyChain, Scope: "repository", Cost: "quick", Command: command}
			identity, err := gaterun.NewIdentity(gaterun.IdentityInput{
				Gate: gaterun.MergeGate, RequestedBase: "main", ExactBase: strings.Repeat("a", 40), Candidate: strings.Repeat("b", 40), PolicyLevel: "recommended",
				Release: gaterun.ReleaseIdentity{Version: "test", Digest: strings.Repeat("a", 64)}, ConfigurationSHA256: strings.Repeat("b", 64),
				Platform: gaterun.Platform{OS: "test", Arch: "test"}, Commands: []gaterun.CommandSpec{gateRunCommandSpec(planned, nil)},
				Environment: []gaterun.EnvironmentInput{}, AmbientEnvironment: []gaterun.EnvironmentInput{}, BehaviorReview: testGateRunBehaviorReview(),
			})
			if err != nil {
				t.Fatal(err)
			}
			run, err := gaterun.Start(gaterun.StartOptions{RepositoryRoot: engine.Repository.Root, Identity: identity})
			if err != nil {
				t.Fatal(err)
			}
			boundary := &gateArtifactRunner{delegate: scans, run: run, expected: []MergeGateExecutionCommand{planned}, progress: io.Discard}
			result, output, err := boundary.Execute(t.Context(), invocation)
			if err != nil || !invocation.Accept(result, output, err) || scans.scans[0].Status != status || scanner.scans != executions {
				t.Fatalf("gate lost snapshot execution or raw reuse: %+v %+v %v", result, scans.scans, err)
			}
			if _, err := run.Finalize(gaterun.FinalizeOptions{Status: gaterun.RunPassed, BehaviorReview: testGateRunBehaviorReview()}); err != nil {
				t.Fatal(err)
			}
			report, err := gaterun.LoadReport(engine.Repository.Root, identity)
			if err != nil || len(report.Commands) != 1 || report.Commands[0].Name != command.Name || report.Commands[0].Status != gaterun.Passed {
				t.Fatalf("gate lost planned inventory accounting: %+v %v", report.Commands, err)
			}
		})
	}
}

func TestOSVInventoryExpiredScanFailureRecovers(t *testing.T) {
	installInventoryRegistry(t)
	for _, kind := range []string{"uv", "cargo"} {
		t.Run(kind, func(t *testing.T) {
			engine, scanner, now, _ := inventorySecurityEngine(t, kind)
			first, _ := runInventorySecurity(t, engine, "release:fixture")
			*now = now.Add(policy.SecurityEvidenceLifetime)
			scanner.fail = true
			failed, scans := runInventorySecurity(t, engine, "release:fixture")
			if !HasFindings(failed) || scans[1].Status != "unavailable" || !scans[1].RefreshAfter.Equal(first.Security.Scans[1].RefreshAfter) {
				t.Fatalf("failed refresh renewed or concealed stale evidence: %+v %+v", failed, scans)
			}
			scanner.fail = false
			recovered, scans := runInventorySecurity(t, engine, "release:fixture")
			if HasFindings(recovered) || scans[1].Status != "scanned" || scanner.scans != 3 || !scans[1].LastSucceededAt.Equal(*now) {
				t.Fatalf("refresh did not recover: %+v %+v", recovered, scans)
			}
			for _, root := range scanner.roots {
				if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("snapshot survived completion: %s %v", root, err)
				}
			}
		})
	}
}

func TestOSVInventoryIncompleteReportsTriggerFreshScanAndRecover(t *testing.T) {
	installInventoryRegistry(t)
	for _, kind := range []string{"uv", "cargo"} {
		t.Run(kind, func(t *testing.T) {
			engine, scanner, _, _ := inventorySecurityEngine(t, kind)
			_, scans := runInventorySecurity(t, engine, "release:fixture")
			var record securityScanRecord
			if _, err := readSecurityEvidence(engine.Repository, scans[1].ReportPath, &record); err != nil {
				t.Fatal(err)
			}
			record.Stdout = `{"results":[]}`
			if err := writeSecurityEvidence(engine.Repository, scans[1].ReportPath, record); err != nil {
				t.Fatal(err)
			}
			scanner.incomplete = true
			incomplete, scans := runInventorySecurity(t, engine, "release:fixture")
			if !HasFindings(incomplete) || scans[1].Status != "unavailable" || scanner.scans != 2 {
				t.Fatalf("incomplete evidence passed coverage: %+v %+v", incomplete, scans)
			}
			scanner.incomplete = false
			_, scans = runInventorySecurity(t, engine, "release:fixture")
			if scans[1].Status != "scanned" || scanner.scans != 3 {
				t.Fatalf("incomplete refresh prevented recovery: %+v", scans)
			}
		})
	}
}
