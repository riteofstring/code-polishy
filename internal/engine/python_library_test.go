package engine

import (
	"io"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPythonLibraryRetainsFrozenLockFailureAndRecovery(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := repository.Repository{Root: root, PolicyRoot: enginePolicyRoot(t)}
	writeEngineFile(t, root, "pyproject.toml", "[project]\ndependencies = ['numpy>=2']\n[dependency-groups]\ndev = ['numpy==2.5.3']\n", 0o600)
	writeEngineFile(t, root, "verify_lock.py", `import pathlib
import tomllib

root = pathlib.Path(__file__).parent
manifest = tomllib.loads((root / "pyproject.toml").read_text())
lock = tomllib.loads((root / "uv.lock").read_text())
required = dict(item.split("==") for item in manifest["dependency-groups"]["dev"])
resolved = {item["name"]: item["version"] for item in lock["package"]}
if required != resolved:
    raise SystemExit("frozen lock differs from exact development requirements")
`, 0o600)
	repo.Config = policy.Config{
		SupplyChain: policy.SupplyChain{PythonLibraries: []string{"pyproject.toml"}},
		Checks: []policy.Command{{
			Name: "python-lock-sync", Provides: []string{"lock-sync"},
			Argv:  []string{repo.PythonTool(), "-I", "-B", "verify_lock.py"},
			RunOn: []string{"supply-chain"}, TimeoutSeconds: 10,
		}},
	}
	engine := &Engine{Repository: repo, Runner: runner.OSRunner{}, Output: io.Discard}
	for _, version := range []string{"2.5.3", "2.5.2", "2.5.3"} {
		writeEngineFile(t, root, "uv.lock", "version = 1\n[[package]]\nname = 'numpy'\nversion = '"+version+"'\nsource = {registry = 'https://pypi.org/simple'}\n", 0o600)
		report, err := engine.SupplyChain(t.Context(), true)
		if err != nil {
			t.Fatal(err)
		}
		if version == "2.5.3" && HasFindings(report) {
			t.Fatalf("valid or repaired library lock rejected: %+v", report.Findings)
		}
		if version == "2.5.2" && !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
			return finding.Subject == "python-lock-sync"
		}) {
			t.Fatalf("library compatibility waived frozen-lock failure: %+v", report.Findings)
		}
	}
}
