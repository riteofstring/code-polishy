package engine

import (
	"fmt"
	"runtime"
	"slices"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

type suiteDurationObservation struct {
	At       time.Time
	Duration time.Duration
	Wait     time.Duration
	Source   string
}

func (observation suiteDurationObservation) label(cost string) string {
	if observation.At.IsZero() {
		return "unknown (no comparable gate execution or exact receipt)"
	}
	label := fmt.Sprintf("%s execution; %s ago; %s", conciseDuration(observation.Duration), conciseDuration(max(0, time.Since(observation.At))), observation.Source)
	if observation.Source != "exact receipt" {
		label += "; wait " + conciseDuration(observation.Wait)
	}
	if cost == "quick" && observation.Duration >= time.Minute {
		label += "; exceeds 1m quick-feedback guidance"
	}
	return label
}

func (engine *Engine) comparableSuiteDurations(suites []policy.TestSuite, history []gaterun.StoredTestOutcome) map[string]suiteDurationObservation {
	observations := map[string]suiteDurationObservation{}
	for _, suite := range suites {
		command := testpolicy.SuiteCommand(engine.Repository, suite)
		for _, outcome := range history {
			if !comparableSuiteCommand(suite, command, outcome) || !outcome.StartedAt.After(observations[suite.Name].At) || outcome.StartedAt.After(time.Now()) {
				continue
			}
			observation := suiteDurationObservation{At: outcome.StartedAt, Source: "gate candidate " + outcome.Candidate}
			for _, attempt := range outcome.Command.Attempts {
				if !attempt.Diagnostic {
					observation.Duration += time.Duration(attempt.DurationMilliseconds) * time.Millisecond
					observation.Wait += time.Duration(attempt.ResourceWaitMilliseconds) * time.Millisecond
				}
			}
			observations[suite.Name] = observation
		}
	}
	return observations
}

func comparableSuiteCommand(suite policy.TestSuite, command policy.Command, outcome gaterun.StoredTestOutcome) bool {
	spec := outcome.Spec
	return executedSuiteOnCurrentPlatform(outcome) &&
		spec.Name == suite.Name && spec.Scope == suite.Scope && spec.Cwd == command.Cwd &&
		slices.Equal(spec.Argv, command.Argv) && slices.Equal(spec.Modules, command.Modules) && slices.Equal(spec.Paths, command.Paths) &&
		slices.Equal(spec.Environment, command.Environment) && spec.TimeoutSeconds == command.TimeoutSeconds
}

func executedSuiteOnCurrentPlatform(outcome gaterun.StoredTestOutcome) bool {
	return !outcome.Command.Reused && len(outcome.Command.Attempts) > 0 && outcome.Command.Status == gaterun.Passed &&
		outcome.Platform.OS == runtime.GOOS && outcome.Platform.Arch == runtime.GOARCH
}
