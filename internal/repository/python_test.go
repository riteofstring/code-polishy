package repository

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParsePythonProjectCollectsTypedRequirements(t *testing.T) {
	t.Parallel()
	commit := "ABCDEF0123456789ABCDEF0123456789ABCDEF01"
	manifest := `[project]
dependencies = [
  "Requests[socks] >= 2.31, < 3 ; python_version >= '3.10' and os_name == \"posix\"",
  "private-tool @ git+https://github.com/example/private-tool.git@` + commit + `#subdirectory=src/tool",
]

[project.optional-dependencies]
dev = ["pytest == 8.3.0"]

[dependency-groups]
lint = ["ruff == 0.8.1"]

[build-system]
requires = ["hatchling == 1.25.0"]
`
	project, err := ParsePythonProject("apps/api/pyproject.toml", []byte(manifest))
	if err != nil {
		t.Fatal(err)
	}
	if !project.IsPythonProject() || project.Root != "apps/api" || len(project.Requirements) != 5 {
		t.Fatalf("project = %+v", project)
	}
	requests := project.Requirements[0]
	if requests.Name != "requests" || !slices.Equal(requests.Extras, []string{"socks"}) || requests.Version != ">=2.31,<3" ||
		requests.Marker != `python_version >= '3.10' and os_name == "posix"` || requests.Usage != "runtime" || requests.Location.Line != 3 {
		t.Fatalf("registry requirement = %+v", requests)
	}
	git := project.Requirements[1]
	if git.Kind != PythonGitRequirement || git.Git.Scheme != "https" || git.Git.Host != "github.com" ||
		git.Git.Path != "/example/private-tool.git" || git.Git.Commit != strings.ToLower(commit) || git.Git.Subdirectory != "src/tool" {
		t.Fatalf("Git requirement = %+v", git)
	}
	usages := []string{}
	for _, requirement := range project.Requirements {
		usages = append(usages, requirement.Usage)
	}
	if !slices.Equal(usages, []string{"runtime", "runtime", "optional", "development", "build"}) {
		t.Fatalf("usages = %v", usages)
	}
}

func TestParsePythonRequirementAcceptsSafeFullCommitGitURLs(t *testing.T) {
	t.Parallel()
	commit := "0123456789abcdef0123456789abcdef01234567"
	values := []string{
		"example @ git+https://github.com/example/project.git@" + commit,
		"example @ git+ssh://git@github.com/example/project.git@" + strings.ToUpper(commit) + "#subdirectory=python/package",
	}
	for _, value := range values {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			requirement, err := ParsePythonRequirement(value)
			if err != nil {
				t.Fatal(err)
			}
			if requirement.Kind != PythonGitRequirement || requirement.Git.Commit != commit {
				t.Fatalf("requirement = %+v", requirement)
			}
		})
	}
}

func TestParsePythonRequirementAcceptsDirectURLBeforeMarker(t *testing.T) {
	t.Parallel()
	requirement, err := ParsePythonRequirement("example @ https://packages.example.test/example-1.0.0.whl ; python_version >= '3.10'")
	if err != nil {
		t.Fatal(err)
	}
	if requirement.Kind != PythonURLRequirement || requirement.URL != "https://packages.example.test/example-1.0.0.whl" || requirement.Marker != "python_version >= '3.10'" {
		t.Fatalf("requirement = %+v", requirement)
	}
}

func TestParsePythonRequirementAcceptsParenthesizedVersionListsAndMarkers(t *testing.T) {
	t.Parallel()
	requirement, err := ParsePythonRequirement("requests ( >=2.31, <3, ) ; (sys_platform == 'linux' or sys_platform == 'darwin') and python_version not in '3.8, 3.9'")
	if err != nil {
		t.Fatal(err)
	}
	if requirement.Version != ">=2.31,<3" || len(requirement.Specifiers) != 2 || requirement.Marker == "" {
		t.Fatalf("requirement = %+v", requirement)
	}
	if _, err := ParsePythonRequirement("requests >=2.31,"); err != nil {
		t.Fatalf("unparenthesized trailing comma was rejected: %v", err)
	}
}

