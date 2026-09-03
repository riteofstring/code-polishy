package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPackageJSONRejectsRangesAndMissingLifecyclePolicy(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@10.0.0","dependencies":{"react":"^19.0.0"}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	findings := Static(t.Context(), repo, []string{"package.json", "pnpm-lock.yaml"})
	checks := supplyChecks(findings)
	if !checks["supplyChain.exactVersion"] || !checks["supplyChain.lifecycleScripts"] {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPeerRangeNeedsExactDevDependency(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"npm@11.0.0","peerDependencies":{"react":">=18"},"devDependencies":{"react":"19.0.0"}}`+"\n")
	writeSupplyFile(t, repo.Root, "package-lock.json", "{}\n")
	_, findings := checkPackageJSON(repo, "package.json")
	if supplyChecks(findings)["supplyChain.peerDependency"] {
		t.Fatalf("exact dev dependency should support peer range: %+v", findings)
	}
}

func TestNestedPackageInheritsManagerAndLockFromAncestor(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@10.0.0","dependencies":{"typescript":"5.9.0"},"pnpm":{"onlyBuiltDependencies":[]}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeSupplyFile(t, repo.Root, "packages/web/package.json", `{"dependencies":{"react":"19.0.0"}}`+"\n")
	manifest, findings := checkPackageJSON(repo, "packages/web/package.json")
	if len(findings) != 0 || manifest.Manager != "pnpm" || manifest.ManagerVersion != "10.0.0" || manifest.Root != "." {
		t.Fatalf("manifest=%+v findings=%+v", manifest, findings)
	}
}

func TestPNPMLifecyclePolicyAtOwnerCoversNestedDependencies(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@10.0.0"}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeSupplyFile(t, repo.Root, "packages/web/package.json", `{"dependencies":{"react":"19.0.0"}}`+"\n")
	files := []string{"package.json", "pnpm-lock.yaml", "packages/web/package.json"}
	findings := Static(t.Context(), repo, files)
	if !supplyChecks(findings)["supplyChain.lifecycleScripts"] {
		t.Fatalf("nested dependency must require owner lifecycle policy: %+v", findings)
	}
}

func TestPNPMLifecyclePolicyCanBeOwnedByWorkspaceFile(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@10.0.0","dependencies":{"typescript":"5.9.0"}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeSupplyFile(t, repo.Root, "pnpm-workspace.yaml", "packages:\n  - packages/*\nonlyBuiltDependencies:\n  - esbuild\n")
	repo.PolicyRoot = installBundle(t, map[string]string{
		"packages": packagesResult("", ""),
		"workspace": workspaceResult("pnpm-workspace.yaml",
			workspaceSetting("packages", "packages/*")+","+workspaceSetting("onlyBuiltDependencies", "esbuild")),
	})
	files := []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"}
	findings := Static(t.Context(), repo, files)
	if supplyChecks(findings)["supplyChain.lifecycleScripts"] {
		t.Fatalf("workspace lifecycle policy was rejected: %+v", findings)
	}
}

func TestPNPMLifecyclePolicyRejectsDuplicateOwners(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@10.0.0","dependencies":{"typescript":"5.9.0"},"pnpm":{"onlyBuiltDependencies":[]}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeSupplyFile(t, repo.Root, "pnpm-workspace.yaml", "ignoredBuiltDependencies: []\n")
	repo.PolicyRoot = installBundle(t, map[string]string{
		"packages":  packagesResult("", ""),
		"workspace": workspaceResult("pnpm-workspace.yaml", workspaceSetting("ignoredBuiltDependencies")),
	})
	findings := Static(t.Context(), repo, []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"})
	if !findingSubject(findings, "pnpm:duplicate-owner") {
		t.Fatalf("duplicate lifecycle owners were accepted: %+v", findings)
	}
}

func TestPNPMLifecyclePolicyRejectsMutuallyExclusiveWorkspaceFields(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@10.0.0","dependencies":{"typescript":"5.9.0"}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeSupplyFile(t, repo.Root, "pnpm-workspace.yaml", "onlyBuiltDependencies: []\nignoredBuiltDependencies: []\n")
	repo.PolicyRoot = installBundle(t, map[string]string{
		"packages": packagesResult("", ""),
		"workspace": workspaceResult("pnpm-workspace.yaml",
			workspaceSetting("onlyBuiltDependencies")+","+workspaceSetting("ignoredBuiltDependencies")),
	})
	findings := Static(t.Context(), repo, []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"})
	if !findingSubject(findings, "pnpm:ambiguous") {
		t.Fatalf("ambiguous workspace lifecycle policy was accepted: %+v", findings)
	}
}

func TestNodeManifestRejectsSilentAuditIgnores(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{
  "packageManager":"pnpm@10.0.0",
  "dependencies":{"example":"1.2.3"},
  "pnpm":{"onlyBuiltDependencies":[],"auditConfig":{"ignoreCves":["CVE-2026-1000"]}}
}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	findings := Static(t.Context(), repo, []string{"package.json", "pnpm-lock.yaml"})
	if !supplyChecks(findings)["supplyChain.auditIgnore"] {
		t.Fatalf("silent audit ignore was accepted: %+v", findings)
	}
}

func TestStaticRejectsRepositoryOSVIgnoreConfiguration(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "osv-scanner.toml", `[[IgnoredVulns]]`+"\n"+`id = "CVE-2026-1000"`+"\n")
	findings := Static(t.Context(), repo, []string{"osv-scanner.toml"})
	if !supplyChecks(findings)["supplyChain.auditIgnore"] {
		t.Fatalf("OSV ignore configuration was accepted: %+v", findings)
	}
}

func TestOnlineReportsYoungGoDependenciesFromSemanticToolResults(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.PolicyRoot = repo.Root
	repo.Config.ActivePolicyModules = []policy.ActivePolicyModule{{Name: "osv", Root: "."}}
	writeSupplyFile(t, repo.Root, "go.mod", "module example.test/supply\n\ngo 1.24\n")
	writeSupplyFile(t, repo.Root, ".tools/bin/osv-scanner", "#!/bin/sh\n")
	writeSupplyFile(t, repo.Root, ".tools/bin/govulncheck", "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(repo.Root, ".tools", "bin", "osv-scanner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repo.Root, ".tools", "bin", "govulncheck"), 0o700); err != nil {
		t.Fatal(err)
	}
	boundary := supplyToolBoundary{goModules: []map[string]any{
		{"Path": "example.test/supply", "Main": true},
		{"Path": "example.test/established", "Version": "v1.0.0", "Time": time.Now().UTC().AddDate(0, 0, -30)},
		{"Path": "example.test/young", "Version": "v2.0.0", "Time": time.Now().UTC().Add(-time.Hour)},
	}}
	findings := Online(context.Background(), repo, []string{"go.mod"}, boundary)
	if len(findings) != 1 || findings[0].Check != "supplyChain.releaseAge" || !strings.Contains(findings[0].Subject, "example.test/young@v2.0.0") {
		t.Fatalf("online findings = %+v", findings)
	}
}

func TestOnlineAcceptsCleanPNPMAuditAndRegistryEvidence(t *testing.T) {
	repo := supplyRepository(t)
	repo.PolicyRoot = installBundle(t, map[string]string{
		"audit":    `{"advisories":[],"unsupported":[]}`,
		"packages": packagesResult("", ""),
	})
	registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(response, `{"time":{"11.13.0":%q}}`, time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339))
	}))
	defer registry.Close()
	repo.Config.SupplyChain.NPMRegistryURL = registry.URL
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@11.13.0","pnpm":{"onlyBuiltDependencies":[]}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	if findings := Online(context.Background(), repo, []string{"package.json", "pnpm-lock.yaml"}, supplyToolBoundary{}); len(findings) != 0 {
		t.Fatalf("pnpm audit findings = %+v", findings)
	}
}

func TestDependencyOverrideBlockRequiresExactGovernance(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	overrides := json.RawMessage(`{"example":"1.2.3"}`)
	digest, err := canonicalJSONSHA256(overrides)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	repo.Config.SupplyChain.DependencyOverridePolicies = []policy.DependencyOverridePolicy{{
		ID: "node-overrides", Ecosystem: "pnpm", Path: "package.json", Field: "pnpm.overrides",
		ContentSHA256: digest, Reason: "one transitive graph", Owner: "platform",
		Reviewed: policy.Date{Time: now}, Expires: policy.Date{Time: now.AddDate(0, 0, 30)},
	}}
	writeSupplyFile(t, repo.Root, "package.json", `{
  "packageManager":"pnpm@10.0.0",
  "dependencies":{"example":"1.2.3"},
  "pnpm":{"onlyBuiltDependencies":[],"overrides":{"example":"1.2.3"}}
}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	files := []string{"package.json", "pnpm-lock.yaml"}
	findings := Static(t.Context(), repo, files)
	if supplyChecks(findings)["supplyChain.dependencyOverride"] {
		t.Fatalf("exact governed override was rejected: %+v", findings)
	}
	if governance := DependencyOverrideGovernanceFindings(repo, files); len(governance) != 0 {
		t.Fatalf("governance findings = %+v", governance)
	}

	repo.Config.SupplyChain.DependencyOverridePolicies[0].ContentSHA256 = strings.Repeat("a", 64)
	findings = Static(t.Context(), repo, files)
	if !supplyChecks(findings)["supplyChain.dependencyOverride"] {
		t.Fatalf("changed override was accepted: %+v", findings)
	}
}

func TestNativeAuditAdvisoryBecomesExactTypedVulnerability(t *testing.T) {
	t.Parallel()
	result := javascript.AuditResult{Advisories: []javascript.Advisory{{
		ID: "GHSA-abcd-1234-5678", Aliases: []string{"npm:1100"}, Package: "example",
		Severity: "high", Title: "unsafe example path", Versions: []string{"1.2.3"},
	}, {
		ID: "npm:1200", Package: "quiet-example", Severity: "low", Versions: []string{"2.0.0"},
	}}}
	findings := nodeAuditFindings("moderate", result, "desktop/pnpm-lock.yaml")
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	want := policy.VulnerabilityIdentity{
		Ecosystem: "pnpm", Advisory: "GHSA-abcd-1234-5678",
		Aliases: []string{"npm:1100"}, Package: "example",
		AffectedVersion: "1.2.3", Scope: "desktop/pnpm-lock.yaml", Severity: "high",
	}
	if findings[0].Vulnerability == nil || !reflect.DeepEqual(*findings[0].Vulnerability, want) ||
		findings[0].Subject != "GHSA-abcd-1234-5678:example@1.2.3" {
		t.Fatalf("identity = %+v", findings[0])
	}
}

func TestNativeAuditFailsClosedOnAdvisoriesItCannotName(t *testing.T) {
	t.Parallel()
	result := javascript.AuditResult{
		Advisories: []javascript.Advisory{{
			ID: "GHSA-abcd-1234-5678", Package: "example", Severity: "high", Versions: []string{"1.2.x"},
		}},
		Unsupported: []javascript.Unsupported{{Path: "pnpm-lock.yaml", Reason: "advisory '1100' omitted the package"}},
	}
	findings := nodeAuditFindings("low", result, "pnpm-lock.yaml")
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	for _, finding := range findings {
		if finding.Check != "policy.securityScanner" || finding.Vulnerability != nil {
			t.Fatalf("incomplete coverage became a vulnerability: %+v", finding)
		}
	}
}

func TestNativeAuditFailsClosedOnPartlyUsableAdvisoryVersions(t *testing.T) {
	t.Parallel()
	result := javascript.AuditResult{Advisories: []javascript.Advisory{{
		ID: "GHSA-abcd-1234-5678", Package: "example", Severity: "high",
		Versions: []string{"1.2.3", "1.2.x"},
	}}}
	findings := nodeAuditFindings("low", result, "pnpm-lock.yaml")
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	vulnerabilities := 0
	coverage := 0
	for _, finding := range findings {
		switch {
		case finding.Check == "supplyChain.nodeVulnerability" && finding.Vulnerability != nil &&
			finding.Vulnerability.AffectedVersion == "1.2.3":
			vulnerabilities++
		case finding.Check == "policy.securityScanner" && strings.Contains(finding.Message, "1.2.x"):
			coverage++
		}
	}
	if vulnerabilities != 1 || coverage != 1 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestHighNotAffectedAssessmentCoversNativeAuditAndOSVIdentities(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	assessment := policy.VulnerabilityAssessment{
		ID: "high-not-affected", Ecosystem: "pnpm", Advisory: "CVE-2026-1000", Package: "example",
		AffectedVersion: "1.2.3", Scope: "pnpm-lock.yaml", Severity: "high", Status: "not-affected", Basis: "unreachable",
		Reason: "the affected code path is not reachable", Impact: "the vulnerable capability is not shipped",
		Evidence: "https://example.test/analysis", Tracking: "https://example.test/issues/1", Owner: "runtime",
		ApprovedBy: "security", Approval: "https://example.test/reviews/1", Reviewed: policy.Date{Time: now},
		Expires: policy.Date{Time: now.AddDate(0, 0, policy.MaximumHighNotAffectedVulnerabilityDays)},
	}
	native := nodeAuditFindings("low", javascript.AuditResult{Advisories: []javascript.Advisory{{
		ID: "GHSA-abcd-1234-5678", Aliases: []string{"CVE-2026-1000"}, Package: "example", Severity: "high", Versions: []string{"1.2.3"},
	}}}, "pnpm-lock.yaml")
	osv, err := parseOSVReport(supplyRepository(t), ".", []byte(`{
  "results":[{
    "source":{"path":"pnpm-lock.yaml","type":"lockfile"},
    "packages":[{
      "package":{"name":"example","version":"1.2.3","ecosystem":"npm"},
      "groups":[{"ids":["GHSA-abcd-1234-5678"],"aliases":["CVE-2026-1000"],"max_severity":"high"}]
    }]
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []struct {
		name     string
		findings []policy.Finding
	}{
		{name: "native audit", findings: native},
		{name: "OSV", findings: osv},
	} {
		if len(source.findings) != 1 || source.findings[0].Vulnerability == nil || source.findings[0].Vulnerability.Severity != "high" {
			t.Fatalf("%s did not produce one high structured finding: %+v", source.name, source.findings)
		}
		kept, accepted := policy.ApplyVulnerabilityAssessments(source.findings, []policy.VulnerabilityAssessment{assessment}, now, true)
		if len(kept) != 0 || len(accepted) != 1 || accepted[0].Assessment.ID != assessment.ID {
			t.Fatalf("%s high finding was not accepted by the exact assessment: kept=%+v accepted=%+v", source.name, kept, accepted)
		}
	}
}

func TestOnlineNodeAuditRunsOncePerLockOwner(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@11.13.0"}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeSupplyFile(t, repo.Root, "packages/web/package.json", "{}\n")
	manifests := validNodeManifests(repo, []string{"package.json", "pnpm-lock.yaml", "packages/web/package.json"})
	if got := uniqueAuditableNodeManifests(manifests); len(got) != 1 || got[0].Root != "." {
		t.Fatalf("audit manifests = %+v", got)
	}
}

func TestNonPNPMNodeManifestNeedsASecurityProvider(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"npm@11.0.0"}`+"\n")
	writeSupplyFile(t, repo.Root, "package-lock.json", "{}\n")
	subjects := map[string]bool{}
	for _, finding := range CoverageFindings(repo, []string{"package.json", "package-lock.json"}) {
		subjects[finding.Subject] = true
	}
	if !subjects["repository:security"] || !subjects["repository:lock-sync"] {
		t.Fatalf("npm coverage requirements = %+v", subjects)
	}
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@11.13.0"}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	for _, finding := range CoverageFindings(repo, []string{"package.json", "pnpm-lock.yaml"}) {
		if strings.HasSuffix(finding.Subject, ":security") || strings.HasSuffix(finding.Subject, ":lock-sync") {
			t.Fatalf("a pnpm manifest was asked for a provider: %+v", finding)
		}
	}
}

func TestResolvedLockParsersIncludeTransitivePackages(t *testing.T) {
	t.Parallel()
	packageLock := []byte(`{
  "lockfileVersion": 3,
  "packages": {
    "": {"name":"app","version":"1.0.0"},
    "node_modules/direct": {"version":"2.0.0"},
    "node_modules/direct/node_modules/transitive": {"version":"3.0.0"}
  }
}`)
	npm, err := parsePackageLock(packageLock, "package-lock.json", "npm")
	if err != nil || len(npm) != 2 || npm[1].Name != "transitive" {
		t.Fatalf("npm packages=%+v err=%v", npm, err)
	}
	uv, err := parseUVLock([]byte("[[package]]\nname = \"direct\"\nversion = \"2.0.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n\n[[package]]\nname = \"local\"\nversion = \"1.0.0\"\nsource = { editable = \".\" }\n"), "uv.lock")
	if err != nil || len(uv) != 2 || uv[0].Name != "local" || uv[0].Source.Kind != "local" || uv[1].Name != "direct" || uv[1].Source.Kind != "registry" {
		t.Fatalf("uv packages=%+v err=%v", uv, err)
	}
}

func TestReleaseMetadataParsersKeepExactVersionTimes(t *testing.T) {
	t.Parallel()
	node, err := decodeNodeReleaseTimes(strings.NewReader(`{"time":{"1.2.3":"2026-07-01T12:00:00Z"}}`))
	if err != nil || node["1.2.3"].IsZero() {
		t.Fatalf("node times=%v err=%v", node, err)
	}
	pypi, err := decodePyPIReleaseTimes(strings.NewReader(`{"releases":{"1.2.3":[{"upload_time_iso_8601":"2026-07-02T12:00:00Z"},{"upload_time_iso_8601":"2026-07-01T12:00:00Z"}]}}`))
	if err != nil || pypi["1.2.3"].Format(time.RFC3339) != "2026-07-01T12:00:00Z" {
		t.Fatalf("pypi times=%v err=%v", pypi, err)
	}
}

func TestOSVReportProducesExactTypedFindingAtUnknownSeverity(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	payload := []byte(`{
  "results":[{
    "source":{"path":"pnpm-lock.yaml","type":"lockfile"},
    "packages":[{
      "package":{"name":"example","version":"1.2.3","ecosystem":"npm"},
      "vulnerabilities":[{"id":"GHSA-abcd-1234-5678","aliases":["CVE-2026-1000"]}],
      "groups":[{"ids":["GHSA-abcd-1234-5678"]}]
    }]
  }]
}`)
	findings, err := parseOSVReport(repo, ".", payload)
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
	want := policy.VulnerabilityIdentity{
		Ecosystem: "pnpm", Advisory: "GHSA-abcd-1234-5678",
		Aliases: []string{"CVE-2026-1000", "GHSA-abcd-1234-5678"}, Package: "example",
		AffectedVersion: "1.2.3", Scope: "pnpm-lock.yaml", Severity: "unknown",
	}
	if findings[0].Vulnerability == nil || !reflect.DeepEqual(*findings[0].Vulnerability, want) || !strings.Contains(findings[0].Message, "unknown") {
		t.Fatalf("finding = %+v", findings[0])
	}
}

func TestKnownExploitedCatalogClassifiesVulnerabilityAliases(t *testing.T) {
	t.Parallel()
	known, err := parseKnownExploitedCatalog([]byte(`{
  "catalogVersion":"2026.08.14",
  "vulnerabilities":[{"cveID":"CVE-2026-1000"}]
}`))
	if err != nil || !known["CVE-2026-1000"] {
		t.Fatalf("known=%v err=%v", known, err)
	}
	identity := policy.VulnerabilityIdentity{
		Ecosystem: "npm", Advisory: "GHSA-abcd-1234-5678", Aliases: []string{"CVE-2026-1000", "CVE-2026-2000"},
		Package: "example", AffectedVersion: "1.2.3", Scope: "package-lock.json", Severity: "low",
	}
	findings := classifyKnownExploited([]policy.Finding{{
		Check: "supplyChain.osvVulnerability", Path: identity.Scope, Vulnerability: &identity, Message: "reported",
	}}, map[string]bool{"CVE-2026-2000": true})
	if len(findings) != 1 || findings[0].Vulnerability == nil || !findings[0].Vulnerability.KnownExploited || !strings.Contains(findings[0].Message, "CISA") {
		t.Fatalf("classified findings=%+v", findings)
	}
	if _, err := parseKnownExploitedCatalog([]byte(`{"catalogVersion":"missing-list"}`)); err == nil {
		t.Fatal("catalog without vulnerabilities was accepted")
	}
}

func TestUnknownNativeAuditSeverityFailsClosed(t *testing.T) {
	t.Parallel()
	if !nodeAuditSeverityAtLeast("unknown", "low") || nodeAuditSeverityAtLeast("low", "high") {
		t.Fatal("native audit severity threshold did not fail closed")
	}
}

func TestSecurityMonitoringRequiresWeeklyOnlineScan(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"npm@11.0.0"}`+"\n")
	writeSupplyFile(t, repo.Root, "package-lock.json", "{}\n")
	files := []string{"package.json", "package-lock.json"}
	if findings := securityMonitoringFindings(repo, files); len(findings) != 0 {
		t.Fatalf("monitoring was required without opt-in: %+v", findings)
	}
	repo.Config.SupplyChain.RecurringSecurityMonitoring = true
	path := ".github/workflows/security.yml"
	writeSupplyFile(t, repo.Root, path, "on:\n  schedule:\n    - cron: '0 4 * * 1'\njobs:\n  scan:\n    steps:\n      - run: ./bin/code-polishy supply-chain\n")
	files = append(files, path)
	if findings := securityMonitoringFindings(repo, files); len(findings) != 0 {
		t.Fatalf("weekly scan rejected: %+v", findings)
	}
	writeSupplyFile(t, repo.Root, path, "on:\n  schedule:\n    - cron: '0 4 1 * *'\njobs:\n  scan:\n    steps:\n      - run: ./bin/code-polishy supply-chain --offline\n")
	if findings := securityMonitoringFindings(repo, files); len(findings) != 1 {
		t.Fatalf("monthly offline scan accepted: %+v", findings)
	}
	lookalike := "env:\n  cron: '0 4 * * 1'\n  NOTE: ./bin/code-polishy supply-chain\njobs: {}\n"
	if workflowRunsWeeklySecurity(lookalike) {
		t.Fatal("non-workflow cron and command text were accepted as monitoring evidence")
	}
	if cronRunsAtLeastWeekly("90 25 * * 1") {
		t.Fatal("invalid weekly-looking cron was accepted")
	}
	repo.Config.Checks = []policy.Command{{Name: "external-monitor", Provides: []string{"security-monitoring"}, RunOn: []string{"security"}}}
	if findings := securityMonitoringFindings(repo, files); len(findings) != 0 {
		t.Fatalf("declared monitoring provider rejected: %+v", findings)
	}
}

func TestWeeklySecurityWorkflowRecognizesSupportedSchedulesAndRunForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		workflow string
		accepted bool
	}{
		{
			name:     "named weekday range and gate",
			workflow: "on:\n  schedule:\n    - cron: '0 0 * * MON-FRI'\njobs:\n  scan:\n    steps:\n      - run: code-polishy gate\n",
			accepted: true,
		},
		{
			name:     "seven day interval and multiline online scan",
			workflow: "on:\n  schedule:\n    cron: '*/15 0,12 */7 * *'\n  push:\njobs:\n  scan:\n    steps:\n      - run: |\n          code-polishy supply-chain\n      - name: record result\n",
			accepted: true,
		},
		{
			name:     "interval exceeds one week",
			workflow: "on:\n  schedule:\n    - cron: '0 0 */8 * *'\njobs:\n  scan:\n    steps:\n      - run: code-polishy gate\n",
		},
		{
			name:     "weekday outside cron range",
			workflow: "on:\n  schedule:\n    - cron: '0 0 * * 8'\njobs:\n  scan:\n    steps:\n      - run: code-polishy gate\n",
		},
		{
			name:     "zero minute step",
			workflow: "on:\n  schedule:\n    - cron: '*/0 0 * * MON'\njobs:\n  scan:\n    steps:\n      - run: code-polishy gate\n",
		},
		{
			name:     "offline multiline scan",
			workflow: "on:\n  schedule:\n    - cron: '0 0 * * MON'\njobs:\n  scan:\n    steps:\n      - run: >-\n          code-polishy supply-chain --offline\n",
		},
		{
			name:     "schedule outside the on block",
			workflow: "on:\n  push:\nschedule:\n  - cron: '0 0 * * MON'\njobs:\n  scan:\n    steps:\n      - run: code-polishy gate\n",
		},
		{
			name:     "cron under a sibling event",
			workflow: "on:\n  schedule:\n  push:\n    - cron: '0 0 * * MON'\njobs:\n  scan:\n    steps:\n      - run: code-polishy gate\n",
		},
		{
			name:     "command after multiline run block",
			workflow: "on:\n  schedule:\n    - cron: '0 0 * * MON'\njobs:\n  scan:\n    steps:\n      - run: |\n          echo complete\n      - name: publish\n        env:\n          COMMAND: code-polishy gate\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if accepted := workflowRunsWeeklySecurity(testCase.workflow); accepted != testCase.accepted {
				t.Fatalf("weekly security workflow accepted = %v", accepted)
			}
		})
	}
}

func TestPNPMNativeSecuritySettingsAreVersionGated(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.MinimumReleaseAgeDays = 30
	repo.Config.SupplyChain.ReleaseAgeAssessments = []policy.ReleaseAgeAssessment{{
		ID: "example-young", Ecosystem: "pnpm", Package: "example", Version: "1.2.3", Scope: "pnpm-lock.yaml",
	}}
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@11.3.0","dependencies":{"example":"1.2.3"}}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeSupplyFile(t, repo.Root, "pnpm-workspace.yaml", "minimumReleaseAge: 43200\n")
	hardened := []string{
		workspaceSetting("minimumReleaseAge", "43200"),
		workspaceSetting("minimumReleaseAgeExclude", "example@1.2.3"),
		workspaceSetting("minimumReleaseAgeIgnoreMissingTime", "false"),
		workspaceSetting("minimumReleaseAgeStrict", "true"),
		workspaceSetting("trustPolicy", "no-downgrade"),
		workspaceSetting("trustLockfile", "false"),
		workspaceSetting("blockExoticSubdeps", "true"),
		workspaceSetting("ignoredBuiltDependencies"),
	}
	settings := func(replacement ...string) string {
		declared := slices.Clone(hardened)
		copy(declared, replacement)
		return workspaceResult("pnpm-workspace.yaml", strings.Join(declared, ","))
	}
	governance := func(result string) []policy.Finding {
		repo.PolicyRoot = installBundle(t, map[string]string{"workspace": result})
		return pnpmGovernanceFindings(t.Context(), repo, []string{"package.json"})
	}
	if findings := governance(settings()); len(findings) != 0 {
		t.Fatalf("hardened settings rejected: %+v", findings)
	}
	findings := governance(settings(workspaceSetting("minimumReleaseAge", "1440")))
	if len(findings) != 1 || findings[0].Subject != "minimumReleaseAge" {
		t.Fatalf("weak minimum accepted: %+v", findings)
	}
	findings = governance(settings(hardened[0], workspaceSetting("minimumReleaseAgeExclude", "example")))
	if len(findings) != 1 || findings[0].Subject != "example" {
		t.Fatalf("broad native exclusion accepted: %+v", findings)
	}
}

func TestDependencyChangesCompareDirectAndTransitiveCandidateVersions(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"npm@11.0.0","dependencies":{"direct":"1.0.0"}}`+"\n")
	writeSupplyFile(t, repo.Root, "package-lock.json", `{"lockfileVersion":3,"packages":{"":{"name":"app","version":"1.0.0"},"node_modules/direct":{"version":"1.0.0"},"node_modules/transitive":{"version":"2.0.0"}}}`+"\n")
	writeSupplyFile(t, repo.Root, "go.mod", "module example.test/app\n\nrequire example.test/dependency v1.0.0\n")
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = [\"requests==2.32.0\"]\n")
	gitSupply(t, repo.Root, "init", "-b", "main")
	gitSupply(t, repo.Root, "config", "user.email", "tests@example.test")
	gitSupply(t, repo.Root, "config", "user.name", "Policy Tests")
	gitSupply(t, repo.Root, "add", ".")
	gitSupply(t, repo.Root, "commit", "-m", "base")
	baseOutput, err := exec.Command("git", "-C", repo.Root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(string(baseOutput))
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"npm@11.0.0","dependencies":{"direct":"1.1.0"}}`+"\n")
	writeSupplyFile(t, repo.Root, "package-lock.json", `{"lockfileVersion":3,"packages":{"":{"name":"app","version":"1.0.0"},"node_modules/direct":{"version":"1.1.0"},"node_modules/transitive":{"version":"2.1.0"}}}`+"\n")
	writeSupplyFile(t, repo.Root, "go.mod", "module example.test/app\n\nrequire example.test/dependency v1.1.0\n")
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = [\"requests==2.33.0\"]\n")
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil {
		t.Fatal(err)
	}
	observed := map[string]string{}
	for _, change := range changes {
		observed[change.Ecosystem+"/"+change.Directness+"/"+change.Package] = change.From + " -> " + change.To
	}
	expected := map[string]string{
		"go/direct/example.test/dependency": "v1.0.0 -> v1.1.0",
		"npm/direct/direct":                 "1.0.0 -> 1.1.0",
		"npm/transitive/transitive":         "2.0.0 -> 2.1.0",
		"pypi/direct/requests":              "2.32.0 -> 2.33.0",
	}
	if !reflect.DeepEqual(observed, expected) {
		t.Fatalf("dependency changes = %+v", changes)
	}
}

func TestNewDependencyAgeAdvisoriesUseRegistryEvidence(t *testing.T) {
	t.Parallel()
	recent := time.Now().UTC().Add(-time.Hour)
	established := time.Now().UTC().AddDate(0, 0, -60)
	registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		released := established
		if request.URL.Path == "/young" {
			released = recent
		}
		_, _ = fmt.Fprintf(response, `{"time":{"1.0.0":%q}}`, released.Format(time.RFC3339))
	}))
	defer registry.Close()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.NPMRegistryURL = registry.URL
	repo.Config.SupplyChain.PreferredNewDependencyAgeDays = 14
	changes := []DependencyChange{
		{Ecosystem: "npm", Scope: "package.json", Directness: "direct", Usage: "runtime", Change: "added", Package: "young", From: "-", To: "1.0.0"},
		{Ecosystem: "npm", Scope: "package.json", Directness: "direct", Usage: "optional", Change: "updated", Package: "established", From: "0.9.0", To: "1.0.0"},
		{Ecosystem: "npm", Scope: "package.json", Directness: "direct", Usage: "development", Change: "added", Package: "ignored-development", From: "-", To: "1.0.0"},
		{Ecosystem: "npm", Scope: "package-lock.json", Directness: "transitive", Usage: "resolved", Change: "added", Package: "ignored-transitive", From: "-", To: "1.0.0"},
	}
	advisories := NewDependencyAgeAdvisories(t.Context(), repo, changes)
	if len(advisories) != 1 || advisories[0].Subject != "young@1.0.0" || advisories[0].Path != "package.json" {
		t.Fatalf("new dependency age advisories = %+v", advisories)
	}
}

func TestWorkflowRequiresFullActionCommit(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, ".github/workflows/ci.yml", "steps:\n  - uses: actions/checkout@v4\n  - uses: ./local\n  - run: echo 'uses: ignored/example@v1'\n")
	findings := checkWorkflowPins(repo, ".github/workflows/ci.yml")
	if len(findings) != 1 || findings[0].Subject != "actions/checkout@v4" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestContainerRequiresDigest(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	pinned := "alpine@sha256:" + strings.Repeat("a", 64)
	contents := "FROM --platform=linux/arm64 golang:1.26 AS build\nFROM build AS compiled\nFROM " + pinned + " AS runtime\nFROM scratch\n"
	writeSupplyFile(t, repo.Root, "Dockerfile", contents)
	findings := checkContainerPins(repo, "Dockerfile")
	if len(findings) != 1 || findings[0].Subject != "golang:1.26" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestContainerRejectsDynamicBaseImage(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "Dockerfile", "ARG BASE\nFROM ${BASE}\n")
	findings := checkContainerPins(repo, "Dockerfile")
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "dynamic") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestUnsupportedManifestsRequireCompleteProvidersAtTheirLanguageModule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		manifest string
		source   string
	}{
		{name: "python requirements", manifest: "requirements-prod.txt", source: "src/main.py"},
		{name: "cargo", manifest: "Cargo.toml", source: "src/lib.rs"},
		{name: "maven", manifest: "pom.xml", source: "src/Main.java"},
		{name: "gradle", manifest: "build.gradle.kts", source: "src/Main.java"},
		{name: "bundler", manifest: "Gemfile", source: "src/main.rb"},
		{name: "composer", manifest: "composer.json", source: "src/main.php"},
		{name: "swift package", manifest: "Package.swift", source: "src/main.swift"},
		{name: "dart package", manifest: "pubspec.yaml", source: "src/main.dart"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repo := supplyRepository(t)
			repo.Config.Modules = []policy.Module{{Name: "application", Paths: []string{"src/**"}}}
			repo.Config.ModuleByName = map[string]int{"application": 0}
			writeSupplyFile(t, repo.Root, testCase.manifest, "fixture\n")
			writeSupplyFile(t, repo.Root, testCase.source, "fixture\n")
			files := []string{testCase.manifest, testCase.source}
			findings := CoverageFindings(repo, files)
			if countFindings(findings, "policy.supplyChainCoverage") != 4 || supplyChecks(findings)["policy.securityMonitoring"] {
				t.Fatalf("missing provider contract = %+v", findings)
			}
			for _, finding := range findings {
				if finding.Check == "policy.supplyChainCoverage" && !strings.HasPrefix(finding.Subject, "application:") {
					t.Fatalf("manifest escaped its language module: %+v", findings)
				}
			}
			repo.Config.Checks = []policy.Command{{
				Name: "application-supply-chain", Provides: []string{"dependency-policy", "lock-sync", "release-age", "security"},
				Modules: []string{"application"}, RunOn: []string{"supply-chain", "supply-chain-online", "security"},
			}}
			if findings := CoverageFindings(repo, files); len(findings) != 0 {
				t.Fatalf("complete module provider was rejected: %+v", findings)
			}
		})
	}
}

func TestGeneratedScopeCannotHideDependencyManifest(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Scope.Generated = []string{"Cargo.toml"}
	findings := CoverageFindings(repo, []string{"Cargo.toml"})
	if countFindings(findings, "policy.supplyChainCoverage") != 4 || supplyChecks(findings)["policy.securityMonitoring"] {
		t.Fatalf("generated scope must not suppress dependency coverage: %+v", findings)
	}
}

func TestManifestCoverageOnlyTargetsMatchingLanguageModules(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Modules = []policy.Module{
		{Name: "backend", Paths: []string{"backend/**"}},
		{Name: "frontend", Paths: []string{"frontend/**"}},
	}
	repo.Config.ModuleByName = map[string]int{"backend": 0, "frontend": 1}
	repo.Config.Checks = []policy.Command{{Name: "monitor", Provides: []string{"security-monitoring"}, RunOn: []string{"security"}}}
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"npm@11.0.0"}`+"\n")
	writeSupplyFile(t, repo.Root, "package-lock.json", "{}\n")
	writeSupplyFile(t, repo.Root, "backend/main.go", "package main\n")
	writeSupplyFile(t, repo.Root, "frontend/main.ts", "export {}\n")
	findings := CoverageFindings(repo, []string{"package.json", "package-lock.json", "backend/main.go", "frontend/main.ts"})
	if len(findings) != 2 {
		t.Fatalf("Node manifest coverage = %+v", findings)
	}
	for _, finding := range findings {
		if !strings.HasPrefix(finding.Subject, "frontend:") {
			t.Fatalf("Node manifest should only govern TypeScript modules: %+v", findings)
		}
	}
}

