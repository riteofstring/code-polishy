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
	if findings[0].Severity != policy.FindingWarning || !strings.Contains(remediation.Summary, "If investigating this candidate") || remediation.NextCommand == nil {
		t.Fatalf("unused candidate lacks conditional investigation guidance: %+v", findings[0])
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
	_, output, err := runner.RunCaptured(t.Context(), runner.OSRunner{}, repo.Root, command, runner.CaptureStructured)
	if err != nil {
		t.Fatal(err)
	}
	return pythonVultureFindings(repo, project, output.Stdout)
}

func TestPythonVultureSeparatesUnusedCandidatesFromUnreachableCode(t *testing.T) {
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = 'example'\nrequires-python = '==3.12.*'\n")
	writeQualityFile(t, repo.Root, "src/app.py", `from dataclasses import asdict, dataclass

@dataclass
class Selection:
    device: str = "cpu"

class Service:
    def status(self):
        return "ready"
    recover = status

def unused_function():
    return 1

def callback(unused_argument):
    return 1

def flow():
    return 1
    print("unreachable")

print(asdict(Selection()), Service().status(), callback(None), flow())
`)
	findings := vultureRemediationFindings(t, repo)
	for _, name := range []string{"device", "recover", "unused_function", "unused_argument"} {
		if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
			return finding.Check == "quality.deadCode" && finding.Severity == policy.FindingWarning && finding.Fields["analysisOutcome"] == "advisory-candidate" && strings.Contains(finding.Message, "'"+name+"'")
		}) {
			t.Fatalf("missing advisory for %s: %+v", name, findings)
		}
	}
	if len(findings) != 5 || !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.deadCode" && finding.Severity == policy.FindingError && finding.Fields["analysisOutcome"] == "" && strings.Contains(finding.Message, "unreachable")
	}) {
		t.Fatalf("unreachable control flow lost its failure: %+v", findings)
	}
}
