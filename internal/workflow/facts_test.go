package workflow

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestParseResolvesEffectiveRunShell(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		workflow string
		job      string
		step     string
		runner   string
		want     string
	}{
		{"Linux default", "", "", "", "ubuntu-latest", "bash"},
		{"macOS default", "", "", "", "macos-latest", "bash"},
		{"Windows default", "", "", "", "windows-latest", "pwsh"},
		{"container default", "", "    container: ubuntu:24.04\n", "", "ubuntu-latest", "sh"},
		{"unknown runner default", "", "", "", "self-hosted", ""},
		{"workflow default", "defaults:\n  run:\n    shell: bash {0}\n", "", "", "ubuntu-latest", "bash {0}"},
		{"job overrides workflow", "defaults:\n  run:\n    shell: bash {0}\n", "    defaults:\n      run:\n        shell: bash\n", "", "ubuntu-latest", "bash"},
		{"step overrides job", "defaults:\n  run:\n    shell: bash\n", "    defaults:\n      run:\n        shell: bash -e {0}\n", "        shell: bash {0}\n", "ubuntu-latest", "bash {0}"},
		{"job directory preserves workflow shell", "defaults:\n  run:\n    shell: bash {0}\n", "    defaults:\n      run:\n        working-directory: src\n", "", "ubuntu-latest", "bash {0}"},
		{"explicit shell overrides platform", "", "", "        shell: bash {0}\n", "windows-latest", "bash {0}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := fmt.Sprintf("on: push\n%sjobs:\n  verify:\n    runs-on: %s\n%s    steps:\n      - run: echo checked\n%s", test.workflow, test.runner, test.job, test.step)
			facts, err := Parse("workflow.yml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := facts.Jobs[0].Steps[0].Shell; got != test.want {
				t.Fatalf("effective shell = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseResolvesShellStartupEnvironment(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		workflow string
		job      string
		step     string
		unknown  bool
	}{
		{"absent", "", "", "", false},
		{"step hook", "", "", "        env:\n          BASH_ENV: ./setup.sh\n", true},
		{"job hook", "", "    env:\n      BASH_ENV: ./setup.sh\n", "", true},
		{"workflow hook", "env:\n  BASH_ENV: ./setup.sh\n", "", "", true},
		{"container hook", "", "    container:\n      image: ubuntu:24.04\n      env:\n        BASH_ENV: ./setup.sh\n", "", true},
		{"step clears hook", "env:\n  BASH_ENV: ./setup.sh\n", "", "        env:\n          BASH_ENV: ''\n", false},
		{"job clears hook", "env:\n  BASH_ENV: ./setup.sh\n", "    env:\n      BASH_ENV: ''\n", "", false},
		{"workflow clears container hook", "env:\n  BASH_ENV: ''\n", "    container:\n      image: ubuntu:24.04\n      env:\n        BASH_ENV: ./setup.sh\n", "", false},
		{"unrelated override preserves hook", "env:\n  BASH_ENV: ./setup.sh\n", "", "        env:\n          OTHER: value\n", true},
		{"case variant cannot clear hook", "env:\n  BASH_ENV: ./setup.sh\n", "", "        env:\n          bash_env: ''\n", true},
		{"unknown hook value", "", "", "        env:\n          BASH_ENV: ${{ vars.STARTUP }}\n", true},
		{"unknown environment", "", "", "        env: ${{ fromJSON(vars.ENVIRONMENT) }}\n", true},
		{"imported shell options", "", "", "        env:\n          SHELLOPTS: noexec\n", true},
		{"cleared shell options", "env:\n  SHELLOPTS: noexec\n", "", "        env:\n          SHELLOPTS: ''\n", false},
		{"whitespace hook path", "", "", "        env:\n          BASH_ENV: ' '\n", true},
		{"step trace expansion", "", "", "        env:\n          PS4: '${BASH_ENV:=./setup.sh}'\n", true},
		{"job trace expansion", "", "    env:\n      PS4: '${BASH_ENV:=./setup.sh}'\n", "", true},
		{"workflow trace expansion", "env:\n  PS4: '${BASH_ENV:=./setup.sh}'\n", "", "", true},
		{"container trace expansion", "", "    container:\n      image: ubuntu:24.04\n      env:\n        PS4: '${BASH_ENV:=./setup.sh}'\n", "", true},
		{"encoded trace expansion", "", "", "        env:\n          PS4: '\\044{BASH_ENV:=./setup.sh}'\n", true},
		{"trace command substitution", "", "", "        env:\n          PS4: '`prepare`'\n", true},
		{"unknown trace prefix", "", "", "        env:\n          PS4: ${{ vars.TRACE_PREFIX }}\n", true},
		{"constant trace prefix", "", "", "        env:\n          PS4: '+ trace: '\n", false},
		{"empty trace prefix", "", "", "        env:\n          PS4: ''\n", false},
		{"step replaces trace expansion", "env:\n  PS4: '${BASH_ENV:=./setup.sh}'\n", "", "        env:\n          PS4: '+ '\n", false},
		{"job clears trace expansion", "env:\n  PS4: '${BASH_ENV:=./setup.sh}'\n", "    env:\n      PS4: ''\n", "", false},
		{"unrelated override preserves trace expansion", "env:\n  PS4: '${BASH_ENV:=./setup.sh}'\n", "", "        env:\n          OTHER: value\n", true},
		{"case variant cannot clear trace expansion", "env:\n  PS4: '${BASH_ENV:=./setup.sh}'\n", "", "        env:\n          ps4: ''\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := fmt.Sprintf("on: push\n%sjobs:\n  verify:\n    runs-on: ubuntu-latest\n%s    steps:\n      - run: ./code-polishyw merge-gate --base main\n%s", test.workflow, test.job, test.step)
			facts, err := Parse("workflow.yml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := facts.Jobs[0].Steps[0].ShellStartupUnknown; got != test.unknown {
				t.Fatalf("unknown shell startup = %v, want %v", got, test.unknown)
			}
		})
	}
}

func TestParseReturnsBoundedStableWorkflowFacts(t *testing.T) {
	t.Parallel()
	facts, err := Parse(".github/workflows/security.yml", []byte("name: Security\non:\n  schedule:\n    - cron: '0 4 * * 1'\n  workflow_dispatch:\njobs:\n  scan:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@0123456789012345678901234567890123456789\n      - run: code-polishy supply-chain\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if facts.ProtocolVersion != ProtocolVersion || facts.ParserIdentity != ParserIdentity || len(facts.Triggers) != 2 || len(facts.Schedules) != 1 || len(facts.Jobs) != 1 || len(facts.Jobs[0].Steps) != 2 {
		t.Fatalf("facts = %+v", facts)
	}
	if facts.Schedules[0].Cron != "0 4 * * 1" || facts.Jobs[0].Steps[0].Uses == "" || !strings.Contains(facts.Jobs[0].Steps[1].Run, "supply-chain") {
		t.Fatalf("facts = %+v", facts)
	}
}

func TestParseCanonicalizesJobDependencies(t *testing.T) {
	t.Parallel()
	source := "on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo build\n  Verify:\n    needs: BuIlD\n    runs-on: ubuntu-latest\n    steps:\n      - run: ./code-polishyw merge-gate --base main\n"
	facts, err := Parse("workflow.yml", []byte(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Jobs) != 2 || facts.Jobs[0].ID != "build" || facts.Jobs[1].ID != "verify" || len(facts.Jobs[1].Needs) != 1 || facts.Jobs[1].Needs[0] != "build" {
		t.Fatalf("job identities and dependencies are not canonical: %+v", facts.Jobs)
	}
}

func TestParseFailsClosedOnActionlintDiagnostics(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{
		"syntax":   "on: [push\njobs: {}\n",
		"semantic": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    needs: missing\n    steps:\n      - run: echo ok\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("workflow.yml", []byte(source), nil); err == nil || !strings.Contains(err.Error(), "actionlint rejected workflow") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseRejectsOversizedWorkflowBeforeParsing(t *testing.T) {
	t.Parallel()
	if _, err := Parse("workflow.yml", bytes.Repeat([]byte{' '}, MaximumInputBytes+1), nil); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("error = %v", err)
	}
	if _, err := Parse(strings.Repeat("p", maximumStringBytes+1), []byte("on: push\njobs: {}\n"), nil); err == nil || !strings.Contains(err.Error(), "encoding limit") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseClassifiesConditionsUnderTheScheduleEvent(t *testing.T) {
	t.Parallel()
	facts, err := Parse("workflow.yml", []byte(`on:
  schedule:
    - cron: '0 4 * * 1'
jobs:
  scheduled:
    if: github.event_name == 'schedule'
    runs-on: ubuntu-latest
    steps:
      - if: ${{ github.event_name != 'push' }}
        run: code-polishy supply-chain
  external:
    if: github.repository == 'example/project'
    runs-on: ubuntu-latest
    steps:
      - run: code-polishy supply-chain
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Jobs) != 2 || facts.Jobs[0].ID != "external" || facts.Jobs[0].RunsOnSchedule || facts.Jobs[1].ID != "scheduled" || !facts.Jobs[1].RunsOnSchedule || !facts.Jobs[1].Steps[0].RunsOnSchedule {
		t.Fatalf("facts = %+v", facts)
	}
}
