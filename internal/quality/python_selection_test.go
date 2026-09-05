package quality

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPythonQualitySkipsUnrelatedFilesWithConfiguredReferences(t *testing.T) {
	t.Parallel()
	for _, selected := range [][]string{nil, {"AGENTS.md"}, {"README.md"}, {"src/other.go"}} {
		t.Run(strings.Join(selected, ","), func(t *testing.T) {
			repo := pythonSelectionRepository(t)
			commandRunner := &pythonQualityRunner{}
			findings := PythonQualityFindings(t.Context(), repo, selected, commandRunner)
			if len(findings) != 0 || len(commandRunner.commands) != 0 {
				t.Fatalf("unrelated selection started Python work: findings = %+v, commands = %+v", findings, commandRunner.commands)
			}
			if commands := pythonQualityCommands(repo, selected); len(commands) != 0 {
				t.Fatalf("execution plan contains unselected Python commands: %+v", commands)
			}
		})
	}
}

func TestPythonQualityExpandsOnlyTheSelectedProject(t *testing.T) {
	t.Parallel()
	repo := pythonSelectionRepository(t)
	writeQualityFile(t, repo.Root, "src/second.py", "def second():\n    return 2\n")
	writeQualityFile(t, repo.Root, "apps/other/pyproject.toml", "[project]\nname = \"other\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "apps/other/other.py", "def handler():\n    return 1\n")
	plan := pythonQualityPlanFor(repo, []string{"src/app.py"})
	if len(plan.projects) != 1 || plan.projects[0].project.Manifest != "pyproject.toml" {
		t.Fatalf("plan = %+v", plan)
	}
	if slices.ContainsFunc(plan.findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.pythonReachability" || finding.Check == "policy.pythonExternalAttribute"
	}) {
		t.Fatalf("unselected declarations were validated: %+v", plan.findings)
	}
	command := pythonQualityRequiredCommand(t, pythonQualityCommandValues(plan.projects[0].commands), "policy-vulture-dead-code-root")
	request, err := pythonVultureInputRequest(command.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(request.Files))
	for _, file := range request.Files {
		paths = append(paths, file.Path)
	}
	if !slices.Equal(paths, []string{"src/app.py", "src/second.py"}) {
		t.Fatalf("Vulture context = %v", paths)
	}
	findings := pythonVultureFindings(repo, plan.projects[0].project, pythonVultureOutput(t, paths, []pythonVultureDiagnostic{{
		Path: "src/second.py", Line: 1, End: 2, Name: "second", Kind: "function", Confidence: 60, Message: "unused function 'second'",
	}}, nil, nil, ""))
	if len(findings) != 1 || findings[0].Check != "quality.deadCode" || findings[0].Path != "src/second.py" {
		t.Fatalf("project context finding was lost: %+v", findings)
	}
}

func TestPythonQualityValidatesDeclarationsWhenTheirConfigurationIsSelected(t *testing.T) {
	t.Parallel()
	for _, configPath := range []string{policy.ConfigFilename, "policy/project.json"} {
		t.Run(configPath, func(t *testing.T) {
			repo := pythonSelectionRepository(t)
			repo.Config.ConfigPath = filepath.Join(repo.Root, filepath.FromSlash(configPath))
			plan := pythonQualityPlanFor(repo, []string{configPath})
			if len(plan.projects) != 0 || len(plan.findings) != 2 {
				t.Fatalf("plan = %+v", plan)
			}
			for _, check := range []string{"policy.pythonReachability", "policy.pythonExternalAttribute"} {
				if !slices.ContainsFunc(plan.findings, func(finding policy.Finding) bool {
					return finding.Check == check && finding.Path == configPath && strings.Contains(finding.Message, "no current contained Python project")
				}) {
					t.Fatalf("missing selected declaration finding %q: %+v", check, plan.findings)
				}
			}
		})
	}
}

