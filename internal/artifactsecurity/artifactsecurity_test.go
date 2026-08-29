package artifactsecurity

import (
	"context"
	"crypto/sha256"
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
	"github.com/riteofstring/code-polishy/internal/runner"
)

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
			Version: 1, Target: "agent", Mode: "archive", Reference: "registry.example/agent:9.9.9",
			ImageID: "sha256:" + strings.Repeat("d", 64), ScannerVersion: "0.72.0", SourceImage: "scanner@example",
			ArchiveSHA256: strings.Repeat("a", 64), DatabaseSHA256: strings.Repeat("b", 64), SBOMSHA256: strings.Repeat("c", 64),
			StartedAt: now.Add(-time.Second), CompletedAt: now,
		},
		sbom: []byte("{\"bomFormat\":\"CycloneDX\",\"specVersion\":\"1.6\"}\n"),
	}
	relative, err := writeArtifacts(repo, target, scanned)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, filepath.FromSlash(relative))
	requirePublishedEvidenceFiles(t, directory)
	requirePublishedManifestDigests(t, directory)
	report := readPublishedArtifactReport(t, directory)
	requirePublishedArtifactReport(t, report)
	requireMarkdownArtifactIdentity(t, directory, report.Reference, report.ImageID)
}

type publishedArtifactReport struct {
	Version        int       `json:"version"`
	Target         string    `json:"target"`
	Mode           string    `json:"mode"`
	Reference      string    `json:"reference"`
	ImageID        string    `json:"imageId"`
	ScannerVersion string    `json:"scannerVersion"`
	ArchiveSHA256  string    `json:"archiveSha256"`
	DatabaseSHA256 string    `json:"databaseSha256"`
	SBOMSHA256     string    `json:"sbomSha256"`
	StartedAt      time.Time `json:"startedAt"`
	CompletedAt    time.Time `json:"completedAt"`
}

func requirePublishedEvidenceFiles(t *testing.T, directory string) {
	t.Helper()
	for _, name := range []string{"manifest.json", "report.json", "report.md", "sbom.cdx.json"} {
		info, statErr := os.Stat(filepath.Join(directory, name))
		if statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing artifact evidence %s: %v", name, statErr)
		}
	}
}

func requirePublishedManifestDigests(t *testing.T, directory string) {
	t.Helper()
	manifestPayload, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version int `json:"version"`
		Files   []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifestPayload, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || len(manifest.Files) != 3 {
		t.Fatalf("published evidence manifest = %+v", manifest)
	}
	for _, entry := range manifest.Files {
		payload, readErr := os.ReadFile(filepath.Join(directory, entry.Name))
		if readErr != nil {
			t.Fatalf("manifest entry %q: %v", entry.Name, readErr)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(payload))
		if entry.SHA256 != digest {
			t.Fatalf("manifest digest for %q = %q, computed %q", entry.Name, entry.SHA256, digest)
		}
	}
}

