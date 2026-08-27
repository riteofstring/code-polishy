package artifactsecurity

import (
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const governedDockerCommandPrefix = "artifact-security-docker-"

// PlannedCommands describes every external process on the successful artifact
// security path before a merge gate starts. Docker arguments include private
// runtime paths and random container names, so the frozen plan owns the stable
// operation identity while the receipt records the exact invocation.
func PlannedCommands(repo repository.Repository) []policy.Command {
	targets := repo.Config.SupplyChain.ArtifactSecurity.Targets
	if len(targets) == 0 {
		return nil
	}
	commands := []policy.Command{
		plannedDockerCommand("preflight"),
		plannedDockerCommand("platform"),
		plannedDockerCommand("pull-source"),
		plannedDockerCommand("verify-source"),
		plannedDockerCommand("build-scanner"),
	}
	commands = appendScannerCommands(commands, "scanner-version")
	commands = appendScannerCommands(commands, "database-download")
	commands = append(commands,
		plannedDockerCommand("save-scanner"),
		plannedDockerCommand("inspect-scanner"),
	)
	commands = appendScannerCommands(commands, "scanner-self-observed")
	commands = appendScannerCommands(commands, "scanner-self-enforced")
	commands = appendScannerCommands(commands, "scanner-self-sbom")
	for _, target := range targets {
		prefix := "target-" + target.Name + "-"
		switch target.Mode {
		case "dockerfile":
			commands = append(commands,
				plannedDockerCommand(prefix+"build"),
				plannedDockerCommand(prefix+"inspect"),
				plannedDockerCommand(prefix+"save"),
			)
		case "command":
			producer := *target.Producer
			commands = append(commands, policy.Command{
				Name: "artifact-security-producer-" + target.Name, Argv: append([]string{}, producer.Argv...), Cwd: producer.Cwd,
				Environment:        append(append([]string{}, producer.Environment...), "CODE_POLISHY_ARTIFACT_OUTPUT=${artifact-output}"),
				ExclusiveResources: []string{"artifact-security"}, TimeoutSeconds: producer.TimeoutSeconds,
			})
		}
		commands = appendScannerCommands(commands, prefix+"observed")
		if target.OpenVEX != "" {
			commands = appendScannerCommands(commands, prefix+"enforced")
		}
		commands = appendScannerCommands(commands, prefix+"sbom")
		if target.Mode == "dockerfile" {
			commands = append(commands, plannedDockerCleanupCommand(prefix+"remove"))
		}
	}
	return append(commands, plannedDockerCleanupCommand("remove-scanner"))
}

func appendScannerCommands(commands []policy.Command, operation string) []policy.Command {
	return append(commands,
		plannedDockerCommand(operation+"-create"),
		plannedDockerCommand(operation+"-start"),
		plannedDockerCleanupCommand(operation+"-remove"),
	)
}

func plannedDockerCommand(operation string) policy.Command {
	return dockerCommand(governedDockerCommandPrefix + operation)
}

func plannedDockerCleanupCommand(operation string) policy.Command {
	return dockerCleanupCommand(governedDockerCommandPrefix + operation)
}

// MatchesPlannedCommand permits the private paths and unpredictable container
// names that are deliberately created after the frozen plan. Every other
// governed command continues to require an exact identity match.
func MatchesPlannedCommand(expected, actual policy.Command) bool {
	if strings.HasPrefix(expected.Name, governedDockerCommandPrefix) {
		return matchesPlannedDockerCommand(expected, actual)
	}
	return matchesPlannedProducerCommand(expected, actual)
}

func matchesPlannedDockerCommand(expected, actual policy.Command) bool {
	if !samePlannedArtifactCommandBoundary(expected, actual) || !expectedDockerPlaceholder(expected) || !actualDockerCommand(actual) {
		return false
	}
	return len(expected.Environment) == 0 && len(actual.Environment) == 0 && artifactSecurityResource(expected) && artifactSecurityResource(actual)
}

func expectedDockerPlaceholder(command policy.Command) bool {
	return len(command.Argv) == 1 && command.Argv[0] == "docker"
}

func actualDockerCommand(command policy.Command) bool {
	return len(command.Argv) > 1 && command.Argv[0] == "docker"
}

func artifactSecurityResource(command policy.Command) bool {
	return len(command.ExclusiveResources) == 1 && command.ExclusiveResources[0] == "artifact-security"
}

func matchesPlannedProducerCommand(expected, actual policy.Command) bool {
	if !strings.HasPrefix(expected.Name, "artifact-security-producer-") || !samePlannedArtifactCommandBoundary(expected, actual) ||
		!sameStrings(expected.Argv, actual.Argv) || !sameStrings(expected.ExclusiveResources, actual.ExclusiveResources) ||
		len(expected.Environment) == 0 || len(expected.Environment) != len(actual.Environment) {
		return false
	}
	last := len(expected.Environment) - 1
	return sameStrings(expected.Environment[:last], actual.Environment[:last]) && expected.Environment[last] == "CODE_POLISHY_ARTIFACT_OUTPUT=${artifact-output}" &&
		strings.HasPrefix(actual.Environment[last], "CODE_POLISHY_ARTIFACT_OUTPUT=") && strings.TrimPrefix(actual.Environment[last], "CODE_POLISHY_ARTIFACT_OUTPUT=") != ""
}

func samePlannedArtifactCommandBoundary(expected, actual policy.Command) bool {
	return expected.Name == actual.Name && expected.Cwd == actual.Cwd && expected.TimeoutSeconds == actual.TimeoutSeconds && expected.SealedEnvironment == actual.SealedEnvironment
}

// IsCleanupCommand identifies the transaction-preserving cleanup operations
// that may run after a failed preparation step.
func IsCleanupCommand(command policy.Command) bool {
	return strings.HasPrefix(command.Name, governedDockerCommandPrefix) && strings.Contains(command.Name, "-remove")
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
