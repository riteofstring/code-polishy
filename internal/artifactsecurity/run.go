package artifactsecurity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type scannerRuntime struct {
	client   dockerClient
	policy   scannerPolicy
	root     string
	cache    string
	image    string
	platform string
	user     string
	runID    string
}

type preparedArtifact struct {
	name      string
	mode      string
	reference string
	imageID   string
	archive   string
	openVEX   string
	cleanup   func() error
}

type producerManifest struct {
	Version   int    `json:"version"`
	Archive   string `json:"archive"`
	Reference string `json:"reference"`
	ImageID   string `json:"imageId,omitempty"`
}

func Run(ctx context.Context, repo repository.Repository, commandRunner runner.Runner) (result Result) {
	targets := repo.Config.SupplyChain.ArtifactSecurity.Targets
	if len(targets) == 0 {
		return Result{}
	}
	outputRunner, ok := commandRunner.(runner.OutputRunner)
	if !ok {
		return operationalResult("command-runner", errors.New("artifact security requires a governed command runner with structured output capture"))
	}
	configured, err := loadScannerPolicy(repo.PolicyRoot)
	if err != nil {
		return operationalResult("scanner-policy", err)
	}
	runtimeState, err := prepareScannerRuntime(ctx, repo, configured, outputRunner)
	if err != nil {
		return operationalResult("scanner-runtime", err)
	}
	defer recordRuntimeCleanup(&result, runtimeState)
	database, err := runtimeState.updateDatabase(ctx)
	if err != nil {
		return operationalResult("vulnerability-database", err)
	}
	if scannerErr := scanScannerRuntime(ctx, repo, runtimeState, database, &result); scannerErr != nil {
		result.Findings = append(result.Findings, artifactOperationalFinding("code-polishy-scanner", "self-scan", scannerErr))
		return result
	}

	scanConfiguredTargets(ctx, repo, runtimeState, database, targets, &result)
	recordDatabaseImmutability(runtimeState, database, &result)
	return result
}

func recordRuntimeCleanup(result *Result, state *scannerRuntime) {
	if err := state.cleanup(); err != nil {
		result.Findings = append(result.Findings, artifactOperationalFinding("scanner", "cleanup", err))
	}
}

func scanConfiguredTargets(ctx context.Context, repo repository.Repository, state *scannerRuntime, database databaseEvidence, targets []policy.ArtifactTarget, result *Result) {
	for _, target := range targets {
		findings, note := scanConfiguredTarget(ctx, repo, state, database, target)
		result.Findings = append(result.Findings, findings...)
		if note != "" {
			result.Notes = append(result.Notes, note)
		}
	}
}

func scanConfiguredTarget(ctx context.Context, repo repository.Repository, state *scannerRuntime, database databaseEvidence, target policy.ArtifactTarget) ([]policy.Finding, string) {
	prepared, err := prepareArtifact(ctx, repo, state, target)
	if err != nil {
		return []policy.Finding{artifactOperationalFinding(target.Name, "prepare", err)}, ""
	}
	scanned, scanErr := state.scan(ctx, repo, "target-"+target.Name, target, prepared, database)
	if err := errors.Join(scanErr, prepared.cleanup()); err != nil {
		return []policy.Finding{artifactOperationalFinding(target.Name, "scan", err)}, ""
	}
	outputPath, err := writeArtifacts(repo, target, scanned)
	if err != nil {
		return []policy.Finding{artifactOperationalFinding(target.Name, "report", err)}, ""
	}
	return scanned.findings, fmt.Sprintf("artifact security report for %s: %s", target.Name, outputPath)
}

func recordDatabaseImmutability(state *scannerRuntime, before databaseEvidence, result *Result) {
	after, err := state.inspectDatabase(time.Now().UTC(), false)
	if combined := errors.Join(err, databaseChangedError(before, after)); combined != nil {
		result.Findings = append(result.Findings, artifactOperationalFinding("scanner", "database-immutability", combined))
	}
}

