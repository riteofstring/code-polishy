package quality

import (
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func nativeGoPackageFiles(repo repository.Repository, selected, allFiles []string, capabilities ...string) ([]string, []policy.Finding) {
	packages := map[string][]string{}
	for _, path := range languageFiles(repo, allFiles, "go") {
		directory := filepath.Dir(path)
		packages[directory] = append(packages[directory], path)
	}
	checked, allowed := map[string]bool{}, map[string]bool{}
	findings := []policy.Finding{}
	for _, path := range selected {
		directory := filepath.Dir(path)
		if checked[directory] {
			continue
		}
		checked[directory] = true
		native, err := repo.NativeUnit(packages[directory], capabilities...)
		if err != nil {
			findings = append(findings, policy.Finding{Check: "policy.checkCoverage", Path: path, Subject: "go-compilation-unit", Message: err.Error()})
		}
		allowed[directory] = native && err == nil
	}
	files := []string{}
	for _, path := range selected {
		if allowed[filepath.Dir(path)] {
			files = append(files, path)
		}
	}
	return files, findings
}

func nativePythonCommands(repo repository.Repository, project pythonQualityProject) (pythonQualityProject, []policy.Finding) {
	commands := []pythonQualityCommand{}
	findings := []policy.Finding{}
	for _, command := range project.commands {
		capability := "lint"
		switch command.kind {
		case pythonRuffComplexityQualityKind:
			capability = "complexity"
		case pythonVultureQualityKind:
			capability = "dead-code"
		case pythonTyQualityKind:
			capability = "typecheck"
		}
		native, err := repo.NativeUnit(project.sources, capability)
		if err != nil {
			findings = append(findings, policy.Finding{Check: "policy.checkCoverage", Path: project.project.Manifest, Subject: capability, Message: err.Error()})
			continue
		}
		if native {
			commands = append(commands, command)
		}
	}
	project.commands = commands
	return project, findings
}

func javascriptUnitProblem(repo repository.Repository, inventory, selected []string, capability string) error {
	configurations := javascriptProjectConfigurations(inventory)
	packages := javascriptPackageRoots(inventory)
	unit := func(path string) string {
		if capability == "typecheck" {
			project, _ := javascriptNearestProject(configurations, repo.JavaScriptContextPath(path))
			return project
		}
		root, _ := javascriptOwningPackage(packages, repo.JavaScriptContextPath(path))
		return root
	}
	units := map[string]bool{}
	for _, path := range selected {
		units[unit(path)] = true
	}
	for name := range units {
		paths := []string{}
		for _, path := range inventory {
			if repo.Language(path) == "typescript" && unit(path) == name {
				paths = append(paths, path)
			}
		}
		if _, err := repo.NativeUnit(paths, capability); err != nil {
			return err
		}
	}
	return nil
}

func nativeGoToolCommands(repo repository.Repository, selected, allFiles []string) ([]policy.Command, []policy.Finding) {
	commands := []policy.Command{}
	findings := []policy.Finding{}
	for _, capabilities := range [][]string{{"lint", "typecheck"}, {"dead-code"}} {
		paths, routing := nativeGoPackageFiles(repo, languageFiles(repo, selected, "go"), allFiles, capabilities...)
		findings = append(findings, routing...)
		packages, inventory := goPackageInventory(repo, paths, repo.GoModules(allFiles))
		findings = append(findings, inventory...)
		scheduled, unavailable := goPackageToolCommands(repo, packages, capabilities[0])
		findings = append(findings, unavailable...)
		commands = append(commands, scheduled...)
	}
	return commands, findings
}
