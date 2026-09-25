package supplychain

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func newCargoGitEvidenceFixture(t *testing.T, private bool) gitEvidenceFixture {
	t.Helper()
	fixture := newGitEvidenceFixture(t, private)
	if err := os.Remove(filepath.Join(fixture.repo.Root, "uv.lock")); err != nil {
		t.Fatal(err)
	}
	subject := &fixture.statement.Subjects[0]
	subject.Name, subject.Subdirectory = "crate-name", ""
	source := subject.Repository + "@" + subject.Commit
	url := subject.Repository
	if private {
		url = strings.Replace(url, "ssh://", "ssh://git@", 1)
	}
	lock := fmt.Sprintf("version=4\n[[package]]\nname='app'\nversion='0.1.0'\n[[package]]\nname='crate-name'\nversion='1.0.0'\nsource='%s?rev=%s#%s'\n[[package]]\nname='crate-name'\nversion='1.0.0'\nsource='registry+https://github.com/rust-lang/crates.io-index'\nchecksum='%s'\n", url, subject.Commit, subject.Commit, strings.Repeat("a", 64))
	writeSupplyFile(t, fixture.repo.Root, "Cargo.lock", lock)
	packages, err := osvCargoInventory([]byte(lock), "Cargo.lock")
	if err != nil {
		t.Fatal(err)
	}
	fixture.packages = packages
	fixture.repo.Config.SupplyChain.GitEvidence.Attestations[0].Scope = "Cargo.lock"
	fixture.statement.Scope, fixture.statement.LockSHA256 = "Cargo.lock", fixtureSHA256([]byte(lock))
	inventory := fmt.Sprintf(`[{"ecosystem":"crates.io","name":"crate-name","version":"1.0.0","source":"registry:https://crates.io"},{"ecosystem":"git","name":"crate-name","version":"1.0.0","source":%q}]`, source)
	fixture.statement.InventorySHA256 = fixtureSHA256([]byte(inventory))
	fixture.statement.Scan.Licenses = []gitEvidenceLicense{
		{Ecosystem: "crates.io", Name: "crate-name", Version: "1.0.0", Source: "registry:https://crates.io", Expression: "MIT"},
		{Ecosystem: "git", Name: "crate-name", Version: "1.0.0", Source: source, Expression: "MIT"},
	}
	refreshGitEvidenceFixture(&fixture)
	return fixture
}

func refreshGitEvidenceFixture(fixture *gitEvidenceFixture) {
	fixture.now = time.Now().UTC()
	issued := fixture.now.Add(-time.Minute)
	fixture.statement.IssuedAt, fixture.statement.ExpiresAt = issued, issued.Add(time.Hour)
	fixture.statement.Scan.CompletedAt, fixture.statement.Scan.AdvisoryUpdated = issued, issued.Add(-time.Hour)
	for index := range fixture.statement.Subjects {
		fixture.statement.Subjects[index].Observation.Timestamp = issued.AddDate(0, 0, -90)
	}
}

func TestCargoGitEvidenceAuthenticatesSourcesWithoutRegistrySubstitution(t *testing.T) {
	t.Parallel()
	for _, private := range []bool{false, true} {
		fixture := newCargoGitEvidenceFixture(t, private)
		fixture.write(t)
		findings, receipt := verifyGitLockEvidence(fixture.repo, "Cargo.lock", fixture.packages, fixture.now)
		if len(findings) != 0 || receipt == nil || receipt.Scope != "Cargo.lock" || receipt.InventorySHA256 != fixture.statement.InventorySHA256 {
			t.Fatalf("Cargo signed evidence = %+v, %+v", findings, receipt)
		}
		if receipt.LockSHA256 != fixture.statement.LockSHA256 || !receipt.RetrievedAt.Equal(fixture.now) {
			t.Fatalf("Cargo receipt lost its authenticated identity: %+v", receipt)
		}
		state, err := ReadGitEvidenceState(fixture.repo, fixture.now)
		if err != nil || state.IdentitySHA256 == "" || len(state.Receipts) != 1 || state.Receipts[0] != *receipt {
			t.Fatalf("Cargo gate state omitted its signed inventory: %+v, %v", state, err)
		}
	}
}

