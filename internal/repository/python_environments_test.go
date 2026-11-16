package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPythonTypecheckEnvironmentRetainsSourceOwnershipAndRequiresItsOwnLock(t *testing.T) {
	repo := Repository{Root: t.TempDir(), Config: policy.Config{Scope: policy.Scope{
		PythonTypecheckEnvironments: []policy.PythonTypecheckEnvironment{{Paths: []string{"src/worker.py"}, EnvironmentProject: "typecheck/worker/pyproject.toml", Reason: "Worker runtime."}},
	}}}
	for _, manifest := range []string{"pyproject.toml", "typecheck/worker/pyproject.toml"} {
		writeFile(t, repo.Root, manifest, "[project]\nname='example'\nrequires-python='==3.12.*'\n")
	}
	writeFile(t, repo.Root, "src/worker.py", "value = 1\n")
	writeFile(t, repo.Root, "uv.lock", "version = 1\n")
	files, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	if findings := repo.PythonTypecheckEnvironmentFindings(files); len(findings) != 1 || findings[0].Subject != "typecheck/worker/pyproject.toml" {
		t.Fatalf("root lock substituted for runtime lock: %+v", findings)
	}
	writeFile(t, repo.Root, "typecheck/worker/uv.lock", "version = 1\n")
	files, err = repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	if findings := repo.PythonTypecheckEnvironmentFindings(files); len(findings) != 0 {
		t.Fatalf("governed environment rejected: %+v", findings)
	}
	_, owners := repo.PythonProjectInventory(files).Owners()
	if owners["src/worker.py"] != "pyproject.toml" || repo.PythonTypecheckEnvironment("src/worker.py") != "typecheck/worker/pyproject.toml" || repo.PythonTypecheckEnvironment("src/other.py") != "" {
		t.Fatalf("source ownership changed: %+v", owners)
	}
	if err := os.Remove(filepath.Join(repo.Root, "src/worker.py")); err != nil {
		t.Fatal(err)
	}
	files, err = repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	if findings := repo.PythonTypecheckEnvironmentFindings(files); len(findings) != 1 || findings[0].Subject != "src/worker.py" {
		t.Fatalf("stale source mapping accepted: %+v", findings)
	}
}
