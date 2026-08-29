package supplychain

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPNPMLockDriftIsAFinding(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, packagesResult(
		`{"path":".","manifest":"package.json","dependencies":[`+
			`{"name":"react","scope":"runtime","declared":"19.0.0","specifier":"18.3.1","resolvedName":"react","resolvedVersion":"18.3.1","link":""}]}`,
		``))
	findings := Static(t.Context(), repo, []string{"package.json", "pnpm-lock.yaml"})
	drift := findingsFor(findings, "supplyChain.lockConsistency")
	if len(drift) != 1 || drift[0].Path != "package.json" || drift[0].Subject != "react" {
		t.Fatalf("findings = %+v", findings)
	}
	if !strings.Contains(drift[0].Message, `declared as "19.0.0"`) {
		t.Fatalf("message = %q", drift[0].Message)
	}
}

func TestPNPMLockDriftCoversBothDirections(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, packagesResult(
		`{"path":".","manifest":"package.json","dependencies":[`+
			`{"name":"absent","scope":"runtime","declared":"1.0.0","specifier":"","resolvedName":"","resolvedVersion":"","link":""},`+
			`{"name":"extra","scope":"development","declared":"","specifier":"2.0.0","resolvedName":"extra","resolvedVersion":"2.0.0","link":""}]}`,
		``))
	findings := Static(t.Context(), repo, []string{"package.json", "pnpm-lock.yaml"})
	if drift := findingsFor(findings, "supplyChain.lockConsistency"); len(drift) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPNPMLockThatMatchesItsManifestsIsClean(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, packagesResult(
		`{"path":".","manifest":"package.json","dependencies":[`+
			`{"name":"react","scope":"runtime","declared":"18.3.1","specifier":"18.3.1","resolvedName":"react","resolvedVersion":"18.3.1","link":""},`+
			`{"name":"lib","scope":"runtime","declared":"workspace:*","specifier":"workspace:*","resolvedName":"","resolvedVersion":"","link":"packages/lib"}]}`,
		`{"name":"react","version":"18.3.1","source":"registry","licenseMetadata":"required"}`))
	findings := Static(t.Context(), repo, []string{"package.json", "pnpm-lock.yaml"})
	if len(findingsFor(findings, "supplyChain.lockConsistency")) != 0 ||
		len(findingsFor(findings, "supplyChain.dependencySource")) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestResolvedPackageWithoutRegistryIntegrityIsRefused(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, packagesResult(``,
		`{"name":"patched","version":"1.0.0","source":"tarball","licenseMetadata":"required"},`+
			`{"name":"forked","version":"2.0.0","source":"git","licenseMetadata":"required"}`))
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	sources := findingsFor(findings, "supplyChain.dependencySource")
	if len(sources) != 2 || sources[0].Path != "pnpm-lock.yaml" || sources[0].Subject != "patched@1.0.0" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestLocalDirectorySourceFollowsAllowedProtocols(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, packagesResult(``,
		`{"name":"vendored","version":"0.0.0","source":"directory","licenseMetadata":"required"}`))
	if findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"}); len(findingsFor(findings, "supplyChain.dependencySource")) != 0 {
		t.Fatalf("allowed local directory was refused: %+v", findings)
	}
	repo.Config.SupplyChain.AllowedDependencyProtocols = []string{"workspace:"}
	if findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"}); len(findingsFor(findings, "supplyChain.dependencySource")) != 1 {
		t.Fatalf("unallowed local directory was accepted: %+v", findings)
	}
}