func readPublishedArtifactReport(t *testing.T, directory string) publishedArtifactReport {
	t.Helper()
	reportPayload, err := os.ReadFile(filepath.Join(directory, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report publishedArtifactReport
	if err := json.Unmarshal(reportPayload, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func requirePublishedArtifactReport(t *testing.T, report publishedArtifactReport) {
	t.Helper()
	if report.Version != 1 || report.Target != "agent" || report.Mode != "archive" ||
		report.Reference != "registry.example/agent:9.9.9" || report.ImageID != "sha256:"+strings.Repeat("d", 64) ||
		report.ScannerVersion != "0.72.0" || report.ArchiveSHA256 != strings.Repeat("a", 64) ||
		report.DatabaseSHA256 != strings.Repeat("b", 64) || report.SBOMSHA256 != strings.Repeat("c", 64) ||
		!report.StartedAt.Before(report.CompletedAt) {
		t.Fatalf("published artifact report = %+v", report)
	}
}

func requireMarkdownArtifactIdentity(t *testing.T, directory, reference, imageID string) {
	t.Helper()
	markdown, err := os.ReadFile(filepath.Join(directory, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), reference) || !strings.Contains(string(markdown), imageID) {
		t.Fatalf("human evidence omitted artifact identity: %q", markdown)
	}
}

func TestRunPublishesEvidenceThroughTheScannerBoundary(t *testing.T) {
	root := t.TempDir()
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "agent.tar")
	if err := os.WriteFile(archive, []byte("target image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := repository.Repository{
		Root: root, PolicyRoot: policyRoot,
		Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{
			OutputDirectory: ".code-polishy-reports/artifact-security",
			Targets: []policy.ArtifactTarget{
				{Name: "agent", Mode: "archive", Archive: "agent.tar"},
				{Name: "image", Mode: "dockerfile", Dockerfile: "Dockerfile", Context: "."},
				{Name: "producer", Mode: "command", Producer: &policy.ArtifactProducer{
					Argv: []string{"build-artifact"}, Cwd: ".", Manifest: "manifest.json", TimeoutSeconds: 30,
				}},
			},
		}}},
	}
	boundary := newScannerBoundary(t, policyRoot)
	result := Run(t.Context(), repo, boundary)
	if len(result.Findings) != 0 || len(result.Notes) != 4 {
		t.Fatalf("artifact security result = %+v", result)
	}
	for _, target := range []string{"agent", "image", "producer", "code-polishy-scanner"} {
		output := filepath.Join(root, ".code-polishy-reports", "artifact-security", target)
		runs, readErr := os.ReadDir(output)
		if readErr != nil || len(runs) != 1 || !runs[0].IsDir() {
			t.Fatalf("%s report runs = %v, error = %v", target, runs, readErr)
		}
		output = filepath.Join(output, runs[0].Name())
		for _, name := range []string{"manifest.json", "report.json", "report.md", "sbom.cdx.json"} {
			if info, statErr := os.Stat(filepath.Join(output, name)); statErr != nil || !info.Mode().IsRegular() {
				t.Fatalf("%s evidence %s: %v", target, name, statErr)
			}
		}
	}
}

func TestArtifactSecurityCoverageAndToolsFailClosed(t *testing.T) {
	root := t.TempDir()
	repo := repository.Repository{
		Root: root, PolicyRoot: root,
		Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{Targets: []policy.ArtifactTarget{{
			Name: "agent", Mode: "command", Dockerfile: "Dockerfile", Archive: "agent.tar", OpenVEX: "security/agent.openvex.json",
			Producer: &policy.ArtifactProducer{Argv: []string{"tools/missing-producer"}, Cwd: "."},
		}}}}},
	}
	coverage := CoverageFindings(repo, nil)
	if len(coverage) != 3 {
		t.Fatalf("artifact coverage findings = %+v", coverage)
	}
	tools := ToolFindings(repo)
	subjects := map[string]bool{}
	for _, finding := range tools {
		subjects[finding.Subject] = true
	}
	if !subjects["trivy"] || !subjects["agent"] {
		t.Fatalf("artifact tool findings = %+v", tools)
	}
}

func TestRunFailsClosedBeforeArtifactMutation(t *testing.T) {
	root := t.TempDir()
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.Repository{
		Root: root, PolicyRoot: policyRoot,
		Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{
			Targets: []policy.ArtifactTarget{{Name: "agent", Mode: "archive", Archive: "agent.tar"}},
		}}},
	}
	withoutOutput := Run(t.Context(), repo, runOnlyBoundary{})
	if len(withoutOutput.Findings) != 1 || withoutOutput.Findings[0].Subject != "scanner:command-runner" {
		t.Fatalf("runner capability failure = %+v", withoutOutput)
	}
	unavailable := Run(t.Context(), repo, unavailableScannerBoundary{})
	if len(unavailable.Findings) != 1 || unavailable.Findings[0].Subject != "scanner:scanner-runtime" ||
		!strings.Contains(unavailable.Findings[0].Message, "scanner unavailable") {
		t.Fatalf("scanner startup failure = %+v", unavailable)
	}
}

func TestPlannedArtifactAuthorizationIsExactAtTheSecurityBoundary(t *testing.T) {
	repo := repository.Repository{Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{Targets: []policy.ArtifactTarget{
		{Name: "archive", Mode: "archive"},
		{Name: "image", Mode: "dockerfile"},
		{Name: "producer", Mode: "command", Producer: &policy.ArtifactProducer{
			Argv: []string{"build-artifact"}, Cwd: "build", Environment: []string{"MODE=release"}, TimeoutSeconds: 60,
		}},
	}}}}}
	plans := PlannedCommands(repo)
	if len(plans) == 0 {
		t.Fatal("artifact process plan is empty")
	}
	for _, expected := range plans {
		actual := expected
		if strings.HasPrefix(expected.Name, governedDockerCommandPrefix) {
			actual.Argv = []string{"docker", "version"}
		} else {
			actual.Environment = append([]string{}, expected.Environment...)
			actual.Environment[len(actual.Environment)-1] = "CODE_POLISHY_ARTIFACT_OUTPUT=/private/output"
		}
		if !MatchesPlannedCommand(expected, actual) {
			t.Fatalf("authorized artifact command was refused: expected=%+v actual=%+v", expected, actual)
		}
		actual.ExclusiveResources = nil
		if MatchesPlannedCommand(expected, actual) {
			t.Fatalf("artifact command escaped its resource boundary: %+v", actual)
		}
	}
	if !IsCleanupCommand(policy.Command{Name: "artifact-security-docker-target-image-remove"}) ||
		IsCleanupCommand(policy.Command{Name: "artifact-security-producer-remove"}) {
		t.Fatal("artifact cleanup boundary was not exact")
	}
}

