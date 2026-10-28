package supplychain

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPythonLibraryCompatibilityRequiresTestedDevelopmentPins(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		runtime string
		dev     string
		valid   bool
	}{
		{"compatible", "numpy>=2.5.3", "numpy==2.5.3", true},
		{"upper bound", "numpy>=2,<3", "numpy==3.0", false},
		{"below floor", "numpy>=2.5.3", "numpy==2.5.2", false},
		{"missing", "numpy>=2", "", false},
		{"different package", "numpy>=2", "torch==2.14.0", false},
		{"floating development", "numpy>=2", "numpy>=2", false},
		{"wildcard development", "numpy>=2", "numpy==2.*", false},
		{"normalized identity", "Some_Package>=2", "some-package==2.0", true},
		{"dependency extras", "requests[socks]>=2", "requests[socks,security]==2.32.0", true},
		{"missing dependency extras", "requests[socks]>=2", "requests==2.32.0", false},
		{"same marker", "numpy>=2; python_version >= '3.12'", "numpy==2.5.3; python_version >= '3.12'", true},
		{"unconditional pin", "numpy>=2; sys_platform == 'win32'", "numpy==2.5.3", true},
		{"uncovered marker", "numpy>=2", "numpy==2.5.3; sys_platform == 'win32'", false},
		{"foreign host coverage", "numpy>=2; sys_platform == 'win32'", "numpy==2.5.3; sys_platform == 'linux'", false},
		{"tested prerelease", "numpy>=2,<3", "numpy==2.5rc1", true},
		{"prerelease below floor", "numpy>=2.5", "numpy==2.5rc1", false},
		{"explicit prerelease", "numpy>=2.5rc1,<3", "numpy==2.5rc2", true},
		{"local version", "torch>=2.14.0", "torch==2.14.0+cu130", true},
		{"exact public metadata", "torch==2.14.0", "torch==2.14.0+cu130", true},
		{"exact metadata without pin", "torch==2.14.0", "", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dev := ""
			if test.dev != "" {
				dev = fmt.Sprintf("%q", test.dev)
			}
			manifest := fmt.Sprintf("[project]\ndependencies = [%q]\n[dependency-groups]\ndev = [%s]\n", test.runtime, dev)
			repo := pythonLibraryRepository(t, manifest)
			findings := checkPythonProject(repo, "pyproject.toml")
			if (len(findings) == 0) != test.valid {
				t.Fatalf("library findings = %+v", findings)
			}
			if !test.valid && !supplyChecks(findings)["supplyChain.pythonCompatibility"] {
				t.Fatalf("missing actionable compatibility finding: %+v", findings)
			}
		})
	}
}

func TestPythonLibraryKeepsOptionalGroupsAndVersionBranches(t *testing.T) {
	t.Parallel()
	manifest := `[project]
dependencies = ["numpy>=1.26,<2; python_version < '3.12'", "numpy>=2; python_version >= '3.12'"]
[project.optional-dependencies]
fast = ["numpy>=2.5; python_version >= '3.12'", "Faiss_CPU>=1.15.1"]
[dependency-groups]
dev = ["numpy==1.26.4; python_version < '3.12'", "numpy==2.5.3; python_version >= '3.12'", "faiss-cpu==1.15.1"]
`
	repo := pythonLibraryRepository(t, manifest)
	if findings := checkPythonProject(repo, "pyproject.toml"); len(findings) != 0 {
		t.Fatalf("compatible optional or marker branches rejected: %+v", findings)
	}
	writeSupplyFile(t, repo.Root, "pyproject.toml", strings.Replace(manifest, "faiss-cpu==1.15.1", "faiss-cpu==1.14.0", 1))
	findings := checkPythonProject(repo, "pyproject.toml")
	if len(findings) != 1 || findings[0].Subject != "faiss-cpu" || findings[0].Line != 4 {
		t.Fatalf("optional floor violation lost declaration location: %+v", findings)
	}
}