func TestParsePythonRequirementRejectsUnsafeGitURLs(t *testing.T) {
	t.Parallel()
	commit := "0123456789abcdef0123456789abcdef01234567"
	values := []string{
		"example @ git+https://github.com/example/project.git@v1.2.3",
		"example @ git+https://github.com/example/project.git@0123456",
		"example @ git+http://github.com/example/project.git@" + commit,
		"example @ git://github.com/example/project.git@" + commit,
		"example @ git+https://token@github.com/example/project.git@" + commit,
		"example @ git+ssh://git:password@github.com/example/project.git@" + commit,
		"example @ git+https://github.com/example/project.git@" + commit + "#branch=main",
		"example @ git+https://github.com/example/project.git@" + commit + "#subdirectory=../escape",
		"example @ git+https://github.com/example/project.git?token=value@" + commit,
		"example @ git+https://${GIT_HOST}/example/project.git@" + commit,
	}
	for _, value := range values {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := ParsePythonRequirement(value); err == nil {
				t.Fatal("unsafe Git requirement was accepted")
			}
		})
	}
}

func TestParsePythonProjectRejectsMalformedAndContradictoryDeclarations(t *testing.T) {
	t.Parallel()
	cases := []string{
		"[project]\ndependencies = [\"requests==2.32.0\"\n",
		"[project]\ndependencies = [\"requests==2.32.0\", 4]\n",
		"[project]\ndependencies = [\"requests==2.32.0\"]\n[project.optional-dependencies]\ntest = [\"requests==2.33.0\"]\n",
		"[project]\ndependencies = [\"requests==2.32.0 ; python_version ==\"]\n",
	}
	for _, manifest := range cases {
		manifest := manifest
		t.Run(manifest, func(t *testing.T) {
			t.Parallel()
			if _, err := ParsePythonProject("pyproject.toml", []byte(manifest)); err == nil {
				t.Fatal("invalid manifest was accepted")
			}
		})
	}
}

func TestParsePythonProjectAcceptsTOMLScalarValues(t *testing.T) {
	t.Parallel()
	manifest := `[tool.scalar]
boolean = true
decimal = +1_000
hexadecimal = 0xDEAD_BEEF
octal = 0o7_55
binary = 0b1101_0110
fraction = -0.01
exponent = 6.626e-34
infinity = +inf
not_a_number = -nan
offset_datetime = 1979-05-27T00:32:00.999999-07:00
spaced_offset_datetime = 1979-05-27 07:32:00Z
local_datetime = 1979-05-27t00:32:00.999999
local_date = 1979-05-27
local_time = 00:32:00.999999
`
	if _, err := ParsePythonProject("pyproject.toml", []byte(manifest)); err != nil {
		t.Fatalf("valid TOML scalars were rejected: %v", err)
	}
}

func TestParsePythonProjectRejectsMalformedTOMLScalarValues(t *testing.T) {
	t.Parallel()
	values := []string{
		"unquoted",
		"TRUE",
		"01",
		"1__0",
		"0XDEAD",
		"0x_DEAD",
		".7",
		"7.",
		"1e_6",
		"1979-02-29",
		"1979-05-27T24:00:00Z",
		"1979-05-27  07:32:00Z",
		"00:32:00.9_9",
	}
	for _, value := range values {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := ParsePythonProject("pyproject.toml", []byte("[tool.scalar]\nvalue = "+value+"\n")); err == nil {
				t.Fatal("malformed TOML scalar was accepted")
			}
		})
	}
}

func TestParsePythonProjectScopesRepeatedTablesAndMarkerDeclarations(t *testing.T) {
	t.Parallel()
	valid := `[project]
dependencies = ["requests==2.32.0"]

[[tool.uv.index]]
name = "internal"
url = "https://packages.example.test/internal"

[[tool.uv.index]]
name = "public"
url = "https://pypi.org/simple"
`
	project, err := ParsePythonProject("pyproject.toml", []byte(valid))
	if err != nil || len(project.Requirements) != 1 {
		t.Fatalf("project = %+v, err = %v", project, err)
	}
	overlapping := `[project]
dependencies = ["requests==2.32.0 ; sys_platform == 'linux'"]

[project.optional-dependencies]
test = ["requests==2.33.0 ; python_version >= '3.10'"]
`
	if _, err := ParsePythonProject("pyproject.toml", []byte(overlapping)); err == nil {
		t.Fatal("overlapping marker declarations were accepted")
	}
	disjoint := `[project]
dependencies = ["requests==2.32.0 ; sys_platform == 'linux'"]

[project.optional-dependencies]
test = ["requests==2.33.0 ; sys_platform == 'win32'"]
`
	if _, err := ParsePythonProject("pyproject.toml", []byte(disjoint)); err != nil {
		t.Fatalf("disjoint marker declarations were rejected: %v", err)
	}
}