func scanScannerRuntime(ctx context.Context, repo repository.Repository, state *scannerRuntime, database databaseEvidence, result *Result) error {
	directory := filepath.Join(state.root, "targets", "code-polishy-scanner")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	archive := filepath.Join(directory, "image.tar")
	if _, err := state.client.run(ctx, governedDockerCommandPrefix+"save-scanner", "image", "save", "--output", archive, state.image); err != nil {
		return err
	}
	imageID, err := inspectImageID(ctx, state.client, governedDockerCommandPrefix+"inspect-scanner", state.image)
	if err != nil {
		return err
	}
	openVEX := filepath.Join(directory, "assessment.openvex.json")
	if err := copyBoundedRegularFile(
		filepath.Join(repo.PolicyRoot, "artifact-security", "scanner.openvex.json"), openVEX, 64*1024,
	); err != nil {
		return err
	}
	target := policy.ArtifactTarget{Name: "code-polishy-scanner", Mode: "command"}
	prepared := preparedArtifact{
		name: "code-polishy-scanner", mode: "scanner", reference: state.image,
		imageID: imageID, archive: archive, openVEX: openVEX, cleanup: func() error { return nil },
	}
	scanned, err := state.scan(ctx, repo, "scanner-self", target, prepared, database)
	if err != nil {
		return err
	}
	outputPath, err := writeArtifacts(repo, target, scanned)
	if err != nil {
		return err
	}
	result.Findings = append(result.Findings, scanned.findings...)
	result.Notes = append(result.Notes, "artifact security report for code-polishy-scanner: "+outputPath)
	return nil
}

func operationalResult(subject string, err error) Result {
	return Result{Findings: []policy.Finding{artifactOperationalFinding("scanner", subject, err)}}
}

func artifactOperationalFinding(target, subject string, err error) policy.Finding {
	message := "artifact security operation failed"
	if err != nil {
		message = err.Error()
	}
	return policy.Finding{Check: "artifactSecurity.operation", Path: policy.ConfigFilename, Subject: target + ":" + subject, Message: message}
}

func prepareScannerRuntime(ctx context.Context, repo repository.Repository, configured scannerPolicy, commandRunner runner.OutputRunner) (*scannerRuntime, error) {
	client := dockerClient{root: repo.Root, executor: commandRunner}
	platform, err := prepareScannerSource(ctx, client, configured.SourceImage)
	if err != nil {
		return nil, err
	}
	state, err := newScannerRuntime(client, configured, platform)
	if err != nil {
		return nil, err
	}
	if err := state.buildAndVerify(ctx, repo.PolicyRoot); err != nil {
		_ = state.cleanup()
		return nil, err
	}
	return state, nil
}

func prepareScannerSource(ctx context.Context, client dockerClient, sourceImage string) (string, error) {
	if _, err := client.run(ctx, governedDockerCommandPrefix+"preflight", "version", "--format", "{{.Server.Version}}"); err != nil {
		return "", fmt.Errorf("docker server preflight: %w", err)
	}
	platform, err := dockerPlatform(ctx, client, governedDockerCommandPrefix+"platform")
	if err != nil {
		return "", err
	}
	if _, err := client.run(ctx, governedDockerCommandPrefix+"pull-source", "image", "pull", "--platform", platform, sourceImage); err != nil {
		return "", fmt.Errorf("pull pinned Trivy source image: %w", err)
	}
	if err := verifyPinnedSourceImage(ctx, client, governedDockerCommandPrefix+"verify-source", sourceImage); err != nil {
		return "", err
	}
	return platform, nil
}

