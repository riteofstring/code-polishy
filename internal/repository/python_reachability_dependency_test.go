package repository

import (
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPythonReachabilityBindsAdmittedExternalDependency(t *testing.T) {
	repo, project := pluginDependencyRepository(t, "plug-dist==1.0", "1.0", "registry = 'https://packages.example.test/simple'")
	project.SourceRoots = []string{"apps/api/src"}
	project.Files = []string{"apps/api/src/app.py"}
	repo.Config.Modules = []policy.Module{{Name: "application", Paths: []string{"apps/api/src/**"}}}
	writeFile(t, repo.Root, project.Files[0], "class Plugin:\n    def run(self):\n        return 1\n")
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{Kind: "target", Project: project.Manifest, Target: &policy.PythonDynamicTarget{Module: "app", Symbol: "Plugin.run"}, Consumer: policy.PythonDynamicConsumer{Kind: "base", Importer: project.Files[0], Module: "app", Distribution: "plug-dist"}}}
	first := repo.PythonReachabilityInputs(project)
	if len(first) != 1 || first[0].Error != "" || first[0].Dependency == nil || first[0].Dependency.Distribution != "plug-dist" {
		t.Fatalf("admitted dependency missing: %+v", first)
	}
	writeFile(t, repo.Root, "apps/api/uv.lock", pluginDependencyLock("1.0", "registry = 'https://other.example.test/simple'"))
	changed := repo.PythonReachabilityInputs(project)
	if len(changed) != 1 || changed[0].Dependency == nil || changed[0].Dependency.Identity == first[0].Dependency.Identity {
		t.Fatalf("dependency source did not invalidate evidence: %+v", changed)
	}
	writeFile(t, repo.Root, project.Manifest, "[project]\nname = 'app'\ndependencies = []\n")
	missing := repo.PythonReachabilityInputs(project)
	if len(missing) != 1 || missing[0].Error == "" || missing[0].Dependency != nil {
		t.Fatalf("transitive-only dependency admitted: %+v", missing)
	}
}
