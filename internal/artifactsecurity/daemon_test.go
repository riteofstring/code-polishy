package artifactsecurity

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func (boundary *scannerBoundary) dockerEndpoint() string {
	if boundary.endpoint != "" {
		return boundary.endpoint
	}
	return "unix:///var/run/docker.sock"
}

func (boundary *scannerBoundary) securityOptions() string {
	if boundary.rootless {
		return `["name=seccomp,profile=builtin","name=rootless","name=cgroupns"]`
	}
	return `["name=seccomp,profile=builtin","name=cgroupns"]`
}

func daemonTestRepository(t *testing.T) repository.Repository {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "agent.tar"), []byte("target image"), 0o600); err != nil {
		t.Fatal(err)
	}
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return repository.Repository{
		Root: root, PolicyRoot: policyRoot,
		Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{
			OutputDirectory: ".code-polishy-reports/artifact-security",
			Targets:         []policy.ArtifactTarget{{Name: "agent", Mode: "archive", Archive: "agent.tar"}},
		}}},
	}
}

func TestRootlessDaemonRunsTheScannerAsTheCallingUserWithItsRestrictions(t *testing.T) {
	repo := daemonTestRepository(t)
	boundary := newScannerBoundary(t, repo.PolicyRoot)
	boundary.rootless = true
	result := Run(t.Context(), repo, boundary)
	if len(result.Findings) != 0 || !strings.Contains(result.Notes[0], "(rootless), scanner user 0:0") {
		t.Fatalf("rootless scan = %+v", result)
	}
	if len(boundary.containers) == 0 {
		t.Fatal("rootless scan created no scanner containers")
	}
	for _, arguments := range boundary.containers {
		if user, _ := flagValue(arguments, "--user"); user != "0:0" {
			t.Fatalf("scanner identity on rootless daemon = %q", user)
		}
		for _, restriction := range []string{"--read-only", "no-new-privileges", "ALL"} {
			if !slices.Contains(arguments, restriction) {
				t.Fatalf("rootless scanner lost %s: %v", restriction, arguments)
			}
		}
	}
}

func TestRootfulDaemonKeepsTheNonRootCallerIdentity(t *testing.T) {
	repo := daemonTestRepository(t)
	boundary := newScannerBoundary(t, repo.PolicyRoot)
	if result := Run(t.Context(), repo, boundary); len(result.Findings) != 0 {
		t.Fatalf("rootful scan = %+v", result)
	}
	for _, arguments := range boundary.containers {
		if user, _ := flagValue(arguments, "--user"); user != currentUserID() || user == "0:0" {
			t.Fatalf("scanner identity on rootful daemon = %q", user)
		}
	}
}

func TestPermissionFailureNamesTheDaemonIdentityAndMounts(t *testing.T) {
	repo := daemonTestRepository(t)
	boundary := newScannerBoundary(t, repo.PolicyRoot)
	boundary.rootless = true
	boundary.databaseError = errors.New("mkdir /cache/db: permission denied")
	result := Run(t.Context(), repo, boundary)
	if len(result.Findings) != 1 {
		t.Fatalf("database failure = %+v", result)
	}
	finding := policy.NormalizeFinding(result.Findings[0])
	for _, expected := range []string{"phase vulnerability-database", "unix:///var/run/docker.sock", "rootless", "scanner user 0:0", "/cache"} {
		if !strings.Contains(finding.Message, expected) {
			t.Errorf("message omitted %q: %s", expected, finding.Message)
		}
	}
	for _, expected := range []string{"ran as 0:0", "rootless", "DOCKER_CONTEXT"} {
		if !strings.Contains(finding.Remediation.Summary, expected) {
			t.Errorf("remedy omitted %q: %s", expected, finding.Remediation.Summary)
		}
	}
	if strings.Contains(finding.Remediation.Summary, "declared artifact") {
		t.Fatalf("scanner failure blamed the artifact: %s", finding.Remediation.Summary)
	}
}