func newScannerRuntime(client dockerClient, configured scannerPolicy, platform string) (*scannerRuntime, error) {
	runID, err := randomHex(6)
	if err != nil {
		return nil, err
	}
	createdRoot, err := os.MkdirTemp("", "code-polishy-artifact-security-")
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(createdRoot)
	if err != nil {
		_ = os.RemoveAll(createdRoot)
		return nil, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	cache := filepath.Join(root, "cache")
	if err := os.Mkdir(cache, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	return &scannerRuntime{
		client: client, policy: configured, root: root, cache: cache,
		image: "code-polishy-artifact-scanner:" + runID, platform: platform, user: currentUserID(), runID: runID,
	}, nil
}

func (state *scannerRuntime) buildAndVerify(ctx context.Context, policyRoot string) error {
	dockerfile := filepath.Join(policyRoot, filepath.FromSlash(scannerDockerfilePath))
	contextRoot := filepath.Dir(dockerfile)
	_, err := state.client.run(ctx, governedDockerCommandPrefix+"build-scanner",
		"image", "build", "--network", "none", "--pull=false", "--provenance=false",
		"--platform", state.platform, "--file", dockerfile,
		"--label", "com.code-polishy.security.role=scanner", "--tag", state.image, contextRoot,
	)
	if err != nil {
		return fmt.Errorf("build minimal scanner runtime: %w", err)
	}
	version, err := state.runScanner(ctx, "scanner-version", "none", nil, []string{"version"})
	if err != nil {
		return fmt.Errorf("minimal scanner runtime version check: %w", err)
	}
	if !strings.Contains(version.stdout, "Version: "+state.policy.TrivyVersion) {
		return errors.New("minimal scanner runtime version mismatch")
	}
	return nil
}

func dockerPlatform(ctx context.Context, client dockerClient, name string) (string, error) {
	result, err := client.run(ctx, name, "info", "--format", "{{.Architecture}}")
	if err != nil {
		return "", err
	}
	switch strings.TrimSpace(result.stdout) {
	case "arm64", "aarch64":
		return "linux/arm64", nil
	case "amd64", "x86_64":
		return "linux/amd64", nil
	default:
		return "", fmt.Errorf("unsupported Docker server architecture %q", strings.TrimSpace(result.stdout))
	}
}

func verifyPinnedSourceImage(ctx context.Context, client dockerClient, name, image string) error {
	result, err := client.run(ctx, name, "image", "inspect", "--format", "{{json .RepoDigests}}", image)
	if err != nil {
		return err
	}
	_, digest, _ := strings.Cut(image, "@")
	if !strings.Contains(result.stdout, "@"+digest) {
		return errors.New("pulled Trivy source image did not retain the configured digest")
	}
	return nil
}

func currentUserID() string {
	current, err := user.Current()
	if err != nil {
		return "65532:65532"
	}
	uid, uidErr := strconv.Atoi(current.Uid)
	gid, gidErr := strconv.Atoi(current.Gid)
	if uidErr != nil || gidErr != nil || uid < 1 || gid < 0 {
		return "65532:65532"
	}
	return fmt.Sprintf("%d:%d", uid, gid)
}

func (state *scannerRuntime) cleanup() error {
	if state == nil {
		return nil
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var cleanupErrors []error
	if state.image != "" {
		if _, err := state.client.cleanup(cleanupContext, governedDockerCommandPrefix+"remove-scanner", "image", "rm", "--force", state.image); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if state.root != "" {
		if err := os.RemoveAll(state.root); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	state.root = ""
	return errors.Join(cleanupErrors...)
}

func prepareArtifact(ctx context.Context, repo repository.Repository, state *scannerRuntime, target policy.ArtifactTarget) (preparedArtifact, error) {
	switch target.Mode {
	case "archive":
		return prepareArchiveArtifact(repo, state, target)
	case "dockerfile":
		return prepareDockerfileArtifact(ctx, repo, state, target)
	case "command":
		return prepareCommandArtifact(ctx, repo, state, target)
	default:
		return preparedArtifact{}, fmt.Errorf("unsupported artifact mode %q", target.Mode)
	}
}

func prepareArchiveArtifact(repo repository.Repository, state *scannerRuntime, target policy.ArtifactTarget) (preparedArtifact, error) {
	source, err := containedRepositoryPath(repo, target.Archive)
	if err != nil {
		return preparedArtifact{}, err
	}
	directory := filepath.Join(state.root, "targets", target.Name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return preparedArtifact{}, err
	}
	destination := filepath.Join(directory, "image.tar")
	before, err := hashRegularFile(source, state.policy.MaximumArchiveBytes)
	if err != nil {
		return preparedArtifact{}, err
	}
	if err := copyBoundedRegularFile(source, destination, state.policy.MaximumArchiveBytes); err != nil {
		return preparedArtifact{}, err
	}
	after, err := hashRegularFile(source, state.policy.MaximumArchiveBytes)
	if err != nil || before != after {
		return preparedArtifact{}, errors.New("source artifact changed while Code Polishy captured its private scan copy")
	}
	openVEX, err := copyTargetOpenVEX(repo, directory, target.OpenVEX)
	if err != nil {
		return preparedArtifact{}, err
	}
	return preparedArtifact{name: target.Name, mode: target.Mode, reference: target.Name, archive: destination, openVEX: openVEX, cleanup: func() error { return nil }}, nil
}

func prepareDockerfileArtifact(ctx context.Context, repo repository.Repository, state *scannerRuntime, target policy.ArtifactTarget) (preparedArtifact, error) {
	dockerfile, err := containedRepositoryPath(repo, target.Dockerfile)
	if err != nil {
		return preparedArtifact{}, err
	}
	contextRoot, err := containedRepositoryPath(repo, target.Context)
	if err != nil {
		return preparedArtifact{}, err
	}
	if !inside(contextRoot, dockerfile) {
		return preparedArtifact{}, errors.New("artifact Dockerfile must be inside its declared build context")
	}
	tag := "code-polishy-artifact-" + state.runID + ":" + target.Name
	arguments := []string{"image", "build", "--pull=false", "--provenance=false", "--file", dockerfile, "--tag", tag}
	if target.Platform != "" {
		arguments = append(arguments, "--platform", target.Platform)
	}
	arguments = append(arguments, contextRoot)
	if _, err := state.client.run(ctx, governedDockerCommandPrefix+"target-"+target.Name+"-build", arguments...); err != nil {
		return preparedArtifact{}, err
	}
	cleanup := func() error {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := state.client.cleanup(cleanupContext, governedDockerCommandPrefix+"target-"+target.Name+"-remove", "image", "rm", "--force", tag)
		return err
	}
	imageID, err := inspectImageID(ctx, state.client, governedDockerCommandPrefix+"target-"+target.Name+"-inspect", tag)
	if err != nil {
		_ = cleanup()
		return preparedArtifact{}, err
	}
	directory := filepath.Join(state.root, "targets", target.Name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		_ = cleanup()
		return preparedArtifact{}, err
	}
	archive := filepath.Join(directory, "image.tar")
	if _, err := state.client.run(ctx, governedDockerCommandPrefix+"target-"+target.Name+"-save", "image", "save", "--output", archive, tag); err != nil {
		_ = cleanup()
		return preparedArtifact{}, err
	}
	openVEX, err := copyTargetOpenVEX(repo, directory, target.OpenVEX)
	if err != nil {
		_ = cleanup()
		return preparedArtifact{}, err
	}
	return preparedArtifact{name: target.Name, mode: target.Mode, reference: tag, imageID: imageID, archive: archive, openVEX: openVEX, cleanup: cleanup}, nil
}

func prepareCommandArtifact(ctx context.Context, repo repository.Repository, state *scannerRuntime, target policy.ArtifactTarget) (preparedArtifact, error) {
	directory := filepath.Join(state.root, "targets", target.Name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return preparedArtifact{}, err
	}
	producer := *target.Producer
	if err := runArtifactProducer(ctx, repo, state.client, target.Name, directory, producer); err != nil {
		return preparedArtifact{}, err
	}
	manifest, archive, err := readProducerManifest(directory, producer.Manifest, state.policy.MaximumArchiveBytes)
	if err != nil {
		return preparedArtifact{}, err
	}
	openVEX, err := copyTargetOpenVEX(repo, directory, target.OpenVEX)
	if err != nil {
		return preparedArtifact{}, err
	}
	return preparedArtifact{name: target.Name, mode: target.Mode, reference: manifest.Reference, imageID: manifest.ImageID, archive: archive, openVEX: openVEX, cleanup: func() error { return nil }}, nil
}

func runArtifactProducer(ctx context.Context, repo repository.Repository, client dockerClient, targetName, directory string, producer policy.ArtifactProducer) error {
	if _, err := containedRepositoryPath(repo, producer.Cwd); err != nil {
		return err
	}
	command := policy.Command{
		Name: "artifact-security-producer-" + targetName, Argv: append([]string{}, producer.Argv...), Cwd: producer.Cwd,
		Environment:        append(append([]string{}, producer.Environment...), "CODE_POLISHY_ARTIFACT_OUTPUT="+directory),
		ExclusiveResources: []string{"artifact-security"}, TimeoutSeconds: producer.TimeoutSeconds,
	}
	if err := client.executor.Run(ctx, repo.Root, command); err != nil {
		return fmt.Errorf("artifact producer %s: %w", targetName, err)
	}
	return nil
}

func readProducerManifest(directory, relative string, maximumArchiveBytes int64) (producerManifest, string, error) {
	manifestPath, err := containedPrivatePath(directory, relative)
	if err != nil {
		return producerManifest{}, "", err
	}
	manifestBytes, err := readBoundedRegularFile(manifestPath, 64*1024)
	if err != nil {
		return producerManifest{}, "", err
	}
	var manifest producerManifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return producerManifest{}, "", fmt.Errorf("decode artifact producer manifest: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return producerManifest{}, "", fmt.Errorf("decode artifact producer manifest: %w", err)
	}
	if manifest.Version != 1 || manifest.Archive == "" || manifest.Reference == "" {
		return producerManifest{}, "", errors.New("artifact producer manifest must be version 1 with archive and reference")
	}
	archive, err := containedPrivatePath(directory, manifest.Archive)
	if err != nil {
		return producerManifest{}, "", err
	}
	if _, err := hashRegularFile(archive, maximumArchiveBytes); err != nil {
		return producerManifest{}, "", err
	}
	return manifest, archive, nil
}

func containedPrivatePath(root, relative string) (string, error) {
	if filepath.IsAbs(filepath.FromSlash(relative)) || strings.Contains(relative, "\\") {
		return "", errors.New("artifact producer path must be portable and relative")
	}
	for _, part := range strings.Split(relative, "/") {
		if part == ".." {
			return "", errors.New("artifact producer path escapes its private output directory")
		}
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(resolvedRoot, filepath.FromSlash(relative)))
	if err != nil || !inside(resolvedRoot, resolved) {
		return "", errors.New("artifact producer path is missing or resolves outside its private output directory")
	}
	return resolved, nil
}

func copyTargetOpenVEX(repo repository.Repository, directory, relative string) (string, error) {
	if relative == "" {
		return "", nil
	}
	source, err := containedRepositoryPath(repo, relative)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(directory, "assessment.openvex.json")
	if err := copyBoundedRegularFile(source, destination, 64*1024); err != nil {
		return "", err
	}
	return destination, nil
}

func copyBoundedRegularFile(source, destination string, maximum int64) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return fmt.Errorf("artifact input must be a regular file no larger than %d bytes", maximum)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, maximum+1))
	closeErr := output.Close()
	if written > maximum {
		_ = os.Remove(destination)
		return fmt.Errorf("artifact input grew beyond the %d-byte limit while being copied", maximum)
	}
	return errors.Join(copyErr, closeErr)
}

func inspectImageID(ctx context.Context, client dockerClient, name, image string) (string, error) {
	result, err := client.run(ctx, name, "image", "inspect", "--format", "{{.Id}}", image)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(result.stdout)
	if !strings.HasPrefix(value, "sha256:") || !exactDigest.MatchString(strings.TrimPrefix(value, "sha256:")) {
		return "", fmt.Errorf("docker image %s has invalid ID %q", image, value)
	}
	return value, nil
}

func (state *scannerRuntime) runScanner(ctx context.Context, operation, network string, mounts []scannerMount, arguments []string) (commandResult, error) {
	if err := state.validateScannerNetwork(network, mounts); err != nil {
		return commandResult{}, err
	}
	suffix, err := randomHex(4)
	if err != nil {
		return commandResult{}, err
	}
	name := "code-polishy-artifact-scan-" + state.runID + "-" + suffix
	create := []string{
		"container", "create", "--name", name, "--pull", "never", "--platform", state.platform,
		"--network", network, "--read-only", "--user", state.user,
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "256",
		"--memory", "2g", "--cpus", "2", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=1073741824,mode=1777",
		"--env", "HOME=/tmp", "--label", "com.code-polishy.security.role=scanner",
		"--label", "com.code-polishy.security.run=" + state.runID,
	}
	create, err = state.appendScannerMounts(create, mounts)
	if err != nil {
		return commandResult{}, err
	}
	create = append(create, state.image)
	create = append(create, arguments...)
	if _, err := state.client.run(ctx, governedDockerCommandPrefix+operation+"-create", create...); err != nil {
		_, _ = state.client.cleanup(context.Background(), governedDockerCommandPrefix+operation+"-remove", "container", "rm", "--force", name)
		return commandResult{}, err
	}
	result, runErr := state.client.run(ctx, governedDockerCommandPrefix+operation+"-start", "container", "start", "--attach", name)
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, removeErr := state.client.cleanup(cleanupContext, governedDockerCommandPrefix+operation+"-remove", "container", "rm", "--force", name)
	return result, errors.Join(runErr, removeErr)
}

func (state *scannerRuntime) validateScannerNetwork(network string, mounts []scannerMount) error {
	if network != "none" && network != "bridge" {
		return fmt.Errorf("unsupported scanner network %q", network)
	}
	if network != "bridge" {
		return nil
	}
	if len(mounts) != 1 {
		return errors.New("networked scanner may receive only the private database cache")
	}
	mount := mounts[0]
	if mount.source != state.cache || mount.target != "/cache" || mount.readOnly {
		return errors.New("networked scanner may receive only the private database cache")
	}
	return nil
}

func (state *scannerRuntime) appendScannerMounts(arguments []string, mounts []scannerMount) ([]string, error) {
	for _, mount := range mounts {
		argument, err := state.mountArgument(mount)
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, "--mount", argument)
	}
	return arguments, nil
}

type scannerMount struct {
	source   string
	target   string
	readOnly bool
}

func (state *scannerRuntime) mountArgument(mount scannerMount) (string, error) {
	if !inside(state.root, mount.source) || strings.ContainsAny(mount.source+mount.target, ",\r\n") || !filepath.IsAbs(mount.source) {
		return "", errors.New("scanner mount escapes the private runtime workspace")
	}
	allowed := map[string]bool{
		"/cache": false, "/input/image.tar": true, "/output": false,
		"/policy/assessment.openvex.json": true,
	}
	wantReadOnly, exists := allowed[mount.target]
	if !exists || wantReadOnly != mount.readOnly {
		return "", fmt.Errorf("unsupported scanner mount target %q", mount.target)
	}
	argument := "type=bind,src=" + mount.source + ",dst=" + mount.target
	if mount.readOnly {
		argument += ",readonly"
	}
	return argument, nil
}

type databaseEvidence struct {
	SHA256         string
	MetadataSHA256 string
	UpdatedAt      time.Time
	DownloadedAt   time.Time
}

type trivyDatabaseMetadata struct {
	Version      int       `json:"Version"`
	NextUpdate   time.Time `json:"NextUpdate"`
	UpdatedAt    time.Time `json:"UpdatedAt"`
	DownloadedAt time.Time `json:"DownloadedAt"`
}

func (state *scannerRuntime) updateDatabase(ctx context.Context) (databaseEvidence, error) {
	_, err := state.runScanner(ctx, "database-download", "bridge", []scannerMount{{source: state.cache, target: "/cache"}}, []string{
		"image", "--cache-dir", "/cache", "--db-repository", state.policy.DatabaseRepository,
		"--download-db-only", "--disable-telemetry", "--skip-version-check", "--no-progress", "--quiet",
	})
	if err != nil {
		return databaseEvidence{}, fmt.Errorf("download Trivy database: %w", err)
	}
	return state.inspectDatabase(time.Now().UTC(), true)
}

func (state *scannerRuntime) inspectDatabase(now time.Time, requireRecentDownload bool) (databaseEvidence, error) {
	databasePath := filepath.Join(state.cache, "db", "trivy.db")
	metadataPath := filepath.Join(state.cache, "db", "metadata.json")
	payload, err := readBoundedRegularFile(metadataPath, metadataMaximumBytes)
	if err != nil {
		return databaseEvidence{}, err
	}
	var metadata trivyDatabaseMetadata
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return databaseEvidence{}, err
	}
	if err := state.validateDatabaseMetadata(metadata, now, requireRecentDownload); err != nil {
		return databaseEvidence{}, err
	}
	databaseHash, err := hashRegularFile(databasePath, 2*1024*1024*1024)
	if err != nil {
		return databaseEvidence{}, err
	}
	return databaseEvidence{SHA256: databaseHash, MetadataSHA256: bytesSHA256(payload), UpdatedAt: metadata.UpdatedAt, DownloadedAt: metadata.DownloadedAt}, nil
}

