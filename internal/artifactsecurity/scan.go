package artifactsecurity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type trivyReport struct {
	SchemaVersion int `json:"SchemaVersion"`
	Trivy         struct {
		Version string `json:"Version"`
	} `json:"Trivy"`
	ArtifactType string `json:"ArtifactType"`
	Metadata     struct {
		ImageID string `json:"ImageID"`
		OS      *struct {
			Family string `json:"Family"`
			Name   string `json:"Name"`
			EOSL   bool   `json:"EOSL"`
		} `json:"OS"`
	} `json:"Metadata"`
	Results []trivyResult `json:"Results"`
}

type trivyResult struct {
	Target            string                  `json:"Target"`
	Class             string                  `json:"Class"`
	Type              string                  `json:"Type"`
	Vulnerabilities   []trivyVulnerability    `json:"Vulnerabilities"`
	Secrets           []trivySecret           `json:"Secrets"`
	Misconfigurations []trivyMisconfiguration `json:"Misconfigurations"`
}

type trivyVulnerability struct {
	VulnerabilityID string `json:"VulnerabilityID"`
	PkgName         string `json:"PkgName"`
	PkgIdentifier   struct {
		PURL string `json:"PURL"`
	} `json:"PkgIdentifier"`
	InstalledVersion string `json:"InstalledVersion"`
	FixedVersion     string `json:"FixedVersion"`
	Status           string `json:"Status"`
	Title            string `json:"Title"`
	Severity         string `json:"Severity"`
}

type trivySecret struct {
	RuleID    string `json:"RuleID"`
	Category  string `json:"Category"`
	Severity  string `json:"Severity"`
	Title     string `json:"Title"`
	StartLine int    `json:"StartLine"`
	EndLine   int    `json:"EndLine"`
}

type trivyMisconfiguration struct {
	ID         string `json:"ID"`
	Title      string `json:"Title"`
	Message    string `json:"Message"`
	Resolution string `json:"Resolution"`
	Severity   string `json:"Severity"`
	Status     string `json:"Status"`
}

func (state *scannerRuntime) scan(
	ctx context.Context,
	repo repository.Repository,
	operation string,
	target policy.ArtifactTarget,
	artifact preparedArtifact,
	database databaseEvidence,
) (scanOutput, error) {
	started := time.Now().UTC()
	archiveHash, err := hashRegularFile(artifact.archive, state.policy.MaximumArchiveBytes)
	if err != nil {
		return scanOutput{}, err
	}
	outputDirectory := filepath.Join(state.root, "scan-output", target.Name)
	if err := os.MkdirAll(outputDirectory, 0o700); err != nil {
		return scanOutput{}, err
	}
	mounts := []scannerMount{
		{source: state.cache, target: "/cache"},
		{source: artifact.archive, target: "/input/image.tar", readOnly: true},
		{source: outputDirectory, target: "/output"},
	}
	observed, imageID, err := state.scanVulnerabilityReport(ctx, operation+"-observed", target.Name, artifact.imageID, outputDirectory, mounts, "observed.json", "")
	if err != nil {
		return scanOutput{}, err
	}
	artifact.imageID = imageID
	blocking, accepted, err := state.applyArtifactOpenVEX(ctx, operation, target, artifact, outputDirectory, mounts, observed, imageID)
	if err != nil {
		return scanOutput{}, err
	}
	sbom, err := state.scanSBOM(ctx, operation+"-sbom", outputDirectory, mounts)
	if err != nil {
		return scanOutput{}, err
	}
	if err := state.validateArchiveUnchanged(artifact.archive, archiveHash); err != nil {
		return scanOutput{}, err
	}
	return buildScanOutput(state, target, artifact, started, database, archiveHash, sbom, blocking, accepted), nil
}