func TestArtifactFailureKeepsTheArtifactRemedy(t *testing.T) {
	repo := daemonTestRepository(t)
	repo.Config.SupplyChain.ArtifactSecurity.Targets[0].Archive = "missing.tar"
	result := Run(t.Context(), repo, newScannerBoundary(t, repo.PolicyRoot))
	if len(result.Findings) != 1 || result.Findings[0].Subject != "agent:prepare" {
		t.Fatalf("missing artifact = %+v", result)
	}
	finding := policy.NormalizeFinding(result.Findings[0])
	if !strings.Contains(finding.Remediation.Summary, "Restore the declared artifact") || !strings.Contains(finding.Message, "phase prepare") {
		t.Fatalf("artifact failure diagnostic = %+v", finding)
	}
}

func TestRemoteEndpointFailsBeforeContactingTheDaemon(t *testing.T) {
	repo := daemonTestRepository(t)
	boundary := newScannerBoundary(t, repo.PolicyRoot)
	boundary.endpoint = "ssh://builder@remote.example"
	result := Run(t.Context(), repo, boundary)
	if len(result.Findings) != 1 || len(boundary.containers) != 0 {
		t.Fatalf("remote endpoint = %+v containers=%v", result, boundary.containers)
	}
	finding := policy.NormalizeFinding(result.Findings[0])
	if !strings.Contains(finding.Message, "ssh://builder@remote.example") || !strings.Contains(finding.Remediation.Summary, "DOCKER_HOST") {
		t.Fatalf("remote endpoint diagnostic = %+v", finding)
	}
}

func TestLocalDockerEndpointClassification(t *testing.T) {
	t.Parallel()
	for endpoint, local := range map[string]bool{
		"unix:///run/user/1000/docker.sock": true,
		"npipe:////./pipe/docker_engine":    true,
		"tcp://127.0.0.1:2375":              true,
		"tcp://[::1]:2375":                  true,
		"tcp://localhost:2375":              true,
		"tcp://10.0.0.8:2376":               false,
		"tcp://docker:2376":                 false,
		"ssh://user@host":                   false,
		"":                                  false,
	} {
		if _, err := localDockerEndpoint(endpoint); (err == nil) != local {
			t.Errorf("localDockerEndpoint(%q) = %v", endpoint, err)
		}
	}
	if _, err := localDockerEndpoint("tcp://user:secret@remote:2376"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("endpoint credentials were not redacted: %v", err)
	}
}

func TestParseDockerInfo(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		output   string
		platform string
		rootless bool
		failure  bool
	}{
		{output: `aarch64 ["name=rootless"]` + "\n", platform: "linux/arm64", rootless: true},
		{output: `x86_64 null`, platform: "linux/amd64"},
		{output: `amd64 ["name=rootlessish"]`, platform: "linux/amd64"},
		{output: `riscv64 []`, failure: true},
		{output: `amd64`, failure: true},
	} {
		platform, rootless, err := parseDockerInfo(testCase.output)
		if platform != testCase.platform || rootless != testCase.rootless || (err != nil) != testCase.failure {
			t.Errorf("parseDockerInfo(%q) = %q, %t, %v", testCase.output, platform, rootless, err)
		}
	}
}

func TestDockerCommandsReceiveOnlyClientSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell Docker stub")
	}
	directory := t.TempDir()
	stub := "#!/bin/sh\nprintf '%s|%s|%s' \"$DOCKER_HOST\" \"$DOCKER_CONTEXT\" \"$UNRELATED_SECRET\"\n"
	if err := os.WriteFile(filepath.Join(directory, "docker"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_HOST", "unix:///run/selected.sock")
	t.Setenv("DOCKER_CONTEXT", "selected")
	t.Setenv("UNRELATED_SECRET", "leaked")
	client := dockerClient{root: directory, executor: runner.OSRunner{}}
	result, err := client.run(t.Context(), governedDockerCommandPrefix+"preflight", "version")
	if err != nil {
		t.Fatal(err)
	}
	if result.stdout != "unix:///run/selected.sock|selected|" {
		t.Fatalf("docker environment = %q", result.stdout)
	}
}
