package repository

import (
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPythonReachabilityBindsAdmittedExternalDependency(t *testing.T) {
	repo, project, _ := distributionSourceFixture(t)
	writeFile(t, repo.Root, project.Manifest, pluginDependencyManifest("framework==1.0"))
	writeFile(t, repo.Root, "uv.lock", strings.ReplaceAll(pluginDependencyLock("1.0", "registry = 'https://packages.example.test/simple'"), "plug-dist", "framework"))
	project.SourceRoots = []string{"src"}
	project.Files = []string{"src/app.py"}
	repo.Config.Modules = []policy.Module{{Name: "application", Paths: []string{"src/**"}}}
	writeFile(t, repo.Root, project.Files[0], "class Plugin:\n    def run(self):\n        return 1\n")
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{Kind: "target", Project: project.Manifest, Target: &policy.PythonDynamicTarget{Module: "app", Symbol: "Plugin.run"}, Consumer: policy.PythonDynamicConsumer{Kind: "base", Importer: project.Files[0], Module: "app", Distribution: "framework", Qualified: "framework.Contract", Member: "run", Implementation: "Plugin"}}}
	first := repo.PythonReachabilityInputs(project)
	if len(first) != 1 || first[0].Error != "" || first[0].Dependency == nil || first[0].Dependency.Distribution != "framework" {
		t.Fatalf("admitted dependency missing: %+v", first)
	}
	writeFile(t, repo.Root, "uv.lock", strings.ReplaceAll(pluginDependencyLock("1.0", "registry = 'https://other.example.test/simple'"), "plug-dist", "framework"))
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
