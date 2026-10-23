package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

func TestRepositoryCleanupReportsPreservesUserAndReviewRecords(t *testing.T) {
	root := t.TempDir()
	policyEngine := &Engine{Repository: repository.Repository{Root: root}}
	report, err := policyEngine.FinalizeReport("check", Report{})
	if err != nil {
		t.Fatal(err)
	}
	retained := []string{
		".code-polishy-reports/notes.md",
		".code-polishy-reports/check/notes.md",
		".code-polishy-reports/check/" + strings.Repeat("a", 64) + "/report.json",
		".code-polishy-reports/behavior-review/request.md",
		".code-polishy-reports/architecture-review/review.json",
		".code-polishy-reports/capability-upgrades/plan.json",
		".code-polishy-artifacts/manual.txt",
	}
	for _, path := range retained {
		writeEngineFile(t, root, path, "keep me", 0o600)
	}
	receipt := ".code-polishy-reports/test-receipts/cached.json"
	writeEngineFile(t, root, receipt, "cached evidence", 0o600)
	preview, err := CleanupRepository(root, CleanupOptions{Reports: true, DryRun: true})
	if err != nil || len(preview.Removed) != 2 || preview.ReclaimedBytes() == 0 {
		t.Fatalf("preview=%+v error=%v", preview, err)
	}
	for _, path := range []string{report.ReportPath, receipt} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("preview removed %s: %v", path, err)
		}
	}
	result, err := CleanupRepository(root, CleanupOptions{Reports: true})
	if err != nil || !reflect.DeepEqual(result, preview) {
		t.Fatalf("cleanup=%+v preview=%+v error=%v", result, preview, err)
	}
	for _, path := range []string{report.ReportPath, receipt} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Fatalf("cleanup retained %s: %v", path, err)
		}
	}
	for _, path := range retained {
		if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))); err != nil || string(data) != "keep me" {
			t.Fatalf("cleanup changed %s: %q error=%v", path, data, err)
		}
	}
	repeated, err := CleanupRepository(root, CleanupOptions{Reports: true})
	if err != nil || len(repeated.Removed) != 0 {
		t.Fatalf("repeated cleanup=%+v error=%v", repeated, err)
	}
}

func TestRepositoryCleanupGateReportsPreservesIncompleteExecutions(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "active"}[active], func(t *testing.T) {
			root, report, _ := runIntermittentMergeGate(t)
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(report.GateRunPolicy.ReportPath)))
			if err != nil {
				t.Fatal(err)
			}
			stored := gaterun.Report{}
			if err := json.Unmarshal(data, &stored); err != nil {
				t.Fatal(err)
			}
			if active {
				if _, err := gaterun.Start(gaterun.StartOptions{RepositoryRoot: root, Identity: stored.Identity}); err != nil {
					t.Fatal(err)
				}
			}
			result, err := CleanupRepository(root, CleanupOptions{Reports: true})
			if err != nil {
				t.Fatal(err)
			}
			_, present, err := gaterun.LoadReportIfPresent(root, stored.Identity)
			if err != nil || present != active {
				t.Fatalf("gate present=%t active=%t result=%+v error=%v", present, active, result, err)
			}
			if !active {
				policyEngine, err := Open(root, enginePolicyRoot(t), "")
				if err != nil {
					t.Fatal(err)
				}
				commandRunner := &recordingEngineRunner{}
				policyEngine.Runner = commandRunner
				rerun, err := policyEngine.MergeGate(t.Context(), "main")
				if err != nil || rerun.GateRunPolicy == nil || rerun.GateRunPolicy.Status != "passed" || len(commandRunner.commands) == 0 {
					t.Fatalf("gate did not rerun after cleanup: %+v commands=%v error=%v", rerun, commandRunner.commands, err)
				}
			}
		})
	}
}

func TestRepositoryCleanupReclaimsSecurityScansAndRefreshes(t *testing.T) {
	t.Parallel()
	policyEngine, scanner, _ := securityTestEngine(t)
	root := policyEngine.Repository.Root
	first, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, first, err)
	second, err := policyEngine.MergeGate(t.Context(), "main")
	requireSuccessfulSecurityReport(t, second, err)
	if first.Security.Scans[0].ReportPath == second.Security.Scans[0].ReportPath || second.Security.ReportPath == "" {
		t.Fatal("gate calls did not produce separate scans and refresh evidence")
	}
	records := []struct{ path, directory string }{
		{first.Security.Scans[0].ReportPath, ".code-polishy-reports/security-scans"},
		{second.Security.Scans[0].ReportPath, ".code-polishy-reports/security-scans"},
		{second.Security.ReportPath, ".code-polishy-reports/security-refresh"},
	}
	expectedBytes := map[string]int64{}
	for _, record := range records {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(record.path)))
		if err != nil {
			t.Fatal(err)
		}
		expectedBytes[record.directory] += info.Size()
	}
	preview, err := CleanupRepository(root, CleanupOptions{Reports: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	for directory, bytes := range expectedBytes {
		if !slices.Contains(preview.Removed, CleanupEntry{Path: directory, Bytes: bytes}) {
			t.Fatalf("cleanup omitted security evidence or its bytes: directory=%s bytes=%d preview=%+v", directory, bytes, preview)
		}
	}
	for _, record := range records {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(record.path))); err != nil {
			t.Fatalf("preview removed security evidence: %s %v", record.path, err)
		}
	}
	result, err := CleanupRepository(root, CleanupOptions{Reports: true})
	if err != nil || !reflect.DeepEqual(result, preview) {
		t.Fatalf("cleanup=%+v preview=%+v error=%v", result, preview, err)
	}
	for _, record := range records {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(record.path))); !os.IsNotExist(err) {
			t.Fatalf("cleanup retained security evidence: %s %v", record.path, err)
		}
	}
	scanner.advisory = true
	fresh, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || !HasFindings(fresh) || fresh.GateRunPolicy.Status != "failed" || countCommand(scanner.commands, "osv-scan-root") != 3 {
		t.Fatalf("verification did not scan again after cleanup: %+v commands=%v error=%v", fresh, scanner.commands, err)
	}
}

