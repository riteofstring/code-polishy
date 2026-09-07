package workflow

import (
	"bytes"
	"strings"
	"testing"
)

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
