package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/supplychain"
	workflowfacts "github.com/riteofstring/code-polishy/internal/workflow"
)

func TestCIFinalGateOwnerRequiresAnActiveMergeWorkflow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		event    string
		job      string
		step     string
		command  string
		accepted bool
	}{
		{"wrapper", "pull_request", "", "", "./code-polishyw merge-gate --base main", true},
		{"base SHA expression", "pull_request", "", "", `./code-polishyw merge-gate --base "${{ github.event.pull_request.base.sha }}"`, true},
		{"base environment binding", "pull_request", "", "        env:\n          BASE: ${{ github.event.pull_request.base.sha }}\n", `./code-polishyw merge-gate --base="$BASE"`, true},
		{"expanded help base", "push", "", "        env:\n          BASE: --help\n", `./code-polishyw merge-gate --base "$BASE"`, false},
		{"expanded short help base", "push", "", "        env:\n          BASE: -h\n", `./code-polishyw merge-gate --base "$BASE"`, false},
		{"escaped early exit", "push", "", "", `ex\it 0; ./code-polishyw merge-gate --base main`, false},
		{"escaped errexit change", "push", "", "", `s\et +e; ./code-polishyw merge-gate --base main; true`, false},
		{"set lone dash", "push", "", "", `set +e; set - -e; ./code-polishyw merge-gate --base main; true`, false},
		{"long help", "push", "", "", "./code-polishyw merge-gate --base main --help", false},
		{"short help", "push", "", "", "./code-polishyw merge-gate --base main -h", false},
		{"expression skips gate", "push", "", "        env:\n          PREPARE: exit 0\n", "|\n          ${{ env.PREPARE }}\n          ./code-polishyw merge-gate --base main", false},
		{"disabled exit builtin", "push", "", "", "enable -n exit; set +e; ./code-polishyw merge-gate --base main || exit 1; true", false},
		{"swallowed gate failure", "push", "", "", "set +e; ./code-polishyw merge-gate --base main; true", false},
		{"gate and success", "push", "", "", "./code-polishyw merge-gate --base main && echo done", false},
		{"gate or exit", "push", "", "", "./code-polishyw merge-gate --base main || exit $?", false},
		{"custom shell without errexit", "push", "", "        shell: bash {0}\n", "./code-polishyw merge-gate --base main; true", false},
		{"custom shell with errexit", "push", "", "        shell: bash -e {0}\n", "./code-polishyw merge-gate --base main; true", false},
		{"PowerShell direct gate", "push", "", "        shell: pwsh\n", `.\code-polishyw.ps1 merge-gate --base main`, true},
		{"PowerShell redirection exits", "push", "", "        shell: pwsh\n", `.\code-polishyw.ps1 merge-gate --base main >$(exit 0)`, false},
		{"top level return", "push", "", "", "set +e; ./code-polishyw merge-gate --base main || return 1; true", false},
		{"alias skips gate", "push", "", "        shell: sh\n", "|\n          alias false=true\n          false || ./code-polishyw merge-gate --base main", false},
		{"PowerShell array", "push", "", "        shell: pwsh\n", `./code-polishyw.ps1 merge-gate --base main,--help`, false},
		{"PowerShell quoted array", "push", "", "        shell: powershell\n", `./code-polishyw.ps1 merge-gate --base "main", "--help"`, false},
		{"alias table skips gate", "push", "", "        shell: sh\n", "|\n          BASH_ALIASES='exit 0'\n          0\n          ./code-polishyw merge-gate --base main", false},
		{"startup hook", "push", "", "        env:\n          BASH_ENV: ./setup.sh\n", "./code-polishyw merge-gate --base main", false},
		{"startup options", "push", "", "        env:\n          SHELLOPTS: noexec\n", "./code-polishyw merge-gate --base main", false},
		{"empty startup hook", "push", "", "        env:\n          BASH_ENV: ''\n", "./code-polishyw merge-gate --base main", true},
		{"echo", "push", "", "", "echo 'code-polishy merge-gate --base main'", false},
		{"schedule only", "\n  schedule:\n    - cron: '0 0 * * *'", "", "", "code-polishy merge-gate --base main", false},
		{"disabled job", "push", "    if: false\n", "", "code-polishy merge-gate --base main", false},
		{"disabled step", "push", "", "        if: false\n", "code-polishy merge-gate --base main", false},
		{"optional job", "push", "    continue-on-error: true\n", "", "code-polishy merge-gate --base main", false},
		{"optional step", "push", "", "        continue-on-error: true\n", "code-polishy merge-gate --base main", false},
		{"wrong event", "pull_request", "    if: github.event_name == 'schedule'\n", "", "code-polishy merge-gate --base main", false},
		{"selected event", "pull_request", "    if: github.event_name == 'pull_request'\n", "", "code-polishy merge-gate --base main", true},
		{"skipped dependency", "push", "    needs: skipped\n", "", "code-polishy merge-gate --base main", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := ".github/workflows/ci.yml"
			source := fmt.Sprintf("on: %s\njobs:\n  skipped:\n    if: github.event_name == 'repository_dispatch'\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo skipped\n  verify:\n%s    runs-on: ubuntu-latest\n    steps:\n      - run: %s\n%s", test.event, test.job, test.command, test.step)
			writeEngineFile(t, root, path, source, 0o600)
			repo := repository.Repository{Root: root, Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}}}
			if _, err := workflowfacts.Read(path, repo.Read); err != nil && test.accepted {
				t.Fatal(err)
			}
			findings := finalGateOwnerFindings(repo, []string{path}, supplychain.GitLabInspection{})
			if (len(findings) == 0) != test.accepted {
				t.Fatalf("active workflow accepted=%v: %+v", test.accepted, findings)
			}
		})
	}
}

