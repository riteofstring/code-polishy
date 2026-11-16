package quality

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func pythonEnvironmentRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	repo.Config.Scope.PythonTypecheckEnvironments = []policy.PythonTypecheckEnvironment{{
		Paths: []string{"src/worker.py"}, EnvironmentProject: "typecheck/worker/pyproject.toml", Reason: "Worker runtime.",
	}}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname='controller'\nrequires-python='==3.12.*'\ndependencies=['controller-api==1.0.0']\n")
	writeQualityFile(t, repo.Root, "typecheck/worker/pyproject.toml", "[project]\nname='worker'\nrequires-python='==3.12.*'\ndependencies=['harbor==0.23.0']\n")
	writeQualityFile(t, repo.Root, "typecheck/worker/uv.lock", "version=1\n")
	writePythonVenv(t, repo.Root, ".venv")
	writePythonVenv(t, repo.Root, "typecheck/worker/.venv")
	for _, environment := range []string{".venv", "typecheck/worker/.venv"} {
		writeQualityFile(t, repo.Root, environment+"/pyvenv.cfg", "home = /python\nversion = 3.12.13\n")
	}
	writeQualityFile(t, repo.Root, ".venv/lib/python3.12/site-packages/controller_api/__init__.py", "def run(value: str) -> str:\n    return value\n")
	writeQualityFile(t, repo.Root, "typecheck/worker/.venv/lib/python3.12/site-packages/harbor/__init__.py", "def run(value: int) -> int:\n    return value\n")
	writeQualityFile(t, repo.Root, "src/controller.py", "from controller_api import run\nprint(run('ok'))\n")
	writeQualityFile(t, repo.Root, "src/worker.py", "from harbor import run\nprint(run('invalid'))\n")
	return repo
}

func TestPythonTypecheckUsesTheDeclaredRuntimeWithoutMovingSources(t *testing.T) {
	repo := pythonEnvironmentRepository(t)
	plan := buildPythonQualityPlan(repo, []string{"src/controller.py", "src/worker.py"}, false)
	if len(plan.findings) != 0 || len(plan.projects) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	project := plan.projects[0]
	if project.project.Manifest != "pyproject.toml" {
		t.Fatalf("ownership changed: %+v", project.project)
	}
	typechecks := 0
	for _, specification := range project.commands {
		if specification.kind != pythonTyQualityKind {
			continue
		}
		typechecks++
		findings := pythonQualityCommandFindings(t.Context(), repo, project, specification, runner.OSRunner{})
		if specification.environment == "" {
			if len(findings) != 0 || !slices.Equal(specification.sources, []string{"src/controller.py"}) {
				t.Fatalf("controller environment changed: %+v, %+v", specification, findings)
			}
			continue
		}
		if len(findings) != 2 || findings[0].Check != "quality.typecheck" || strings.Contains(findings[0].Message, "unresolved") || findings[1].Check != "quality.typecheckEnvironment" || len(findings[1].Related) != 1 || findings[1].Related[0].Path != "src/worker.py" {
			t.Fatalf("worker API was not checked or attributed: %+v", findings)
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(repo.Root, "typecheck/worker/.venv"))
		if err != nil {
			t.Fatal(err)
		}
		if specification.command.Cwd != "." || !slices.Contains(specification.command.Argv, resolved) {
			t.Fatalf("worker environment or source root changed: %+v", specification.command)
		}
	}
	if typechecks != 2 {
		t.Fatalf("typechecks = %d", typechecks)
	}
}

func TestPythonWorkerImportsRemainUnresolvedWithoutTheRuntimeMapping(t *testing.T) {
	repo := pythonEnvironmentRepository(t)
	repo.Config.Scope.PythonTypecheckEnvironments = nil
	plan := buildPythonQualityPlan(repo, []string{"src/worker.py"}, false)
	if len(plan.findings) != 0 || len(plan.projects) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	project := plan.projects[0]
	for _, command := range project.commands {
		if command.kind != pythonTyQualityKind {
			continue
		}
		findings := pythonQualityCommandFindings(t.Context(), repo, project, command, runner.OSRunner{})
		if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
			return finding.Check == "quality.typecheck" && strings.Contains(finding.Message, "unresolved-import")
		}) {
			t.Fatalf("worker dependency leaked into controller environment: %+v", findings)
		}
		return
	}
	t.Fatal("missing typecheck")
}

func TestPythonTypecheckMissingRuntimeDoesNotFallbackOrDiscardOtherChecks(t *testing.T) {
	repo := pythonEnvironmentRepository(t)
	if err := os.RemoveAll(filepath.Join(repo.Root, "typecheck/worker/.venv")); err != nil {
		t.Fatal(err)
	}
	plan := buildPythonQualityPlan(repo, []string{"src/controller.py", "src/worker.py"}, false)
	if len(plan.findings) != 1 || plan.findings[0].Check != "quality.typecheckCoverage" || len(plan.projects) != 1 {
		t.Fatalf("missing environment did not fail coverage: %+v", plan)
	}
	commands := plan.projects[0].commands
	if len(commands) != 5 || commands[4].kind != pythonTyQualityKind || !slices.Equal(commands[4].sources, []string{"src/controller.py"}) {
		t.Fatalf("missing runtime changed unrelated analysis: %+v", commands)
	}
	if !slices.Contains(commands[0].command.Argv, "src/worker.py") {
		t.Fatalf("worker lost Ruff coverage: %+v", commands[0])
	}
}

