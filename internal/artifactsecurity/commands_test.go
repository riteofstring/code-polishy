package artifactsecurity

import (
	"context"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPlannedCommandsDescribeEveryGovernedArtifactProcess(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: policy.Config{SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{Targets: []policy.ArtifactTarget{
		{Name: "archive", Mode: "archive"},
		{Name: "dockerfile", Mode: "dockerfile", OpenVEX: "security/openvex.json"},
		{Name: "producer", Mode: "command", Producer: &policy.ArtifactProducer{Argv: []string{"./build-artifact.sh"}, Cwd: ".", Environment: []string{"MODE=release"}, TimeoutSeconds: 61}},
	}}}}}
	commands := PlannedCommands(repo)
	wantNames := []string{
		"artifact-security-docker-preflight", "artifact-security-docker-platform", "artifact-security-docker-pull-source", "artifact-security-docker-verify-source", "artifact-security-docker-build-scanner",
		"artifact-security-docker-scanner-version-create", "artifact-security-docker-scanner-version-start", "artifact-security-docker-scanner-version-remove",
		"artifact-security-docker-database-download-create", "artifact-security-docker-database-download-start", "artifact-security-docker-database-download-remove",
		"artifact-security-docker-save-scanner", "artifact-security-docker-inspect-scanner",
		"artifact-security-docker-scanner-self-observed-create", "artifact-security-docker-scanner-self-observed-start", "artifact-security-docker-scanner-self-observed-remove",
		"artifact-security-docker-scanner-self-enforced-create", "artifact-security-docker-scanner-self-enforced-start", "artifact-security-docker-scanner-self-enforced-remove",
		"artifact-security-docker-scanner-self-sbom-create", "artifact-security-docker-scanner-self-sbom-start", "artifact-security-docker-scanner-self-sbom-remove",
		"artifact-security-docker-target-archive-observed-create", "artifact-security-docker-target-archive-observed-start", "artifact-security-docker-target-archive-observed-remove",
		"artifact-security-docker-target-archive-sbom-create", "artifact-security-docker-target-archive-sbom-start", "artifact-security-docker-target-archive-sbom-remove",
		"artifact-security-docker-target-dockerfile-build", "artifact-security-docker-target-dockerfile-inspect", "artifact-security-docker-target-dockerfile-save",
		"artifact-security-docker-target-dockerfile-observed-create", "artifact-security-docker-target-dockerfile-observed-start", "artifact-security-docker-target-dockerfile-observed-remove",
		"artifact-security-docker-target-dockerfile-enforced-create", "artifact-security-docker-target-dockerfile-enforced-start", "artifact-security-docker-target-dockerfile-enforced-remove",
		"artifact-security-docker-target-dockerfile-sbom-create", "artifact-security-docker-target-dockerfile-sbom-start", "artifact-security-docker-target-dockerfile-sbom-remove",
		"artifact-security-docker-target-dockerfile-remove",
		"artifact-security-producer-producer",
		"artifact-security-docker-target-producer-observed-create", "artifact-security-docker-target-producer-observed-start", "artifact-security-docker-target-producer-observed-remove",
		"artifact-security-docker-target-producer-sbom-create", "artifact-security-docker-target-producer-sbom-start", "artifact-security-docker-target-producer-sbom-remove",
		"artifact-security-docker-remove-scanner",
	}
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Name)
		if !slices.Equal(command.ExclusiveResources, []string{"artifact-security"}) || command.TimeoutSeconds <= 0 {
			t.Fatalf("ungoverned artifact command = %+v", command)
		}
	}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("planned command names = %v", names)
	}
	producer := policy.Command{}
	for _, command := range commands {
		if command.Name == "artifact-security-producer-producer" {
			producer = command
			break
		}
	}
	if !slices.Equal(producer.Environment, []string{"MODE=release", "CODE_POLISHY_ARTIFACT_OUTPUT=${artifact-output}"}) {
		t.Fatalf("producer plan environment = %v", producer.Environment)
	}
}

func TestArtifactProcessesUseTheGovernedRunner(t *testing.T) {
	t.Parallel()
	commandRunner := &recordingArtifactRunner{output: runner.Output{Stdout: []byte("amd64\n")}}
	client := dockerClient{root: t.TempDir(), executor: commandRunner}
	result, err := client.run(context.Background(), governedDockerCommandPrefix+"platform", "info", "--format", "{{.Architecture}}")
	if err != nil || result.stdout != "amd64\n" {
		t.Fatalf("docker result=%+v error=%v", result, err)
	}
	if len(commandRunner.commands) != 1 || commandRunner.commands[0].Name != governedDockerCommandPrefix+"platform" ||
		!slices.Equal(commandRunner.commands[0].Argv, []string{"docker", "info", "--format", "{{.Architecture}}"}) {
		t.Fatalf("docker did not use the governed command: %+v", commandRunner.commands)
	}

	repo := repository.Repository{Root: t.TempDir()}
	producer := policy.ArtifactProducer{Argv: []string{"./produce.sh"}, Cwd: ".", Environment: []string{"MODE=release"}, TimeoutSeconds: 61}
	if err := runArtifactProducer(context.Background(), repo, client, "image", t.TempDir(), producer); err != nil {
		t.Fatal(err)
	}
	if len(commandRunner.commands) != 2 {
		t.Fatalf("governed commands = %+v", commandRunner.commands)
	}
	actual := commandRunner.commands[1]
	if actual.Name != "artifact-security-producer-image" || !slices.Equal(actual.Argv, producer.Argv) ||
		!slices.Equal(actual.ExclusiveResources, []string{"artifact-security"}) || actual.TimeoutSeconds != producer.TimeoutSeconds ||
		len(actual.Environment) != 2 || actual.Environment[0] != "MODE=release" || actual.Environment[1] == "CODE_POLISHY_ARTIFACT_OUTPUT=" {
		t.Fatalf("producer did not use the governed command: %+v", actual)
	}
}

type recordingArtifactRunner struct {
	commands []policy.Command
	output   runner.Output
}

func (commandRunner *recordingArtifactRunner) Run(_ context.Context, _ string, command policy.Command) error {
	commandRunner.commands = append(commandRunner.commands, command)
	return nil
}

func (commandRunner *recordingArtifactRunner) RunWithOutput(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	commandRunner.commands = append(commandRunner.commands, command)
	return runner.Result{ExitStatus: 0}, commandRunner.output, nil
}
