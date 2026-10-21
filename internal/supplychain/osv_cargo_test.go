package supplychain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func TestOSVCargoRetainsAllLockedRegistryVersionsWithoutPrivateSources(t *testing.T) {
	t.Parallel()
	repo := cargoOSVRepository(t, cargoOSVMixedLock())
	repo.Config.SupplyChain.GitEvidence.Required = true
	commands, err := osvCommands(repo)
	if err != nil || len(commands) != 2 {
		t.Fatalf("Cargo public scan plan = %+v, %v", commands, err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, osvInputDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning wrote a projection: %v", err)
	}
	boundary := &cargoOSVBoundary{t: t}
	findings := scanOSVWithCommands(t.Context(), repo, commands, boundary)
	if boundary.scans != 2 || len(findings) != 1 {
		t.Fatalf("Cargo scan = %+v, commands %d", findings, boundary.scans)
	}
	identity := findings[0].Vulnerability
	if identity == nil || identity.Ecosystem != "crates.io" || identity.Package != "registry-crate" || identity.AffectedVersion != "1.0.0" || identity.Scope != "Cargo.lock" {
		t.Fatalf("Cargo advisory lost its exact identity: %+v", findings)
	}
	if identity.Advisory != "GHSA-cargo-example" || !slices.Contains(identity.Aliases, "RUSTSEC-2026-0001") || identity.Severity != "low" {
		t.Fatalf("Cargo advisory lost aliases or severity: %+v", identity)
	}
}

func TestOSVCargoRejectsUnsupportedSourcesBeforePlanningPublicScans(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"registry+https://private.example.test/index", "sparse+https://private.example.test/index/",
		"registry+https://token@github.com/rust-lang/crates.io-index", "git+https://private.example.test/repo#short",
	} {
		repo := cargoOSVRepository(t, reviewCargoLock("private-crate", "1.0.0", source))
		commands, err := osvCommands(repo)
		if err == nil || len(commands) != 0 || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "token") {
			t.Fatalf("unsafe Cargo scan plan = %+v, %v", commands, err)
		}
	}
}

