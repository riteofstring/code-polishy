package architecture

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const externalGuardPredicate = `import keyword
import unicodedata
def permitted(value):
    return (
        type(value) is str
        and unicodedata.normalize('NFKC', value) == value
        and value.count(':') == 1
        and value.startswith(('third_party.plugins:', 'third_party.plugins.'))
        and all(part.isidentifier() and not keyword.iskeyword(part)
                for part in value.replace(':', '.').split('.'))
    )
`

func TestPythonExternalPluginAcceptsGuardedRuntimeInputAndBindsPredicateChanges(t *testing.T) {
	repo, runner := externalGuardArchitectureFixture(t)
	first := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, runner)
	if first.Graph == nil || len(first.Findings) != 0 || len(first.Graph.External) != 1 {
		t.Fatalf("guarded runtime input did not acquire composition evidence: %+v", first)
	}
	if len(first.Graph.Nodes) != 3 || len(first.Graph.Edges) != 2 || first.Graph.External[0].Dependency.Namespace != "third_party.plugins" {
		t.Fatalf("runtime domain invented local targets or lost dependencies: %+v", first.Graph)
	}
	writeArchitectureFile(t, repo.Root, "src/app/validator.py", externalGuardPredicate+"\n")
	updated := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/validator.py"}, runner)
	if updated.Graph == nil || len(updated.Findings) != 0 || updated.Graph.External[0].Contract.InputSHA256 == first.Graph.External[0].Contract.InputSHA256 || updated.Graph.Identity == first.Graph.Identity {
		t.Fatalf("predicate source change retained stale input proof: %+v", updated)
	}
	writeArchitectureFile(t, repo.Root, "src/app/validator.py", strings.Replace(externalGuardPredicate, "== 1", ">= 1", 1))
	assertExternalArchitectureFailure(t, AnalyzeWithRunner(t.Context(), repo, []string{"src/app/validator.py"}, runner))
}

func TestPythonExternalPluginGuardCannotAdmitForeignOrLocalNamespaces(t *testing.T) {
	for _, namespace := range []string{"app", "collections", "other.plugins"} {
		t.Run(namespace, func(t *testing.T) {
			repo, runner := externalGuardArchitectureFixture(t)
			writeArchitectureFile(t, repo.Root, "src/app/validator.py", strings.ReplaceAll(externalGuardPredicate, "third_party.plugins", namespace))
			if namespace != "other.plugins" {
				repo.Config.Scope.PythonExternalPluginImports[0].Namespace = namespace
			}
			assertExternalArchitectureFailure(t, AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, runner))
		})
	}
}

func TestPythonExternalPluginGuardRequiresAnIndependentDeclarationForEveryLoader(t *testing.T) {
	repo, runner := externalGuardArchitectureFixture(t)
	declaration := repo.Config.Scope.PythonExternalPluginImports[0]
	repo.Config.Scope.PythonExternalPluginImports = nil
	assertExternalArchitectureFailure(t, AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, runner))
	repo.Config.Scope.PythonExternalPluginImports = []policy.PythonExternalPluginImport{declaration}
	changeExternalLoader(t, &repo, func(source string) string {
		return strings.Replace(source, "    return plugin", "    sibling = resolve_name(name)\n    return plugin, sibling", 1)
	})
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, runner)
	assertExternalArchitectureFailure(t, analysis)
	if !slices.ContainsFunc(analysis.Findings, func(value policy.Finding) bool {
		return value.Check == "policy.pythonExternalPluginImport" && value.Subject == "undeclared-loader"
	}) {
		t.Fatalf("guard authorized an undeclared sibling loader: %+v", analysis)
	}
}

func externalGuardArchitectureFixture(t *testing.T) (repository.Repository, *pythonGraphRunner) {
	t.Helper()
	repo, runner := externalArchitectureFixture(t, false)
	writeArchitectureFile(t, repo.Root, "src/app/validator.py", externalGuardPredicate)
	changeExternalLoader(t, &repo, func(source string) string {
		source = strings.Replace(source, "def load(name):", "from app.validator import permitted\ndef load(name):\n    if not permitted(name):\n        raise ValueError", 1)
		return strings.Replace(source, "'third_party.plugins:Plugin'", "name", 1)
	})
	declaration := &repo.Config.Scope.PythonExternalPluginImports[0]
	declaration.Consumer.Argument = "name"
	declaration.Consumer.Site.Line += 3
	declaration.Check.Site.Line += 3
	runner.outputs["."] = `{"src/app/loader.py":["src/app/contracts.py","src/app/validator.py"],"src/app/contracts.py":[],"src/app/validator.py":[]}`
	return repo, runner
}