func TestParsePythonProjectSeparatesBuildIsolationRequirements(t *testing.T) {
	t.Parallel()
	project, err := ParsePythonProject("pyproject.toml", []byte(`[project]
dependencies = ["setuptools==69.0.0"]

[build-system]
requires = ["setuptools==70.0.0"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Requirements) != 2 {
		t.Fatalf("requirements = %+v", project.Requirements)
	}
}

func TestReadPythonProjectRecordsProjectVenv(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "apps/api/pyproject.toml", "[project]\nrequires-python = \"==3.12.*\"\n\n[build-system]\nrequires = []\n")
	if err := os.MkdirAll(filepath.Join(root, "apps", "api", ".venv"), 0o755); err != nil {
		t.Fatal(err)
	}
	project, err := (Repository{Root: root}).ReadPythonProject("apps/api/pyproject.toml")
	if err != nil {
		t.Fatal(err)
	}
	if project.Root != "apps/api" || project.Venv != "apps/api/.venv" || !project.HasBuildSystemTable {
		t.Fatalf("project = %+v", project)
	}
}

func TestPythonProjectInventoryCacheReusesProjectsAndCopiesValues(t *testing.T) {
	t.Parallel()
	repo, manifest := preparedPythonProjectInventoryCache(t)
	inventory := cachedPythonProjectInventory(t, repo, manifest)
	mutateCachedPythonProjectInventory(inventory)
	assertPythonProjectInventoryClone(t, repo, manifest)
	assertPythonProjectReadClone(t, repo, manifest)
	assertUnseenPythonProjectIsNotCached(t, repo)
}

func preparedPythonProjectInventoryCache(t *testing.T) (Repository, string) {
	t.Helper()
	root := t.TempDir()
	manifest := "apps/api/pyproject.toml"
	writeFile(t, root, manifest, "[project]\nrequires-python = \"==3.12.*\"\ndependencies = [\"requests[socks] == 2.32.0\"]\n")
	writeFile(t, root, "apps/api/src/api/main.py", "")
	files := []string{"apps/api/src/api/main.py", "./" + manifest}
	repo, err := (Repository{Root: root}).WithPythonProjectInventory(files)
	if err != nil {
		t.Fatal(err)
	}
	files[0] = "changed.py"
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(manifest))); err != nil {
		t.Fatal(err)
	}
	return repo, manifest
}

func cachedPythonProjectInventory(t *testing.T, repo Repository, manifest string) PythonProjectInventory {
	t.Helper()
	inventory := repo.PythonProjectInventory([]string{manifest, "apps/api/src/api/main.py"})
	if len(inventory.Projects) != 1 || len(inventory.Assignments) != 1 || len(inventory.Projects[0].Requirements) != 1 || len(inventory.Projects[0].PackageCandidates) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	project := inventory.Projects[0]
	if !slices.Equal(project.Files, []string{"apps/api/src/api/main.py"}) || !slices.Equal(project.Requirements[0].Extras, []string{"socks"}) ||
		project.Requirements[0].Specifiers[0].Version != "2.32.0" {
		t.Fatalf("project = %+v", project)
	}
	return inventory
}

func mutateCachedPythonProjectInventory(inventory PythonProjectInventory) {
	inventory.Projects[0].Files[0] = "changed.py"
	inventory.Projects[0].PackageCandidates[0].Name = "changed"
	inventory.Projects[0].Requirements[0].Extras[0] = "changed"
	inventory.Projects[0].Requirements[0].Specifiers[0].Version = "9"
	inventory.Assignments[0].Path = "changed.py"
}

func assertPythonProjectInventoryClone(t *testing.T, repo Repository, manifest string) {
	t.Helper()
	inventory := repo.PythonProjectInventory([]string{"apps/api/src/api/main.py", manifest})
	project := inventory.Projects[0]
	if !slices.Equal(project.Files, []string{"apps/api/src/api/main.py"}) ||
		project.PackageCandidates[0].Name != "api" || !slices.Equal(project.Requirements[0].Extras, []string{"socks"}) ||
		project.Requirements[0].Specifiers[0].Version != "2.32.0" || inventory.Assignments[0].Path != "apps/api/src/api/main.py" {
		t.Fatalf("same inventory = %+v", inventory)
	}
}

func assertPythonProjectReadClone(t *testing.T, repo Repository, manifest string) {
	t.Helper()
	cached, err := repo.ReadPythonProject("./" + manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cached.Requirements[0].Extras, []string{"socks"}) ||
		cached.Requirements[0].Specifiers[0].Version != "2.32.0" {
		t.Fatalf("cached project = %+v", cached)
	}
	cached.Requirements[0].Extras[0] = "changed"
	fresh, err := repo.ReadPythonProject(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fresh.Requirements[0].Extras, []string{"socks"}) {
		t.Fatalf("fresh project = %+v", fresh)
	}
}

func assertUnseenPythonProjectIsNotCached(t *testing.T, repo Repository) {
	t.Helper()
	unseen := "apps/other/pyproject.toml"
	writeFile(t, repo.Root, unseen, "[project]\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	if _, err := repo.ReadPythonProject(unseen); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo.Root, filepath.FromSlash(unseen))); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReadPythonProject(unseen); err == nil {
		t.Fatal("finalized cache retained an unseen manifest")
	}
}

func TestPythonProjectInventoryCacheFreezesManifestErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	manifest := "pyproject.toml"
	writeFile(t, root, manifest, "[project\ndependencies = []\n")
	repo, err := (Repository{Root: root}).WithPythonProjectInventory([]string{manifest})
	if err != nil {
		t.Fatal(err)
	}
	_, cachedErr := repo.ReadPythonProject(manifest)
	if cachedErr == nil {
		t.Fatal("malformed manifest was accepted")
	}
	writeFile(t, root, manifest, "[project]\nrequires-python = \"==3.12.*\"\ndependencies = [\"urllib3 == 2.5.0\"]\n")
	if _, err := repo.ReadPythonProject(manifest); err == nil || err.Error() != cachedErr.Error() {
		t.Fatalf("cached error = %v, want %v", err, cachedErr)
	}
	writeFile(t, root, "src/current.py", "")
	inventory := repo.PythonProjectInventory([]string{manifest, "src/current.py"})
	if len(inventory.Projects) != 1 || len(inventory.Projects[0].Requirements) != 1 || inventory.Projects[0].Requirements[0].Name != "urllib3" ||
		!slices.Equal(inventory.Projects[0].Files, []string{"src/current.py"}) {
		t.Fatalf("inventory = %+v", inventory)
	}
}

func TestPythonProjectInventoryCacheDoesNotReuseDifferentFileSets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "pyproject.toml", "[project]\nrequires-python = \"==3.12.*\"\ndependencies = [\"requests == 2.32.0\"]\n")
	writeFile(t, root, "src/first.py", "")
	writeFile(t, root, "src/second.py", "")
	repo, err := (Repository{Root: root}).WithPythonProjectInventory([]string{"pyproject.toml", "src/first.py"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "pyproject.toml", "[project]\nrequires-python = \"==3.12.*\"\ndependencies = [\"urllib3 == 2.5.0\"]\n")
	inventory := repo.PythonProjectInventory([]string{"./pyproject.toml", "src/second.py"})
	if len(inventory.Projects) != 1 || len(inventory.Projects[0].Requirements) != 1 ||
		inventory.Projects[0].Requirements[0].Name != "urllib3" || !slices.Equal(inventory.Projects[0].Files, []string{"src/second.py"}) {
		t.Fatalf("inventory = %+v", inventory)
	}
}

func TestPythonProjectInventorySeparatesNestedProjectsAndRecordsCandidates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "pyproject.toml", "[project]\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	writeFile(t, root, "src/root_package/__init__.py", "")
	writeFile(t, root, "src/root_package/service.py", "")
	writeFile(t, root, "scripts/task.py", "")
	writeFile(t, root, "apps/child/pyproject.toml", "[project]\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	writeFile(t, root, "apps/child/src/child_package/service.py", "")
	if err := os.MkdirAll(filepath.Join(root, ".venv"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := Repository{Root: root}
	inventory := repo.PythonProjectInventory([]string{
		"pyproject.toml", "src/root_package/__init__.py", "src/root_package/service.py", "scripts/task.py",
		"apps/child/pyproject.toml", "apps/child/src/child_package/service.py",
	})
	if len(inventory.Problems) != 0 || len(inventory.Projects) != 2 {
		t.Fatalf("inventory = %+v", inventory)
	}
	wantAssignments := []PythonProjectAssignment{
		{Path: "apps/child/src/child_package/service.py", Manifest: "apps/child/pyproject.toml"},
		{Path: "scripts/task.py", Manifest: "pyproject.toml"},
		{Path: "src/root_package/__init__.py", Manifest: "pyproject.toml"},
		{Path: "src/root_package/service.py", Manifest: "pyproject.toml"},
	}
	if !slices.Equal(inventory.Assignments, wantAssignments) {
		t.Fatalf("assignments = %+v", inventory.Assignments)
	}
	rootProject := inventory.Projects[1]
	if rootProject.Root != "." {
		rootProject = inventory.Projects[0]
	}
	if rootProject.Root != "." || !slices.Equal(rootProject.SourceRoots, []string{".", "src"}) || rootProject.Venv != ".venv" ||
		!slices.Equal(rootProject.Files, []string{"scripts/task.py", "src/root_package/__init__.py", "src/root_package/service.py"}) {
		t.Fatalf("root project = %+v", rootProject)
	}
	wantCandidates := []PythonPackageCandidate{{Name: "scripts", Path: "scripts", Namespace: true}, {Name: "root_package", Path: "src/root_package", Namespace: false}}
	if !slices.Equal(rootProject.PackageCandidates, wantCandidates) {
		t.Fatalf("root candidates = %+v", rootProject.PackageCandidates)
	}
	childProject := inventory.Projects[0]
	if childProject.Root == "." {
		childProject = inventory.Projects[1]
	}
	if childProject.Root != "apps/child" || !slices.Equal(childProject.SourceRoots, []string{"apps/child", "apps/child/src"}) ||
		!slices.Equal(childProject.Files, []string{"apps/child/src/child_package/service.py"}) ||
		!slices.Equal(childProject.PackageCandidates, []PythonPackageCandidate{{Name: "child_package", Path: "apps/child/src/child_package", Namespace: true}}) {
		t.Fatalf("child project = %+v", childProject)
	}
}

func TestPythonProjectInventoryReportsNoOwnerAndUnreadableNestedProject(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "outer/pyproject.toml", "[project]\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	writeFile(t, root, "outer/nested/pyproject.toml", "[project\ndependencies = []\n")
	writeFile(t, root, "outer/nested/main.py", "")
	writeFile(t, root, "orphan.py", "")
	inventory := (Repository{Root: root}).PythonProjectInventory([]string{"outer/pyproject.toml", "outer/nested/pyproject.toml", "outer/nested/main.py", "orphan.py"})
	if len(inventory.Projects) != 1 || inventory.Projects[0].Root != "outer" || len(inventory.Projects[0].Files) != 0 {
		t.Fatalf("projects = %+v", inventory.Projects)
	}
	kinds := map[PythonInventoryProblemKind]bool{}
	for _, problem := range inventory.Problems {
		kinds[problem.Kind] = true
	}
	if !kinds[PythonNoProjectOwnerProblem] || !kinds[PythonUnreadableInputProblem] || len(inventory.Problems) != 2 {
		t.Fatalf("problems = %+v", inventory.Problems)
	}
}

func TestPythonProjectInventoryReportsEscapingSourcePaths(t *testing.T) {
	t.Parallel()
	inventory := (Repository{Root: t.TempDir()}).PythonProjectInventory([]string{"../outside.py"})
	if len(inventory.Projects) != 0 || len(inventory.Problems) != 1 || inventory.Problems[0].Kind != PythonEscapingPathProblem {
		t.Fatalf("inventory = %+v", inventory)
	}
}
