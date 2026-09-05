package supplychain

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestSignedGitEvidenceAcceptsPublicAndPrivateCommits(t *testing.T) {
	t.Parallel()
	for _, private := range []bool{false, true} {
		fixture := newGitEvidenceFixture(t, private)
		fixture.write(t)
		findings, receipt := verifyGitLockEvidence(fixture.repo, "uv.lock", fixture.packages, fixture.now)
		if len(findings) != 0 || receipt == nil || receipt.LockSHA256 != fixture.statement.LockSHA256 || receipt.PolicySHA256 != fixture.statement.PolicySHA256 {
			t.Fatalf("verified Git assessment = %+v, %+v", findings, receipt)
		}
		data, err := os.ReadFile(filepath.Join(fixture.repo.Root, receipt.Path))
		if err != nil || receipt.SHA256 != fixtureSHA256(data) {
			t.Fatalf("receipt did not bind signed bytes: %v", err)
		}
	}
}

func TestSignedGitEvidenceRejectsIncompleteStaleAndMismatchedAttestations(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name     string
		category string
		change   func(*gitEvidenceStatement)
	}{
		{"scope", "stale", func(s *gitEvidenceStatement) { s.Scope = "other/uv.lock" }},
		{"lock", "stale", func(s *gitEvidenceStatement) { s.LockSHA256 = strings.Repeat("0", 64) }},
		{"inventory", "stale", func(s *gitEvidenceStatement) { s.InventorySHA256 = strings.Repeat("0", 64) }},
		{"repository", "stale", func(s *gitEvidenceStatement) { s.Subjects[0].Repository = "git+https://public.example.test/other.git" }},
		{"commit", "stale", func(s *gitEvidenceStatement) { s.Subjects[0].Commit = strings.Repeat("a", 40) }},
		{"subdirectory", "stale", func(s *gitEvidenceStatement) { s.Subjects[0].Subdirectory = "other" }},
		{"source identity", "coverage", func(s *gitEvidenceStatement) { s.Subjects[0].TreeSHA256 = "" }},
		{"no age", "age-coverage", func(s *gitEvidenceStatement) { s.Subjects[0].Observation.Timestamp = time.Time{} }},
		{"Git author time", "age-coverage", func(s *gitEvidenceStatement) { s.Subjects[0].Observation.Kind = "git-author" }},
		{"foreign observation", "age-coverage", func(s *gitEvidenceStatement) { s.Subjects[0].Observation.Record = "https://untrusted.example.test/old" }},
		{"partial", "coverage", func(s *gitEvidenceStatement) { s.Scan.Coverage = "incomplete" }},
		{"authentication", "authentication", func(s *gitEvidenceStatement) { s.Scan.Coverage = "authentication-unavailable" }},
		{"unsupported", "unsupported-scanner", func(s *gitEvidenceStatement) { s.Scan.Coverage = "unsupported" }},
		{"scanner", "unsupported-scanner", func(s *gitEvidenceStatement) { s.Scan.Scanner.Version = "9.9.9" }},
		{"policy", "untrusted", func(s *gitEvidenceStatement) { s.PolicySHA256 = strings.Repeat("b", 64) }},
		{"no scan result", "invalid", func(s *gitEvidenceStatement) { s.Scan.Vulnerabilities = nil }},
		{"missing dependency", "coverage", func(s *gitEvidenceStatement) { s.Scan.Licenses = s.Scan.Licenses[:1] }},
		{"expired", "expired", func(s *gitEvidenceStatement) { s.ExpiresAt = s.IssuedAt }},
		{"future", "stale", func(s *gitEvidenceStatement) { s.IssuedAt = s.IssuedAt.Add(2 * time.Hour) }},
		{"stale advisories", "stale", func(s *gitEvidenceStatement) { s.Scan.AdvisoryUpdated = s.IssuedAt.Add(-48 * time.Hour) }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newGitEvidenceFixture(t, true)
			testCase.change(&fixture.statement)
			fixture.write(t)
			findings, receipt := verifyGitLockEvidence(fixture.repo, "uv.lock", fixture.packages, fixture.now)
			if receipt != nil || len(findings) != 1 || findings[0].Subject != testCase.category {
				t.Fatalf("evidence rejection = %+v, %+v", findings, receipt)
			}
		})
	}
}

