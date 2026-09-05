package supplychain

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestOSVPartitionsGitEvidenceAndPublicRegistryCoverage(t *testing.T) {
	t.Parallel()
	fixture := currentGitOnlineFixture(t)
	fixture.write(t)
	writeSupplyFile(t, fixture.repo.Root, "public/uv.lock", publicUVTestLock())
	commands, err := osvCommands(fixture.repo)
	if err != nil || len(commands) != 2 {
		t.Fatalf("public scan plan = %+v, %v", commands, err)
	}
	if _, err := os.Stat(filepath.Join(fixture.repo.Root, osvInputDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning wrote a scan input: %v", err)
	}
	boundary := &gitOnlineBoundary{t: t}
	findings := scanOSVWithCommands(t.Context(), fixture.repo, commands, boundary)
	if boundary.scans != 2 || !boundary.projection || len(findings) != 1 {
		t.Fatalf("partitioned scan = %+v, scans %d", findings, boundary.scans)
	}
	finding := findings[0]
	if finding.Check != "supplyChain.osvVulnerability" || finding.Path != "public/uv.lock" || finding.Vulnerability == nil || finding.Vulnerability.Scope != "public/uv.lock" {
		t.Fatalf("public vulnerability lost its original lock scope: %+v", finding)
	}
}

func TestOSVPreservesMissingNativeInputFailureOutsideUVRoots(t *testing.T) {
	t.Parallel()
	fixture := currentGitOnlineFixture(t)
	writeSupplyFile(t, fixture.repo.Root, "native/go.mod", "module example.test/native\n")
	fixture.repo.Config.ActivePolicyModules = append(fixture.repo.Config.ActivePolicyModules, policy.ActivePolicyModule{Name: "osv", Root: "native"})
	commands, err := osvCommands(fixture.repo)
	if err != nil || len(commands) != 2 {
		t.Fatalf("native scan plan = %+v, %v", commands, err)
	}
	if !slices.Contains(commands[0].Argv, "--allow-no-lockfiles") || slices.Contains(commands[1].Argv, "--allow-no-lockfiles") {
		t.Fatalf("independent native coverage was relaxed: %+v", commands)
	}
}

func TestOSVRejectsChangedLocksBeforeAnyPublicScan(t *testing.T) {
	t.Parallel()
	fixture := currentGitOnlineFixture(t)
	writeSupplyFile(t, fixture.repo.Root, "public/uv.lock", publicUVTestLock())
	commands, err := osvCommands(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	writeSupplyFile(t, fixture.repo.Root, "public/uv.lock", strings.ReplaceAll(publicUVTestLock(), "2.31.0", "2.32.0"))
	boundary := &gitOnlineBoundary{t: t}
	findings := scanOSVWithCommands(t.Context(), fixture.repo, commands, boundary)
	if len(findings) != 1 || findings[0].Check != "policy.supplyChain" || boundary.scans != 0 {
		t.Fatalf("changed public plan executed: %+v, scans %d", findings, boundary.scans)
	}
}

func TestOSVRejectsTamperedAndSymlinkedProjectionArtifacts(t *testing.T) {
	t.Parallel()
	for _, unsafe := range []string{"content", "file-symlink", "directory-symlink"} {
		t.Run(unsafe, func(t *testing.T) {
			fixture := currentGitOnlineFixture(t)
			writeSupplyFile(t, fixture.repo.Root, "public/uv.lock", publicUVTestLock())
			scans, err := osvScanPlan(fixture.repo)
			if err != nil || len(scans) != 2 {
				t.Fatalf("plan = %+v, %v", scans, err)
			}
			scan := scans[1]
			if unsafe == "directory-symlink" {
				err = os.Symlink(t.TempDir(), filepath.Join(fixture.repo.Root, ".code-polishy-reports"))
			} else {
				writeSupplyFile(t, fixture.repo.Root, scan.InputPath, "private scanner input")
				if unsafe == "file-symlink" {
					path := filepath.Join(fixture.repo.Root, filepath.FromSlash(scan.InputPath))
					err = os.Remove(path)
					if err == nil {
						err = os.Symlink(filepath.Join(fixture.repo.Root, "uv.lock"), path)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := prepareOSVInput(fixture.repo, scan); err == nil {
				t.Fatal("unsafe projection became a public scanner input")
			}
		})
	}
}

func TestOSVRejectsNonPublicAndMalformedUVSources(t *testing.T) {
	t.Parallel()
	for _, source := range []string{`registry = "https://private.example.test/simple"`, `git = "https://private.example.test/kit.git#bad"`, `url = "https://private.example.test/kit.whl"`} {
		fixture := currentGitOnlineFixture(t)
		writeSupplyFile(t, fixture.repo.Root, "public/uv.lock", "[[package]]\nname = \"secret-package\"\nversion = \"1.0.0\"\nsource = { "+source+" }\n")
		commands, err := osvCommands(fixture.repo)
		if err == nil || len(commands) != 0 || strings.Contains(err.Error(), "secret-package") || strings.Contains(err.Error(), "private.example.test") {
			t.Fatalf("non-public source planning = %+v, %v", commands, err)
		}
	}
}

func TestOSVProjectedReportRejectsForeignInputScope(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	scan := osvScan{Root: ".", Scope: "public/uv.lock", InputPath: osvInputDirectory + "/input.osv-scanner.json"}
	payload := []byte(`{"results":[{"source":{"path":"other/uv.lock","type":"lockfile"},"packages":[]}]}`)
	if _, err := parseOSVScanReport(repo, scan, payload); err == nil {
		t.Fatal("foreign source scope satisfied projected coverage")
	}
}

func TestOnlineUVInventoryRejectsSymbolicLinkLocks(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "private.lock", publicUVTestLock())
	if err := os.Symlink("private.lock", filepath.Join(repo.Root, "uv.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := onlineUVInputs(repo); err == nil {
		t.Fatal("symbolic link lock escaped required source coverage")
	}
}

func publicUVTestLock() string {
	return "[[package]]\nname = \"requests\"\nversion = \"2.31.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n"
}