func (state *scannerRuntime) scanVulnerabilityReport(
	ctx context.Context,
	operation string,
	artifactName, expectedImageID, outputDirectory string,
	mounts []scannerMount,
	filename, vexPath string,
) ([]normalizedFinding, string, error) {
	containerPath := "/output/" + filename
	if _, err := state.runScanner(ctx, operation, "none", mounts, state.vulnerabilityArguments(containerPath, vexPath)); err != nil {
		return nil, "", err
	}
	payload, err := readBoundedRegularFile(filepath.Join(outputDirectory, filename), state.policy.MaximumReportBytes)
	if err != nil {
		return nil, "", err
	}
	return parseTrivyReport(artifactName, expectedImageID, state.policy.TrivyVersion, payload)
}

func (state *scannerRuntime) applyArtifactOpenVEX(
	ctx context.Context,
	operation string,
	target policy.ArtifactTarget,
	artifact preparedArtifact,
	outputDirectory string,
	mounts []scannerMount,
	observed []normalizedFinding,
	imageID string,
) ([]normalizedFinding, []acceptedFinding, error) {
	if artifact.openVEX == "" {
		return observed, nil, nil
	}
	document, err := parseOpenVEX(artifact.openVEX, time.Now().UTC())
	if err != nil {
		return nil, nil, err
	}
	vexMounts := append(append([]scannerMount{}, mounts...), scannerMount{
		source: artifact.openVEX, target: "/policy/assessment.openvex.json", readOnly: true,
	})
	enforced, enforcedImageID, err := state.scanVulnerabilityReport(
		ctx, operation+"-enforced", target.Name, artifact.imageID, outputDirectory, vexMounts, "enforced.json", "/policy/assessment.openvex.json",
	)
	if err != nil {
		return nil, nil, err
	}
	if enforcedImageID != imageID {
		return nil, nil, errors.New("OpenVEX enforcement changed the scanned image identity")
	}
	return reconcileOpenVEX(observed, enforced, document)
}

func (state *scannerRuntime) scanSBOM(ctx context.Context, operation, outputDirectory string, mounts []scannerMount) ([]byte, error) {
	if _, err := state.runScanner(ctx, operation, "none", mounts, state.sbomArguments()); err != nil {
		return nil, err
	}
	sbom, err := readBoundedRegularFile(filepath.Join(outputDirectory, "sbom.json"), state.policy.MaximumReportBytes)
	if err != nil {
		return nil, err
	}
	if err := validateCycloneDX(sbom); err != nil {
		return nil, err
	}
	return sbom, nil
}

func (state *scannerRuntime) validateArchiveUnchanged(path, expectedHash string) error {
	observedHash, err := hashRegularFile(path, state.policy.MaximumArchiveBytes)
	if err != nil {
		return err
	}
	if observedHash != expectedHash {
		return errors.New("artifact archive changed during read-only scans")
	}
	return nil
}

func buildScanOutput(
	state *scannerRuntime,
	target policy.ArtifactTarget,
	artifact preparedArtifact,
	started time.Time,
	database databaseEvidence,
	archiveHash string,
	sbom []byte,
	blocking []normalizedFinding,
	accepted []acceptedFinding,
) scanOutput {
	sortNormalizedFindings(blocking)
	findings := make([]policy.Finding, 0, len(blocking))
	for _, finding := range blocking {
		findings = append(findings, policyFinding(target, finding))
	}
	return scanOutput{
		report: artifactReport{
			Version: 1, Target: target.Name, Mode: artifact.mode, Reference: artifact.reference, ImageID: artifact.imageID,
			ScannerVersion: state.policy.TrivyVersion,
			SourceImage:    state.policy.SourceImage, DatabaseSHA256: database.SHA256,
			ArchiveSHA256: archiveHash, SBOMSHA256: bytesSHA256(sbom),
			StartedAt: started, CompletedAt: time.Now().UTC(), Findings: blocking, AcceptedFindings: accepted,
		},
		sbom: sbom, findings: findings,
	}
}

func (state *scannerRuntime) vulnerabilityArguments(output, vex string) []string {
	arguments := append(state.offlineArguments(),
		"--scanners", "vuln,secret",
		"--image-config-scanners", "misconfig,secret",
		"--severity", strings.Join(state.policy.FailSeverities, ","),
		"--format", "json", "--output", output,
	)
	if vex != "" {
		arguments = append(arguments, "--vex", vex)
	}
	return arguments
}