func TestSignedGitEvidenceEnforcesVulnerabilitiesAgeAndLicensePolicy(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		check  string
		change func(*gitEvidenceStatement)
	}{
		{"young", "supplyChain.releaseAge", func(s *gitEvidenceStatement) { s.Subjects[0].Observation.Timestamp = s.IssuedAt.Add(-time.Hour) }},
		{"license", "supplyChain.dependencyLicense", func(s *gitEvidenceStatement) { s.Scan.Licenses[1].Expression = "AGPL-3.0-only" }},
		{"vulnerable", "supplyChain.gitVulnerability", func(s *gitEvidenceStatement) {
			exploited := true
			item := s.Scan.Licenses[0]
			s.Scan.Vulnerabilities = []gitEvidenceVulnerability{{Ecosystem: item.Ecosystem, Name: item.Name, Version: item.Version, Source: item.Source, ID: "CVE-2026-12345", Severity: "critical", KnownExploited: &exploited}}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newGitEvidenceFixture(t, true)
			testCase.change(&fixture.statement)
			fixture.write(t)
			findings, receipt := verifyGitLockEvidence(fixture.repo, "uv.lock", fixture.packages, fixture.now)
			if receipt == nil || len(findings) != 1 || findings[0].Check != testCase.check {
				t.Fatalf("admission result = %+v, %+v", findings, receipt)
			}
			if testCase.name == "vulnerable" && (!findings[0].Vulnerability.KnownExploited || !strings.Contains(findings[0].Vulnerability.AffectedVersion, fixture.statement.Subjects[0].Commit)) {
				t.Fatal("Git vulnerability lost commit identity or known exploitation")
			}
		})
	}
}

func TestSignedGitEvidenceRejectsTamperingAndUnsafeArtifacts(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"tampered", "unknown key", "missing", "symlink", "oversized", "duplicate field", "wrong field case", "changed lock"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newGitEvidenceFixture(t, true)
			fixture.write(t)
			path := filepath.Join(fixture.repo.Root, "evidence/git.dsse.json")
			mutateGitEvidenceArtifact(t, &fixture, path, operation)
			findings, receipt := verifyGitLockEvidence(fixture.repo, "uv.lock", fixture.packages, fixture.now)
			if receipt != nil || len(findings) != 1 || strings.Contains(findings[0].Message, "SECRET-CREDENTIAL") {
				t.Fatalf("unsafe artifact result = %+v, %+v", findings, receipt)
			}
		})
	}
}

func TestSignedGitEvidenceRequiresTheCompleteVersionedPayload(t *testing.T) {
	t.Parallel()
	fixture := newGitEvidenceFixture(t, true)
	for _, omission := range []string{"scope", "expiresAt", "subjects", "scan"} {
		payload, err := json.Marshal(fixture.statement)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, omission)
		payload, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		data := fixture.sign(t, payload)
		_, err = verifySignedGitEvidence(data, fixture.repo.Config.SupplyChain.GitEvidence.Providers[0])
		if err == nil {
			t.Fatalf("signed payload omitted %s", omission)
		}
	}
}

