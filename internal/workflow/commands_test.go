package workflow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestJobRunsOnEventHandlesSharedPrerequisites(t *testing.T) {
	if os.Getenv("CODE_POLISHY_WORKFLOW_GRAPH_CHILD") != "1" {
		t.Parallel()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestJobRunsOnEventHandlesSharedPrerequisites$")
		command.Env = append(os.Environ(), "CODE_POLISHY_WORKFLOW_GRAPH_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("shared dependency graph did not complete within five seconds: %v: %s", err, output)
		}
		return
	}
	jobs := make([]Job, 512)
	for index := range jobs {
		jobs[index].ID = "job" + strconv.Itoa(index)
		for previous := max(0, index-2); previous < index; previous++ {
			jobs[index].Needs = append(jobs[index].Needs, jobs[previous].ID)
		}
	}
	if !JobRunsOnEvent(jobs, "job511", "push") {
		t.Fatal("an active graph with shared prerequisites was rejected")
	}
	jobs[0].Condition = "github.event_name == 'push'"
	if JobRunsOnEvent(jobs, "job511", "pull_request") {
		t.Fatal("a graph with a skipped prerequisite was accepted")
	}
	if !JobRunsOnEvent(jobs, "job511", "push") {
		t.Fatal("a skipped event contaminated an active event")
	}
}

func TestJobRunsOnEventRejectsMissingAndCyclicDependencies(t *testing.T) {
	t.Parallel()
	for name, jobs := range map[string][]Job{
		"missing": {{ID: "gate", Needs: []string{"missing"}}},
		"self":    {{ID: "gate", Needs: []string{"gate"}}},
		"cycle":   {{ID: "gate", Needs: []string{"build"}}, {ID: "build", Needs: []string{"gate"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if JobRunsOnEvent(jobs, "gate", "push") {
				t.Fatal("an incomplete dependency graph was accepted")
			}
		})
	}
}

func TestMergeGateCommandRequiresDedicatedStep(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		shell    string
		source   string
		accepted bool
	}{
		{"bash", "./code-polishyw merge-gate --base main", true},
		{"sh", "code-polishy merge-gate --base=origin/main --resume", true},
		{"bash", `./code-polishy merge-gate --base "main"`, true},
		{"sh", `./code-polishyw merge-gate --base 'main'`, true},
		{"bash", `./code-polishyw merge-gate --base="$BASE"`, true},
		{"sh", `./code-polishyw merge-gate --base="$CI_MERGE_REQUEST_DIFF_BASE_SHA"`, true},
		{"bash", `./code-polishyw merge-gate --base "${{ github.event.pull_request.base.sha }}"`, true},
		{"bash", `./code-polishyw merge-gate --base=${{ github.event.pull_request.base.sha || github.event.before }}`, true},
		{"pwsh", `./code-polishyw.ps1 merge-gate --base main`, true},
		{"powershell", `.\code-polishyw.ps1 merge-gate --base=main`, true},
		{"pwsh", `code-polishy.exe merge-gate --base="${{ github.sha }}"`, true},
		{"cmd", `code-polishy.exe merge-gate --base "main"`, true},
		{"cmd", `code-polishy.exe merge-gate --base=main --resume`, true},
		{"", "./code-polishyw merge-gate --base main", false},
		{"python", "code-polishy merge-gate --base main", false},
		{"bash {0}", "./code-polishyw merge-gate --base main", false},
		{"bash -e {0}", "./code-polishyw merge-gate --base main", false},
		{"bash -n {0}", "./code-polishyw merge-gate --base main", false},
		{"cmd", `./code-polishyw.ps1 merge-gate --base main`, false},
		{"pwsh", `./code-polishyw.ps1 merge-gate --base main,--help`, false},
		{"powershell", `./code-polishyw.ps1 merge-gate --base main,-h`, false},
		{"pwsh", `./code-polishyw.ps1 merge-gate --base "main", "--help"`, false},
		{"pwsh", `./code-polishyw.ps1 merge-gate --base @("main", "--help")`, false},
		{"pwsh", `./code-polishyw.ps1 merge-gate --base main >$(exit 0)`, false},
		{"cmd", `code-polishy.exe merge-gate --base main # --help`, false},
		{"cmd", `code-polishy.exe merge-gate --base main 'ignored --help ignored'`, false},
		{"cmd", `code-polishy.exe merge-gate --base main\ --help`, false},
		{"cmd", `code-polishy.exe merge-gate --base %BASE%`, false},
		{"bash", "BASH_ALIASES='exit 0'\n0\n./code-polishyw merge-gate --base main", false},
		{"sh", "BASH_ALIASES='exit 0'\n0\n./code-polishyw merge-gate --base main", false},
		{"bash", "echo prepare\n./code-polishyw merge-gate --base main", false},
		{"bash", "./code-polishyw merge-gate --base main\necho done", false},
		{"bash", "prepare && ./code-polishyw merge-gate --base main", false},
		{"bash", "./code-polishyw merge-gate --base main && echo done", false},
		{"bash", "./code-polishyw merge-gate --base main || exit $?", false},
		{"bash", "set -e; ./code-polishyw merge-gate --base main", false},
		{"bash", "set +e; ./code-polishyw merge-gate --base main; true", false},
		{"bash", `BASH_ENV= ./code-polishyw merge-gate --base main`, false},
		{"bash", `./code-polishyw merge-gate --base main >gate.log`, false},
		{"bash", `./code-polishyw merge-gate --base main # explanation`, false},
		{"bash", `echo "./code-polishyw merge-gate --base main"`, false},
		{"bash", `# ./code-polishyw merge-gate --base main`, false},
		{"bash", `./code-polishyw merge-gate --base main --help`, false},
		{"bash", `./code-polishyw merge-gate --base main -h`, false},
		{"bash", `./code-polishyw merge-gate --base "$BASE"`, false},
		{"bash", `./code-polishyw merge-gate --base=$BASE`, false},
		{"bash", `./code-polishyw merge-gate --base="$(prepare)"`, false},
		{"bash", `./code-polishyw merge-gate --base="${BASE:=main}"`, false},
		{"bash", `./code-polishyw merge-gate --base "${{ env.BASE }}"`, false},
		{"bash", `${{ env.PREPARE }}; ./code-polishyw merge-gate --base main`, false},
		{"bash", `./code-polishyw merge-gate --base main BASH_ENV=./setup.sh`, false},
		{"bash", `./code-polishyw merge-gate --base ""`, false},
		{"bash", `./code-polishyw merge-gate --base`, false},
	} {
		t.Run(test.shell+"/"+test.source, func(t *testing.T) {
			t.Parallel()
			if got := HasMergeGateCommand(test.source, test.shell); got != test.accepted {
				t.Fatalf("dedicated step accepted=%v, want %v", got, test.accepted)
			}
		})
	}
}

