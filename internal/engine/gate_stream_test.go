package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func TestGateStreamsLargeRustFactsAndRecordsBoundedLogs(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir(), PolicyRoot: enginePolicyRoot(t)}
	var source strings.Builder
	for index := range 1200 {
		fmt.Fprintf(&source, "pub fn function_%d() {}\n", index)
	}
	paths := make([]string, 20)
	for index := range paths {
		paths[index] = fmt.Sprintf("source_%02d.rs", index)
		writeEngineFile(t, repo.Root, paths[index], source.String(), 0o600)
	}
	plan := rustfacts.PlanSources(repo, paths)
	boundary := rustSourceGate(t, repo, plan)
	planned := &mergeGatePlannedRunner{root: repo.Root, delegate: boundary, expected: boundary.expected}
	result := rustfacts.ReadSources(t.Context(), repo, plan, planned)
	if len(result.Findings) != 0 || len(result.Files) != len(paths) || planned.next != 1 {
		t.Fatalf("gate lost streamed syntax evidence: files=%d findings=%+v", len(result.Files), result.Findings)
	}
	for index, file := range result.Files {
		if file.Path != paths[index] || len(file.Functions) != 1200 || file.Functions[1199].Name != "function_1199" {
			t.Fatalf("gate changed parsed function inventory for %s", file.Path)
		}
	}
	outcome := finishRustSourceGate(t, boundary, gaterun.RunPassed)
	data, err := os.ReadFile(filepath.Join(repo.Root, outcome.Attempts[0].LogPath))
	if err != nil {
		t.Fatal(err)
	}
	var log struct {
		StdoutBytes     int64  `json:"stdout_bytes"`
		StdoutTruncated bool   `json:"stdout_truncated"`
		StdoutTail      []byte `json:"stdout_tail"`
	}
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatal(err)
	}
	if log.StdoutBytes <= 8<<20 || !log.StdoutTruncated || !strings.Contains(string(log.StdoutTail), "function_1199") {
		t.Fatalf("gate lost its bounded large-stream log: bytes=%d truncated=%v", log.StdoutBytes, log.StdoutTruncated)
	}
}

func TestGateRecordsStreamConsumerFailures(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir(), PolicyRoot: enginePolicyRoot(t)}
	writeEngineFile(t, repo.Root, "source.rs", "pub fn example() {}\n", 0o600)
	plan := rustfacts.PlanSources(repo, []string{"source.rs"})
	boundary := rustSourceGate(t, repo, plan)
	planned := &mergeGatePlannedRunner{root: repo.Root, delegate: boundary, expected: boundary.expected}
	result, err := runner.RunStreamed(t.Context(), planned, repo.Root, plan.Commands[0], rejectingGateStream{}, io.Discard)
	if err == nil || result.FailureCategory != runner.FailureOperational {
		t.Fatalf("consumer failure was accepted: result=%+v error=%v", result, err)
	}
	outcome := finishRustSourceGate(t, boundary, gaterun.RunFailed)
	if outcome.Attempts[0].FailureCategory != gaterun.FailureCategory(runner.FailureOperational) {
		t.Fatalf("gate lost the stream failure category: %+v", outcome)
	}
}

func TestGateRejectsUnplannedRustStream(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir(), PolicyRoot: enginePolicyRoot(t)}
	writeEngineFile(t, repo.Root, "source.rs", "pub fn example() {}\n", 0o600)
	plan := rustfacts.PlanSources(repo, []string{"source.rs"})
	boundary := rustSourceGate(t, repo, plan)
	planned := &mergeGatePlannedRunner{root: repo.Root, delegate: boundary, expected: boundary.expected}
	command := plan.Commands[0]
	command.Stdin = append(append([]byte{}, command.Stdin...), '\n')
	result, err := runner.RunStreamed(t.Context(), planned, repo.Root, command, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "request") || result.FailureCategory != runner.FailureOperational || boundary.next != 0 {
		t.Fatalf("unplanned stream reached gate execution: result=%+v next=%d error=%v", result, boundary.next, err)
	}
}

type rejectingGateStream struct{}

func (rejectingGateStream) Write([]byte) (int, error) {
	return 0, errors.New("syntax consumer rejected the output")
}

func rustSourceGate(t *testing.T, repo repository.Repository, plan rustfacts.SourcePlan) *gateArtifactRunner {
	t.Helper()
	if len(plan.Findings) != 0 || len(plan.Commands) != 1 {
		t.Fatalf("unexpected parser plan: %+v", plan.Findings)
	}
	planned := MergeGateExecutionCommand{Root: repo.Root, Category: gaterun.Check, Scope: "repository", Cost: "quick", Command: plan.Commands[0]}
	identity, err := gaterun.NewIdentity(gaterun.IdentityInput{
		Gate: gaterun.MergeGate, RequestedBase: "main", ExactBase: strings.Repeat("a", 40), Candidate: strings.Repeat("b", 40), PolicyLevel: "recommended",
		Release: gaterun.ReleaseIdentity{Version: "test", Digest: strings.Repeat("a", 64)}, ConfigurationSHA256: strings.Repeat("b", 64),
		Platform: gaterun.Platform{OS: "test", Arch: "test"}, Commands: []gaterun.CommandSpec{gateRunCommandSpec(planned, nil)},
		Environment: []gaterun.EnvironmentInput{}, AmbientEnvironment: []gaterun.EnvironmentInput{}, BehaviorReview: testGateRunBehaviorReview(),
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := gaterun.Start(gaterun.StartOptions{RepositoryRoot: repo.Root, Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	return &gateArtifactRunner{delegate: runner.OSRunner{}, run: run, expected: []MergeGateExecutionCommand{planned}, progress: io.Discard}
}

func finishRustSourceGate(t *testing.T, boundary *gateArtifactRunner, status gaterun.RunStatus) gaterun.CommandOutcome {
	t.Helper()
	report, err := boundary.run.Finalize(gaterun.FinalizeOptions{Status: status, BehaviorReview: testGateRunBehaviorReview()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gaterun.LoadReport(boundary.expected[0].Root, report.Identity); err != nil {
		t.Fatal(err)
	}
	if len(report.Commands) != 1 || len(report.Commands[0].Attempts) != 1 || string(report.Commands[0].Status) != string(status) {
		t.Fatalf("gate did not record its command attempt: %+v", report.Commands)
	}
	return report.Commands[0]
}
