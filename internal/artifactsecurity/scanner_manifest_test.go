package artifactsecurity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const fixedScannerAssessment = `{"@context":"https://openvex.dev/ns/v0.2.0","@id":"https://example.test/scanner-fixture","author":"Test fixture","timestamp":"2026-01-01T00:00:00Z","version":1,"statements":[{"vulnerability":{"name":"CVE-2026-1000"},"products":[{"@id":"pkg:golang/example.org/scannerdep@v1.0.0"}],"status":"not_affected","justification":"vulnerable_code_not_in_execute_path","impact_statement":"Synthetic fixed advisory fixture exercises authenticated assessment adoption."}]}`

func scannerFixture(t *testing.T, repo string) string {
	t.Helper()
	root := t.TempDir()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "artifact-security"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifact-security/scanner-contract.txt"), []byte("code-polishy-trivy/v1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "artifact-security/publisher.pem"), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	dockerfile, err := os.ReadFile("../../artifact-security/scanner.Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifact-security/scanner.Dockerfile"), dockerfile, 0600); err != nil {
		t.Fatal(err)
	}
	var manifest release.ScannerManifest
	source, err := os.ReadFile("../../artifact-security/scanner-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(source, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ReassessBy = time.Now().Add(time.Hour)

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "signer.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret}), 0600); err != nil {
		t.Fatal(err)
	}

	signed, err := release.SignScanner(data, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret}))
	if err != nil {
		t.Fatal(err)
	}
	host, err := release.Host()
	if err != nil {
		t.Fatal(err)
	}
	installed := release.Manifest{
		ManifestVersion: release.ManifestVersion, CodePolishyVersion: "1.0.0",
		SourceRevision: strings.Repeat("a", 40), Host: host, Features: []string{"javascript-bundle"},
		Tools: release.Tools{
			Go: "1.0.0", Govulncheck: "1.0.0", Node: "1.0.0", OSVScanner: "1.0.0",
			PNPM: "1.0.0", Packaging: "1.0.0", Python: "1.0.0", Ruff: "1.0.0",
			Shellcheck: "1.0.0", Staticcheck: "1.0.0", Deadcode: "1.0.0",
			Ty: "1.0.0", Vulture: "1.0.0", Rust: "1.0.0", Nextest: "1.0.0",
		},
		CapabilityCatalogSHA256: strings.Repeat("a", 64),
		Entries:                 []release.Entry{{Path: release.BinaryPath, SHA256: strings.Repeat("b", 64)}, {Path: release.CapabilityCatalogPath, SHA256: strings.Repeat("a", 64)}},
	}
	installed.ReleaseDigest = installed.Identity()
	installed.EntryCount = len(installed.Entries)
	installed.ContentDigest = release.EntriesDigest(installed.Entries)
	encoded, err := json.Marshal(installed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, release.ManifestFilename), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	lock := release.Lock{LockVersion: release.LockVersion, CodePolishyVersion: installed.CodePolishyVersion, ReleaseDigest: installed.ReleaseDigest, Features: []string{"javascript-bundle"}, Scanner: &signed, Publication: &release.LockPublication{IndexURL: "https://example.com/index", IndexSHA256: strings.Repeat("a", 64)}}
	for _, host := range []string{"darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "windows-x64"} {
		lock.Publication.Archives = append(lock.Publication.Archives, release.LockedArchive{Host: host, URL: "https://example.com/" + host, SHA256: strings.Repeat("a", 64), Size: 1})
	}
	if err := release.WriteLock(repo, lock); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAuthenticatedAssessmentRevisionUnblocksTheSameScanner(t *testing.T) {
	for _, architecture := range []string{"amd64", "arm64"} {
		t.Run(architecture, func(t *testing.T) {
			root := t.TempDir()
			policyRoot := scannerFixture(t, root)
			if err := os.WriteFile(filepath.Join(root, "agent.tar"), []byte("fixed image"), 0600); err != nil {
				t.Fatal(err)
			}
			selected, _, err := release.ReadLock(root)
			if err != nil {
				t.Fatal(err)
			}
			var assessed release.ScannerManifest
			if err := json.Unmarshal(selected.Scanner.Payload, &assessed); err != nil {
				t.Fatal(err)
			}
			assessed.OpenVEX = json.RawMessage(fixedScannerAssessment)
			assessed.Version = "1.0.1"
			selected.Scanner = signAssessmentFixture(t, policyRoot, assessed)

			unassessed := assessed
			unassessed.Version = "1.0.0"
			var vex map[string]any
			if err := json.Unmarshal(assessed.OpenVEX, &vex); err != nil {
				t.Fatal(err)
			}
			vex["statements"] = []any{}
			unassessed.OpenVEX, err = json.Marshal(vex)
			if err != nil {
				t.Fatal(err)
			}
			old := selected
			old.Scanner = signAssessmentFixture(t, policyRoot, unassessed)
			if err := release.WriteLock(root, old); err != nil {
				t.Fatal(err)
			}
			repo := repository.Repository{Root: root, PolicyRoot: policyRoot, Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{OutputDirectory: ".code-polishy-reports/artifact-security", Targets: []policy.ArtifactTarget{{Name: "agent", Mode: "archive", Archive: "agent.tar"}}}}}}
			beforeBoundary := fixedAdvisoryBoundary(t, assessed.TrivyVersion, architecture)
			before := Run(t.Context(), repo, beforeBoundary)
			if len(before.Findings) == 0 || before.Findings[0].Check != "artifactSecurity.vulnerability" {
				t.Fatal("fixed advisory fixture did not block scanner")
			}
			if err := release.WriteScannerLock(root, policyRoot, old, selected.Scanner); err != nil {
				t.Fatal(err)
			}
			afterBoundary := fixedAdvisoryBoundary(t, assessed.TrivyVersion, architecture)
			after := Run(t.Context(), repo, afterBoundary)
			if len(after.Findings) != 0 {
				t.Fatalf("authenticated assessment failed to unblock: %+v", after.Findings)
			}
			current, _, err := release.ReadLock(root)
			if err != nil {
				t.Fatal(err)
			}
			if current.CodePolishyVersion != old.CodePolishyVersion || current.ReleaseDigest != old.ReleaseDigest || assessed.SourceImage != unassessed.SourceImage || assessed.TrivyVersion != unassessed.TrivyVersion {
				t.Fatal("assessment update changed release or scanner binary")
			}
			requireAssessmentReports(t, root, architecture)
		})
	}
}

