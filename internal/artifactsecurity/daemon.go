package artifactsecurity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

var (
	dockerClientEnvironment = []string{"DOCKER_CERT_PATH", "DOCKER_CONFIG", "DOCKER_CONTEXT", "DOCKER_HOST", "DOCKER_TLS_VERIFY"}
	errRemoteDockerEndpoint = errors.New("artifact security requires a Docker daemon on this machine because scans bind-mount private local files")
)

type dockerDaemon struct {
	endpoint string
	rootless bool
	platform string
}

func (daemon dockerDaemon) scannerUser() string {
	if daemon.rootless {
		return "0:0"
	}
	return currentUserID()
}

func (daemon dockerDaemon) mode() string {
	if daemon.rootless {
		return "rootless"
	}
	return "rootful"
}

func inspectDockerDaemon(ctx context.Context, client dockerClient) (dockerDaemon, error) {
	result, err := client.run(ctx, governedDockerCommandPrefix+"endpoint", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	if err != nil {
		return dockerDaemon{}, fmt.Errorf("resolve Docker endpoint: %w", err)
	}
	endpoint, err := localDockerEndpoint(strings.TrimSpace(result.stdout))
	if err != nil {
		return dockerDaemon{}, err
	}
	daemon := dockerDaemon{endpoint: endpoint}
	if _, err := client.run(ctx, governedDockerCommandPrefix+"preflight", "version", "--format", "{{.Server.Version}}"); err != nil {
		return daemon, fmt.Errorf("docker server preflight: %w", err)
	}
	info, err := client.run(ctx, governedDockerCommandPrefix+"platform", "info", "--format", "{{.Architecture}} {{json .SecurityOptions}}")
	if err != nil {
		return daemon, err
	}
	daemon.platform, daemon.rootless, err = parseDockerInfo(info.stdout)
	return daemon, err
}

func localDockerEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("%w; the selected endpoint is not a valid URL", errRemoteDockerEndpoint)
	}
	display := parsed.Redacted()
	switch parsed.Scheme {
	case "unix", "npipe":
		return display, nil
	case "tcp", "http", "https":
		address := net.ParseIP(parsed.Hostname())
		if parsed.Hostname() == "localhost" || address != nil && address.IsLoopback() {
			return display, nil
		}
	}
	return "", fmt.Errorf("%w; the selected endpoint is %s", errRemoteDockerEndpoint, display)
}

func parseDockerInfo(output string) (string, bool, error) {
	architecture, securityOptions, _ := strings.Cut(strings.TrimSpace(output), " ")
	var options []string
	if err := json.Unmarshal([]byte(securityOptions), &options); err != nil {
		return "", false, fmt.Errorf("decode Docker security options: %w", err)
	}
	switch architecture {
	case "arm64", "aarch64":
		return "linux/arm64", slices.Contains(options, "name=rootless"), nil
	case "amd64", "x86_64":
		return "linux/amd64", slices.Contains(options, "name=rootless"), nil
	default:
		return "", false, fmt.Errorf("unsupported Docker server architecture %q", architecture)
	}
}

type operationContext struct {
	daemon    dockerDaemon
	user      string
	workspace string
}

func (operation operationContext) describe(phase string) string {
	if operation.daemon.endpoint == "" {
		return "phase " + phase
	}
	description := fmt.Sprintf("phase %s; Docker daemon %s, %s", phase, operation.daemon.endpoint, operation.daemon.mode())
	if operation.workspace == "" {
		return description
	}
	return fmt.Sprintf("%s; scanner user %s; private mounts from %s at /cache, /input/image.tar, and /output", description, operation.user, operation.workspace)
}

func (operation operationContext) remedy(phase string, err error) string {
	switch {
	case errors.Is(err, errRemoteDockerEndpoint):
		return "Select a Docker daemon on this machine with DOCKER_HOST or DOCKER_CONTEXT; a remote daemon cannot read the scanner's private bind mounts."
	case phase == "scanner-policy":
		return "Reinstall the exact locked Code Polishy release so its scanner policy, Dockerfile, and OpenVEX statements are intact; repository configuration cannot repair them."
	case phase == "docker-daemon":
		return "Start the selected Docker daemon, or grant this user access to it, or select a reachable local daemon with DOCKER_HOST or DOCKER_CONTEXT. Confirm that docker version reports a server, then rerun artifact-security."
	case err != nil && strings.Contains(err.Error(), "permission denied"):
		return fmt.Sprintf("The scanner ran as %s on a %s daemon and could not use its private mounts. Confirm that this identity maps to the calling user on the selected daemon and that TMPDIR is on a filesystem the daemon can access, or select another local daemon with DOCKER_HOST or DOCKER_CONTEXT.", operation.user, operation.daemon.mode())
	case phase == "vulnerability-database":
		return "Confirm that containers on the selected Docker daemon can reach the scanner's database registry over HTTPS, then rerun artifact-security; do not reuse a stale database."
	case phase == "scanner-runtime" || phase == "self-scan" || phase == "database-immutability" || phase == "cleanup":
		return "Resolve the reported Docker or scanner failure on the selected daemon, then rerun artifact-security; the locked release owns the scanner image, its limits, and its OpenVEX statements."
	}
	return ""
}

func artifactOperationalFinding(operation operationContext, target, phase string, err error) policy.Finding {
	message := "artifact security operation failed"
	if err != nil {
		message = err.Error()
	}
	return policy.Finding{
		Check: "artifactSecurity.operation", Path: policy.ConfigFilename, Subject: target + ":" + phase,
		Message:     message + " (" + operation.describe(phase) + ")",
		Remediation: policy.FindingRemediation{Summary: operation.remedy(phase, err)},
	}
}
