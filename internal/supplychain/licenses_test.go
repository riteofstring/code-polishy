package supplychain

import (
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

// A repository that declares no allowed licenses declares no license policy, so
// none is invented for it and the bundle is never asked for metadata.
func TestNoConfiguredLicensePolicyEnforcesNone(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, packagesResult(``,
		`{"name":"proprietary","version":"1.0.0","source":"registry","licenseMetadata":"required"}`))
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	if len(findingsFor(findings, "supplyChain.dependencyLicense")) != 0 ||
		len(findingsFor(findings, "supplyChain.licenseCoverage")) != 0 ||
		len(findingsFor(findings, "policy.tool")) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

// Every resolved release is decided against the configured policy: a license it
// allows passes, and one it does not is named with the expression it declared.
func TestConfiguredLicensePolicyDecidesEveryResolvedRelease(t *testing.T) {
	t.Parallel()
	repo := licenseRepository(t, []string{"MIT", "Apache-2.0"},
		`{"name":"allowed","version":"1.0.0","license":"MIT"},`+
			`{"name":"refused","version":"2.0.0","license":"AGPL-3.0-only"}`,
		`{"name":"allowed","version":"1.0.0","source":"registry","licenseMetadata":"required"},`+
			`{"name":"refused","version":"2.0.0","source":"registry","licenseMetadata":"required"}`)
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	refused := findingsFor(findings, "supplyChain.dependencyLicense")
	if len(refused) != 1 || refused[0].Subject != "refused@2.0.0" || refused[0].Path != "pnpm-lock.yaml" {
		t.Fatalf("findings = %+v", findings)
	}
	if !strings.Contains(refused[0].Message, `"AGPL-3.0-only"`) {
		t.Fatalf("message = %q", refused[0].Message)
	}
}

// A release the installed tree has no metadata for was never decided, so it is
// missing coverage rather than a release with nothing against it. Only the
// registry releases the lock resolved are in scope.
func TestUnreadableLicenseMetadataIsMissingCoverage(t *testing.T) {
	t.Parallel()
	repo := licenseRepository(t, []string{"MIT"}, ``,
		`{"name":"absent","version":"1.0.0","source":"registry","licenseMetadata":"required"},`+
			`{"name":"vendored","version":"0.0.0","source":"directory","licenseMetadata":"required"}`)
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	coverage := findingsFor(findings, "supplyChain.licenseCoverage")
	if len(coverage) != 1 || coverage[0].Subject != "absent@1.0.0" {
		t.Fatalf("findings = %+v", findings)
	}
}

// A normal frozen install on this host omits an optional release that the lock
// declares only for another platform. The installed tree therefore has no
// manifest to read, but the sealed package fact makes that absence expected.
func TestPlatformExcludedLicenseMetadataIsNotMissingCoverage(t *testing.T) {
	t.Parallel()
	repo := licenseRepository(t, []string{"MIT"}, ``,
		`{"name":"foreign-optional","version":"1.0.0","source":"registry","licenseMetadata":"platform-excluded"}`)
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	if coverage := findingsFor(findings, "supplyChain.licenseCoverage"); len(coverage) != 0 {
		t.Fatalf("platform-excluded release reported missing coverage: %+v", coverage)
	}
	if tools := findingsFor(findings, "policy.tool"); len(tools) != 0 {
		t.Fatalf("platform-excluded fact was not accepted by the sealed protocol: %+v", tools)
	}
}

// Only a foreign optional release may omit its metadata without blocking. A
// universal release, a host-compatible optional release, a release reached by
// both optional and required snapshots, and an undecidable one all remain
// coverage obligations.
func TestRequiredAndUnknownLicenseMetadataRemainMissingCoverage(t *testing.T) {
	t.Parallel()
	repo := licenseRepository(t, []string{"MIT"}, ``,
		`{"name":"universal","version":"1.0.0","source":"registry","licenseMetadata":"required"},`+
			`{"name":"compatible","version":"1.0.0","source":"registry","licenseMetadata":"required"},`+
			`{"name":"mixed","version":"1.0.0","source":"registry","licenseMetadata":"required"},`+
			`{"name":"undecidable","version":"1.0.0","source":"registry","licenseMetadata":"unknown"}`)
	findings := findingsFor(Static(t.Context(), repo, []string{"pnpm-lock.yaml"}), "supplyChain.licenseCoverage")
	if len(findings) != 4 {
		t.Fatalf("coverage = %+v", findings)
	}
	bySubject := map[string]string{}
	for _, finding := range findings {
		bySubject[finding.Subject] = finding.Message
	}
	for _, subject := range []string{"universal@1.0.0", "compatible@1.0.0", "mixed@1.0.0"} {
		if strings.Contains(bySubject[subject], "undecidable") {
			t.Fatalf("required metadata was reported as undecidable: %+v", findings)
		}
	}
	if !strings.Contains(bySubject["undecidable@1.0.0"], "platform metadata is undecidable") {
		t.Fatalf("unknown platform metadata was not explicit: %+v", findings)
	}
}

// A platform exclusion changes only the expectation that a normal host install
// has metadata. If the release is nevertheless installed, its declared license
// is still governed rather than ignored.
func TestInstalledPlatformExcludedLicenseMetadataIsStillEnforced(t *testing.T) {
	t.Parallel()
	repo := licenseRepository(t, []string{"MIT"},
		`{"name":"foreign-optional","version":"1.0.0","license":"AGPL-3.0-only"}`,
		`{"name":"foreign-optional","version":"1.0.0","source":"registry","licenseMetadata":"platform-excluded"}`)
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	refused := findingsFor(findings, "supplyChain.dependencyLicense")
	if len(refused) != 1 || refused[0].Subject != "foreign-optional@1.0.0" {
		t.Fatalf("installed foreign metadata was not enforced: %+v", findings)
	}
	if coverage := findingsFor(findings, "supplyChain.licenseCoverage"); len(coverage) != 0 {
		t.Fatalf("installed foreign metadata reported missing coverage: %+v", findings)
	}
}

// A tree the reader could not open at all is reported as the reason it gave,
// against the path it names, so an uninstalled project fails closed.
func TestUnreadableInstalledTreeIsMissingCoverage(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
	repo.PolicyRoot = installBundle(t, map[string]string{
		"packages": packagesResult(``, ``),
		"licenses": `{"packages":[],"unsupported":[{"path":"node_modules/.pnpm",` +
			`"reason":"the dependencies of this project are not installed"}]}`,
	})
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	coverage := findingsFor(findings, "supplyChain.licenseCoverage")
	if len(coverage) != 1 || coverage[0].Path != "node_modules/.pnpm" {
		t.Fatalf("findings = %+v", findings)
	}
	if !strings.Contains(coverage[0].Message, "are not installed") {
		t.Fatalf("message = %q", coverage[0].Message)
	}
}

// A lock the reader could not read resolves nothing, so it is reported as
// missing lock coverage alone rather than as that plus a license lane with
// nothing to govern.
func TestAnUnreadableLockReportsNoLicenseCoverage(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
	repo.PolicyRoot = installBundle(t, map[string]string{
		"packages": `{"lockfileVersion":"","importers":[],"packages":[],` +
			`"unsupported":[{"path":"pnpm-lock.yaml","reason":"the lockfile is not a YAML map"}]}`,
	})
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	if len(findingsFor(findings, "supplyChain.lockCoverage")) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	if len(findingsFor(findings, "supplyChain.licenseCoverage")) != 0 ||
		len(findingsFor(findings, "policy.tool")) != 0 {
		t.Fatalf("an unreadable lock reported license findings: %+v", findings)
	}
}

// A missing bundle fails closed: a license is never decided by an ambient
// runtime or by assuming a package declared what it should have.
func TestLicenseFactsFailClosedWithoutTheBundle(t *testing.T) {
	t.Parallel()
	repo := pnpmRepository(t)
	repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
	repo.PolicyRoot = installBundle(t, map[string]string{"packages": packagesResult(``, ``)})
	findings := Static(t.Context(), repo, []string{"pnpm-lock.yaml"})
	if len(findingsFor(findings, "policy.tool")) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
}

// A dual license offers a choice, so allowing either side admits it; a package
// licensed under two licenses at once binds the target to both.
func TestLicenseExpressionOperatorsDecideTheChoiceTheyOffer(t *testing.T) {
	t.Parallel()
	allowed := licenseAllowList([]string{"MIT", "Apache-2.0 WITH LLVM-exception"})
	admitted := []string{"MIT", "MIT OR AGPL-3.0-only", "AGPL-3.0-only OR MIT", "(MIT)",
		"MIT AND (MIT OR AGPL-3.0-only)", "mit", "Apache-2.0 WITH LLVM-exception"}
	for _, expression := range admitted {
		if err := licenseAdmitted(expression, allowed); err != nil {
			t.Fatalf("%q was refused: %v", expression, err)
		}
	}
	refused := []string{"AGPL-3.0-only", "MIT AND AGPL-3.0-only", "Apache-2.0",
		"Apache-2.0 WITH Classpath-exception-2.0", "", "   "}
	for _, expression := range refused {
		if err := licenseAdmitted(expression, allowed); err == nil {
			t.Fatalf("%q was admitted", expression)
		}
	}
}

// An expression this reader cannot parse is refused rather than admitted on a
// guess: a license nobody can name is not one the target reviewed.
func TestUnreadableLicenseExpressionIsRefused(t *testing.T) {
	t.Parallel()
	allowed := licenseAllowList([]string{"MIT"})
	for _, expression := range []string{"MIT OR", "(MIT", "MIT)", "MIT WITH", "AND MIT",
		"SEE LICENSE IN LICENSE.md", "MIT MIT"} {
		err := licenseAdmitted(expression, allowed)
		if err == nil || !strings.Contains(err.Error(), "not a readable SPDX expression") {
			t.Fatalf("%q reported %v", expression, err)
		}
	}
}

// licenseRepository is one pnpm project whose installed tree declares exactly
// these licenses and whose lockfile resolved exactly these packages.
func licenseRepository(t *testing.T, allowed []string, installed, resolved string) repository.Repository {
	t.Helper()
	repo := pnpmRepository(t)
	repo.Config.SupplyChain.AllowedLicenses = allowed
	repo.PolicyRoot = installBundle(t, map[string]string{
		"packages": packagesResult(``, resolved),
		"licenses": `{"packages":[` + installed + `],"unsupported":[]}`,
	})
	return repo
}
