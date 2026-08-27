package testing

import (
	"bytes"
	"testing"
	"time"
)

func TestExecutionReporterRendersExactLevelContracts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		level string
		want  string
	}{
		{
			name: "focused", level: "focused",
			want: "EXECUTION PLAN stage=\"direct\" level=\"focused\" trusted-base=\"not-applicable\" candidate=\"working-tree\"\n" +
				"EXECUTION SELECTION reason=\"policy selected focused feedback\"\n" +
				"EXECUTION IMPACT modules=\"api,domain\"\n" +
				"EXECUTION COMMANDS total=1 checks=0 suites=1 quick=1 standard=0 expensive=0\n" +
				"EXECUTION DISCLOSURE focused-not-full=true\n" +
				"EXECUTION DURATION expected=history-unavailable timeout-ceiling=30s\n",
		},
		{
			name: "recommended", level: "recommended",
			want: "EXECUTION PLAN stage=\"direct\" level=\"recommended\" trusted-base=\"not-applicable\" candidate=\"working-tree\"\n" +
				"EXECUTION SELECTION reason=\"policy selected recommended feedback\"\n" +
				"EXECUTION IMPACT modules=\"api,domain\"\n" +
				"EXECUTION COMMANDS total=1 checks=0 suites=1 quick=1 standard=0 expensive=0\n" +
				"EXECUTION DISCLOSURE focused-not-full=false\n" +
				"EXECUTION DURATION expected=history-unavailable timeout-ceiling=30s\n",
		},
		{
			name: "full", level: "full",
			want: "EXECUTION PLAN stage=\"direct\" level=\"full\" trusted-base=\"not-applicable\" candidate=\"working-tree\"\n" +
				"EXECUTION SELECTION reason=\"policy selected full feedback\"\n" +
				"EXECUTION IMPACT modules=\"api,domain\"\n" +
				"EXECUTION COMMANDS total=1 checks=0 suites=1 quick=1 standard=0 expensive=0\n" +
				"EXECUTION DISCLOSURE focused-not-full=false\n" +
				"EXECUTION DURATION expected=history-unavailable timeout-ceiling=30s\n",
		},
		{
			name: "supplemental", level: "supplemental",
			want: "EXECUTION PLAN stage=\"direct\" level=\"supplemental\" trusted-base=\"not-applicable\" candidate=\"working-tree\"\n" +
				"EXECUTION SELECTION reason=\"policy selected supplemental feedback\"\n" +
				"EXECUTION IMPACT modules=\"api,domain\"\n" +
				"EXECUTION COMMANDS total=1 checks=0 suites=1 quick=1 standard=0 expensive=0\n" +
				"EXECUTION DISCLOSURE focused-not-full=false\n" +
				"EXECUTION DURATION expected=history-unavailable timeout-ceiling=30s\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			plan := executionProgressFixture(testCase.level)
			var output bytes.Buffer
			newExecutionReporter(&output, plan, true)
			if got := output.String(); got != testCase.want {
				t.Fatalf("output = %q\nwant = %q", got, testCase.want)
			}
		})
	}
}

func TestExecutionReporterRendersProgressOutcomesAndResourceWait(t *testing.T) {
	t.Parallel()
	plan := executionProgressFixture("focused")
	plan.Commands = append(plan.Commands, ExecutionCommand{Name: "suppressed", Kind: "suite", Scope: "repository", Cost: "standard", TimeoutSeconds: 10})
	plan.Commands[0].ExclusiveResources = []string{"performance"}
	var output bytes.Buffer
	reporter := newExecutionReporter(&output, plan, true)
	reporter.CommandWaiting(0)
	reporter.CommandResourceWaiting(0, time.Second)
	reporter.CommandFinished(0, ExecutionResult{
		Status: executionStatusFailed, ExecutionDuration: 2 * time.Second, ResourceWait: time.Second, ResourceWaitKnown: true,
	})
	reporter.CommandNotStarted(1)
	want := "EXECUTION PLAN stage=\"direct\" level=\"focused\" trusted-base=\"not-applicable\" candidate=\"working-tree\"\n" +
		"EXECUTION SELECTION reason=\"policy selected focused feedback\"\n" +
		"EXECUTION IMPACT modules=\"api,domain\"\n" +
		"EXECUTION COMMANDS total=2 checks=0 suites=2 quick=1 standard=1 expensive=0\n" +
		"EXECUTION DISCLOSURE focused-not-full=true\n" +
		"EXECUTION DURATION expected=history-unavailable timeout-ceiling=40s\n" +
		"EXECUTION PROGRESS command=1/2 name=\"focused\" kind=\"suite\" scope=\"module\" cost=\"quick\" timeout=30s resources=\"performance\" resource-wait=\"waiting\" elapsed=0s\n" +
		"EXECUTION PROGRESS command=1/2 name=\"focused\" kind=\"suite\" scope=\"module\" cost=\"quick\" timeout=30s resources=\"performance\" resource-wait=\"waiting\" elapsed=1s\n" +
		"EXECUTION RESULT command=1/2 status=\"failed\" execution=2s resource-wait=1s elapsed=3s\n" +
		"EXECUTION RESULT command=2/2 status=\"not-started\" execution=0s resource-wait=0s elapsed=0s\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q\nwant = %q", got, want)
	}
}

func executionProgressFixture(level string) ExecutionPlan {
	return ExecutionPlan{
		Stage: "direct", Level: level, TrustedBase: "not-applicable", Candidate: "working-tree",
		Reasons: []string{"policy selected " + level + " feedback"}, ImpactedModules: []string{"domain", "api"},
		Commands: []ExecutionCommand{{
			Name: "focused", Kind: "suite", Scope: "module", Cost: "quick", Argv: []string{"./focused.sh"}, Cwd: ".", TimeoutSeconds: 30,
		}},
	}
}