func TestPythonProjectRequiresLockProviderAndUsesBuiltInOSV(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "python", Paths: []string{"app/**"}}}
	repo.Config.ModuleByName = map[string]int{"python": 0}
	repo.Config.Checks = []policy.Command{{Name: "monitor", Provides: []string{"security-monitoring"}, RunOn: []string{"security"}}}
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = []\n")
	writeSupplyFile(t, repo.Root, "uv.lock", "version = 1\n")
	writeSupplyFile(t, repo.Root, "app/main.py", "print('ok')\n")
	files := []string{"pyproject.toml", "uv.lock", "app/main.py"}
	findings := CoverageFindings(repo, files)
	if len(findings) != 1 || findings[0].Subject != "python:lock-sync" {
		t.Fatalf("expected only lock-sync coverage; OSV security is built in: %+v", findings)
	}
}

func TestContainerCapabilityNeedsSecurityProviderAtDeclaredScope(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "api", Paths: []string{"api/**"}, Capabilities: []string{"container"}}}
	findings := CoverageFindings(repo, nil)
	if len(findings) != 1 || findings[0].Subject != "api:container:security" {
		t.Fatalf("findings = %+v", findings)
	}
	repo.Config.Checks = []policy.Command{{Name: "scan", Provides: []string{"security"}, Modules: []string{"api"}, RunOn: []string{"security"}}}
	if findings = CoverageFindings(repo, nil); len(findings) != 0 {
		t.Fatalf("module scanner should cover container: %+v", findings)
	}
}