func requireAssessmentReports(t *testing.T, root, architecture string) {
	t.Helper()

	reports, err := filepath.Glob(filepath.Join(root, ".code-polishy-reports/artifact-security/code-polishy-scanner/*/report.json"))
	if err != nil || len(reports) != 2 {
		t.Fatalf("reports %v %v", reports, err)
	}
	identities := map[string]bool{}
	for _, path := range reports {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var report artifactReport
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
		if report.Platform != "linux/"+architecture {
			t.Fatalf("wrong scanner architecture %q", report.Platform)
		}
		identities[report.ScannerPackageSHA256] = true
		if report.DatabaseSHA256 != bytesSHA256([]byte("database")) {
			t.Fatal("database fixture changed")
		}
	}
	if len(identities) != 2 {
		t.Fatal("assessment revision reused package evidence")
	}
}

func signAssessmentFixture(t *testing.T, policyRoot string, manifest release.ScannerManifest) *release.SignedScanner {
	t.Helper()
	private, err := os.ReadFile(filepath.Join(policyRoot, "signer.pem"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := release.SignScanner(data, private)
	if err != nil {
		t.Fatal(err)
	}
	return &signed
}

func fixedAdvisoryBoundary(t *testing.T, version, architecture string) *scannerBoundary {
	t.Helper()
	data, err := os.ReadFile("testdata/scanner-advisory.json")
	if err != nil {
		t.Fatal(err)
	}
	var findings []map[string]any
	if err := json.Unmarshal(data, &findings); err != nil {
		t.Fatal(err)
	}
	return &scannerBoundary{t: t, containers: map[string][]string{}, findings: findings, imageID: "sha256:" + bytesSHA256([]byte(archiveFixtureConfig)), version: version, architecture: architecture}
}

func TestProjectOpenVEXCannotAuthorizeScannerSuppression(t *testing.T) {
	root := t.TempDir()
	policyRoot := scannerFixture(t, root)
	selected, _, err := release.ReadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	var manifest release.ScannerManifest
	if err := json.Unmarshal(selected.Scanner.Payload, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.OpenVEX = json.RawMessage(`{"@context":"https://openvex.dev/ns/v0.2.0","@id":"https://example.test/empty","author":"Fixture","timestamp":"2026-01-01T00:00:00Z","version":1,"statements":[]}`)
	selected.Scanner = signAssessmentFixture(t, policyRoot, manifest)
	if err := release.WriteLock(root, selected); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "project-vex.json"), []byte(fixedScannerAssessment), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent.tar"), []byte("fixture image"), 0600); err != nil {
		t.Fatal(err)
	}
	repo := repository.Repository{Root: root, PolicyRoot: policyRoot, Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{OutputDirectory: ".code-polishy-reports/artifact-security", Targets: []policy.ArtifactTarget{{Name: "agent", Mode: "archive", Archive: "agent.tar", OpenVEX: "project-vex.json"}}}}}}
	result := Run(t.Context(), repo, fixedAdvisoryBoundary(t, manifest.TrivyVersion, "amd64"))
	for _, finding := range result.Findings {
		if finding.Check == "artifactSecurity.vulnerability" && finding.Path == policy.LockFilename && strings.Contains(finding.Subject, "CVE-2026-1000") {
			return
		}
	}
	t.Fatalf("project OpenVEX hid scanner finding: %+v", result.Findings)
}

