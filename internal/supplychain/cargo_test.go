package supplychain

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestCargoStaticPolicyRejectsCompatibleRangesAcrossDependencyKinds(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\nnormal='1.0.0'\n[dev-dependencies]\ntesting='^1.0.0'\n[build-dependencies]\nbuild='~1.0.0'\n[target.'cfg(windows)'.dependencies]\nplatform='*'\n[workspace.dependencies]\ninherited='>=1.0.0'\n")
	findings := Static(t.Context(), repo, []string{"Cargo.toml"})
	if countFindings(findings, "supplyChain.exactVersion") != 5 {
		t.Fatalf("Cargo version ranges escaped static policy: %+v", findings)
	}
	for _, dependency := range []string{"normal", "testing", "build", "platform", "inherited"} {
		if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
			return finding.Path == "Cargo.toml" && finding.Fields["package"] == dependency && finding.Check == "supplyChain.exactVersion"
		}) {
			t.Errorf("dependency %s was not attributed to its manifest", dependency)
		}
	}
}

func TestCargoStaticPolicyAcceptsExactRenamedAndWorkspaceDeclarations(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['app']\n[workspace.dependencies]\njson={package='serde_json', version='=1.0.140', features=['raw_value']}\n")
	writeSupplyFile(t, repo.Root, "app/Cargo.toml", "[package]\nname='app'\n[dependencies]\njson={workspace=true, optional=true, features=['preserve_order']}\n[build-dependencies]\ncc='= 1.2.0'\n[dev-dependencies]\npreview='=1.0.0-alpha.3'\n")
	findings := DependencyReviewStatic(t.Context(), repo, []string{"Cargo.toml", "app/Cargo.toml"})
	if len(findings) != 0 {
		t.Fatalf("exact Cargo source declarations rejected: %+v", findings)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, ".tools")); !os.IsNotExist(err) {
		t.Fatalf("static dependency review ran tooling or installed dependencies: %v", err)
	}
}

func TestCargoStaticPolicyRetainsContainedPathDependencies(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\nlocal={path='crates/local'}\n")
	writeSupplyFile(t, repo.Root, "crates/local/Cargo.toml", "[package]\nname='local'\n")
	if findings := checkCargoManifest(repo, "Cargo.toml"); len(findings) != 0 {
		t.Fatalf("contained path dependency rejected: %+v", findings)
	}
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\nlocal={path='../outside'}\n")
	if findings := checkCargoManifest(repo, "Cargo.toml"); !supplyChecks(findings)["supplyChain.cargoSource"] {
		t.Fatalf("escaping path dependency accepted: %+v", findings)
	}
}

func TestCargoStaticPolicyRejectsLinkedPathDependencies(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	outside := t.TempDir()
	writeSupplyFile(t, outside, "Cargo.toml", "[package]\nname='outside'\n")
	if err := os.Symlink(outside, filepath.Join(repo.Root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\nlocal={path='linked'}\n")
	if findings := checkCargoManifest(repo, "Cargo.toml"); !supplyChecks(findings)["supplyChain.cargoSource"] {
		t.Fatalf("linked path dependency accepted: %+v", findings)
	}
}

func TestCargoStaticPolicyRejectsIgnoredAndAmbiguousSourceSelectors(t *testing.T) {
	t.Parallel()
	for name, declaration := range map[string]string{
		"short exact": "'=1.0'", "metadata": "'=1.0.0+ignored'", "invalid prerelease": "'=1.0.0-01'",
		"Git branch": "{git='https://example.test/repo', branch='main'}", "Git short revision": "{git='https://example.test/repo', rev='abc123'}",
		"dual source": "{git='https://example.test/repo', path='local'}", "alternate registry": "{version='=1.0.0', registry='private'}",
		"inheritance override": "{workspace=true, version='=1.0.0'}", "unknown source": "{version='=1.0.0', registry-index='https://example.test/index'}",
		"revision without Git": "{version='=1.0.0', rev='fixed'}",
	} {
		t.Run(name, func(t *testing.T) {
			repo := supplyRepository(t)
			writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\ncrate="+declaration+"\n")
			if findings := checkCargoManifest(repo, "Cargo.toml"); len(findings) == 0 {
				t.Fatal("unproven dependency declaration passed")
			}
		})
	}
}

func TestCargoGitPinsDoNotUnconditionallyRequireSignedAssessments(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\ncrate={git='https://github.com/example/repo', rev='"+strings.Repeat("a", 40)+"'}\n")
	if findings := checkCargoManifest(repo, "Cargo.toml"); len(findings) != 0 {
		t.Fatalf("immutable source pin required a separate attestation service: %+v", findings)
	}
}

func TestCargoNativePinsDoNotClaimUnimplementedSupplyCoverage(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Scope.Generated = []string{"Cargo.toml"}
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\ncrate='1.0.0'\n")
	if findings := Static(t.Context(), repo, []string{"Cargo.toml"}); !supplyChecks(findings)["supplyChain.exactVersion"] {
		t.Fatalf("generated classification hid an unpinned dependency: %+v", findings)
	}
	findings := CoverageFindings(repo, []string{"Cargo.toml"})
	for _, missing := range []string{"lock-sync", "release-age", "security"} {
		if !slices.ContainsFunc(findings, func(finding policy.Finding) bool { return finding.Subject == "repository:"+missing }) {
			t.Errorf("unimplemented %s received native coverage: %+v", missing, findings)
		}
	}
	if slices.ContainsFunc(findings, func(finding policy.Finding) bool { return finding.Subject == "repository:dependency-policy" }) {
		t.Fatal("implemented Cargo pin policy still required a custom provider")
	}
}
