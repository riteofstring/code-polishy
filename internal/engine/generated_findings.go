package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func (engine *Engine) enrichGeneratedReport(report *Report) {
	if !engine.reportHasGeneratedFinding(*report) {
		return
	}
	files, err := engine.Repository.AllFiles()
	if err != nil {
		return
	}
	inventory := engine.Repository.InspectGeneration(files)
	remediations := engine.generationRemediations(inventory)
	for index := range report.Findings {
		enrichGeneratedFinding(&report.Findings[index], remediations)
	}
	for index := range report.Suppressed {
		enrichGeneratedFinding(&report.Suppressed[index].Finding, remediations)
	}
	for index := range report.Assessed {
		enrichGeneratedFinding(&report.Assessed[index].Finding, remediations)
	}
	for index := range report.ReleaseAges {
		enrichGeneratedFinding(&report.ReleaseAges[index].Finding, remediations)
	}
}

func (engine *Engine) reportHasGeneratedFinding(report Report) bool {
	for _, finding := range report.Findings {
		if engine.Repository.IsGenerated(finding.Path) {
			return true
		}
	}
	for _, entry := range report.Suppressed {
		if engine.Repository.IsGenerated(entry.Finding.Path) {
			return true
		}
	}
	for _, entry := range report.Assessed {
		if engine.Repository.IsGenerated(entry.Finding.Path) {
			return true
		}
	}
	for _, entry := range report.ReleaseAges {
		if engine.Repository.IsGenerated(entry.Finding.Path) {
			return true
		}
	}
	return false
}

func (engine *Engine) generationRemediations(inventory repository.GenerationInventory) map[string]policy.GenerationRemediation {
	remediations := map[string]policy.GenerationRemediation{}
	if len(inventory.Findings) != 0 {
		return remediations
	}
	for _, producer := range inventory.Producers {
		declaration := producer.Declaration
		remediation := policy.GenerationRemediation{
			Producer: declaration.Name, Inputs: slices.Clone(producer.Inputs),
			Generate: declaration.Generate.Clone(), Verify: declaration.Verify.Clone(),
			Prerequisites: []policy.GenerationPrerequisite{},
		}
		remediation.Prerequisites = append(remediation.Prerequisites, engine.generationPrerequisites("generate", declaration.Generate)...)
		remediation.Prerequisites = append(remediation.Prerequisites, engine.generationPrerequisites("verify", declaration.Verify)...)
		for _, output := range producer.Outputs {
			remediations[output] = remediation
		}
	}
	return remediations
}

func enrichGeneratedFinding(finding *policy.Finding, remediations map[string]policy.GenerationRemediation) {
	remediation, found := remediations[finding.Path]
	if !found {
		return
	}
	finding.GeneratedProducer = remediation.Producer
	finding.Remediation = policy.FindingRemediation{
		Summary:    "Repair the authoritative producer inputs for this defect, regenerate the output, and run the declared verification command.",
		Generation: &remediation,
	}
	if len(remediation.Prerequisites) > 0 {
		finding.Remediation.Summary = "Restore the declared command prerequisites, repair the authoritative producer inputs, then regenerate and verify the output."
	}
}

func (engine *Engine) generationPrerequisites(operation string, command policy.GenerationCommand) []policy.GenerationPrerequisite {
	problems := []policy.GenerationPrerequisite{}
	cwd, err := generationCommandDirectory(engine.Repository, command.Cwd)
	if err != nil {
		return []policy.GenerationPrerequisite{{Operation: operation, Message: "The declared working directory is unavailable or escapes the repository."}}
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		problems = append(problems, policy.GenerationPrerequisite{Operation: operation, Message: "The declared working directory is missing or is not a directory."})
	}
	if !engine.generationExecutableAvailable(command.Argv[0]) {
		problems = append(problems, policy.GenerationPrerequisite{Operation: operation, Message: fmt.Sprintf("The declared command requires unavailable executable %q.", command.Argv[0])})
	}
	return problems
}

func generationCommandDirectory(repo repository.Repository, cwd string) (string, error) {
	if cwd == "." {
		return filepath.EvalSymlinks(repo.Root)
	}
	return repo.Resolve(cwd)
}

func (engine *Engine) generationExecutableAvailable(executable string) bool {
	if strings.ContainsAny(executable, "/\\") {
		path, err := engine.Repository.Resolve(executable)
		if err != nil {
			return false
		}
		_, err = exec.LookPath(path)
		return err == nil
	}
	for _, prefix := range engine.Repository.CommandEnvironment().PathEntries {
		if _, err := exec.LookPath(filepath.Join(prefix, executable)); err == nil {
			return true
		}
	}
	_, err := exec.LookPath(executable)
	return err == nil
}