func TestGatePlanMatchesScansWithoutScannerOrTargetSuppressions(t *testing.T) {
	root := t.TempDir()
	policyRoot := scannerFixture(t, root)
	selected, _, err := release.ReadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	var manifest release.ScannerManifest
	if err := json.Unmarshal(selected.Scanner.Payload, &manifest); err != nil {
		t.Fatal(err)
	}
	empty := []byte(`{"@context":"https://openvex.dev/ns/v0.2.0","@id":"https://example.test/empty","author":"Fixture","timestamp":"2026-01-01T00:00:00Z","version":1,"statements":[]}`)
	manifest.OpenVEX = empty
	selected.Scanner = signAssessmentFixture(t, policyRoot, manifest)
	if err := release.WriteLock(root, selected); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "empty.json"), empty, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent.tar"), []byte("fixture image"), 0600); err != nil {
		t.Fatal(err)
	}
	repo := repository.Repository{Root: root, PolicyRoot: policyRoot, Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{OutputDirectory: ".code-polishy-reports/artifact-security", Targets: []policy.ArtifactTarget{{Name: "agent", Mode: "archive", Archive: "agent.tar", OpenVEX: "empty.json"}}}}}}
	planned := PlannedCommands(repo)
	boundary := fixedAdvisoryBoundary(t, manifest.TrivyVersion, "amd64")
	boundary.findings = nil
	result := Run(t.Context(), repo, boundary)
	if len(result.Findings) != 0 {
		t.Fatalf("empty assessments failed scan: %+v", result.Findings)
	}
	if len(boundary.commands) != len(planned) {
		t.Fatalf("gate expects %d commands, runtime executed %d", len(planned), len(boundary.commands))
	}
	for index, actual := range boundary.commands {
		if !MatchesPlannedCommand(planned[index], actual) {
			t.Fatalf("gate command %d differs: planned %s, actual %s", index, planned[index].Name, actual.Name)
		}
	}
}

func TestBundledScannerBootstrapHasTheEnrolledPublisherAndCompatiblePolicy(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "artifact-security/scanner.json"))
	if err != nil {
		t.Fatal(err)
	}
	var signed release.SignedScanner
	if err := json.Unmarshal(data, &signed); err != nil {
		t.Fatal(err)
	}
	var payload release.ScannerManifest
	if err := json.Unmarshal(signed.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		time.Sleep(time.Until(payload.ReassessBy.Add(time.Second)))
		encoded, err := os.ReadFile(filepath.Join(root, "artifact-security/publisher.pem"))
		if err != nil {
			t.Fatal(err)
		}
		block, rest := pem.Decode(encoded)
		if block == nil || len(rest) != 0 {
			t.Fatal("invalid bundled publisher public key")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		public, ok := parsed.(ed25519.PublicKey)
		if err != nil || !ok || !ed25519.Verify(public, append([]byte("code-polishy-scanner-manifest/v1\x00"), signed.Payload...), signed.Signature) {
			t.Fatal("bootstrap has no enrolled publisher signature")
		}
		contract, err := os.ReadFile(filepath.Join(root, "artifact-security/scanner-contract.txt"))
		if err != nil || payload.Contract != strings.TrimSpace(string(contract)) {
			t.Fatalf("bootstrap contract does not match the release: %v", err)
		}
		var document openVEXDocument
		if err := json.Unmarshal(payload.OpenVEX, &document); err != nil {
			t.Fatal(err)
		}
		if _, err := parseOpenVEXBytes(payload.OpenVEX, document.Timestamp); err != nil {
			t.Fatal(err)
		}
		if _, err := release.VerifyScanner(root, &signed); err == nil {
			t.Fatal("expired bootstrap still authorizes scanner assessments")
		}
	})
}
