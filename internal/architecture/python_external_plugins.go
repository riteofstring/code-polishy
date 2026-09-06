package architecture

import (
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonExternalDeclarations(repo repository.Repository, project string) []policy.PythonExternalPluginImport {
	result := []policy.PythonExternalPluginImport{}
	for _, declaration := range repo.Config.Scope.PythonExternalPluginImports {
		if declaration.Project == project {
			result = append(result, declaration)
		}
	}
	return result
}

func pythonExternalFinding(declaration policy.PythonExternalPluginImport, message string) policy.Finding {
	return policy.Finding{Check: "policy.pythonExternalPluginImport", Path: declaration.Consumer.Importer, Line: declaration.Consumer.Site.Line, Column: declaration.Consumer.Site.Column, Subject: declaration.Distribution, Message: message,
		Remediation: policy.FindingRemediation{Summary: "Verify the direct dependency, namespace, loader input, and rejecting runtime check before updating this exact external plug-in declaration.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "architecture", "--files", declaration.Consumer.Importer}, Cwd: "."}}}
}