func (state *scannerRuntime) sbomArguments() []string {
	return append(state.offlineArguments(), "--scanners", "vuln", "--format", "cyclonedx", "--output", "/output/sbom.json")
}

func (state *scannerRuntime) offlineArguments() []string {
	return []string{
		"image", "--input", "/input/image.tar", "--cache-dir", "/cache",
		"--db-repository", state.policy.DatabaseRepository, "--skip-db-update", "--skip-java-db-update",
		"--skip-check-update", "--skip-vex-repo-update", "--offline-scan", "--disable-telemetry",
		"--skip-version-check", "--no-progress", "--quiet", "--parallel", "2", "--timeout", "10m",
	}
}

func parseTrivyReport(artifact, expectedImageID, scannerVersion string, payload []byte) ([]normalizedFinding, string, error) {
	var report trivyReport
	if err := json.Unmarshal(payload, &report); err != nil {
		return nil, "", err
	}
	if err := validateTrivyReportIdentity(report, expectedImageID, scannerVersion); err != nil {
		return nil, "", err
	}
	findings := []normalizedFinding{}
	for _, result := range report.Results {
		normalized, err := normalizeTrivyResult(artifact, result)
		if err != nil {
			return nil, "", err
		}
		findings = append(findings, normalized...)
	}
	findings = append(findings, endOfLifeFinding(artifact, report)...)
	sortNormalizedFindings(findings)
	return findings, report.Metadata.ImageID, nil
}

func validateTrivyReportIdentity(report trivyReport, expectedImageID, scannerVersion string) error {
	if report.SchemaVersion < 1 {
		return errors.New("trivy report omitted a supported schema version")
	}
	if report.Trivy.Version != scannerVersion {
		return errors.New("trivy report omitted the expected scanner version")
	}
	if report.ArtifactType != "container_image" {
		return errors.New("trivy report omitted the expected container-image identity")
	}
	if !validImageID(report.Metadata.ImageID) {
		return fmt.Errorf("trivy report image ID %q is invalid", report.Metadata.ImageID)
	}
	if expectedImageID != "" && report.Metadata.ImageID != expectedImageID {
		return fmt.Errorf("trivy report image ID %q does not match %q", report.Metadata.ImageID, expectedImageID)
	}
	return nil
}

func normalizeTrivyResult(artifact string, result trivyResult) ([]normalizedFinding, error) {
	target := boundedText(result.Target, 1024)
	if target == "" {
		target = artifact
	}
	findings, err := normalizeTrivyVulnerabilities(artifact, target, result.Vulnerabilities)
	if err != nil {
		return nil, err
	}
	secrets, err := normalizeTrivySecrets(artifact, target, result.Secrets)
	if err != nil {
		return nil, err
	}
	misconfigurations, err := normalizeTrivyMisconfigurations(artifact, target, result.Misconfigurations)
	if err != nil {
		return nil, err
	}
	findings = append(findings, secrets...)
	return append(findings, misconfigurations...), nil
}