func TestCIFinalGateOwnerRequiresSupportedCMDSyntax(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		command  string
		accepted bool
	}{
		{"plain", `code-polishy.exe merge-gate --base main`, true},
		{"double quotes", `code-polishy.exe merge-gate --base "main"`, true},
		{"attached base", `code-polishy.exe merge-gate --base="main"`, true},
		{"SHA expression", `code-polishy.exe merge-gate --base "${{ github.event.pull_request.base.sha }}"`, true},
		{"hash before help", `code-polishy.exe merge-gate --base main # --help`, false},
		{"hash before short help", `code-polishy.exe merge-gate --base main # -h`, false},
		{"single quotes", `code-polishy.exe merge-gate --base main 'ignored --help ignored'`, false},
		{"single quoted base", `code-polishy.exe merge-gate --base='main -h ignored'`, false},
		{"backslash before space", `code-polishy.exe merge-gate --base main\ --help`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := ".github/workflows/ci.yml"
			source := fmt.Sprintf("on: pull_request\njobs:\n  verify:\n    runs-on: windows-latest\n    steps:\n      - shell: cmd\n        run: %s\n", strconv.Quote(test.command))
			writeEngineFile(t, root, path, source, 0o600)
			repo := repository.Repository{Root: root, Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}}}
			if _, err := workflowfacts.Read(path, repo.Read); err != nil {
				t.Fatal(err)
			}
			if findings := finalGateOwnerFindings(repo, []string{path}, supplychain.GitLabInspection{}); (len(findings) == 0) != test.accepted {
				t.Fatalf("CMD workflow accepted=%v, want %v: %+v", len(findings) == 0, test.accepted, findings)
			}
		})
	}
}

