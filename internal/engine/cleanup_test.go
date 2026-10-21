package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