func mutateGitEvidenceArtifact(t *testing.T, fixture *gitEvidenceFixture, path, operation string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	switch operation {
	case "tampered":
		var envelope gitEvidenceEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		envelope.Payload = base64.StdEncoding.EncodeToString([]byte("SECRET-CREDENTIAL"))
		data, err = json.Marshal(envelope)
	case "unknown key":
		public, _, keyErr := ed25519.GenerateKey(rand.Reader)
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		fixture.repo.Config.SupplyChain.GitEvidence.Providers[0].PublicKey = base64.StdEncoding.EncodeToString(public)
	case "missing":
		err = os.Remove(path)
	case "symlink":
		err = os.Remove(path)
		if err == nil {
			err = os.Symlink(filepath.Join(fixture.repo.Root, "uv.lock"), path)
		}
	case "oversized":
		data = []byte(strings.Repeat(" ", maximumGitEvidenceBytes+1))
	case "duplicate field":
		data = []byte(strings.Replace(string(data), `"payloadType":`, `"payloadType":"invalid","payloadType":`, 1))
	case "wrong field case":
		data = []byte(strings.Replace(string(data), `"payloadType":`, `"PayloadType":`, 1))
	case "changed lock":
		writeSupplyFile(t, fixture.repo.Root, "uv.lock", "changed lock\n")
	}
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains([]string{"tampered", "oversized", "duplicate field", "wrong field case"}, operation) {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

type gitEvidenceFixture struct {
	repo      repository.Repository
	packages  []resolvedPackage
	statement gitEvidenceStatement
	key       ed25519.PrivateKey
	now       time.Time
}

func newGitEvidenceFixture(t *testing.T, private bool) gitEvidenceFixture {
	t.Helper()
	fixture := gitEvidenceFixture{repo: supplyRepository(t), now: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fixture.key = key
	provider := policy.GitEvidenceProvider{Name: "ci", Kind: "ed25519-ci/v1", Issuer: "https://ci.example.test/attestor", PublicKey: base64.StdEncoding.EncodeToString(public), PolicySHA256: strings.Repeat("1", 64), Scanners: []policy.GitEvidenceScanner{{Name: "approved-scanner", Version: "1.0.0", SHA256: strings.Repeat("2", 64)}}}
	fixture.repo.Config.SupplyChain.GitEvidence = policy.GitEvidence{Providers: []policy.GitEvidenceProvider{provider}, Attestations: []policy.GitEvidenceAttestation{{Scope: "uv.lock", Provider: "ci", Path: "evidence/git.dsse.json"}}}
	fixture.repo.Config.SupplyChain.AllowedLicenses = []string{"MIT"}
	fixture.repo.Config.SupplyChain.MinimumReleaseAgeDays = 30
	repoURL, identity := "https://public.example.test/kit.git", "git+https://public.example.test/kit.git"
	if private {
		repoURL, identity = "ssh://git@private.example.test/kit.git", "git+ssh://private.example.test/kit.git"
	}
	commit := "0123456789abcdef0123456789abcdef01234567"
	lock := "[[package]]\nname = \"private-kit\"\nversion = \"1.0.0\"\nsource = { git = \"" + repoURL + "?subdirectory=src%2Fkit&rev=" + commit + "#" + commit + "\" }\n[[package]]\nname = \"support-lib\"\nversion = \"2.0.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n"
	writeSupplyFile(t, fixture.repo.Root, "uv.lock", lock)
	fixture.packages, err = parseUVLock([]byte(lock), "uv.lock")
	if err != nil {
		t.Fatal(err)
	}
	source := identity + "@" + commit + "#subdirectory=src/kit"
	inventory := fmt.Sprintf(`[{"ecosystem":"git","name":"private-kit","version":"1.0.0","source":%q},{"ecosystem":"pypi","name":"support-lib","version":"2.0.0","source":"registry:https://pypi.org/simple"}]`, source)
	issued := fixture.now.Add(-10 * time.Minute)
	fixture.statement = gitEvidenceStatement{
		Protocol: "git-evidence/v1", Issuer: provider.Issuer, PolicySHA256: provider.PolicySHA256,
		Scope: "uv.lock", LockSHA256: fixtureSHA256([]byte(lock)), InventorySHA256: fixtureSHA256([]byte(inventory)), IssuedAt: issued, ExpiresAt: issued.Add(time.Hour),
		Subjects: []gitEvidenceSubject{{Ecosystem: "git", Name: "private-kit", Version: "1.0.0", Repository: identity, Commit: commit, Subdirectory: "src/kit", TreeSHA256: fixtureSHA256([]byte("private source bytes")), Observation: gitEvidenceObservation{Kind: "first-observed", Record: provider.Issuer + "/records/immutable-id", Timestamp: issued.AddDate(0, 0, -40)}}},
		Scan:     gitEvidenceScan{Coverage: "complete", Target: "git-contents-and-resolved-lock/v1", Scanner: provider.Scanners[0], CompletedAt: issued.Add(-time.Minute), AdvisoryVersion: "snapshot-20260904", AdvisoryUpdated: issued.Add(-time.Hour), Vulnerabilities: []gitEvidenceVulnerability{}, Licenses: []gitEvidenceLicense{{Ecosystem: "git", Name: "private-kit", Version: "1.0.0", Source: source, Expression: "MIT"}, {Ecosystem: "pypi", Name: "support-lib", Version: "2.0.0", Source: "registry:https://pypi.org/simple", Expression: "MIT"}}},
	}
	return fixture
}

func (fixture gitEvidenceFixture) write(t *testing.T) {
	t.Helper()
	payload, err := json.Marshal(fixture.statement)
	if err != nil {
		t.Fatal(err)
	}
	writeSupplyFile(t, fixture.repo.Root, "evidence/git.dsse.json", string(fixture.sign(t, payload)))
}

func (fixture gitEvidenceFixture) sign(t *testing.T, payload []byte) []byte {
	t.Helper()
	mediaType := "application/vnd.code-polishy.git-evidence.v1+json"
	message := []byte(fmt.Sprintf("DSSEv1 %d %s %d %s", len(mediaType), mediaType, len(payload), payload))
	envelope := map[string]any{"payloadType": mediaType, "payload": base64.StdEncoding.EncodeToString(payload), "signatures": []map[string]string{{"keyid": "ci", "sig": base64.StdEncoding.EncodeToString(ed25519.Sign(fixture.key, message))}}}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