func TestCIFinalGateOwnerUsesDeclaredRunnerLabels(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := ".github/workflows/ci.yml"
	writeEngineFile(t, root, path, "on: push\njobs:\n  verify:\n    runs-on: builder-linux-arm64\n    steps:\n      - shell: bash\n        run: code-polishy merge-gate --base main\n", 0o600)
	configuration := ".github/actionlint.yaml"
	writeEngineFile(t, root, configuration, "self-hosted-runner:\n  labels: [builder-linux-arm64]\n", 0o600)
	repo := repository.Repository{Root: root, Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}}}
	if findings := finalGateOwnerFindings(repo, []string{path}, supplychain.GitLabInspection{}); len(findings) != 0 {
		t.Fatalf("declared runner concealed the final gate: %+v", findings)
	}
	if err := os.Remove(filepath.Join(root, configuration)); err != nil {
		t.Fatal(err)
	}
	if findings := finalGateOwnerFindings(repo, []string{path}, supplychain.GitLabInspection{}); len(findings) != 1 {
		t.Fatalf("undeclared runner proved gate coverage: %+v", findings)
	}
}

func TestCIFinalGateOwnerMatchesCaseInsensitiveDependencies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		needs     string
		condition string
		accepted  bool
	}{
		{"Build", "github.event_name == 'push'", true},
		{"build", "github.event_name == 'push'", true},
		{"BUILD", "github.event_name == 'push'", true},
		{"Build", "github.event_name == 'schedule'", false},
	} {
		t.Run(test.needs+"/"+test.condition, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := ".github/workflows/ci.yml"
			source := fmt.Sprintf("on: push\njobs:\n  Build:\n    if: %s\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo build\n  Gate:\n    needs: %s\n    runs-on: ubuntu-latest\n    steps:\n      - run: ./code-polishyw merge-gate --base main\n", test.condition, test.needs)
			writeEngineFile(t, root, path, source, 0o600)
			repo := repository.Repository{Root: root, Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}}}
			if _, err := workflowfacts.Read(path, repo.Read); err != nil {
				t.Fatal(err)
			}
			if findings := finalGateOwnerFindings(repo, []string{path}, supplychain.GitLabInspection{}); (len(findings) == 0) != test.accepted {
				t.Fatalf("workflow accepted=%v, want %v: %+v", len(findings) == 0, test.accepted, findings)
			}
		})
	}
}

func TestCIFinalGateOwnerRequiresAnExecutableWorkflowCommand(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - run: code-polishy test --all\n", 0o600)
	repo := repository.Repository{
		Root:   root,
		Config: policy.Config{Version: 3, Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}},
	}
	if findings := finalGateOwnerFindings(repo, []string{".github/workflows/ci.yml"}, supplychain.GitLabInspection{}); len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	writeEngineFile(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - run: ./code-polishyw merge-gate --base main\n", 0o600)
	if findings := finalGateOwnerFindings(repo, []string{".github/workflows/ci.yml"}, supplychain.GitLabInspection{}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCIFinalGateOwnerAcceptsParsedGitLabScripts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "ci/policy.yml", "verify:\n  script: code-polishy merge-gate --base main\n", 0o600)
	repo := repository.Repository{
		Root: root, DynamicControlInputs: []string{"ci/policy.yml"},
		Config: policy.Config{Version: 3, Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}},
	}
	if findings := finalGateOwnerFindings(repo, []string{"ci/policy.yml"}, supplychain.GitLabInspection{UnconditionalScripts: []string{"code-polishy merge-gate --base main"}}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestLocalFinalGateOwnerNeedsNoWorkflow(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir(), Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerLocal}}}
	if findings := finalGateOwnerFindings(repo, nil, supplychain.GitLabInspection{}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCIFinalGateOwnerAcceptsSeparateSetupStep(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := ".github/workflows/ci.yml"
	source := "on: push\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo prepare\n      - run: ./code-polishyw merge-gate --base main\n"
	writeEngineFile(t, root, path, source, 0o600)
	repo := repository.Repository{Root: root, Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}}}
	if findings := finalGateOwnerFindings(repo, []string{path}, supplychain.GitLabInspection{}); len(findings) != 0 {
		t.Fatalf("separate setup hid the dedicated gate: %+v", findings)
	}
}