func TestCargoGitEvidenceRejectsIncompleteAndForeignSignedInventories(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, category string
		change         func(*gitEvidenceStatement)
	}{
		{"Python scope", "stale", func(statement *gitEvidenceStatement) { statement.Scope = "uv.lock" }},
		{"invented crate directory", "stale", func(statement *gitEvidenceStatement) { statement.Subjects[0].Subdirectory = "src/crate-name" }},
		{"registry subject", "stale", func(statement *gitEvidenceStatement) { statement.Subjects[0].Ecosystem = "crates.io" }},
		{"wrong commit", "stale", func(statement *gitEvidenceStatement) { statement.Subjects[0].Commit = strings.Repeat("b", 40) }},
		{"missing registry license", "coverage", func(statement *gitEvidenceStatement) { statement.Scan.Licenses = statement.Scan.Licenses[1:] }},
		{"registry substitutes Git", "coverage", func(statement *gitEvidenceStatement) { statement.Scan.Licenses[1] = statement.Scan.Licenses[0] }},
		{"missing Git subject", "coverage", func(statement *gitEvidenceStatement) { statement.Subjects = []gitEvidenceSubject{} }},
		{"unsigned lock contents", "stale", func(statement *gitEvidenceStatement) { statement.LockSHA256 = strings.Repeat("b", 64) }},
		{"stale assessment", "expired", func(statement *gitEvidenceStatement) { statement.ExpiresAt = statement.IssuedAt }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCargoGitEvidenceFixture(t, true)
			test.change(&fixture.statement)
			fixture.write(t)
			findings, receipt := verifyGitLockEvidence(fixture.repo, "Cargo.lock", fixture.packages, fixture.now)
			if receipt != nil || len(findings) != 1 || findings[0].Check != "supplyChain.gitEvidence" || findings[0].Subject != test.category {
				t.Fatalf("invalid Cargo assessment = %+v, %+v", findings, receipt)
			}
		})
	}
}

func TestCargoGitEvidenceEnforcesSignedFindingsAndIndependentRegistryAge(t *testing.T) {
	fixture := newCargoGitEvidenceFixture(t, true)
	item := fixture.statement.Scan.Licenses[1]
	exploited := false
	fixture.statement.Scan.Vulnerabilities = []gitEvidenceVulnerability{{Ecosystem: item.Ecosystem, Name: item.Name, Version: item.Version, Source: item.Source, ID: "CVE-2026-12345", Severity: "critical", KnownExploited: &exploited}}
	fixture.statement.Scan.Licenses[1].Expression = "AGPL-3.0-only"
	fixture.statement.Subjects[0].Observation.Timestamp = fixture.now.Add(-time.Hour)
	fixture.write(t)
	transport := &cargoAgeTransport{released: map[string]time.Time{"1.0.0": fixture.now.Add(-time.Hour)}}
	installCargoAgeTransport(t, transport)
	findings := Online(t.Context(), fixture.repo, []string{"Cargo.lock"}, &cargoSupplyRunner{})
	checks := supplyChecks(findings)
	if len(findings) != 4 || len(transport.requests) != 1 || !checks["supplyChain.gitVulnerability"] || !checks["supplyChain.dependencyLicense"] || !checks["supplyChain.releaseAge"] {
		t.Fatalf("Cargo signed policy or registry age omitted: %+v, requests = %+v", findings, transport.requests)
	}
	for _, ecosystem := range []string{"git", "crates.io"} {
		if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
			return finding.ReleaseAge != nil && finding.ReleaseAge.Ecosystem == ecosystem && finding.Path == "Cargo.lock"
		}) {
			t.Fatalf("%s release-age identity omitted: %+v", ecosystem, findings)
		}
	}
}

