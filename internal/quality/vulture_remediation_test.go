package quality

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPythonVultureRemediationRechecksTheEditedProject(t *testing.T) {
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	if !pythonVultureRuntimeInstalled(t, repo) {
		t.Fatal("remediation test requires carried CPython and Vulture")
	}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = 'example'\nrequires-python = '==3.12.*'\n")
	writeQualityFile(t, repo.Root, "src/app.py", "def unused():\n    return 1\n\nprint('ready')\n")
	findings := vultureRemediationFindings(t, repo)
	if len(findings) != 1 || findings[0].Check != "quality.deadCode" {
		t.Fatalf("unused definition findings = %+v", findings)
	}
	remediation := findings[0].Remediation
	if !strings.HasPrefix(remediation.Summary, "Delete the unused definition.") || remediation.NextCommand == nil {
		t.Fatalf("no actionable deletion remedy: %+v", remediation)
	}
	if !slices.Equal(remediation.NextCommand.Argv, []string{"code-polishy", "check", "--all"}) || remediation.NextCommand.Cwd != "." {
		t.Fatalf("recheck lost its complete project scope: %+v", remediation.NextCommand)
	}
	writeQualityFile(t, repo.Root, "src/app.py", "print('ready')\n")
	selection, err := repo.Select("all", nil)
	if err != nil {
		t.Fatal(err)
	}
	plan := pythonQualityPlanFor(repo, selection.Files)
	if len(plan.projects) != 1 || plan.projects[0].project.Manifest != "pyproject.toml" {
		t.Fatalf("remediation recheck missed its project: %+v", plan)
	}
	if findings := vultureRemediationFindings(t, repo); len(findings) != 0 {
		t.Fatalf("deletion left dead-code findings: %+v", findings)
	}
}

func vultureRemediationFindings(t *testing.T, repo repository.Repository) []policy.Finding {
	t.Helper()
	project := pythonVultureProject(t, repo)
	command, err := pythonVultureCommand(repo, project)
	if err != nil {
		t.Fatal(err)
	}
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, command)
	if err != nil {
		t.Fatal(err)
	}
	return pythonVultureFindings(repo, project, output.Stdout)
}