func TestPythonLibraryScopePreservesApplicationAndToolPins(t *testing.T) {
	t.Parallel()
	manifest := "[project]\ndependencies = ['numpy>=2']\n[dependency-groups]\ndev = ['numpy==2.5.3']\n"
	repo := pythonLibraryRepository(t, manifest)
	writeSupplyFile(t, repo.Root, "app/pyproject.toml", manifest)
	writeSupplyFile(t, repo.Root, "app/uv.lock", "version = 1\n")
	if findings := checkPythonProject(repo, "app/pyproject.toml"); !supplyChecks(findings)["supplyChain.pythonExactVersion"] {
		t.Fatalf("library declaration relaxed another manifest: %+v", findings)
	}
	writeSupplyFile(t, repo.Root, "pyproject.toml", manifest+"[build-system]\nrequires = ['hatchling>=1']\n")
	if findings := checkPythonProject(repo, "pyproject.toml"); !supplyChecks(findings)["supplyChain.pythonExactVersion"] {
		t.Fatalf("library declaration relaxed build requirements: %+v", findings)
	}
	repo.Config.SupplyChain.PythonLibraries = nil
	repo.Config.Project.Kind = "library"
	if findings := checkPythonProject(repo, "pyproject.toml"); !supplyChecks(findings)["supplyChain.pythonExactVersion"] {
		t.Fatalf("descriptive project kind bypassed pin policy: %+v", findings)
	}
}

func TestPythonLibraryRequiresManifestAndFrozenLockProvider(t *testing.T) {
	t.Parallel()
	repo := pythonLibraryRepository(t, "[project]\ndependencies = []\n")
	if findings := CoverageFindings(repo, []string{"pyproject.toml", "uv.lock"}); len(findings) == 0 || !strings.Contains(fmt.Sprint(findings), "lock-sync") {
		t.Fatalf("library mode waived frozen lock coverage: %+v", findings)
	}
	if findings := CoverageFindings(repo, []string{"uv.lock"}); !supplyChecks(findings)["policy.pythonLibrary"] {
		t.Fatalf("missing library manifest ignored: %+v", findings)
	}
	writeSupplyFile(t, repo.Root, "pyproject.toml", "[tool.example]\nvalue = 1\n")
	if findings := checkPythonProject(repo, "pyproject.toml"); !supplyChecks(findings)["policy.pythonLibrary"] {
		t.Fatalf("non-project manifest accepted: %+v", findings)
	}
}

func TestPythonLibraryReviewPreservesCompatibilityContractChanges(t *testing.T) {
	t.Parallel()
	manifest := "[project]\ndependencies = []\n[project.optional-dependencies]\nnetwork = [\"requests[socks]>=2,<3; sys_platform == 'win32'\"]\n[dependency-groups]\ndev = ['requests[socks]==2.32.0']\n"
	repo := pythonLibraryRepository(t, manifest)
	base := commitReviewInventory(t, repo)
	writeSupplyFile(t, repo.Root, "pyproject.toml", strings.Replace(manifest, ">=2,<3", ">=2.32,<3", 1))
	changes, err := DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 1 {
		t.Fatalf("compatibility review = %+v, error = %v", changes, err)
	}
	change := changes[0]
	if !change.PackageAPI || change.From != "<3,>=2" || change.To != "<3,>=2.32" || change.Usage != `optional:network [socks]; sys_platform == "win32"` {
		t.Fatalf("published contract lost metadata: %+v", change)
	}
	writeSupplyFile(t, repo.Root, "pyproject.toml", strings.Replace(manifest, "win32", "linux", 1))
	changes, err = DependencyChanges(t.Context(), repo, base)
	if err != nil || len(changes) != 2 || !changes[0].PackageAPI || !changes[1].PackageAPI {
		t.Fatalf("marker-only API change disappeared: %+v, error = %v", changes, err)
	}
	if !slices.Contains([]string{changes[0].Change, changes[1].Change}, "removed") {
		t.Fatalf("removed marker branch was not reported: %+v", changes)
	}
}

func TestPythonDependencyUsageRetainsAdmissionAdvice(t *testing.T) {
	t.Parallel()
	for _, usage := range []string{"runtime [socks]", `runtime; sys_platform == "win32"`, "optional:network [socks]"} {
		change := DependencyChange{Ecosystem: "pypi", Directness: "direct", Usage: usage, Change: "added", To: "2.32.0"}
		if !newDependencyAgeCandidate(change) {
			t.Errorf("exact runtime dependency lost age advice: %s", usage)
		}
		change.To = ">=2.32.0"
		if newDependencyAgeCandidate(change) {
			t.Errorf("compatibility range selected an exact-release lookup: %s", usage)
		}
	}
}

func pythonLibraryRepository(t *testing.T, manifest string) repository.Repository {
	t.Helper()
	repo := supplyRepository(t)
	repo.Config.SupplyChain.PythonLibraries = []string{"pyproject.toml"}
	writeSupplyFile(t, repo.Root, "pyproject.toml", manifest)
	writeSupplyFile(t, repo.Root, "uv.lock", "version = 1\n")
	return repo
}
