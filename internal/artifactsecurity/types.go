package artifactsecurity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const (
	scannerDockerfilePath         = "artifact-security/scanner.Dockerfile"
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
	ReassessBy              time.Time
	OpenVEX                 []byte
	PackageSHA256           string
	TrivyVersion            string
	SourceImage             string
	DatabaseRepository      string
	DatabaseSchemaVersion   int
	DatabaseMaximumAgeHours int
	FailSeverities          []string
	MaximumArchiveBytes     int64
	MaximumReportBytes      int64
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
	Platform             string              `json:"platform"`
	ScannerPackageSHA256 string              `json:"scannerPackageSha256"`
	Version              int                 `json:"version"`
	Target               string              `json:"target"`
	Mode                 string              `json:"mode"`
	Reference            string              `json:"reference"`
	ImageID              string              `json:"imageId"`
	ScannerVersion       string              `json:"scannerVersion"`
	SourceImage          string              `json:"sourceImage"`
	DatabaseSHA256       string              `json:"databaseSha256"`
	ArchiveSHA256        string              `json:"archiveSha256"`
	SBOMSHA256           string              `json:"sbomSha256"`
	StartedAt            time.Time           `json:"startedAt"`
	CompletedAt          time.Time           `json:"completedAt"`
	Findings             []normalizedFinding `json:"findings"`
	AcceptedFindings     []acceptedFinding   `json:"acceptedFindings"`
}

type scanOutput struct {
	report   artifactReport
	sbom     []byte
	findings []policy.Finding
}

func loadScannerPolicy(repoRoot, policyRoot string) (scannerPolicy, error) {
	lock, _, err := release.ReadLock(repoRoot)
	if err != nil {
		return scannerPolicy{}, err
	}
	manifest, err := release.VerifyScanner(policyRoot, lock.Scanner)
	if err != nil {
		return scannerPolicy{}, err
	}
	if err := ValidateScannerManifest(manifest); err != nil {
		return scannerPolicy{}, err
	}
	if _, err := readBoundedRegularFile(filepath.Join(policyRoot, filepath.FromSlash(scannerDockerfilePath)), 64*1024); err != nil {
		return scannerPolicy{}, err
	}

	return scannerPolicy{TrivyVersion: manifest.TrivyVersion, SourceImage: manifest.SourceImage,
		DatabaseRepository: "ghcr.io/aquasecurity/trivy-db:2", DatabaseSchemaVersion: 2,
		DatabaseMaximumAgeHours: 24, FailSeverities: []string{"HIGH", "CRITICAL"},
		MaximumArchiveBytes: 2147483648, MaximumReportBytes: 67108864,
		ReassessBy: manifest.ReassessBy, OpenVEX: manifest.OpenVEX, PackageSHA256: bytesSHA256(lock.Scanner.Payload)}, nil
}

func ValidateScannerManifest(manifest release.ScannerManifest) error {
	_, err := parseOpenVEXBytes(manifest.OpenVEX, time.Now().UTC())
	return err
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
	executor runner.Runner
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
	_, output, err := runner.RunCaptured(ctx, client.executor, client.root, command, runner.CaptureOutput)
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
		Environment:        append([]string{}, dockerClientEnvironment...),
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
