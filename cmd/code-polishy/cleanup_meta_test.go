package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/release"
)

func cleanupTestLock(version, digest string) release.Lock {
	archives := []release.LockedArchive{}
	for _, host := range []string{"darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "windows-x64"} {
		archives = append(archives, release.LockedArchive{
			Host: host, URL: "https://example.invalid/" + version + "-" + host + ".zip", SHA256: digest, Size: 1,
		})
	}
	return release.Lock{
		LockVersion: release.LockVersion, CodePolishyVersion: version, ReleaseDigest: digest,
		Features: []string{"javascript-bundle"},
		Publication: &release.LockPublication{
			IndexURL: "https://example.invalid/index.json", IndexSHA256: digest, Archives: archives,
		},
	}
}

func TestCleanupRepositoryScopesDoNotRequireAnInstalledRelease(t *testing.T) {
	root := cleanupTestDirectory(t)
	custodian, err := engine.OpenReportCustodian(root)
	if err != nil {
		t.Fatal(err)
	}
	report, err := custodian.FinalizeReport("check", engine.Report{})
	if err != nil {
		t.Fatal(err)
	}
	arguments := []string{"--repo-root", root, "cleanup", "--reports", "--artifacts"}
	status, stdout, stderr := captureRunOutput(t, append(arguments, "--dry-run"))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "removing 1 repository output would free") ||
		!strings.Contains(stdout, "discards cached gate results, test receipts, and security evidence") {
		t.Fatalf("preview status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(report.ReportPath))); err != nil {
		t.Fatalf("preview removed report: %v", err)
	}
	status, stdout, stderr = captureRunOutput(t, []string{"--repo-root", root, "cleanup", "--artifacts"})
	if status != 0 || stderr != "" || !strings.Contains(stdout, "removed 0 repository outputs") {
		t.Fatalf("artifact cleanup status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(report.ReportPath))); err != nil {
		t.Fatalf("artifact scope removed a report: %v", err)
	}
	status, stdout, stderr = captureRunOutput(t, arguments)
	if status != 0 || stderr != "" || !strings.Contains(stdout, "removed 1 repository output") {
		t.Fatalf("report cleanup status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(report.ReportPath))); !os.IsNotExist(err) {
		t.Fatalf("report remains: %v", err)
	}
}

func cleanupTestDirectory(t *testing.T) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func cleanupTestRepository(t *testing.T, prefix string, lock release.Lock) string {
	t.Helper()
	root := cleanupTestDirectory(t)
	if err := os.WriteFile(filepath.Join(root, release.LockFilename), release.RenderLock(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	status, _, stderr := captureRunOutput(t, []string{"--repo-root", root, "--policy-root", release.Directory(prefix, lock), "version"})
	if status != 0 || stderr != "" {
		t.Fatalf("version status=%d stderr=%q", status, stderr)
	}
	return root
}

func TestCleanupKeepsReleasesUsedByRepositoriesThatRanCommands(t *testing.T) {
	prefix := cleanupTestDirectory(t)
	current := cleanupTestLock("9.9.9", strings.Repeat("1", 64))
	other := cleanupTestLock("9.9.8", strings.Repeat("2", 64))
	unused := cleanupTestLock("9.9.7", strings.Repeat("3", 64))
	for _, lock := range []release.Lock{current, other, unused} {
		directory := release.Directory(prefix, lock)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "VERSION"), []byte(lock.CodePolishyVersion+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	currentRepository := cleanupTestRepository(t, prefix, current)
	otherRepository := cleanupTestRepository(t, prefix, other)

	arguments := []string{"--repo-root", currentRepository, "--policy-root", release.Directory(prefix, current), "cleanup"}
	status, stdout, stderr := captureRunOutput(t, append(arguments, "--dry-run"))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "WOULD REMOVE 9.9.7-") ||
		!strings.Contains(stdout, "PASS cleanup dry run: removing 1 release would free") {
		t.Fatalf("dry run status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, arguments)
	if status != 0 || stderr != "" {
		t.Fatalf("cleanup status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	for _, line := range []string{
		"KEPT " + filepath.Base(release.Directory(prefix, other)) + " for " + otherRepository,
		"KEPT " + filepath.Base(release.Directory(prefix, current)) + " for " + currentRepository,
		"REMOVED " + filepath.Base(release.Directory(prefix, unused)),
		"PASS cleanup removed 1 release and freed",
	} {
		if !strings.Contains(stdout, line) {
			t.Fatalf("cleanup output omitted %q: %q", line, stdout)
		}
	}
	if _, err := os.Stat(release.Directory(prefix, unused)); !os.IsNotExist(err) {
		t.Fatalf("unused release remains: %v", err)
	}
	if _, err := os.Stat(release.Directory(prefix, other)); err != nil {
		t.Fatalf("another repository's release was removed: %v", err)
	}
}