func TestCIFinalGateOwnerRejectsCompoundGitLabScripts(t *testing.T) {
	t.Parallel()
	for _, script := range []string{
		"echo prepare\n./code-polishyw merge-gate --base main",
		"./code-polishyw merge-gate --base main\necho done",
		"./code-polishyw merge-gate --base main || exit $?",
		"BASH_ALIASES='exit 0'\n0\n./code-polishyw merge-gate --base main",
	} {
		repo := repository.Repository{Root: t.TempDir(), Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}}}
		if findings := finalGateOwnerFindings(repo, nil, supplychain.GitLabInspection{UnconditionalScripts: []string{script}}); len(findings) != 1 {
			t.Errorf("compound script counted as a dedicated gate: %q: %+v", script, findings)
		}
	}
}

func TestCIFinalGateOwnerExplainsRejectedDeclarations(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, job, step, command, reason string
	}{
		{"optional job", "    continue-on-error: true\n", "", "./code-polishyw merge-gate --base main", "job permits failure"},
		{"inactive job", "    if: github.event_name == 'schedule'\n", "", "./code-polishyw merge-gate --base main", "job or a prerequisite"},
		{"optional step", "", "        continue-on-error: true\n", "./code-polishyw merge-gate --base main", "step permits failure"},
		{"inactive step", "", "        if: github.event_name == 'schedule'\n", "./code-polishyw merge-gate --base main", "step condition"},
		{"startup hook", "", "        env:\n          BASH_ENV: ./setup.sh\n", "./code-polishyw merge-gate --base main", "BASH_ENV"},
		{"custom shell", "", "        shell: bash {0}\n", "./code-polishyw merge-gate --base main", "effective shell"},
		{"command chain", "", "", "./code-polishyw merge-gate --base main && echo done", "trailing syntax"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := repository.Repository{Root: t.TempDir(), Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}}}
			path := ".github/workflows/ci.yml"
			source := fmt.Sprintf("on: push\njobs:\n  verify:\n%s    runs-on: ubuntu-latest\n    steps:\n      - id: final_gate\n        run: %s\n%s", test.job, test.command, test.step)
			writeEngineFile(t, repo.Root, path, source, 0o600)
			findings := finalGateOwnerFindings(repo, []string{path}, supplychain.GitLabInspection{})
			if len(findings) != 1 || findings[0].Path != path || findings[0].Line == 0 || findings[0].Subject != "job verify, step final_gate" || !strings.Contains(findings[0].Message, test.reason) {
				t.Fatalf("rejection lost declaration context: %+v", findings)
			}
			valid := ".github/workflows/gate.yml"
			writeEngineFile(t, repo.Root, valid, "on: push\njobs:\n  gate:\n    runs-on: ubuntu-latest\n    steps:\n      - run: ./code-polishyw merge-gate --base main\n", 0o600)
			if findings := finalGateOwnerFindings(repo, []string{path, valid}, supplychain.GitLabInspection{}); len(findings) != 0 {
				t.Fatalf("rejected candidate concealed a valid gate: %+v", findings)
			}
		})
	}
}

func TestCIFinalGateOwnerPreservesGitLabRejectionContext(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir(), Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}}}
	inspection := supplychain.GitLabInspection{GateRejections: []policy.Finding{{
		Check: "policy.finalGateOwner", Path: "ci/gate.yml", Subject: "job verify",
		Message: "job inherits from ci/setup.yml a nonempty before_script",
	}}}
	findings := finalGateOwnerFindings(repo, nil, inspection)
	if len(findings) != 1 || findings[0].Path != "ci/gate.yml" || findings[0].Subject != "job verify" || !strings.Contains(findings[0].Message, "ci/setup.yml") {
		t.Fatalf("GitLab rejection context lost: %+v", findings)
	}
}
