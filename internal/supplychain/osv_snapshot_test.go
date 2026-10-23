package supplychain

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestOSVUVPinnedRuntimeUsesIsolatedCompleteProjection(t *testing.T) {
	t.Parallel()
	fixture := currentGitOnlineFixture(t)
	writeSupplyFile(t, fixture.repo.Root, "public/uv.lock", publicUVTestLock())
	writeSupplyFile(t, fixture.repo.Root, "osv-scanner.toml", "invalid repository configuration")
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fixture.repo.PolicyRoot = root
	scans, err := osvScanPlan(fixture.repo)
	if err != nil || len(scans) != 2 {
		t.Fatalf("uv projection plan: %+v %v", scans, err)
	}
	scan := scans[1]
	scan.Command.Argv = append(scan.Command.Argv, "--offline")
	scan.Command.EnvironmentOverrides = []string{"OSV_SCANNER_LOCAL_DB_CACHE_DIRECTORY=" + uvSnapshotDatabase(t)}
	findings := scanOSVRoot(t.Context(), fixture.repo, scan, runner.OSRunner{})
	if len(findings) != 1 || findings[0].Vulnerability == nil || findings[0].Vulnerability.Advisory != "GHSA-uv-snapshot" || findings[0].Path != "public/uv.lock" {
		t.Fatalf("isolated pinned scanner lost the uv advisory: %+v", findings)
	}
}

func uvSnapshotDatabase(t *testing.T) string {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	entry, err := archive.Create("GHSA-uv-snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = entry.Write([]byte(`{"schema_version":"1.7.0","id":"GHSA-uv-snapshot","modified":"2026-01-01T00:00:00Z","affected":[{"package":{"ecosystem":"PyPI","name":"requests"},"versions":["2.31.0"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeSupplyFile(t, root, "osv-scanner/PyPI/all.zip", data.String())
	return root
}

func TestOSVUVReportRequiresItsCompleteInventory(t *testing.T) {
	t.Parallel()
	fixture := currentGitOnlineFixture(t)
	writeSupplyFile(t, fixture.repo.Root, "public/uv.lock", publicUVTestLock())
	scans, err := osvScanPlan(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	scan := scans[1]
	for _, mutation := range []string{"missing", "duplicate", "foreign-version", "foreign-ecosystem", "foreign-source", "empty"} {
		var report osvReport
		if err := json.Unmarshal([]byte(scan.Snapshot.data), &report); err != nil {
			t.Fatal(err)
		}
		report.Results[0].Source.Path = scan.InputPath
		mutateCargoOSVReport(&report, mutation)
		if mutation == "missing" {
			report.Results[0].Packages = nil
		}
		payload, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseOSVScanReport(fixture.repo, scan, payload); err == nil {
			t.Fatalf("%s uv report satisfied complete inventory coverage", mutation)
		}
	}
}

func TestOSVSnapshotCleansUpOnCancellationAndRejectsForeignSources(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		repo := supplyRepository(t)
		scan := osvProjectionScan(repo, "uv", "uv.lock", []byte(`{"results":[]}`))
		probe := &osvSnapshotProbe{cancel: cancel}
		result, _, err := runOSVCommandWithOutput(t.Context(), repo, scan, probe)
		if err == nil || result.FailureCategory == "" {
			t.Fatalf("invalid snapshot execution succeeded: %+v %v", result, err)
		}
		if cancel && result.FailureCategory != runner.FailureCanceled {
			t.Fatalf("truncated output hid cancellation: %+v %v", result, err)
		}
		if _, err := os.Stat(probe.root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("snapshot survived failure: %s %v", probe.root, err)
		}
	}
}

type osvSnapshotProbe struct {
	root   string
	cancel bool
}

func (probe *osvSnapshotProbe) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Snapshot != nil {
		return runner.ExecuteSnapshot(ctx, probe, invocation)
	}
	probe.root = invocation.Root
	if probe.cancel {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureCanceled}, runner.Output{Stdout: []byte(`{"results":[`)}, context.Canceled
	}
	return runner.Result{}, runner.Output{Stdout: []byte(`{"results":[{"source":{"path":"foreign/uv.lock"},"packages":[]}]}`)}, nil
}

func TestOSVSnapshotPreservesProviderMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	payload := []byte(`{"results":[{"source":{"path":"inventory.json","type":"lockfile","provider":"retained"},"packages":[],"extra":true}],"database_version":"fixture"}`)
	data, err := normalizeOSVSnapshotReport(payload, root, "inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range [][]byte{[]byte(`"provider":"retained"`), []byte(`"extra":true`), []byte(`"database_version":"fixture"`)} {
		if !bytes.Contains(data, field) {
			t.Fatalf("raw metadata was discarded: %s", data)
		}
	}
}