func TestDedicatedMergeGatePreservesArgumentsAndExit(t *testing.T) {
	t.Parallel()
	for _, shell := range []string{"bash", "sh"} {
		for _, test := range []struct{ command, arguments string }{
			{`./code-polishyw merge-gate --base main`, "merge-gate\n--base\nmain\n"},
			{`./code-polishyw merge-gate --base=main`, "merge-gate\n--base=main\n"},
			{`./code-polishyw merge-gate --base="$BASE"`, "merge-gate\n--base=--help\n"},
		} {
			t.Run(shell+"/"+test.command, func(t *testing.T) {
				t.Parallel()
				executable, err := exec.LookPath(shell)
				if err != nil {
					t.Skipf("%s is unavailable", shell)
				}
				if !HasMergeGateCommand(test.command, shell) {
					t.Fatal("supported dedicated step was rejected")
				}
				for _, status := range []int{0, 17} {
					root := t.TempDir()
					wrapper := "#!/bin/sh\nprintf '%s\\n' \"$@\" > arguments\nexit \"$GATE_EXIT\"\n"
					if err := os.WriteFile(filepath.Join(root, "code-polishyw"), []byte(wrapper), 0o700); err != nil {
						t.Fatal(err)
					}
					command := exec.Command(executable, "-e", "-c", test.command)
					command.Dir = root
					command.Env = append(os.Environ(), "BASH_ENV=", "SHELLOPTS=", "BASE=--help", "GATE_EXIT="+strconv.Itoa(status))
					output, err := command.CombinedOutput()
					if command.ProcessState == nil || command.ProcessState.ExitCode() != status {
						t.Fatalf("want exit %d, got %v: %s", status, err, output)
					}
					arguments, err := os.ReadFile(filepath.Join(root, "arguments"))
					if err != nil || string(arguments) != test.arguments {
						t.Fatalf("arguments=%q, want %q: %v", arguments, test.arguments, err)
					}
				}
			})
		}
	}
}
