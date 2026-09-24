package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	policyschema "github.com/riteofstring/code-polishy/schema"
)

func TestReliabilityReminderMatchesDirectModulesAndExactPaths(t *testing.T) {
	t.Parallel()
	policyEngine := &Engine{Repository: repository.Repository{Config: policy.Config{
		Quality: policy.Quality{ReliabilityReminder: &policy.ReliabilityReminder{
			Modules: []string{"jobs"}, SourcePaths: []string{"shared/retry.go"},
		}},
		Modules: []policy.Module{
			{Name: "jobs", Paths: []string{"jobs/**"}},
			{Name: "shared", Paths: []string{"shared/**"}},
		},
	}}}
	reminder := policyEngine.reliabilityReminder(repository.CandidateDelta{AddedOrModified: []string{
		"shared/retry.go", "jobs/run.go", "unowned.txt",
	}})
	if reminder == nil || reminder.PolicyDocument != ReliabilityReminderPolicyDocument || reminder.Principle == "" || len(reminder.Questions) != 4 ||
		!slices.Equal(reminder.MatchedModules, []string{"jobs"}) || !slices.Equal(reminder.MatchedPaths, []string{"shared/retry.go"}) {
		t.Fatalf("reminder = %+v", reminder)
	}
}

func TestReliabilityReminderStaysQuietOutsideConfiguredScope(t *testing.T) {
	t.Parallel()
	policyEngine := &Engine{Repository: repository.Repository{Config: policy.Config{
		Quality: policy.Quality{ReliabilityReminder: &policy.ReliabilityReminder{Modules: []string{"jobs"}}},
		Modules: []policy.Module{{Name: "jobs", Paths: []string{"jobs/**"}}, {Name: "api", Paths: []string{"api/**"}}},
	}}}
	if reminder := policyEngine.reliabilityReminder(repository.CandidateDelta{AddedOrModified: []string{"api/handler.go"}}); reminder != nil {
		t.Fatalf("reminder = %+v", reminder)
	}
	policyEngine.Repository.Config.Quality.ReliabilityReminder = nil
	if reminder := policyEngine.reliabilityReminder(repository.CandidateDelta{AddedOrModified: []string{"jobs/run.go"}}); reminder != nil {
		t.Fatalf("unconfigured reminder = %+v", reminder)
	}
}

func TestCombinedReportsPreserveReliabilityReminderMatches(t *testing.T) {
	t.Parallel()
	report := (&Engine{}).combine(
		Report{ReliabilityReminder: newReliabilityReminder([]string{"jobs"}, nil)},
		Report{ReliabilityReminder: newReliabilityReminder([]string{"state"}, []string{"shared/retry.go"})},
	)
	reminder := report.ReliabilityReminder
	if reminder == nil || !slices.Equal(reminder.MatchedModules, []string{"jobs", "state"}) || !slices.Equal(reminder.MatchedPaths, []string{"shared/retry.go"}) {
		t.Fatalf("reminder = %+v", reminder)
	}
}

func TestReliabilityReminderValidatesAgainstPinnedReportSchema(t *testing.T) {
	t.Parallel()
	report := (&Engine{}).normalizeReport(Report{
		Command:             "merge-gate",
		ReportPath:          ".code-polishy-reports/merge-gate/example/report.json",
		ReliabilityReminder: newReliabilityReminder([]string{"jobs"}, []string{"jobs/retry.go"}),
	})
	data, err := JSONReport(report)
	if err != nil {
		t.Fatal(err)
	}
	validateSchemaDocument(t, ReportSchemaURL, policyschema.CodePolishyReport, data)
}

func TestCheckpointAndMergeGatesAttachReliabilityReminder(t *testing.T) {
	for _, mode := range []string{"checkpoint", "merge"} {
		t.Run(mode, func(t *testing.T) {
			policyEngine := reliabilityReminderGateCandidate(t)
			var report Report
			var err error
			if mode == "checkpoint" {
				report, err = policyEngine.CheckpointGate(t.Context(), "main")
			} else {
				report, err = policyEngine.MergeGate(t.Context(), "main")
			}
			if err != nil || report.GateRunPolicy == nil || report.GateRunPolicy.Status != "passed" {
				t.Fatalf("gate report = %+v, error = %v", report, err)
			}
			reminder := report.ReliabilityReminder
			if reminder == nil || !slices.Equal(reminder.MatchedModules, []string{"content"}) || len(reminder.MatchedPaths) != 0 {
				t.Fatalf("reminder = %+v", reminder)
			}
			if mode == "merge" {
				reused, reuseErr := policyEngine.MergeGate(t.Context(), "main")
				if reuseErr != nil || reused.GateRunPolicy == nil || reused.GateRunPolicy.Status != "already-passed" || reused.ReliabilityReminder == nil {
					t.Fatalf("reused gate report = %+v, error = %v", reused, reuseErr)
				}
			}
		})
	}
}

func reliabilityReminderGateCandidate(t *testing.T) *Engine {
	t.Helper()
	root := contentRepository(t, nil)
	configPath := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(data), `"quality": {}`, `"quality": {"reliabilityReminder":{"modules":["content"]}}`, 1)
	if configured == string(data) {
		t.Fatal("quality configuration fixture was not updated")
	}
	writeEngineFile(t, root, policy.ConfigFilename, configured, 0o600)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	commitEngineCandidate(t, root, "update content")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	policyEngine.Runner = &recordingEngineRunner{}
	return policyEngine
}