func TestDockerfileAutomaticallyRequiresSecurityCoverage(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "api", Paths: []string{"api/**"}}}
	findings := CoverageFindings(repo, []string{"api/Dockerfile"})
	if len(findings) != 1 || findings[0].Subject != "api:container:security" {
		t.Fatalf("findings = %+v", findings)
	}
	repo.Config.Checks = []policy.Command{{Name: "scan", Provides: []string{"security"}, Modules: []string{"api"}, RunOn: []string{"security"}}}
	if findings = CoverageFindings(repo, []string{"api/Dockerfile"}); len(findings) != 0 {
		t.Fatalf("scanner should cover Dockerfile: %+v", findings)
	}
}

func TestArtifactSecurityTargetCoversItsDockerfile(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "api", Paths: []string{"api/**"}}}
	repo.Config.SupplyChain.ArtifactSecurity.Targets = []policy.ArtifactTarget{{
		Name: "api-image", Module: "api", Mode: "dockerfile", Dockerfile: "api/Dockerfile", Context: "api",
	}}
	if findings := CoverageFindings(repo, []string{"api/Dockerfile"}); len(findings) != 0 {
		t.Fatalf("declared artifact target did not cover its Dockerfile: %+v", findings)
	}
}

func TestCustomDependencyCapabilityRequiresCompleteProvider(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "beam", Paths: []string{"lib/**"}, Capabilities: []string{"custom-dependencies"}}}
	if findings := CoverageFindings(repo, nil); countFindings(findings, "policy.supplyChainCoverage") != 4 || supplyChecks(findings)["policy.securityMonitoring"] {
		t.Fatalf("expected complete custom dependency contract: %+v", findings)
	}
	repo.Config.Checks = []policy.Command{{
		Name: "mix-supply", Provides: []string{"dependency-policy", "lock-sync", "release-age", "security"},
		Modules: []string{"beam"}, RunOn: []string{"supply-chain", "supply-chain-online", "security"},
	}}
	if findings := CoverageFindings(repo, nil); len(findings) != 0 {
		t.Fatalf("provider should cover custom dependencies: %+v", findings)
	}
}

