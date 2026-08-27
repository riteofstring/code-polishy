package artifactsecurity

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const (
	scannerPolicyRelativePath     = "artifact-security/scanner-policy.json"
	scannerDockerfilePath         = "artifact-security/scanner.Dockerfile"
	scannerPolicyMaximumBytes     = 32 * 1024
	metadataMaximumBytes          = 64 * 1024
	artifactCommandTimeoutSeconds = 15 * 60
	artifactCleanupTimeoutSeconds = 30
)

var exactDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Result struct {
	Findings []policy.Finding
	Notes    []string
}

type scannerPolicy struct {
	Version                 int      `json:"version"`
	TrivyVersion            string   `json:"trivyVersion"`
	SourceImage             string   `json:"sourceImage"`
	DockerfileSHA256        string   `json:"dockerfileSha256"`
	OpenVEXSHA256           string   `json:"openVexSha256"`
	DatabaseRepository      string   `json:"databaseRepository"`
	DatabaseSchemaVersion   int      `json:"databaseSchemaVersion"`
	DatabaseMaximumAgeHours int      `json:"databaseMaximumAgeHours"`
	FailSeverities          []string `json:"failSeverities"`
	MaximumArchiveBytes     int64    `json:"maximumArchiveBytes"`
	MaximumReportBytes      int64    `json:"maximumReportBytes"`
}

type normalizedFinding struct {
	Artifact        string `json:"artifact"`
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	Severity        string `json:"severity"`
	Target          string `json:"target"`
	Package         string `json:"package,omitempty"`
	PackagePURL     string `json:"packagePurl,omitempty"`
	AffectedVersion string `json:"affectedVersion,omitempty"`
	FixedVersion    string `json:"fixedVersion,omitempty"`
	Status          string `json:"status,omitempty"`
	Title           string `json:"title,omitempty"`
}

type acceptedFinding struct {
	Finding         normalizedFinding `json:"finding"`
	Justification   string            `json:"justification"`
	ImpactStatement string            `json:"impactStatement"`
}

type artifactReport struct {
	Version          int                 `json:"version"`
	Target           string              `json:"target"`
	Mode             string              `json:"mode"`
	Reference        string              `json:"reference"`
	ImageID          string              `json:"imageId"`
	ScannerVersion   string              `json:"scannerVersion"`
	SourceImage      string              `json:"sourceImage"`
	DatabaseSHA256   string              `json:"databaseSha256"`
	ArchiveSHA256    string              `json:"archiveSha256"`
	SBOMSHA256       string              `json:"sbomSha256"`
	StartedAt        time.Time           `json:"startedAt"`
	CompletedAt      time.Time           `json:"completedAt"`
	Findings         []normalizedFinding `json:"findings"`
	AcceptedFindings []acceptedFinding   `json:"acceptedFindings"`
}

type scanOutput struct {
	report   artifactReport
	sbom     []byte
	findings []policy.Finding
}

type scannerPolicyEvidence struct {
	versionText    string
	dockerfileHash string
	openVEXHash    string
}