type scannerBoundary struct {
	t          *testing.T
	containers map[string][]string
	findings   []map[string]any
	imageID    string
}

type runOnlyBoundary struct{}

func (runOnlyBoundary) Run(context.Context, string, policy.Command) error {
	return nil
}

type unavailableScannerBoundary struct{}

func (unavailableScannerBoundary) Run(context.Context, string, policy.Command) error {
	return fmt.Errorf("scanner unavailable")
}

func (unavailableScannerBoundary) RunWithOutput(context.Context, string, policy.Command) (runner.Result, runner.Output, error) {
	return runner.Result{}, runner.Output{Stderr: []byte("scanner unavailable")}, fmt.Errorf("scanner unavailable")
}

func newScannerBoundary(t *testing.T, policyRoot string) *scannerBoundary {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(policyRoot, "artifact-security", "scanner.openvex.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Statements []struct {
			Vulnerability struct {
				Name string `json:"name"`
			} `json:"vulnerability"`
			Products []struct {
				ID string `json:"@id"`
			} `json:"products"`
		} `json:"statements"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	findings := make([]map[string]any, 0, len(document.Statements))
	for _, statement := range document.Statements {
		if statement.Vulnerability.Name == "" || len(statement.Products) != 1 || statement.Products[0].ID == "" {
			t.Fatalf("scanner assessment cannot seed boundary findings: %+v", statement)
		}
		version := "1.0.0"
		if _, value, found := strings.Cut(statement.Products[0].ID, "@"); found && value != "" {
			version = value
		}
		findings = append(findings, map[string]any{
			"VulnerabilityID":  statement.Vulnerability.Name,
			"PkgName":          "scanner-dependency",
			"PkgIdentifier":    map[string]any{"PURL": statement.Products[0].ID},
			"InstalledVersion": version,
			"Severity":         "HIGH",
			"Title":            "scanner dependency finding",
		})
	}
	return &scannerBoundary{
		t: t, containers: map[string][]string{}, findings: findings,
		imageID: "sha256:" + strings.Repeat("a", 64),
	}
}

func (boundary *scannerBoundary) Run(ctx context.Context, root string, command policy.Command) error {
	if len(command.Argv) > 0 && command.Argv[0] != "docker" {
		output := ""
		for _, value := range command.Environment {
			if path, found := strings.CutPrefix(value, "CODE_POLISHY_ARTIFACT_OUTPUT="); found {
				output = path
			}
		}
		if output == "" {
			return fmt.Errorf("artifact producer omitted its private output")
		}
		archive := filepath.Join(output, "image.tar")
		if err := os.WriteFile(archive, []byte("target image"), 0o600); err != nil {
			return err
		}
		manifest := []byte(`{"version":1,"archive":"image.tar","reference":"produced-image"}`)
		return os.WriteFile(filepath.Join(output, "manifest.json"), manifest, 0o600)
	}
	_, _, err := boundary.RunWithOutput(ctx, root, command)
	return err
}

func (boundary *scannerBoundary) RunWithOutput(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	arguments := command.Argv
	if len(arguments) < 2 || arguments[0] != "docker" {
		return runner.Result{}, runner.Output{}, fmt.Errorf("unexpected external command %v", arguments)
	}
	output := runner.Output{}
	switch arguments[1] {
	case "version":
		output.Stdout = []byte("27.0.0\n")
	case "info":
		output.Stdout = []byte("amd64\n")
	case "image":
		var err error
		output, err = boundary.imageCommand(arguments[2:])
		if err != nil {
			return runner.Result{}, output, err
		}
	case "container":
		var err error
		output, err = boundary.containerCommand(arguments[2:])
		if err != nil {
			return runner.Result{}, output, err
		}
	default:
		return runner.Result{}, output, fmt.Errorf("unsupported Docker command %v", arguments)
	}
	return runner.Result{ExitStatus: 0}, output, nil
}

func (boundary *scannerBoundary) imageCommand(arguments []string) (runner.Output, error) {
	if len(arguments) == 0 {
		return runner.Output{}, fmt.Errorf("missing image operation")
	}
	switch arguments[0] {
	case "pull", "build", "rm":
		return runner.Output{}, nil
	case "inspect":
		format, _ := flagValue(arguments, "--format")
		if strings.Contains(format, "RepoDigests") {
			return runner.Output{Stdout: []byte(`["` + arguments[len(arguments)-1] + `"]` + "\n")}, nil
		}
		return runner.Output{Stdout: []byte(boundary.imageID + "\n")}, nil
	case "save":
		path, ok := flagValue(arguments, "--output")
		if !ok {
			return runner.Output{}, fmt.Errorf("image save omitted output")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return runner.Output{}, err
		}
		payload := []byte("target image")
		if strings.Contains(arguments[len(arguments)-1], "artifact-scanner") {
			payload = []byte("scanner image")
		}
		return runner.Output{}, os.WriteFile(path, payload, 0o600)
	default:
		return runner.Output{}, fmt.Errorf("unsupported image operation %v", arguments)
	}
}

func (boundary *scannerBoundary) containerCommand(arguments []string) (runner.Output, error) {
	if len(arguments) == 0 {
		return runner.Output{}, fmt.Errorf("missing container operation")
	}
	switch arguments[0] {
	case "create":
		name, ok := flagValue(arguments, "--name")
		if !ok {
			return runner.Output{}, fmt.Errorf("container omitted name")
		}
		boundary.containers[name] = append([]string{}, arguments...)
		return runner.Output{Stdout: []byte(name + "\n")}, nil
	case "start":
		name := arguments[len(arguments)-1]
		created, ok := boundary.containers[name]
		if !ok {
			return runner.Output{}, fmt.Errorf("unknown container %s", name)
		}
		return boundary.startContainer(created)
	case "rm":
		return runner.Output{}, nil
	default:
		return runner.Output{}, fmt.Errorf("unsupported container operation %v", arguments)
	}
}

func (boundary *scannerBoundary) startContainer(arguments []string) (runner.Output, error) {
	if slices.Contains(arguments, "version") {
		return runner.Output{Stdout: []byte("Version: 0.72.0\n")}, nil
	}
	if slices.Contains(arguments, "--download-db-only") {
		cache, ok := mountSource(arguments, "/cache")
		if !ok {
			return runner.Output{}, fmt.Errorf("database download omitted cache")
		}
		directory := filepath.Join(cache, "db")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return runner.Output{}, err
		}
		now := time.Now().UTC()
		metadata, err := json.Marshal(map[string]any{
			"Version": 2, "NextUpdate": now.Add(time.Hour), "UpdatedAt": now.Add(-time.Hour), "DownloadedAt": now,
		})
		if err != nil {
			return runner.Output{}, err
		}
		if err := os.WriteFile(filepath.Join(directory, "metadata.json"), metadata, 0o600); err != nil {
			return runner.Output{}, err
		}
		return runner.Output{}, os.WriteFile(filepath.Join(directory, "trivy.db"), []byte("database"), 0o600)
	}
	format, _ := flagValue(arguments, "--format")
	switch format {
	case "json":
		return runner.Output{}, boundary.writeVulnerabilityReport(arguments)
	case "cyclonedx":
		output, ok := mountSource(arguments, "/output")
		if !ok {
			return runner.Output{}, fmt.Errorf("SBOM scan omitted output")
		}
		return runner.Output{}, os.WriteFile(filepath.Join(output, "sbom.json"), []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6"}`), 0o600)
	default:
		return runner.Output{}, fmt.Errorf("unsupported scanner invocation %v", arguments)
	}
}

