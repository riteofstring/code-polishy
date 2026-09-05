package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGateWarningsRetainEveryRequiredExecutionPhase(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"gate", "recommended", "full", "checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			policyEngine := gateWarningCandidate(t, mode)
			commandRunner := &recordingEngineRunner{}
			policyEngine.Runner = commandRunner
			report, err := runGateWarningCandidate(t, policyEngine, mode)
			if err != nil || HasFindings(report) || report.Summary.Warnings != 1 || report.Summary.Information != 1 {
				t.Fatalf("gate did not pass with its two nonblocking findings: %+v, error = %v", report, err)
			}
			expected := []string{"content-check", "focused"}
			if mode != "checkpoint" {
				expected = append(expected, "content-build", "offline-supply")
			}
			if mode == "gate" || mode == "full" {
				expected = append(expected, "full")
			}
			for _, name := range expected {
				if !slices.Contains(commandRunner.commands, name) {
					t.Fatalf("warning skipped required %s command: %v", name, commandRunner.commands)
				}
			}
			if mode != "gate" && (report.GateRunPolicy == nil || report.GateRunPolicy.Status != "passed") {
				t.Fatalf("gate warnings changed successful execution state: %+v", report.GateRunPolicy)
			}
		})
	}
}

func TestGateWarningsCannotHideALaterFailure(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"gate", "recommended", "full", "checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			policyEngine := gateWarningCandidate(t, mode)
			commandRunner := &failingEngineRunner{failure: "focused"}
			policyEngine.Runner = commandRunner
			report, err := runGateWarningCandidate(t, policyEngine, mode)
			if err != nil || !HasFindings(report) || report.Summary.Warnings != 1 || report.Summary.Errors != 1 {
				t.Fatalf("warning hid later test failure: %+v, error = %v", report, err)
			}
			if !slices.Contains(commandRunner.commands, "focused") || slices.Contains(commandRunner.commands, "content-build") || slices.Contains(commandRunner.commands, "offline-supply") {
				t.Fatalf("gate did not stop at its failing test: %v", commandRunner.commands)
			}
		})
	}
}

func TestGateWarningsDoNotPermitAnIncompleteCommandPlan(t *testing.T) {
	t.Parallel()
	report := (&Engine{}).finish(gateWarningFindings(), nil)
	commands := []MergeGateExecutionCommand{{}}
	for _, failure := range []error{
		mergeGateExecutionError(nil, report, &mergeGatePlannedRunner{}, commands),
		checkpointPlannedRunnerError(&mergeGatePlannedRunner{}, report, commands),
	} {
		if failure == nil || !strings.Contains(failure.Error(), "completed before planned command") {
			t.Fatalf("nonblocking findings allowed a missing planned command: %v", failure)
		}
	}
}

func TestReusedMergeGateRetainsCanonicalWarningCounts(t *testing.T) {
	t.Parallel()
	policyEngine := gateWarningCandidate(t, "recommended")
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	first, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || first.GateRunPolicy == nil || first.GateRunPolicy.Status != "passed" {
		t.Fatalf("first gate = %+v, error = %v", first, err)
	}
	executed := len(commandRunner.commands)
	reused, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || reused.GateRunPolicy == nil || reused.GateRunPolicy.Status != "already-passed" || len(commandRunner.commands) != executed {
		t.Fatalf("gate reuse reran work: %+v, error = %v, commands = %v", reused, err, commandRunner.commands)
	}
	if reused.Summary.Warnings != first.Summary.Warnings || reused.Summary.Information != first.Summary.Information || len(reused.Findings) != len(first.Findings) {
		t.Fatalf("gate reuse discarded canonical findings: first=%+v, reused=%+v", first, reused)
	}
	reused, err = policyEngine.FinalizeReport("merge-gate", reused)
	if err != nil {
		t.Fatal(err)
	}
	data, err := JSONReport(reused)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Summary.Warnings != 1 || decoded.Summary.Information != 1 {
		t.Fatalf("reused machine report lost nonblocking findings: %+v, error = %v", decoded, err)
	}
}

func gateWarningCandidate(t *testing.T, mode string) *Engine {
	t.Helper()
	root := contentRepository(t, nil)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	if mode == "full" {
		config, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
		if err != nil {
			t.Fatal(err)
		}
		writeEngineFile(t, root, policy.ConfigFilename, string(config)+"\n", 0o600)
	}
	commitEngineCandidate(t, root, "update content")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	policyEngine.PolicyModuleFindings = append(policyEngine.PolicyModuleFindings, gateWarningFindings()...)
	return policyEngine
}

func gateWarningFindings() []policy.Finding {
	return []policy.Finding{
		{Check: "portability.machinePath", Path: "content/data.json", Subject: "path", Message: "Configure a portable input path.", Severity: policy.FindingWarning},
		{Check: "policy.scope", Path: "content/data.json", Subject: "scope", Message: "The selected content scope is explicit.", Severity: policy.FindingInformation},
	}
}

func runGateWarningCandidate(t *testing.T, policyEngine *Engine, mode string) (Report, error) {
	t.Helper()
	switch mode {
	case "gate":
		return policyEngine.Gate(t.Context())
	case "checkpoint":
		return policyEngine.CheckpointGate(t.Context(), "main")
	default:
		return policyEngine.MergeGate(t.Context(), "main")
	}
}