func TestGovernedInputsIncludeManifestsAndContainers(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"Cargo.toml", "requirements-prod.txt", "Dockerfile.release", "Containerfile", ".github/workflows/ci.yml"} {
		if !IsGovernedInput(path) {
			t.Fatalf("%s should be governed", path)
		}
	}
}

func TestGoModuleRequiresSumForRegistryDependencies(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "go.mod", "module example.test/app\n\nrequire example.com/dependency v1.2.3\n")
	findings := checkGoModule(repo, "go.mod")
	if len(findings) != 1 || findings[0].Check != "supplyChain.lockfile" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoModuleRejectsMalformedAndUnpinnedReplaceVersions(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	contents := "module example.test/app\n\nrequire example.test/dependency version\nreplace example.test/dependency => example.test/fork latest\n"
	writeSupplyFile(t, repo.Root, "go.mod", contents)
	findings := checkGoModule(repo, "go.mod")
	checks := supplyChecks(findings)
	if !checks["supplyChain.goVersion"] || !checks["supplyChain.goReplace"] {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoModuleRejectsEscapingReplaceInsideBlock(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	outside := filepath.Base(t.TempDir())
	contents := "module example.test/app\n\nreplace (\n  example.test/dependency => ../../" + outside + "\n)\n"
	writeSupplyFile(t, repo.Root, "go.mod", contents)
	findings := checkGoModule(repo, "go.mod")
	if len(findings) != 1 || findings[0].Check != "supplyChain.goReplace" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoWorkspaceRejectsEscapingUseAndReplace(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "module/go.mod", "module example.test/module\n")
	contents := "go 1.26.5\n\nuse (\n  ./module\n  ../../outside\n)\n\nreplace (\n  example.test/dependency => ../../replacement\n)\n"
	writeSupplyFile(t, repo.Root, "go.work", contents)
	findings := checkGoWorkspace(repo, "go.work")
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonDependenciesMustBeExactAndLocked(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "python", Paths: []string{"**"}}}
	repo.Config.ModuleByName = map[string]int{"python": 0}
	repo.Config.Checks = []policy.Command{{Name: "audit", Provides: []string{"security"}, Modules: []string{"python"}, RunOn: []string{"security"}}}
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = [\n  \"requests>=2\",\n]\n")
	writeSupplyFile(t, repo.Root, "uv.lock", "version = 1\n")
	findings := checkPythonProject(repo, "pyproject.toml")
	if len(findings) != 1 || findings[0].Check != "supplyChain.pythonExactVersion" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonWildcardIsNotAnExactVersion(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	project, err := repository.ParsePythonProject("pyproject.toml", []byte("[project]\ndependencies = [\"requests==2.*\", \"urllib3==2.5.0\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	findings := pythonDependencyFindings(repo, "pyproject.toml", project.Requirements)
	if len(findings) != 1 || findings[0].Subject != "requests" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonFileDependencyMustRemainInsideRepository(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	project, err := repository.ParsePythonProject("pyproject.toml", []byte("[project]\ndependencies = [\"shared @ file:../../outside\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	findings := pythonDependencyFindings(repo, "pyproject.toml", project.Requirements)
	if len(findings) != 1 || findings[0].Check != "supplyChain.localDependency" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonDependencyParserIncludesOptionalAndDependencyGroups(t *testing.T) {
	t.Parallel()
	manifest := `[project]
dependencies = ["requests==2.32.0"]

[project.optional-dependencies]
dev = [
  "pytest==9.0.0",
]

[dependency-groups]
docs = ["mkdocs==1.6.0"]
`
	got := parsePythonDependencies(manifest)
	want := []string{"requests==2.32.0", "pytest==9.0.0", "mkdocs==1.6.0"}
	if !slices.Equal(got, want) {
		t.Fatalf("dependencies = %v", got)
	}
}

func TestPythonGitDependenciesRequireMatchingUVSourceCommitAndSubdirectory(t *testing.T) {
	t.Parallel()
	commit := "0123456789abcdef0123456789abcdef01234567"
	repo := supplyRepository(t)
	manifest := "[project]\ndependencies = [\"private-tool @ git+https://github.com/example/private-tool.git@" + commit + "#subdirectory=src/tool\"]\n"
	writeSupplyFile(t, repo.Root, "pyproject.toml", manifest)
	lock := "[[package]]\nname = \"private-tool\"\nversion = \"1.0.0\"\nsource = { git = \"https://github.com/example/private-tool.git?subdirectory=src%2Ftool&rev=" + commit + "#" + commit + "\" }\n"
	writeSupplyFile(t, repo.Root, "uv.lock", lock)
	if findings := checkPythonProject(repo, "pyproject.toml"); len(findings) != 0 {
		t.Fatalf("matching Git lock was rejected: %+v", findings)
	}
	for name, replacement := range map[string]string{
		"repository":   "https://github.com/example/other.git",
		"commit":       "abcdef0123456789abcdef0123456789abcdef01",
		"subdirectory": "src%2Fother",
	} {
		name, replacement := name, replacement
		t.Run(name, func(t *testing.T) {
			candidate := strings.Replace(lock, map[string]string{
				"repository":   "https://github.com/example/private-tool.git",
				"commit":       commit,
				"subdirectory": "src%2Ftool",
			}[name], replacement, 1)
			writeSupplyFile(t, repo.Root, "uv.lock", candidate)
			findings := checkPythonProject(repo, "pyproject.toml")
			if len(findings) != 1 || findings[0].Check != "supplyChain.lockConsistency" || findings[0].Subject != "private-tool" {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
	writeSupplyFile(t, repo.Root, "uv.lock", "[[package]]\nname = \"other\"\nversion = \"1.0.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n")
	if findings := checkPythonProject(repo, "pyproject.toml"); len(findings) != 1 || findings[0].Check != "supplyChain.lockConsistency" || findings[0].Subject != "private-tool" {
		t.Fatalf("missing Git package findings = %+v", findings)
	}
}

func TestPythonBuildRequirementsUseTheSameExactSourcePolicy(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = []\n[build-system]\nrequires = [\"hatchling>=1.25\"]\n")
	writeSupplyFile(t, repo.Root, "uv.lock", "version = 1\n")
	findings := checkPythonProject(repo, "pyproject.toml")
	if len(findings) != 1 || findings[0].Check != "supplyChain.pythonExactVersion" || findings[0].Subject != "hatchling" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonBuildGitRequirementDoesNotRequireUVLockEvidence(t *testing.T) {
	t.Parallel()
	commit := "0123456789abcdef0123456789abcdef01234567"
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = []\n[build-system]\nrequires = [\"private-build @ git+https://github.com/example/private-build.git@"+commit+"\"]\n")
	writeSupplyFile(t, repo.Root, "uv.lock", "version = 1\n")
	if findings := checkPythonProject(repo, "pyproject.toml"); len(findings) != 0 {
		t.Fatalf("build-isolation Git requirement was incorrectly required in uv.lock: %+v", findings)
	}
}

func TestPythonGitLockMatchesRepositoryAcrossSSHUsers(t *testing.T) {
	t.Parallel()
	commit := "0123456789abcdef0123456789abcdef01234567"
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = [\"private-tool @ git+ssh://git@github.com/example/private-tool.git@"+commit+"\"]\n")
	writeSupplyFile(t, repo.Root, "uv.lock", "[[package]]\nname = \"private-tool\"\nversion = \"1.0.0\"\nsource = { git = \"ssh://lock-user@github.com/example/private-tool.git?rev="+commit+"#"+commit+"\" }\n")
	if findings := checkPythonProject(repo, "pyproject.toml"); len(findings) != 0 {
		t.Fatalf("same Git repository with another SSH user was rejected: %+v", findings)
	}
}

func TestUVLockRetainsGitSourceFacts(t *testing.T) {
	t.Parallel()
	commit := "0123456789abcdef0123456789abcdef01234567"
	packages, err := parseUVLock([]byte("[[package]]\nname = \"private-tool\"\nversion = \"1.0.0\"\nsource = {\n  git = \"https://github.com/example/private-tool.git?subdirectory=src%2Ftool&rev="+commit+"#"+commit+"\",\n}\n"), "uv.lock")
	if err != nil || len(packages) != 1 || packages[0].Source.Kind != "git" || packages[0].Source.Git.DeclaredRef != commit || packages[0].Source.Git.Commit != commit || packages[0].Source.Git.Subdirectory != "src/tool" {
		t.Fatalf("packages = %+v, err = %v", packages, err)
	}
}

func TestUVLockFailsClosedForMutableGitReferenceEvidence(t *testing.T) {
	t.Parallel()
	commit := "0123456789abcdef0123456789abcdef01234567"
	manifest := "[project]\ndependencies = [\"private-tool @ git+https://github.com/example/private-tool.git@" + commit + "\"]\n"
	for name, reference := range map[string]string{
		"branch":   "main",
		"short":    commit[:12],
		"mismatch": "abcdef0123456789abcdef0123456789abcdef01",
	} {
		name, reference := name, reference
		t.Run(name, func(t *testing.T) {
			repo := supplyRepository(t)
			writeSupplyFile(t, repo.Root, "pyproject.toml", manifest)
			writeSupplyFile(t, repo.Root, "uv.lock", "[[package]]\nname = \"private-tool\"\nversion = \"1.0.0\"\nsource = { git = \"https://github.com/example/private-tool.git?rev="+reference+"#"+commit+"\" }\n")
			findings := checkPythonProject(repo, "pyproject.toml")
			if len(findings) != 1 || findings[0].Check != "supplyChain.lockConsistency" || findings[0].Subject != "private-tool" {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestUVLockScopesPackageFieldsToThePackageTable(t *testing.T) {
	t.Parallel()
	commit := "0123456789abcdef0123456789abcdef01234567"
	lock := "[[package]]\nname = \"kept\"\nversion = \"1.0.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n\n[package.metadata]\nname = \"overwritten\"\nversion = \"9.9.9\"\nsource = { git = \"https://github.com/example/private-tool.git?rev=" + commit + "#" + commit + "\" }\n"
	packages, err := parseUVLock([]byte(lock), "uv.lock")
	if err != nil || len(packages) != 1 || packages[0].Name != "kept" || packages[0].Version != "1.0.0" || packages[0].Source.Kind != "registry" {
		t.Fatalf("packages = %+v, err = %v", packages, err)
	}
}

func TestPythonGitSourceCoverageIsExplicit(t *testing.T) {
	t.Parallel()
	commit := "0123456789abcdef0123456789abcdef01234567"
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "uv.lock", "[[package]]\nname = \"private-tool\"\nversion = \"1.0.0\"\nsource = { git = \"https://github.com/example/private-tool.git?rev="+commit+"#"+commit+"\" }\n")
	findings := checkResolvedPythonReleaseAge(t.Context(), repo, "pyproject.toml")
	checks := map[string]bool{}
	for _, finding := range findings {
		checks[finding.Check] = true
	}
	if len(findings) != 2 || !checks["supplyChain.releaseAgeCoverage"] || !checks["policy.securityScanner"] {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDependencyReviewRepresentsGitSourceAndCommitWithoutReleaseAge(t *testing.T) {
	t.Parallel()
	commitBefore := "0123456789abcdef0123456789abcdef01234567"
	commitAfter := "abcdef0123456789abcdef0123456789abcdef01"
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = [\"private-tool @ git+ssh://git@github.com/example/private-tool.git@"+commitBefore+"\"]\n")
	gitSupply(t, repo.Root, "init", "-b", "main")
	gitSupply(t, repo.Root, "config", "user.email", "tests@example.test")
	gitSupply(t, repo.Root, "config", "user.name", "Policy Tests")
	gitSupply(t, repo.Root, "add", ".")
	gitSupply(t, repo.Root, "commit", "-m", "base")
	output, err := exec.Command("git", "-C", repo.Root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(string(output))
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[project]\ndependencies = [\"private-tool @ git+ssh://git@github.com/example/private-tool.git@"+commitAfter+"\"]\n")
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes = %+v, err = %v", changes, err)
	}
	change := changes[0]
	if change.Ecosystem != "git" || change.Directness != "direct" || change.Package != "private-tool" || !strings.Contains(change.From, commitBefore) || !strings.Contains(change.To, commitAfter) || newDependencyAgeCandidate(change) {
		t.Fatalf("Git dependency change = %+v", change)
	}
}

func supplyRepository(t *testing.T) repository.Repository {
	t.Helper()
	return repository.Repository{Root: t.TempDir(), Config: policy.Config{SupplyChain: policy.SupplyChain{MinimumReleaseAgeDays: 7, AuditLevel: "moderate", AllowedDependencyProtocols: []string{"workspace:", "file:", "link:"}, NPMRegistryURL: "https://registry.npmjs.org"}}}
}

func writeSupplyFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func supplyChecks(findings []policy.Finding) map[string]bool {
	checks := map[string]bool{}
	for _, finding := range findings {
		checks[finding.Check] = true
	}
	return checks
}

type supplyToolBoundary struct {
	goModules []map[string]any
}

func (boundary supplyToolBoundary) Run(context.Context, string, policy.Command) error {
	return nil
}

func (boundary supplyToolBoundary) RunWithOutput(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	if request, found := argumentAfter(command.Argv, "--request-json"); found {
		var envelope struct {
			Operation string `json:"operation"`
		}
		if err := json.Unmarshal([]byte(request), &envelope); err != nil {
			return runner.Result{}, runner.Output{}, err
		}
		if envelope.Operation != "audit" {
			return runner.Result{}, runner.Output{}, fmt.Errorf("unsupported JavaScript operation %q", envelope.Operation)
		}
		payload := []byte(`{"protocolVersion":3,"operation":"audit","result":{"advisories":[],"unsupported":[]}}` + "\n")
		return runner.Result{ExitStatus: 0}, runner.Output{Stdout: payload}, nil
	}
	executable := filepath.Base(command.Argv[0])
	switch executable {
	case "go":
		var output strings.Builder
		encoder := json.NewEncoder(&output)
		for _, module := range boundary.goModules {
			if err := encoder.Encode(module); err != nil {
				return runner.Result{}, runner.Output{}, err
			}
		}
		return runner.Result{ExitStatus: 0}, runner.Output{Stdout: []byte(output.String())}, nil
	case "osv-scanner":
		return runner.Result{ExitStatus: 0}, runner.Output{Stdout: []byte(`{"results":[]}` + "\n")}, nil
	default:
		return runner.Result{}, runner.Output{}, fmt.Errorf("unsupported structured tool %q", executable)
	}
}

func argumentAfter(arguments []string, wanted string) (string, bool) {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == wanted {
			return arguments[index+1], true
		}
	}
	return "", false
}

func countFindings(findings []policy.Finding, check string) int {
	count := 0
	for _, finding := range findings {
		if finding.Check == check {
			count++
		}
	}
	return count
}

func gitSupply(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func findingSubject(findings []policy.Finding, subject string) bool {
	for _, finding := range findings {
		if finding.Subject == subject {
			return true
		}
	}
	return false
}