func TestUnreadablePNPMLockIsMissingCoverage(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, `{"lockfileVersion":"","importers":[],"packages":[],`+
		`"unsupported":[{"path":"pnpm-lock.yaml","reason":"the lockfile declares version '6.0', not 9.0"}]}`)
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	coverage := findingsFor(findings, "supplyChain.lockCoverage")
	if len(coverage) != 1 || coverage[0].Path != "pnpm-lock.yaml" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestResolvedPNPMPackagesKeepOnlyExactRegistryReleases(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, packagesResult(``,
		`{"name":"react","version":"18.3.1","source":"registry","licenseMetadata":"required"},`+
			`{"name":"patched","version":"1.0.0","source":"tarball","licenseMetadata":"required"},`+
			`{"name":"forked","version":"main","source":"git","licenseMetadata":"required"}`))
	manifest := nodeManifest{Path: "package.json", Root: ".", Manager: "pnpm", ManagerVersion: "11.13.0"}
	packages, err := resolvedNodePackages(t.Context(), repo, manifest)
	if err != nil || len(packages) != 1 || packages[0].Name != "react" || packages[0].Ecosystem != "pnpm" {
		t.Fatalf("packages=%+v err=%v", packages, err)
	}
	if packages[0].Scope != "pnpm-lock.yaml" || packages[0].Locations[0] != "react@18.3.1" {
		t.Fatalf("packages = %+v", packages)
	}
}

func TestUnreadablePNPMLockResolvesNoPackages(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, `{"lockfileVersion":"","importers":[],"packages":[],`+
		`"unsupported":[{"path":"pnpm-lock.yaml","reason":"the lockfile is not a YAML map"}]}`)
	manifest := nodeManifest{Path: "package.json", Root: ".", Manager: "pnpm", ManagerVersion: "11.13.0"}
	if _, err := resolvedNodePackages(t.Context(), repo, manifest); err == nil {
		t.Fatal("an unreadable lockfile resolved successfully")
	}
}

func TestPNPMManifestNeedsNoLockSyncOrSecurityProvider(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	findings := CoverageFindings(repo, []string{"package.json", "pnpm-lock.yaml"})
	if len(findingsFor(findings, "policy.supplyChainCoverage")) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	writeSupplyFile(t, repo.Root, "other/package.json", `{"packageManager":"npm@11.0.0"}`+"\n")
	findings = CoverageFindings(repo, []string{"other/package.json"})
	if len(findingsFor(findings, "policy.supplyChainCoverage")) != 2 {
		t.Fatalf("a manifest no pnpm project governs still needs its own providers: %+v", findings)
	}
}