func (boundary *scannerBoundary) writeVulnerabilityReport(arguments []string) error {
	outputRoot, ok := mountSource(arguments, "/output")
	if !ok {
		return fmt.Errorf("vulnerability scan omitted output")
	}
	containerOutput, ok := flagValue(arguments, "--output")
	if !ok {
		return fmt.Errorf("vulnerability scan omitted report path")
	}
	findings := []map[string]any{}
	archive, archiveFound := mountSource(arguments, "/input/image.tar")
	if archiveFound && !slices.Contains(arguments, "--vex") {
		payload, err := os.ReadFile(archive)
		if err != nil {
			return err
		}
		if string(payload) == "scanner image" {
			findings = boundary.findings
		}
	}
	report := map[string]any{
		"SchemaVersion": 2,
		"Trivy":         map[string]any{"Version": "0.72.0"},
		"ArtifactType":  "container_image",
		"Metadata":      map[string]any{"ImageID": boundary.imageID},
		"Results":       []map[string]any{{"Target": "scanner", "Vulnerabilities": findings}},
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputRoot, filepath.Base(containerOutput)), payload, 0o600)
}

func flagValue(arguments []string, name string) (string, bool) {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == name {
			return arguments[index+1], true
		}
	}
	return "", false
}

func mountSource(arguments []string, target string) (string, bool) {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] != "--mount" {
			continue
		}
		fields := strings.Split(arguments[index+1], ",")
		source := ""
		destination := ""
		for _, field := range fields {
			if value, found := strings.CutPrefix(field, "src="); found {
				source = value
			}
			if value, found := strings.CutPrefix(field, "dst="); found {
				destination = value
			}
		}
		if destination == target {
			return source, source != ""
		}
	}
	return "", false
}
