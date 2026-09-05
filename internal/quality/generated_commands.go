package quality

import (
	"fmt"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func generatedStyleCommandProblem(repo repository.Repository, command policy.Command, files []string) string {
	if command.Adapter != nil || !hasStyleCapability(command) || command.PassFiles && styleOnlyCommand(command) {
		return ""
	}
	for _, path := range files {
		if !repo.IsGenerated(path) || !commandOwnsPath(repo, command, path) {
			continue
		}
		return fmt.Sprintf("configured check %q cannot separate style-exempt generated output %q from its style operation; declare separate style and non-style providers with bounded file inputs", command.Name, path)
	}
	return ""
}

func commandOwnsPath(repo repository.Repository, command policy.Command, path string) bool {
	return len(command.Paths) == 0 && len(command.Modules) == 0 || pathMatchesCommand(repo, path, command)
}

func hasStyleCapability(command policy.Command) bool {
	return slices.Contains(command.Provides, "format") || slices.Contains(command.Provides, "complexity")
}

func styleOnlyCommand(command policy.Command) bool {
	if len(command.Provides) == 0 {
		return false
	}
	for _, capability := range command.Provides {
		if capability != "format" && capability != "complexity" {
			return false
		}
	}
	return true
}

func generatedStyleCommandFinding(command policy.Command, problem string) policy.Finding {
	return policy.Finding{
		Check: "policy.generatedStyleCoverage", Path: policy.ConfigFilename, Subject: command.Name, Message: problem,
		Remediation: policy.FindingRemediation{Summary: "Separate style from semantic and security checks. Use a capability-specific pack adapter or an exact managed file-list provider so generated outputs remain covered by non-style analysis."},
	}
}

func generatedStyleCoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, command := range repo.Config.Checks {
		if problem := generatedStyleCommandProblem(repo, command, files); problem != "" {
			findings = append(findings, generatedStyleCommandFinding(command, problem))
		}
	}
	return findings
}

func generatedStyleExecutionFinding(repo repository.Repository, command policy.Command) *policy.Finding {
	if command.Adapter != nil || !hasStyleCapability(command) || command.PassFiles && styleOnlyCommand(command) {
		return nil
	}
	files, err := repo.AllFiles()
	if err != nil {
		finding := generatedStyleCommandFinding(command, "cannot inspect generated source before the style operation: "+err.Error())
		return &finding
	}
	if problem := generatedStyleCommandProblem(repo, command, files); problem != "" {
		finding := generatedStyleCommandFinding(command, problem)
		return &finding
	}
	return nil
}