func TestPythonQualitySelectsConfiguredProjectWithoutSelectedSource(t *testing.T) {
	t.Parallel()
	repo := pythonSelectionRepository(t)
	writeQualityFile(t, repo.Root, "apps/other/pyproject.toml", "[project]\nname = \"other\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "apps/other/other.py", "def handler():\n    return 1\n")
	plan := pythonQualityPlanFor(repo, []string{policy.ConfigFilename})
	if len(plan.projects) != 1 || plan.projects[0].project.Manifest != "apps/other/pyproject.toml" {
		t.Fatalf("plan = %+v", plan)
	}
	project := plan.projects[0]
	references, _ := pythonVultureReferences(repo, project.project)
	findings := pythonQualityOutputFindings(repo, project, pythonVultureQualityKind, pythonVultureOutput(t,
		project.project.Files, nil, nil, []pythonVultureProblem{{ID: references[0].ID, Message: "symbol is stale or ambiguous"}}, ""))
	if len(findings) != 1 || findings[0].Check != "policy.pythonReachability" || findings[0].Path != policy.ConfigFilename {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonQualityDoesNotValidateUnselectedProjectInventory(t *testing.T) {
	t.Parallel()
	repo := pythonSelectionRepository(t)
	writeQualityFile(t, repo.Root, "apps/other/pyproject.toml", "[project\n")
	writeQualityFile(t, repo.Root, "apps/other/other.py", "value = 1\n")
	plan := pythonQualityPlanFor(repo, []string{"src/app.py"})
	if len(plan.findings) != 0 || len(plan.projects) != 1 || plan.projects[0].project.Manifest != "pyproject.toml" {
		t.Fatalf("unrelated project affected selected source: %+v", plan)
	}
	plan = pythonQualityPlanFor(repo, []string{"apps/other/pyproject.toml"})
	if len(plan.projects) != 0 || !slices.ContainsFunc(plan.findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.pythonProject" && finding.Path == "apps/other/pyproject.toml"
	}) {
		t.Fatalf("selected invalid project was not checked: %d commands, findings = %+v", len(plan.projects), plan.findings)
	}
}

func TestPythonQualitySelectedManifestValidatesInferredReferencesWithoutRuntimeSource(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\n[project.scripts]\nexample = \"app:handler\"\n")
	plan := pythonQualityPlanFor(repo, []string{"pyproject.toml"})
	if len(plan.projects) != 0 || len(plan.findings) != 1 || plan.findings[0].Check != "policy.pythonDynamicReference" ||
		plan.findings[0].Path != "pyproject.toml" || !strings.Contains(plan.findings[0].Message, "no runtime .py definitions") {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPythonQualitySelectedConfigurationValidatesReferencesWithoutRuntimeSource(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{Kind: "target", Project: "pyproject.toml", Target: &policy.PythonDynamicTarget{Module: "app", Symbol: "handler"}}}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\n")
	plan := pythonQualityPlanFor(repo, []string{policy.ConfigFilename})
	if len(plan.projects) != 0 || len(plan.findings) != 1 || plan.findings[0].Check != "policy.pythonReachability" ||
		plan.findings[0].Subject != pythonVultureConfigReferenceID(repo.Config.Scope.PythonDynamicReferences[0]) || !strings.Contains(plan.findings[0].Message, "no runtime .py definitions") {
		t.Fatalf("plan = %+v", plan)
	}
}

func pythonSelectionRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := pythonQualityRepository(t)
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{Kind: "target", Project: "apps/other/pyproject.toml", Target: &policy.PythonDynamicTarget{Module: "other", Symbol: "missing"}}}
	repo.Config.Scope.PythonExternalAttributes = []policy.PythonExternalAttribute{{
		Project: "apps/plugin/pyproject.toml", Module: "plugin", Callable: "configure",
		Receiver:  policy.PythonExternalReceiver{Kind: "parameter", Name: "settings", Binding: policy.PythonSourceLocation{Line: 3, Column: 15}, Type: "external.runtime.Settings"},
		Attribute: "output_path", Write: policy.PythonSourceLocation{Line: 4, Column: 5},
	}}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "src/app.py", "value = 1\n")
	return repo
}