func TestCargoGitEvidenceReceiptInvalidatesChangedLockAndProof(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"lock bytes", "registry checksum", "proof bytes", "expiry", "trust"} {
		t.Run(change, func(t *testing.T) {
			fixture := newCargoGitEvidenceFixture(t, true)
			fixture.write(t)
			before, err := ReadGitEvidenceState(fixture.repo, fixture.now)
			if err != nil || len(before.Receipts) != 1 {
				t.Fatalf("initial state = %+v, %v", before, err)
			}
			unchanged, err := ReadGitEvidenceState(fixture.repo, fixture.now.Add(time.Minute))
			if err != nil || len(unchanged.Receipts) != 1 || unchanged.IdentitySHA256 != before.IdentitySHA256 || !unchanged.Receipts[0].RetrievedAt.Equal(fixture.now.Add(time.Minute)) {
				t.Fatalf("unchanged Cargo proof could not be reused: %+v, %v", unchanged, err)
			}
			mutateCargoEvidenceState(t, &fixture, change)
			after, err := ReadGitEvidenceState(fixture.repo, fixture.now)
			if err != nil || after.IdentitySHA256 == before.IdentitySHA256 {
				t.Fatalf("%s retained Cargo proof identity: %+v, %v", change, after, err)
			}
			if change != "proof bytes" && len(after.Receipts) != 0 {
				t.Fatalf("%s retained an invalid receipt: %+v", change, after)
			}
		})
	}
}

func mutateCargoEvidenceState(t *testing.T, fixture *gitEvidenceFixture, change string) {
	t.Helper()
	switch change {
	case "expiry":
		fixture.now = fixture.now.Add(2 * time.Hour)
	case "trust":
		fixture.repo.Config.SupplyChain.GitEvidence.Providers[0].PolicySHA256 = strings.Repeat("b", 64)
	default:
		path := "Cargo.lock"
		if change == "proof bytes" {
			path = "evidence/git.dsse.json"
		}
		data, err := os.ReadFile(filepath.Join(fixture.repo.Root, path))
		if err != nil {
			t.Fatal(err)
		}
		if change == "registry checksum" {
			data = []byte(strings.ReplaceAll(string(data), strings.Repeat("a", 64), strings.Repeat("b", 64)))
		} else {
			data = append(data, '\n')
		}
		writeSupplyFile(t, fixture.repo.Root, path, string(data))
	}
}

func TestCargoGitEvidenceRequiresEachPackageFromTheSameRepository(t *testing.T) {
	t.Parallel()
	fixture := newCargoGitEvidenceFixture(t, true)
	data, err := os.ReadFile(filepath.Join(fixture.repo.Root, "Cargo.lock"))
	if err != nil {
		t.Fatal(err)
	}
	subject := fixture.statement.Subjects[0]
	lock := string(data) + fmt.Sprintf("[[package]]\nname='sibling-crate'\nversion='2.0.0'\nsource='%s?rev=%s#%s'\n", subject.Repository, subject.Commit, subject.Commit)
	writeSupplyFile(t, fixture.repo.Root, "Cargo.lock", lock)
	fixture.packages, err = osvCargoInventory([]byte(lock), "Cargo.lock")
	if err != nil {
		t.Fatal(err)
	}
	fixture.statement.LockSHA256 = fixtureSHA256([]byte(lock))
	source := subject.Repository + "@" + subject.Commit
	inventory := fmt.Sprintf(`[{"ecosystem":"crates.io","name":"crate-name","version":"1.0.0","source":"registry:https://crates.io"},{"ecosystem":"git","name":"crate-name","version":"1.0.0","source":%q},{"ecosystem":"git","name":"sibling-crate","version":"2.0.0","source":%q}]`, source, source)
	fixture.statement.InventorySHA256 = fixtureSHA256([]byte(inventory))
	fixture.write(t)
	findings, receipt := verifyGitLockEvidence(fixture.repo, "Cargo.lock", fixture.packages, fixture.now)
	if receipt != nil || len(findings) != 1 || findings[0].Subject != "coverage" {
		t.Fatalf("one Git subject covered two Cargo packages: %+v, %+v", findings, receipt)
	}
	subject.Name, subject.Version = "sibling-crate", "2.0.0"
	fixture.statement.Subjects = append(fixture.statement.Subjects, subject)
	fixture.statement.Scan.Licenses = append(fixture.statement.Scan.Licenses, gitEvidenceLicense{Ecosystem: "git", Name: "sibling-crate", Version: "2.0.0", Source: source, Expression: "MIT"})
	fixture.write(t)
	findings, receipt = verifyGitLockEvidence(fixture.repo, "Cargo.lock", fixture.packages, fixture.now)
	if len(findings) != 0 || receipt == nil {
		t.Fatalf("complete same-repository Cargo assessment failed: %+v, %+v", findings, receipt)
	}
}