func normalizeTrivyVulnerabilities(artifact, target string, values []trivyVulnerability) ([]normalizedFinding, error) {
	findings := make([]normalizedFinding, 0, len(values))
	for _, vulnerability := range values {
		severity, err := blockingSeverity(vulnerability.Severity)
		if err != nil {
			return nil, err
		}
		finding := normalizedFinding{
			Artifact: artifact, Kind: "vulnerability", ID: boundedText(vulnerability.VulnerabilityID, 128),
			Severity: severity, Target: target, Package: boundedText(vulnerability.PkgName, 512),
			PackagePURL:     boundedText(vulnerability.PkgIdentifier.PURL, 1024),
			AffectedVersion: boundedText(vulnerability.InstalledVersion, 256), FixedVersion: boundedText(vulnerability.FixedVersion, 256),
			Status: boundedText(vulnerability.Status, 128), Title: boundedText(vulnerability.Title, 1024),
		}
		if finding.ID == "" || finding.Package == "" || finding.AffectedVersion == "" {
			return nil, errors.New("trivy vulnerability omitted its exact identity")
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func normalizeTrivySecrets(artifact, target string, values []trivySecret) ([]normalizedFinding, error) {
	findings := make([]normalizedFinding, 0, len(values))
	for _, secret := range values {
		severity, err := blockingSeverity(secret.Severity)
		if err != nil {
			return nil, err
		}
		if secret.RuleID == "" || secret.StartLine < 0 || secret.EndLine < secret.StartLine {
			return nil, errors.New("trivy secret finding omitted its exact location")
		}
		findings = append(findings, normalizedFinding{
			Artifact: artifact, Kind: "secret", ID: boundedText(secret.RuleID, 128), Severity: severity,
			Target: fmt.Sprintf("%s:%d-%d", target, secret.StartLine, secret.EndLine), Title: boundedText(secret.Title, 1024),
		})
	}
	return findings, nil
}

func normalizeTrivyMisconfigurations(artifact, target string, values []trivyMisconfiguration) ([]normalizedFinding, error) {
	findings := make([]normalizedFinding, 0, len(values))
	for _, value := range values {
		severity, err := blockingSeverity(value.Severity)
		if err != nil {
			return nil, err
		}
		if value.ID == "" {
			return nil, errors.New("trivy misconfiguration omitted its ID")
		}
		findings = append(findings, normalizedFinding{
			Artifact: artifact, Kind: "misconfiguration", ID: boundedText(value.ID, 128),
			Severity: severity, Target: target, Status: boundedText(value.Status, 128),
			Title: boundedText(firstText(value.Title, value.Message), 1024),
		})
	}
	return findings, nil
}

func endOfLifeFinding(artifact string, report trivyReport) []normalizedFinding {
	if report.Metadata.OS == nil || !report.Metadata.OS.EOSL {
		return nil
	}
	family := boundedText(report.Metadata.OS.Family, 128)
	version := boundedText(report.Metadata.OS.Name, 128)
	return []normalizedFinding{{
		Artifact: artifact, Kind: "end-of-life", ID: "end-of-life:" + family + ":" + version,
		Severity: "HIGH", Target: strings.TrimSpace(family + " " + version), Title: "container operating system has reached end of life",
	}}
}

func validImageID(value string) bool {
	return strings.HasPrefix(value, "sha256:") && exactDigest.MatchString(strings.TrimPrefix(value, "sha256:"))
}

func blockingSeverity(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if !slices.Contains([]string{"HIGH", "CRITICAL"}, value) {
		return "", fmt.Errorf("trivy returned non-blocking or unknown severity %q after severity filtering", value)
	}
	return value, nil
}

func firstText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func validateCycloneDX(payload []byte) error {
	var document struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		return err
	}
	if document.BOMFormat != "CycloneDX" || document.SpecVersion == "" {
		return errors.New("trivy SBOM is not a versioned CycloneDX document")
	}
	return nil
}

func policyFinding(target policy.ArtifactTarget, finding normalizedFinding) policy.Finding {
	path := targetScope(target)
	result := policy.Finding{
		Check: "artifactSecurity." + finding.Kind, Path: path,
		Subject: finding.Artifact + ":" + finding.ID, Message: finding.Severity + " " + finding.Title,
	}
	if finding.Kind == "vulnerability" {
		packageID := finding.Package
		if finding.PackagePURL != "" {
			packageID = finding.PackagePURL
		}
		identity := policy.VulnerabilityIdentity{
			Ecosystem: "container", Advisory: finding.ID, Package: packageID,
			AffectedVersion: finding.AffectedVersion, Scope: path,
			Severity: policy.NormalizeVulnerabilitySeverity(finding.Severity),
		}
		result.Subject = policy.VulnerabilitySubject(identity)
		result.Vulnerability = &identity
	}
	return result
}

func targetScope(target policy.ArtifactTarget) string {
	switch target.Mode {
	case "dockerfile":
		return target.Dockerfile
	case "archive":
		return target.Archive
	default:
		return policy.ConfigFilename
	}
}

func normalizedFindingCounts(findings []normalizedFinding) map[string]int {
	counts := map[string]int{}
	for _, finding := range findings {
		counts[normalizedFindingKey(finding)]++
	}
	return counts
}

func sortedFindingKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
