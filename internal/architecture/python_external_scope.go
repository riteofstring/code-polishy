package architecture

import (
	"path"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonExternalInputSelected(repo repository.Repository, selected []string, declaration policy.PythonExternalPluginImport) bool {
	input := policy.PythonComputedImport{Project: declaration.Project}
	if declaration.Configuration != nil {
		input.Configuration = []policy.PythonComputedImportInput{*declaration.Configuration}
	}
	return pythonComputedInputSelected(repo, selected, input) || slices.Contains(selected, path.Join(path.Dir(declaration.Project), "uv.lock"))
}

func pythonExternalDeclarationCoverage(repo repository.Repository, selected []string, plan pythonSelectionPlan) []policy.Finding {
	findings := []policy.Finding{}
	for _, declaration := range repo.Config.Scope.PythonExternalPluginImports {
		actual, exists := plan.owners[declaration.Consumer.Importer]
		_, active := plan.selectedByProject[actual]
		if !active && !pythonExternalInputSelected(repo, selected, declaration) {
			continue
		}
		if !exists || actual != declaration.Project {
			findings = append(findings, pythonExternalFinding(declaration, "loader source does not belong to the declared Python project"))
		}
	}
	return findings
}
