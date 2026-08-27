package artifactsecurity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCheckedInScannerPolicyPinsRuntimeInputs(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	configured, err := loadScannerPolicy(root)
	if err != nil {
		t.Fatal(err)
	}
	if configured.TrivyVersion != "0.72.0" || !strings.Contains(configured.SourceImage, "@sha256:") {
		t.Fatalf("scanner policy = %+v", configured)
	}
}

func TestParseTrivyReportNormalizesFindingsWithoutSecretContent(t *testing.T) {
	t.Parallel()
	imageID := "sha256:" + strings.Repeat("a", 64)
	payload := []byte(`{
  "SchemaVersion": 2,
  "Trivy": {"Version":"0.72.0"},
  "ArtifactType":"container_image",
  "Metadata":{"ImageID":"` + imageID + `","OS":{"Family":"alpine","Name":"3.18","EOSL":true}},
  "Results":[{
    "Target":"app",
    "Vulnerabilities":[{"VulnerabilityID":"CVE-2026-1000","PkgName":"example","PkgIdentifier":{"PURL":"pkg:apk/alpine/example@1.2.3"},"InstalledVersion":"1.2.3","FixedVersion":"1.2.4","Severity":"HIGH","Title":"unsafe path"}],
    "Secrets":[{"RuleID":"private-key","Category":"key","Severity":"CRITICAL","Title":"private key","StartLine":2,"EndLine":4,"Match":"SUPER-SECRET"}],
    "Misconfigurations":[{"ID":"DS001","Title":"root user","Severity":"HIGH","Status":"FAIL"}]
  }]
}`)
	findings, gotImageID, err := parseTrivyReport("agent", imageID, "0.72.0", payload)
	if err != nil {
		t.Fatal(err)
	}
	if gotImageID != imageID || len(findings) != 4 {
		t.Fatalf("image=%q findings=%+v", gotImageID, findings)
	}
	for _, finding := range findings {
		if strings.Contains(normalizedFindingKey(finding), "SUPER-SECRET") {
			t.Fatal("secret match leaked into normalized finding")
		}
	}
}

func TestParseTrivyReportRejectsNonBlockingSeverity(t *testing.T) {
	t.Parallel()
	imageID := "sha256:" + strings.Repeat("a", 64)
	payload := []byte(`{
  "SchemaVersion":1,"Trivy":{"Version":"0.72.0"},"ArtifactType":"container_image",
  "Metadata":{"ImageID":"` + imageID + `"},
  "Results":[{"Target":"app","Vulnerabilities":[{"VulnerabilityID":"CVE-2026-1000","PkgName":"example","InstalledVersion":"1.2.3","Severity":"MEDIUM"}]}]
}`)
	if _, _, err := parseTrivyReport("agent", imageID, "0.72.0", payload); err == nil {
		t.Fatal("non-blocking severity was accepted after scanner filtering")
	}
}

func TestOpenVEXReconciliationIsExactAndRejectsUnusedStatements(t *testing.T) {
	t.Parallel()
	finding := normalizedFinding{
		Artifact: "agent", Kind: "vulnerability", ID: "CVE-2026-1000", Severity: "HIGH",
		Target: "app", Package: "example", PackagePURL: "pkg:apk/alpine/example@1.2.3", AffectedVersion: "1.2.3",
	}
	assessment := openVEXAssessment{
		Advisory: finding.ID, Product: finding.PackagePURL,
		Justification: "vulnerable_code_not_in_execute_path", ImpactStatement: "The affected call path is not present in this artifact.",
	}
	key := openVEXKey(assessment.Advisory, assessment.Product)
	blocking, accepted, err := reconcileOpenVEX([]normalizedFinding{finding}, nil, map[string]openVEXAssessment{key: assessment})
	if err != nil || len(blocking) != 0 || len(accepted) != 1 {
		t.Fatalf("blocking=%+v accepted=%+v err=%v", blocking, accepted, err)
	}
	extra := assessment
	extra.Advisory = "CVE-2026-2000"
	if _, _, err := reconcileOpenVEX([]normalizedFinding{finding}, nil, map[string]openVEXAssessment{key: assessment, openVEXKey(extra.Advisory, extra.Product): extra}); err == nil {
		t.Fatal("unused OpenVEX statement was accepted")
	}
}

func TestParseOpenVEXRequiresBoundedExactAssessment(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "assessment.openvex.json")
	now := time.Now().UTC().Truncate(time.Second)
	document := `{
  "@context":"https://openvex.dev/ns/v0.2.0",
  "@id":"https://example.test/vex/1",
  "author":"Security team",
  "timestamp":"` + now.Format(time.RFC3339) + `",
  "version":1,
  "statements":[{
    "vulnerability":{"name":"CVE-2026-1000"},
    "products":[{"@id":"pkg:apk/alpine/example@1.2.3"}],
    "status":"not_affected",
    "justification":"vulnerable_code_not_in_execute_path",
    "impact_statement":"The affected call path is not present in this artifact."
  }]
}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	assessments, err := parseOpenVEX(path, now)
	if err != nil || len(assessments) != 1 {
		t.Fatalf("assessments=%+v err=%v", assessments, err)
	}
}

func TestScannerMountRejectsOptionInjectionAndExternalPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	state := scannerRuntime{root: root}
	insidePath := filepath.Join(root, "image.tar")
	if err := os.WriteFile(insidePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := state.mountArgument(scannerMount{source: insidePath + ",readonly", target: "/input/image.tar", readOnly: true}); err == nil {
		t.Fatal("Docker mount option injection was accepted")
	}
	if _, err := state.mountArgument(scannerMount{source: filepath.Join(t.TempDir(), "image.tar"), target: "/input/image.tar", readOnly: true}); err == nil {
		t.Fatal("external scanner mount was accepted")
	}
}

func TestWriteArtifactsPublishesHashedNormalizedEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := repository.Repository{Root: root, Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{OutputDirectory: ".code-polishy-reports/artifact-security"}}}}
	target := policy.ArtifactTarget{Name: "agent", Mode: "archive", Archive: "agent.tar"}
	now := time.Now().UTC()
	scanned := scanOutput{
		report: artifactReport{
			Version: 1, Target: "agent", Mode: "archive", ScannerVersion: "0.72.0",
			ArchiveSHA256: strings.Repeat("a", 64), DatabaseSHA256: strings.Repeat("b", 64), SBOMSHA256: strings.Repeat("c", 64),
			StartedAt: now.Add(-time.Second), CompletedAt: now,
		},
		sbom: []byte("{\"bomFormat\":\"CycloneDX\",\"specVersion\":\"1.6\"}\n"),
	}
	relative, err := writeArtifacts(repo, target, scanned)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "report.json", "report.md", "sbom.cdx.json"} {
		info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(relative), name))
		if statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing artifact evidence %s: %v", name, statErr)
		}
	}
}