func TestNodeManifestRequiresNoAmbientPackageManager(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	writeSupplyFile(t, repo.Root, "other/package.json", `{"packageManager":"npm@11.0.0"}`+"\n")
	findings := ToolFindings(repo, []string{"package.json", "other/package.json"})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPNPMFactsFailClosedWithoutTheBundle(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = t.TempDir()
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	if len(findingsFor(findings, "policy.tool")) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestRepositoryWithoutPNPMNeverLaunchesTheBundle(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeSupplyFile(t, repo.Root, "go.mod", "module example.test\n\ngo 1.26.0\n")
	if findings := Static(t.Context(), repo, []string{"go.mod"}); len(findingsFor(findings, "policy.tool")) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPNPMWorkspaceSettingsAreReadOutsideTheSelection(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.MinimumReleaseAgeDays = 30
	writeSupplyFile(t, repo.Root, "package.json",
		`{"packageManager":"pnpm@10.0.0","dependencies":{"react":"18.3.1"}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-workspace.yaml", "onlyBuiltDependencies: []\n")
	repo.PolicyRoot = installBundle(t, map[string]string{
		"workspace": workspaceResult("pnpm-workspace.yaml", workspaceSetting("onlyBuiltDependencies")),
	})
	findings := pnpmGovernanceFindings(t.Context(), repo, []string{"package.json"})
	if len(findings) != 0 {
		t.Fatalf("settings outside the selection were ignored: %+v", findings)
	}
}

func TestUnreadablePNPMWorkspaceSettingsFailClosed(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json",
		`{"packageManager":"pnpm@10.0.0","dependencies":{"react":"18.3.1"}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-workspace.yaml", "onlyBuiltDependencies: [\n")
	repo.PolicyRoot = installBundle(t, map[string]string{
		"workspace": `{"files":[],"unsupported":[{"path":"pnpm-workspace.yaml","reason":"unexpected end of the stream"}]}`,
	})
	findings := pnpmGovernanceFindings(t.Context(), repo, []string{"package.json"})
	unreadable := findingsFor(findings, "supplyChain.pnpmSecurity")
	if len(unreadable) != 1 || unreadable[0].Path != "pnpm-workspace.yaml" {
		t.Fatalf("findings = %+v", findings)
	}
	if !strings.Contains(unreadable[0].Message, "unexpected end of the stream") {
		t.Fatalf("message = %q", unreadable[0].Message)
	}
}

func TestPNPMWorkspaceSettingsRejectTwoOwnerFiles(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeSupplyFile(t, repo.Root, "package.json",
		`{"packageManager":"pnpm@10.0.0","dependencies":{"react":"18.3.1"}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-workspace.yaml", "onlyBuiltDependencies: []\n")
	writeSupplyFile(t, repo.Root, "pnpm-workspace.yml", "onlyBuiltDependencies: []\n")
	findings := pnpmGovernanceFindings(t.Context(), repo, []string{"package.json"})
	if len(findings) != 1 || findings[0].Subject != "pnpm:workspace-owner" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPNPMWorkspaceSettingsFailClosedWithoutTheBundle(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.PolicyRoot = t.TempDir()
	writeSupplyFile(t, repo.Root, "package.json",
		`{"packageManager":"pnpm@10.0.0","dependencies":{"react":"18.3.1"}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-workspace.yaml", "onlyBuiltDependencies: []\n")
	findings := pnpmGovernanceFindings(t.Context(), repo, []string{"package.json"})
	if len(findingsFor(findings, "policy.tool")) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
}

func pnpmRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json",
		`{"packageManager":"pnpm@11.13.0","dependencies":{"react":"18.3.1"},"pnpm":{"onlyBuiltDependencies":[]}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	return repo
}

func packagesResult(importers, packages string) string {
	return `{"lockfileVersion":"9.0","importers":[` + importers + `],"packages":[` + packages + `],"unsupported":[]}`
}

func findingsFor(findings []policy.Finding, check string) []policy.Finding {
	matching := []policy.Finding{}
	for _, finding := range findings {
		if finding.Check == check {
			matching = append(matching, finding)
		}
	}
	return matching
}

func installPackagesBundle(t *testing.T, result string) string {
	t.Helper()
	return installBundle(t, map[string]string{"packages": result})
}

func installBundle(t *testing.T, results map[string]string) string {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("the sealed JavaScript bundle does not support %s", runtime.GOOS)
	}
	architecture := runtime.GOARCH
	if architecture == "amd64" {
		architecture = "x64"
	}
	root := t.TempDir()
	installed := filepath.Join(root, ".tools", "javascript")
	writeBundleFile(t, filepath.Join(installed, runtime.GOOS+"-"+architecture, "node", "bin", "node"),
		"#!/bin/sh\nexec /bin/sh \"$1\"\n")
	runner := "#!/bin/sh\nrequest=$(/bin/cat)\ncase \"${request}\" in\n"
	for _, operation := range slices.Sorted(maps.Keys(results)) {
		runner += fmt.Sprintf("  *'\"operation\":\"%s\"'*) printf '{\"protocolVersion\":3,\"operation\":\"%s\",\"result\":%%s}\\n' '%s' ;;\n",
			operation, operation, results[operation])
	}
	runner += "  *) exit 1 ;;\nesac\n"
	writeBundleFile(t, filepath.Join(installed, "bundle", "runner.mjs"), runner)
	return root
}

func workspaceResult(path, settings string) string {
	return `{"files":[{"path":"` + path + `","settings":[` + settings + `]}],"unsupported":[]}`
}

func workspaceSetting(name string, values ...string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, `"`+value+`"`)
	}
	return `{"name":"` + name + `","values":[` + strings.Join(quoted, ",") + `]}`
}

func writeBundleFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
