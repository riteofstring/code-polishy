package supplychain

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestOSVCargoPinnedRuntimeUsesCompleteOfflineProjection(t *testing.T) {
	t.Parallel()
	repo := cargoOSVRepository(t, cargoOSVMixedLock())
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo.PolicyRoot = root
	if !isExecutable(repo.PolicyTool("osv-scanner")) {
		t.Fatal("pinned OSV-Scanner is required; run ./tools/install-policy-tools.sh")
	}
	scans, err := osvScanPlan(repo)
	if err != nil || len(scans) != 2 {
		t.Fatalf("Cargo runtime scan plan = %+v, %v", scans, err)
	}
	cache := cargoOSVDatabase(t)
	for _, scan := range scans {
		scan.Command.Argv = append(scan.Command.Argv, "--offline")
		scan.Command.SealedEnvironment = true
		scan.Command.EnvironmentOverrides = []string{"OSV_SCANNER_LOCAL_DB_CACHE_DIRECTORY=" + cache}
		if err := prepareOSVInput(repo, scan); err != nil {
			t.Fatal(err)
		}
		findings := scanOSVRoot(t.Context(), repo, scan, runner.OSRunner{})
		if scan.InputPath == "" {
			if len(findings) != 0 {
				t.Fatalf("recursive scanner read raw Cargo sources: %+v", findings)
			}
			continue
		}
		if len(findings) != 1 || findings[0].Vulnerability == nil || findings[0].Vulnerability.Advisory != "RUSTSEC-2026-0001" || findings[0].Vulnerability.Scope != "Cargo.lock" {
			t.Fatalf("pinned Cargo scanner lost complete package or vulnerability evidence: %+v", findings)
		}
	}
}

func cargoOSVDatabase(t *testing.T) string {
	t.Helper()
	buffer := bytes.Buffer{}
	archive := zip.NewWriter(&buffer)
	file, err := archive.Create("RUSTSEC-2026-0001.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write([]byte(`{
  "schema_version":"1.7.0","id":"RUSTSEC-2026-0001","modified":"2026-01-01T00:00:00Z",
  "published":"2026-01-01T00:00:00Z","summary":"Offline scanner fixture",
  "affected":[{"package":{"ecosystem":"crates.io","name":"registry-crate"},
  "ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"2.0.0"}]}]}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	directory := filepath.Join(cache, "osv-scanner", "crates.io")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "all.zip"), buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return cache
}