func TestPythonTypecheckRejectsDiagnosticsFromAnotherRuntimePartition(t *testing.T) {
	repo := pythonEnvironmentRepository(t)
	plan := buildPythonQualityPlan(repo, []string{"src/controller.py", "src/worker.py"}, false)
	if len(plan.findings) != 0 || len(plan.projects) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	project := plan.projects[0]
	for _, command := range project.commands {
		if command.environment == "" {
			continue
		}
		boundary := &pythonQualityRunner{outputs: map[string]string{command.command.Name: `[{"check_name":"invalid-argument-type","description":"wrong runtime","location":{"path":"src/controller.py","lines":{"begin":1}}}]`}}
		findings := pythonQualityCommandFindings(t.Context(), repo, project, command, boundary)
		if len(findings) != 1 || findings[0].Check != "quality.typecheckCoverage" || findings[0].Path != "src/worker.py" {
			t.Fatalf("foreign diagnostic received runtime evidence: %+v", findings)
		}
		return
	}
	t.Fatal("missing mapped typecheck")
}

func TestPythonTypecheckRejectsBrokenRuntimeContracts(t *testing.T) {
	for name, invalidate := range map[string]func(*testing.T, repository.Repository){
		"lock missing": func(t *testing.T, repo repository.Repository) {
			if err := os.Remove(filepath.Join(repo.Root, "typecheck/worker/uv.lock")); err != nil {
				t.Fatal(err)
			}
		},
		"manifest malformed": func(t *testing.T, repo repository.Repository) {
			writeQualityFile(t, repo.Root, "typecheck/worker/pyproject.toml", "[project\n")
		},
		"environment escapes": func(t *testing.T, repo repository.Repository) {
			environment := filepath.Join(repo.Root, "typecheck/worker/.venv")
			if err := os.RemoveAll(environment); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), environment); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := pythonEnvironmentRepository(t)
			invalidate(t, repo)
			plan := buildPythonQualityPlan(repo, []string{"src/worker.py"}, false)
			if len(plan.findings) == 0 {
				t.Fatalf("invalid runtime accepted: %+v", plan)
			}
			for _, project := range plan.projects {
				if slices.ContainsFunc(project.commands, func(command pythonQualityCommand) bool { return command.kind == pythonTyQualityKind }) {
					t.Fatalf("invalid runtime silently typechecked with another environment: %+v", plan)
				}
			}
		})
	}
}

func TestPythonFocusedCheckReportsDeferredDeadCodeAnalysis(t *testing.T) {
	repo := pythonEnvironmentRepository(t)
	selection := repository.Selection{Files: []string{"src/worker.py"}}
	if notes := PythonCheckNotes(repo, selection, "check"); len(notes) != 1 || !strings.Contains(notes[0], "not run") {
		t.Fatalf("focused coverage is ambiguous: %v", notes)
	}
	selection.All = true
	if notes := PythonCheckNotes(repo, selection, "check"); len(notes) != 0 {
		t.Fatalf("complete selection described as deferred: %v", notes)
	}
}

func TestPythonTypecheckLargeRuntimeKeepsCompleteEvidenceAndBoundedRecovery(t *testing.T) {
	repo := pythonEnvironmentRepository(t)
	sources := make([]string, 260)
	for index := range sources {
		sources[index] = fmt.Sprintf("src/worker_%03d.py", index)
		writeQualityFile(t, repo.Root, sources[index], "from harbor import run\nprint(run(1))\n")
	}
	repo.Config.Scope.PythonTypecheckEnvironments[0].Paths = sources
	plan := buildPythonQualityPlan(repo, sources, false)
	if len(plan.findings) != 0 || len(plan.projects) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	project := plan.projects[0]
	for _, command := range project.commands {
		if command.kind != pythonTyQualityKind {
			continue
		}
		findings := pythonQualityCommandFindings(t.Context(), repo, project, command, runner.OSRunner{})
		if len(findings) != 1 || findings[0].Check != "quality.typecheckEnvironment" || len(findings[0].Related) != len(sources) {
			t.Fatalf("runtime evidence omitted analyzed files: %+v", findings)
		}
		recovery := findings[0].Remediation.NextCommand
		if recovery == nil || len(recovery.Argv) > 256 || !slices.Contains(recovery.Argv, "--all") {
			t.Fatalf("report recovery exceeds the command contract or loses files: %+v", recovery)
		}
		return
	}
	t.Fatal("missing mapped typecheck")
}