func TestRepositoryCleanupDiscardsPNPMAuditCache(t *testing.T) {
	t.Parallel()
	root, scanner, now := t.TempDir(), &changingSecurityRunner{}, time.Now().UTC()
	writeEngineFile(t, root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n", 0o600)
	command := auditSecurityCommand(t, root, ".")
	first := auditSecurityScanRunner(t, root, scanner, &now, command)
	runAuditSecurityScan(t, first, command)
	cached := auditSecurityScanRunner(t, root, scanner, &now, command)
	runAuditSecurityScan(t, cached, command)
	if cached.scans[0].Status != "reused" || len(scanner.commands) != 1 {
		t.Fatal("audit did not establish reusable evidence")
	}
	path := filepath.Join(root, filepath.FromSlash(first.scans[0].ReportPath))
	if _, err := CleanupRepository(root, CleanupOptions{Artifacts: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("artifact cleanup removed security evidence: %v", err)
	}
	result, err := CleanupRepository(root, CleanupOptions{Reports: true})
	if err != nil || len(result.Removed) != 1 || result.ReclaimedBytes() == 0 {
		t.Fatalf("audit cleanup=%+v error=%v", result, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup retained cached audit: %v", err)
	}
	scanner.advisory = true
	fresh := auditSecurityScanRunner(t, root, scanner, &now, command)
	if result := runAuditSecurityScan(t, fresh, command); len(result.Advisories) != 1 || fresh.scans[0].Status != "scanned" || len(scanner.commands) != 2 {
		t.Fatalf("cleanup did not force a fresh audit: %+v scans=%+v commands=%v", result, fresh.scans, scanner.commands)
	}
}

func TestRepositoryCleanupArtifactScopeRetainsReports(t *testing.T) {
	root := t.TempDir()
	policyEngine := &Engine{Repository: repository.Repository{Root: root}}
	report, err := policyEngine.FinalizeReport("test", Report{})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := testartifact.Start(root, "")
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, execution.Directory(), "suite/output.txt", "test output", 0o600)
	if err := execution.Complete(); err != nil {
		t.Fatal(err)
	}
	preview, err := CleanupRepository(root, CleanupOptions{Reports: true, Artifacts: true, DryRun: true})
	if err != nil || len(preview.Removed) != 2 || preview.ReclaimedBytes() < int64(len("test output")) {
		t.Fatalf("combined preview=%+v error=%v", preview, err)
	}
	if _, err := os.Stat(execution.Directory()); err != nil {
		t.Fatalf("preview removed artifact directory: %v", err)
	}
	result, err := CleanupRepository(root, CleanupOptions{Artifacts: true})
	if err != nil || len(result.Removed) != 1 || result.Removed[0].Path != testartifact.RootName+"/"+execution.ID() {
		t.Fatalf("artifact cleanup=%+v error=%v", result, err)
	}
	if _, err := os.Stat(execution.Directory()); !os.IsNotExist(err) {
		t.Fatalf("artifact directory remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(report.ReportPath))); err != nil {
		t.Fatalf("artifact cleanup removed report: %v", err)
	}
}

func TestRepositoryCleanupRefusesLinkedRootsAndPreservesLinkedTrees(t *testing.T) {
	for _, name := range []string{".code-polishy-reports", testartifact.RootName} {
		t.Run(name, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			writeEngineFile(t, outside, "keep.txt", "outside", 0o600)
			if err := os.Symlink(outside, filepath.Join(root, name)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := CleanupRepository(root, CleanupOptions{Reports: true, Artifacts: true}); err == nil {
				t.Fatal("linked output root accepted")
			}
			if data, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(data) != "outside" {
				t.Fatalf("outside file changed: %q error=%v", data, err)
			}
		})
	}
	root := t.TempDir()
	policyEngine := &Engine{Repository: repository.Repository{Root: root}}
	report, err := policyEngine.FinalizeReport("check", Report{})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(filepath.Dir(report.ReportPath)), "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := CleanupRepository(root, CleanupOptions{Reports: true})
	if err != nil || len(result.Removed) != 0 || len(result.Kept) != 1 {
		t.Fatalf("linked tree cleanup=%+v error=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(report.ReportPath))); err != nil {
		t.Fatalf("linked report tree removed: %v", err)
	}
}

func TestRepositoryCleanupPreservesLinkedSecurityCaches(t *testing.T) {
	t.Parallel()
	for _, directory := range []string{".code-polishy-reports/security-scans", ".code-polishy-reports/security-refresh"} {
		t.Run(directory, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			path := directory + "/retained.txt"
			writeEngineFile(t, root, path, "retained cache entry", 0o600)
			writeEngineFile(t, outside, "keep.txt", "outside", 0o600)
			if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(directory), "linked")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			for _, dryRun := range []bool{true, false} {
				result, err := CleanupRepository(root, CleanupOptions{Reports: true, DryRun: dryRun})
				if err != nil || len(result.Removed) != 0 || len(result.Kept) != 1 || result.Kept[0].Path != directory {
					t.Fatalf("linked security cache cleanup=%+v error=%v", result, err)
				}
			}
			if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))); err != nil || string(data) != "retained cache entry" {
				t.Fatalf("linked cache tree changed: %q error=%v", data, err)
			}
			if data, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(data) != "outside" {
				t.Fatalf("outside file changed: %q error=%v", data, err)
			}
		})
	}
}
