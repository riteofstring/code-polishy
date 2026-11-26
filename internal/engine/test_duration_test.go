package engine

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestSuiteDurationUsesComparableExecutionsAndSeparatesWaiting(t *testing.T) {
	policyEngine := &Engine{}
	suite := policy.TestSuite{Name: "boundary", Scope: "module", Cost: "quick", Modules: []string{"app"}, Argv: []string{"go", "test", "./app"}, Cwd: "."}
	command := testpolicy.SuiteCommand(policyEngine.Repository, suite)
	outcome := gaterun.StoredTestOutcome{
		StartedAt: time.Now().Add(-2 * time.Hour), Candidate: "candidate",
		Platform: gaterun.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
		Spec:     gaterun.CommandSpec{Name: suite.Name, Scope: suite.Scope, Argv: command.Argv, Cwd: command.Cwd, Modules: command.Modules},
		Command: gaterun.CommandOutcome{Status: gaterun.Passed, Attempts: []gaterun.Attempt{
			{DurationMilliseconds: 90000, ResourceWaitMilliseconds: 7000},
			{DurationMilliseconds: 5000, Diagnostic: true},
		}},
	}
	newer := outcome
	newer.StartedAt = time.Now().Add(-time.Hour)
	newer.Spec.Argv = []string{"go", "test", "./..."}
	reused := outcome
	reused.StartedAt, reused.Command.Reused = time.Now().Add(-time.Minute), true
	observations := policyEngine.comparableSuiteDurations([]policy.TestSuite{suite}, []gaterun.StoredTestOutcome{outcome, newer, reused})
	observation := observations[suite.Name]
	if !observation.At.Equal(outcome.StartedAt) || observation.Duration != 90*time.Second || observation.Wait != 7*time.Second {
		t.Fatalf("duration included incomparable, reused or diagnostic execution: %+v", observation)
	}
	label := observation.label(suite.Cost)
	for _, want := range []string{"execution", "ago", "gate candidate candidate", "wait 7s", "exceeds 1m"} {
		if !strings.Contains(label, want) {
			t.Fatalf("duration label lacks %q: %s", want, label)
		}
	}
	if unknown := policyEngine.comparableSuiteDurations([]policy.TestSuite{suite}, []gaterun.StoredTestOutcome{newer, reused})[suite.Name].label(suite.Cost); !strings.HasPrefix(unknown, "unknown") {
		t.Fatalf("invented timing without comparable history: %s", unknown)
	}
}
