package supplychain

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCargoGovernedReplacementRetainsTransitiveRangesAndSourceCoverage(t *testing.T) {
	t.Parallel()
	repo := governedCargoPatchRepository(t)
	for _, selected := range [][]string{{"Cargo.toml", "vendor/replacement/Cargo.toml"}, {"vendor/replacement/Cargo.toml"}} {
		findings := Static(t.Context(), repo, selected)
		if len(findings) != 1 || findings[0].Check != "supplyChain.cargoPatchSourceCoverage" || findings[0].Severity != policy.FindingWarning {
			t.Fatalf("governed replacement findings = %+v", findings)
		}
	}
	if findings := DependencyOverrideGovernanceFindings(repo, []string{"Cargo.toml"}); len(findings) != 0 {
		t.Fatalf("used Cargo override was classified as unused: %+v", findings)
	}
}

func TestCargoReplacementGovernanceDoesNotHideChangedOrUnownedInputs(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(repository.Repository){
		"unowned": func(repo repository.Repository) {
			repo.Config.SupplyChain.DependencyOverridePolicies[0].Path = "other/Cargo.toml"
		},
		"changed owner": func(repo repository.Repository) {
			repo.Config.SupplyChain.DependencyOverridePolicies[0].ContentSHA256 = strings.Repeat("0", 64)
		},
		"wrong ecosystem": func(repo repository.Repository) {
			repo.Config.SupplyChain.DependencyOverridePolicies[0].Ecosystem = "npm"
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := governedCargoPatchRepository(t)
			change(repo)
			findings := Static(t.Context(), repo, []string{"Cargo.toml", "vendor/replacement/Cargo.toml"})
			if !supplyChecks(findings)["supplyChain.exactVersion"] || !supplyChecks(findings)["supplyChain.cargoSource"] {
				t.Fatalf("unreviewed replacement bypassed native policy: %+v", findings)
			}
		})
	}
}

func TestCargoReplacementStillPinsConsumerAndGitDependencies(t *testing.T) {
	t.Parallel()
	repo := governedCargoPatchRepository(t)
	data, err := repo.Read("Cargo.toml")
	if err != nil {
		t.Fatal(err)
	}
	writeSupplyFile(t, repo.Root, "Cargo.toml", strings.Replace(string(data), "'=1.0.0'", "'1.0'", 1))
	writeSupplyFile(t, repo.Root, "vendor/replacement/Cargo.toml", "[package]\nname='replacement'\n[dependencies]\ntransitive='1.2'\nmutable={git='https://github.com/example/crate',branch='main'}\n")
	findings := Static(t.Context(), repo, []string{"Cargo.toml", "vendor/replacement/Cargo.toml"})
	if !slices.ContainsFunc(findings, func(f policy.Finding) bool { return f.Path == "Cargo.toml" && f.Check == "supplyChain.exactVersion" }) {
		t.Fatalf("consumer range passed as a transitive dependency: %+v", findings)
	}
	if !slices.ContainsFunc(findings, func(f policy.Finding) bool {
		return f.Path == "vendor/replacement/Cargo.toml" && f.Check == "supplyChain.cargoSource"
	}) {
		t.Fatalf("moving Git source passed in replacement: %+v", findings)
	}
}

func TestCargoOverrideExpiryAndRemovalRemainVisible(t *testing.T) {
	t.Parallel()
	repo := governedCargoPatchRepository(t)
	repo.Config.SupplyChain.DependencyOverridePolicies[0].Expires = policy.Date{Time: time.Now().UTC().AddDate(0, 0, -1)}
	if findings := DependencyOverrideGovernanceFindings(repo, []string{"Cargo.toml"}); !supplyChecks(findings)["policy.dependencyOverridePolicyExpired"] {
		t.Fatalf("expired Cargo owner passed: %+v", findings)
	}
	repo.Config.SupplyChain.DependencyOverridePolicies[0].Expires = policy.Date{Time: time.Now().UTC().AddDate(0, 0, 1)}
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n")
	if findings := DependencyOverrideGovernanceFindings(repo, []string{"Cargo.toml"}); !supplyChecks(findings)["policy.dependencyOverridePolicyUnused"] {
		t.Fatalf("removed Cargo patch retained stale governance: %+v", findings)
	}
}

func governedCargoPatchRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\n[dependencies]\nreplacement='=1.0.0'\n[patch.crates-io]\nreplacement={path='vendor/replacement'}\n")
	writeSupplyFile(t, repo.Root, "vendor/replacement/Cargo.toml", "[package]\nname='replacement'\n[dependencies]\ntransitive='1.2'\n[dev-dependencies]\nnot-consumed='9'\n")
	manifest := repo.InspectCargoManifests([]string{"Cargo.toml"}).Manifests[0]
	digest, err := canonicalJSONSHA256(manifest.Patches["patch.crates-io"])
	if err != nil {
		t.Fatal(err)
	}
	repo.Config.SupplyChain.DependencyOverridePolicies = []policy.DependencyOverridePolicy{{
		ID: "replacement", Ecosystem: "cargo", Path: "Cargo.toml", Field: "patch.crates-io", ContentSHA256: digest,
		Reason: "Reviewed contained replacement", Owner: "fixture", Reviewed: policy.Date{Time: time.Now().UTC()}, Expires: policy.Date{Time: time.Now().UTC().AddDate(0, 0, 1)},
	}}
	return repo
}