func loadScannerPolicy(policyRoot string) (scannerPolicy, error) {
	path := filepath.Join(policyRoot, filepath.FromSlash(scannerPolicyRelativePath))
	payload, err := readBoundedRegularFile(path, scannerPolicyMaximumBytes)
	if err != nil {
		return scannerPolicy{}, fmt.Errorf("read artifact scanner policy: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var configured scannerPolicy
	if err := decoder.Decode(&configured); err != nil {
		return scannerPolicy{}, fmt.Errorf("decode artifact scanner policy: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return scannerPolicy{}, fmt.Errorf("decode artifact scanner policy: %w", err)
	}
	if err := configured.validate(policyRoot); err != nil {
		return scannerPolicy{}, err
	}
	return configured, nil
}

func (configured scannerPolicy) validate(policyRoot string) error {
	evidence, err := loadScannerPolicyEvidence(policyRoot)
	if err != nil {
		return err
	}
	if _, err := parseOpenVEX(filepath.Join(policyRoot, "artifact-security", "scanner.openvex.json"), time.Now().UTC()); err != nil {
		return fmt.Errorf("validate scanner OpenVEX: %w", err)
	}
	return configured.validateFields(evidence)
}

func loadScannerPolicyEvidence(policyRoot string) (scannerPolicyEvidence, error) {
	versionBytes, err := readBoundedRegularFile(filepath.Join(policyRoot, "tools", "trivy-version.txt"), 64)
	if err != nil {
		return scannerPolicyEvidence{}, err
	}
	dockerfile := filepath.Join(policyRoot, filepath.FromSlash(scannerDockerfilePath))
	dockerfileHash, err := hashRegularFile(dockerfile, 64*1024)
	if err != nil {
		return scannerPolicyEvidence{}, err
	}
	openVEXHash, err := hashRegularFile(filepath.Join(policyRoot, "artifact-security", "scanner.openvex.json"), 64*1024)
	if err != nil {
		return scannerPolicyEvidence{}, err
	}
	return scannerPolicyEvidence{
		versionText: strings.TrimSpace(string(versionBytes)), dockerfileHash: dockerfileHash, openVEXHash: openVEXHash,
	}, nil
}

func (configured scannerPolicy) validateFields(evidence scannerPolicyEvidence) error {
	if err := configured.validateProvenanceFields(evidence); err != nil {
		return err
	}
	return configured.validateLimitFields()
}

func (configured scannerPolicy) validateProvenanceFields(evidence scannerPolicyEvidence) error {
	if configured.Version != 1 {
		return errors.New("artifact scanner policy version must be 1")
	}
	if evidence.versionText != configured.TrivyVersion {
		return errors.New("artifact scanner policy and version pin do not agree")
	}
	if configured.DockerfileSHA256 != evidence.dockerfileHash || configured.OpenVEXSHA256 != evidence.openVEXHash {
		return errors.New("artifact scanner policy, version pin, and runtime Dockerfile do not agree")
	}
	const imagePrefix = "docker.io/aquasec/trivy@sha256:"
	if !strings.HasPrefix(configured.SourceImage, imagePrefix) {
		return errors.New("artifact scanner source image must use the exact official Trivy digest")
	}
	if !exactDigest.MatchString(strings.TrimPrefix(configured.SourceImage, imagePrefix)) {
		return errors.New("artifact scanner source image digest must contain 64 lowercase hexadecimal characters")
	}
	if configured.DatabaseRepository != fmt.Sprintf("ghcr.io/aquasecurity/trivy-db:%d", configured.DatabaseSchemaVersion) {
		return errors.New("artifact scanner database policy is invalid")
	}
	return nil
}

func (configured scannerPolicy) validateLimitFields() error {
	if configured.DatabaseMaximumAgeHours < 1 || configured.DatabaseMaximumAgeHours > 72 {
		return errors.New("artifact scanner database maximum age must be between 1 and 72 hours")
	}
	if !slices.Equal(configured.FailSeverities, []string{"HIGH", "CRITICAL"}) {
		return errors.New("artifact scanner finding or size policy is invalid")
	}
	if configured.MaximumArchiveBytes < 1 || configured.MaximumReportBytes < 1 {
		return errors.New("artifact scanner size limits must be positive")
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("more than one JSON value")
}

func readBoundedRegularFile(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return nil, fmt.Errorf("%s must be a regular file no larger than %d bytes", path, maximum)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maximum {
		return nil, fmt.Errorf("%s grew beyond the %d-byte limit while being read", path, maximum)
	}
	return payload, nil
}

func hashRegularFile(path string, maximum int64) (string, error) {
	payload, err := readBoundedRegularFile(path, maximum)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func bytesSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

type dockerClient struct {
	root     string
	executor runner.OutputRunner
}

type commandResult struct {
	stdout string
	stderr string
}

func (client dockerClient) run(ctx context.Context, name string, arguments ...string) (commandResult, error) {
	return client.runCommand(ctx, dockerCommand(name, arguments...))
}

func (client dockerClient) cleanup(ctx context.Context, name string, arguments ...string) (commandResult, error) {
	return client.runCommand(ctx, dockerCleanupCommand(name, arguments...))
}

func (client dockerClient) runCommand(ctx context.Context, command policy.Command) (commandResult, error) {
	_, output, err := client.executor.RunWithOutput(ctx, client.root, command)
	result := commandResult{stdout: string(output.Stdout), stderr: string(output.Stderr)}
	if err == nil {
		return result, nil
	}
	message := strings.TrimSpace(result.stderr)
	if message == "" {
		message = err.Error()
	}
	return result, fmt.Errorf("docker %s: %s", strings.Join(command.Argv[1:], " "), boundedText(message, 2048))
}

func dockerCommand(name string, arguments ...string) policy.Command {
	return policy.Command{
		Name: name, Argv: append([]string{"docker"}, arguments...), Cwd: ".",
		ExclusiveResources: []string{"artifact-security"}, TimeoutSeconds: artifactCommandTimeoutSeconds,
	}
}

func dockerCleanupCommand(name string, arguments ...string) policy.Command {
	command := dockerCommand(name, arguments...)
	command.TimeoutSeconds = artifactCleanupTimeoutSeconds
	return command
}

func boundedText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return value[:maximum] + "…"
	}
	return value
}

func containedRepositoryPath(repo repository.Repository, relative string) (string, error) {
	root, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if !inside(root, resolved) {
		return "", fmt.Errorf("repository path escapes its root: %s", relative)
	}
	return resolved, nil
}

func inside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func sortNormalizedFindings(findings []normalizedFinding) {
	sort.Slice(findings, func(left, right int) bool {
		return normalizedFindingKey(findings[left]) < normalizedFindingKey(findings[right])
	})
}

func normalizedFindingKey(finding normalizedFinding) string {
	return strings.Join([]string{
		finding.Artifact, finding.Kind, finding.ID, finding.Severity, finding.Target,
		finding.Package, finding.PackagePURL, finding.AffectedVersion, finding.FixedVersion,
		finding.Status, finding.Title,
	}, "\x00")
}