func (state *scannerRuntime) validateDatabaseMetadata(metadata trivyDatabaseMetadata, now time.Time, requireRecentDownload bool) error {
	maximumAge := time.Duration(state.policy.DatabaseMaximumAgeHours) * time.Hour
	if metadata.Version != state.policy.DatabaseSchemaVersion {
		return errors.New("trivy database schema version is inconsistent")
	}
	if !timestampWithin(metadata.UpdatedAt, now.Add(-maximumAge), now.Add(5*time.Minute)) {
		return errors.New("trivy database update timestamp is stale or inconsistent")
	}
	if !timestampWithin(metadata.DownloadedAt, time.Time{}, now.Add(5*time.Minute)) {
		return errors.New("trivy database download timestamp is inconsistent")
	}
	if metadata.NextUpdate.IsZero() {
		return errors.New("trivy database next-update timestamp is missing")
	}
	if requireRecentDownload && metadata.DownloadedAt.Before(now.Add(-time.Hour)) {
		return errors.New("trivy database was not downloaded during this run")
	}
	return nil
}

func timestampWithin(value, earliest, latest time.Time) bool {
	if value.IsZero() || value.After(latest) {
		return false
	}
	return earliest.IsZero() || !value.Before(earliest)
}

func databaseChangedError(before, after databaseEvidence) error {
	if before == after {
		return nil
	}
	return errors.New("trivy database changed during offline artifact scans")
}