func TestOSVCargoRejectsLinkedLocksAndChangedPublicPlans(t *testing.T) {
	t.Parallel()
	repo := cargoOSVRepository(t, cargoOSVMixedLock())
	commands, err := osvCommands(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeSupplyFile(t, repo.Root, "Cargo.lock", strings.ReplaceAll(cargoOSVMixedLock(), "1.0.0", "1.1.0"))
	boundary := &cargoOSVBoundary{t: t}
	findings := scanOSVWithCommands(t.Context(), repo, commands, boundary)
	if boundary.scans != 0 || len(findings) != 1 || findings[0].Check != "policy.supplyChain" {
		t.Fatalf("changed Cargo scan plan executed: %+v, %d scans", findings, boundary.scans)
	}
	if err := os.Rename(filepath.Join(repo.Root, "Cargo.lock"), filepath.Join(repo.Root, "original.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("original.lock", filepath.Join(repo.Root, "Cargo.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := osvCommands(repo); err == nil {
		t.Fatal("linked Cargo lock became a public scanner input")
	}
}

func TestOSVCargoPartitionKeepsIndependentNativeAndLockScopes(t *testing.T) {
	t.Parallel()
	repo := cargoOSVRepository(t, "version=4\n[[package]]\nname='local'\nversion='1.0.0'\n")
	repo.Config.ActivePolicyModules = []policy.ActivePolicyModule{{Name: "osv", Root: "native"}, {Name: "osv", Root: "rust"}}
	writeSupplyFile(t, repo.Root, "native/go.mod", "module example.test/native\n")
	for _, scope := range []string{"rust/Cargo.lock", "rust/nested/Cargo.lock", "outside/Cargo.lock"} {
		writeSupplyFile(t, repo.Root, scope, cargoOSVMixedLock())
	}
	scans, err := osvScanPlan(repo)
	if err != nil || len(scans) != 4 {
		t.Fatalf("scoped Cargo scan plan = %+v, %v", scans, err)
	}
	if slices.Contains(scans[0].Command.Argv, "--allow-no-lockfiles") || !slices.Contains(scans[1].Command.Argv, "--allow-no-lockfiles") {
		t.Fatalf("unrelated native missing-input coverage relaxed: %+v", scans)
	}
	if scans[2].Scope != "rust/Cargo.lock" || scans[3].Scope != "rust/nested/Cargo.lock" || scans[2].InputPath == scans[3].InputPath {
		t.Fatalf("independent Cargo lock scopes collapsed: %+v", scans)
	}
}

func TestOSVCargoProjectionDoesNotHideUncoveredDependencyInputs(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"package-lock.json", "other/Cargo.toml", "dist/go.mod"} {
		t.Run(path, func(t *testing.T) {
			repo := cargoOSVRepository(t, cargoOSVMixedLock())
			writeSupplyFile(t, repo.Root, path, "unreadable dependency input\n")
			commands, err := osvCommands(repo)
			if err != nil {
				t.Fatal(err)
			}
			if len(commands) != 2 || slices.Contains(commands[0].Argv, "--allow-no-lockfiles") {
				t.Fatalf("Cargo projection concealed an uncovered input: %+v", commands)
			}
		})
	}
}

func TestOSVCargoRejectsIncompleteOrForeignPackageReports(t *testing.T) {
	t.Parallel()
	repo := cargoOSVRepository(t, cargoOSVMixedLock())
	scans, err := osvScanPlan(repo)
	if err != nil {
		t.Fatal(err)
	}
	scan := scans[1]
	for _, mutation := range []string{"missing", "duplicate", "foreign-version", "foreign-ecosystem", "foreign-source", "empty"} {
		t.Run(mutation, func(t *testing.T) {
			var report osvReport
			if err := json.Unmarshal(scan.Projection, &report); err != nil {
				t.Fatal(err)
			}
			report.Results[0].Source.Path = scan.InputPath
			mutateCargoOSVReport(&report, mutation)
			payload, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseOSVScanReport(repo, scan, payload); err == nil {
				t.Fatalf("%s Cargo report satisfied locked package coverage", mutation)
			}
		})
	}
}

func TestOSVCargoAssessmentAndReportExitUseObservedExactCoordinates(t *testing.T) {
	t.Parallel()
	repo := cargoOSVRepository(t, cargoOSVMixedLock())
	scans, err := osvScanPlan(repo)
	if err != nil {
		t.Fatal(err)
	}
	scan := scans[1]
	output := runner.Output{Stdout: cargoOSVResponse(t, scan.Projection, scan.InputPath)}
	failure := errors.New("advisories found")
	result := runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}
	if !acceptedOSVReportExit(t.Context(), repo, scan, result, output, failure) {
		t.Fatal("complete Cargo vulnerability report lost report-bearing exit handling")
	}
	findings, err := parseOSVScanReport(repo, scan, output.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	assessment := policy.VulnerabilityAssessment{ID: "cargo", Ecosystem: "crates.io", Advisory: "RUSTSEC-2026-0001", Package: "registry-crate", AffectedVersion: "1.0.0", Scope: "Cargo.lock", Severity: "low", Expires: policy.Date{Time: now.AddDate(0, 0, 1)}}
	kept, accepted := policy.ApplyVulnerabilityAssessments(findings, []policy.VulnerabilityAssessment{assessment}, now, true)
	if len(kept) != 0 || len(accepted) != 1 {
		t.Fatalf("exact Cargo advisory assessment rejected: %+v, %+v", kept, accepted)
	}
	assessment.Scope = "nested/Cargo.lock"
	kept, accepted = policy.ApplyVulnerabilityAssessments(findings, []policy.VulnerabilityAssessment{assessment}, now, false)
	if len(kept) != 1 || len(accepted) != 0 {
		t.Fatal("assessment for a different Cargo lock hid the advisory")
	}
}

func TestOSVCargoSecurityCapabilityRequiresActiveScannerScope(t *testing.T) {
	t.Parallel()
	repo := cargoOSVRepository(t, cargoOSVMixedLock())
	files := []string{"Cargo.toml", "other/Cargo.toml"}
	repo.Config.ActivePolicyModules[0].Root = "other"
	findings := CoverageFindings(repo, files)
	if len(findings) != 1 || findings[0].Path != "Cargo.toml" || findings[0].Subject != "repository:security" {
		t.Fatalf("Cargo scanner scope coverage = %+v", findings)
	}
	repo.Config.ActivePolicyModules[0].Root = "."
	if findings := CoverageFindings(repo, files); len(findings) != 0 {
		t.Fatalf("native Cargo advisory coverage still required a provider: %+v", findings)
	}
}

func TestOSVCargoMemberScopeDoesNotCoverAnAncestorWorkspaceLock(t *testing.T) {
	t.Parallel()
	repo := cargoOSVRepository(t, cargoOSVMixedLock())
	repo.Config.ActivePolicyModules[0].Root = "members/app"
	lock, err := repo.ReadCargoLock("Cargo.lock")
	if err != nil {
		t.Fatal(err)
	}
	resolution := rustfacts.Resolution{Manifest: "members/app/Cargo.toml", Lock: lock}
	files := []string{resolution.Manifest}
	findings := cargoAdvisoryScopeFindings(repo, files, resolution)
	if len(findings) != 1 || findings[0].Path != "Cargo.lock" || findings[0].Check != "policy.supplyChainCoverage" {
		t.Fatalf("member-only scan claimed an unscanned workspace lock: %+v", findings)
	}
	repo.Config.Checks = []policy.Command{{Name: "workspace-security", Provides: []string{"security"}, RunOn: []string{"security"}}}
	if findings := cargoAdvisoryScopeFindings(repo, files, resolution); len(findings) != 0 {
		t.Fatalf("governed workspace security provider was ignored: %+v", findings)
	}
	repo.Config.Checks = nil
	repo.Config.ActivePolicyModules[0].Root = "."
	if findings := cargoAdvisoryScopeFindings(repo, files, resolution); len(findings) != 0 {
		t.Fatalf("actual workspace scanner scope was rejected: %+v", findings)
	}
}

func TestOSVCargoKeepsOperationalFailuresBesideValidAdvisories(t *testing.T) {
	t.Parallel()
	repo := cargoOSVRepository(t, cargoOSVMixedLock())
	scans, err := osvScanPlan(repo)
	if err != nil {
		t.Fatal(err)
	}
	scan := scans[1]
	output := runner.Output{Stdout: cargoOSVResponse(t, scan.Projection, scan.InputPath)}
	for _, result := range []runner.Result{
		{ExitStatus: 1, FailureCategory: runner.FailureOperational},
		{ExitStatus: 2, FailureCategory: runner.FailureCommandExit},
		{ExitStatus: 124, FailureCategory: runner.FailureTimeout},
	} {
		boundary := cargoOSVFailure{result: result, output: output}
		findings := scanOSVRoot(t.Context(), repo, scan, boundary)
		checks := supplyChecks(findings)
		if !checks["policy.securityScanner"] || !checks["supplyChain.osvVulnerability"] {
			t.Fatalf("scanner failure %v disappeared behind advisories: %+v", result, findings)
		}
	}
}

type cargoOSVFailure struct {
	result runner.Result
	output runner.Output
}

func (boundary cargoOSVFailure) runPlain(context.Context, string, policy.Command) error {
	return fmt.Errorf("unexpected unstructured scan")
}

func (boundary cargoOSVFailure) runOutput(context.Context, string, policy.Command) (runner.Result, runner.Output, error) {
	return boundary.result, boundary.output, fmt.Errorf("scanner failed")
}

func cargoOSVRepository(t *testing.T, lock string) repository.Repository {
	t.Helper()
	repo := supplyRepository(t)
	repo.PolicyRoot = repo.Root
	repo.Config.ActivePolicyModules = []policy.ActivePolicyModule{{Name: "osv", Root: "."}}
	writeSupplyFile(t, repo.Root, ".tools/bin/osv-scanner", "fixture")
	if err := os.Chmod(repo.PolicyTool("osv-scanner"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSupplyFile(t, repo.Root, "Cargo.lock", lock)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='private-workspace'\nversion='1.0.0'\n[dependencies]\nprivate={git='https://private.example.test/repo', version='=1.0.0', rev='"+strings.Repeat("a", 40)+"'}\n")
	return repo
}

func cargoOSVMixedLock() string {
	public := reviewCargoLock("registry-crate", "1.0.0", repository.CargoCratesIOSource)
	newer := strings.TrimPrefix(reviewCargoLock("registry-crate", "2.0.0", repository.CargoCratesIOSource), "version=4\n")
	git := strings.TrimPrefix(reviewCargoLock("registry-crate", "1.0.0", "git+https://private.example.test/repo#"+strings.Repeat("a", 40)), "version=4\n")
	return public + newer + git + "[[package]]\nname='private-workspace'\nversion='1.0.0'\n"
}

func mutateCargoOSVReport(report *osvReport, mutation string) {
	result := &report.Results[0]
	switch mutation {
	case "missing":
		result.Packages = result.Packages[:1]
	case "duplicate":
		result.Packages = append(result.Packages, result.Packages[0])
	case "foreign-version":
		result.Packages[0].Package.Version = "3.0.0"
	case "foreign-ecosystem":
		result.Packages[0].Package.Ecosystem = "npm"
	case "foreign-source":
		result.Source.Path = "other/Cargo.lock"
	case "empty":
		report.Results = nil
	}
}

type cargoOSVBoundary struct {
	t     *testing.T
	scans int
}

func (boundary *cargoOSVBoundary) runPlain(context.Context, string, policy.Command) error {
	return fmt.Errorf("unexpected unstructured Cargo scan")
}

func (boundary *cargoOSVBoundary) runOutput(_ context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	boundary.t.Helper()
	boundary.scans++
	input, projected := argumentAfter(command.Argv, "--lockfile")
	if !projected {
		for _, plugin := range []string{"rust/cargolock", "rust/cargotoml"} {
			index := slices.Index(command.Argv, plugin)
			if index < 1 || command.Argv[index-1] != "--experimental-disable-plugins" {
				boundary.t.Fatal("recursive scanner can read raw Cargo sources")
			}
		}
		return runner.Result{}, runner.Output{Stdout: []byte(`{"results":[]}`)}, nil
	}
	if !slices.Contains(command.Argv, "--all-packages") || !slices.Contains(command.Argv, "--no-resolve") || !slices.Contains(command.Argv, "--no-call-analysis") {
		boundary.t.Fatal("Cargo scanner may resolve, compile, or omit locked packages")
	}
	path, _ := strings.CutPrefix(input, "osv-scanner:")
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		boundary.t.Fatal(err)
	}
	for _, private := range []string{"private.example.test", "private-workspace", strings.Repeat("a", 40)} {
		if strings.Contains(string(data), private) {
			boundary.t.Fatal("private Cargo source reached a public scanner")
		}
	}
	return runner.Result{ExitStatus: 1}, runner.Output{Stdout: cargoOSVResponse(boundary.t, data, path)}, fmt.Errorf("advisories found")
}

func cargoOSVResponse(t *testing.T, projection []byte, path string) []byte {
	t.Helper()
	var report osvReport
	if err := json.Unmarshal(projection, &report); err != nil {
		t.Fatal(err)
	}
	packages := report.Results[0].Packages
	if len(packages) != 2 || packages[0].Package.Name != "registry-crate" || packages[1].Package.Name != "registry-crate" || packages[0].Package.Version == packages[1].Package.Version {
		t.Fatalf("complete Cargo lock inventory lost public versions: %+v", packages)
	}
	for index := range packages {
		if packages[index].Package.Version == "1.0.0" {
			packages[index].Groups = []osvGroup{{IDs: []string{"RUSTSEC-2026-0001", "GHSA-cargo-example"}, MaxSeverity: "low"}}
		}
	}
	report.Results[0].Source.Path = path
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (double cargoOSVFailure) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Capture != runner.CaptureNone {
		return double.runOutput(ctx, invocation.Root, invocation.Command)
	}
	return runner.Result{}, runner.Output{}, double.runPlain(ctx, invocation.Root, invocation.Command)
}

func (double *cargoOSVBoundary) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Capture != runner.CaptureNone {
		return double.runOutput(ctx, invocation.Root, invocation.Command)
	}
	return runner.Result{}, runner.Output{}, double.runPlain(ctx, invocation.Root, invocation.Command)
}
