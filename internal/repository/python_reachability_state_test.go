package repository

import (
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPythonReachabilityStateSelectsOnlyDeclaredDistributions(t *testing.T) {
	t.Parallel()
	repo, project, _ := distributionSourceFixture(t)
	writeFile(t, repo.Root, project.Manifest, "[project]\nname='app'\ndependencies=['framework==1.0']\n")
	writeFile(t, repo.Root, "uv.lock", "version=1\n[[package]]\nname='framework'\nversion='1.0'\nsource={registry='https://packages.example.test/simple'}\n")
	writeFile(t, repo.Root, "unrelated/pyproject.toml", "invalid syntax")
	first, err := repo.PythonReachabilityStateSHA256()
	if err != nil || first != "" {
		t.Fatalf("undeclared sources scanned: %s, %v", first, err)
	}
	declaration := policy.PythonDynamicReference{Kind: "target", Project: project.Manifest, Consumer: policy.PythonDynamicConsumer{Kind: "base", Distribution: "framework"}}
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{declaration}
	first, err = repo.PythonReachabilityStateSHA256()
	if err != nil || first == "" {
		t.Fatalf("declared state missing: %s, %v", first, err)
	}
	repo.Config.Scope.PythonDynamicReferences = append(repo.Config.Scope.PythonDynamicReferences, declaration, policy.PythonDynamicReference{Project: "unrelated/pyproject.toml", Consumer: policy.PythonDynamicConsumer{Kind: "callsite"}})
	second, err := repo.PythonReachabilityStateSHA256()
	if err != nil || first != second {
		t.Fatalf("duplicate or unrelated declaration changed input state: %s, %v", second, err)
	}
	root := ".venv/lib/python3.12/site-packages"
	writeFile(t, repo.Root, root+"/other-1.0.dist-info/METADATA", "invalid metadata")
	second, err = repo.PythonReachabilityStateSHA256()
	if err != nil || first != second {
		t.Fatalf("unrelated installed distribution changed input state: %s, %v", second, err)
	}
	source := "class Contract:\n    def run(self):\n        return 2\n"
	writeFile(t, repo.Root, root+"/framework.py", source)
	if _, err := repo.PythonReachabilityStateSHA256(); err == nil {
		t.Fatal("damaged source custody retained a usable state")
	}
	writeDistributionRecord(t, repo, root, source)
	second, err = repo.PythonReachabilityStateSHA256()
	if err != nil || first == second {
		t.Fatalf("changed installed source retained input state: %s, %v", second, err)
	}
	writeFile(t, repo.Root, project.Manifest, "[project]\nname='app'\ndependencies=[]\n")
	if _, err := repo.PythonReachabilityStateSHA256(); err == nil {
		t.Fatal("removed dependency retained a usable state")
	}
}
