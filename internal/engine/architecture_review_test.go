package engine

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestAutomatedGateIgnoresOptionalArchitectureReviewArtifacts(t *testing.T) {
	t.Parallel()
	for _, gate := range []gaterun.GateKind{gaterun.MergeGate, gaterun.CheckpointGate} {
		t.Run(string(gate), func(t *testing.T) {
			engine := newArchitectureReviewEngine(t)
			controller := architectureGateController(t, engine, gate)
			passed, err := controller.finalize(engine, Report{}, nil)
			if err != nil || passed.GateRunPolicy.Status != "passed" {
				t.Fatalf("unreviewed gate = %+v, %v", passed, err)
			}
			writeEngineFile(t, engine.Repository.Root, ".code-polishy-reports/architecture-review/prepare.json", "malformed optional evidence", 0o600)
			controller = architectureGateController(t, engine, gate)
			if gate == gaterun.MergeGate && controller.alreadyPassed == nil {
				t.Fatal("optional review artifacts invalidated the passed gate")
			}
			if err := controller.candidateIntegrityError(engine); err != nil {
				t.Fatalf("optional evidence blocked gate publication: %v", err)
			}
			if gate == gaterun.CheckpointGate {
				passed, err = controller.finalize(engine, Report{}, nil)
				if err != nil || passed.GateRunPolicy.Status != "passed" {
					t.Fatalf("optional evidence blocked checkpoint: %+v, %v", passed, err)
				}
			}
		})
	}
}

func TestExplicitArchitectureReviewRetainsAcceptanceValidation(t *testing.T) {
	t.Parallel()
	engine := newArchitectureReviewEngine(t)
	prepared := acceptEngineArchitecture(t, engine)
	writeEngineFile(t, engine.Repository.Root, prepared.ResultPath, "malformed", 0o600)
	report, err := engine.ArchitectureReview(t.Context(), "main", "status")
	if err != nil || !HasFindings(report) || report.ArchitectureReview.State != "required" {
		t.Fatalf("explicit review accepted invalid evidence: %+v, %v", report, err)
	}
}

func acceptEngineArchitecture(t *testing.T, engine *Engine) behaviorreview.ArchitecturePrepareResult {
	t.Helper()
	prepared, err := engine.ArchitectureReview(t.Context(), "main", "prepare")
	if err != nil || HasFindings(prepared) || prepared.ArchitecturePreparation == nil {
		t.Fatalf("prepare = %+v, %v", prepared, err)
	}
	writeEngineArchitectureAcceptance(t, engine, *prepared.ArchitecturePreparation)
	finalized, err := engine.ArchitectureReview(t.Context(), "main", "finalize")
	if err != nil || HasFindings(finalized) || finalized.ArchitectureReview.State != "passed" {
		t.Fatalf("finalize = %+v, %v", finalized, err)
	}
	return *prepared.ArchitecturePreparation
}

func TestExplicitArchitectureReviewRejectsUnownedTests(t *testing.T) {
	t.Parallel()
	engine := newArchitectureReviewEngine(t)
	writeEngineFile(t, engine.Repository.Root, "checks/value_test.go", "package checks_test\n\nimport _ \"example.test/app/app\"\n", 0o600)
	gitBehaviorReview(t, engine.Repository.Root, "add", "--all")
	gitBehaviorReview(t, engine.Repository.Root, "commit", "-m", "add an unowned test")
	report, err := engine.ArchitectureReview(t.Context(), "main", "prepare")
	if err != nil || !HasFindings(report) || report.SourceDependencyGraph != nil || report.ArchitecturePreparation != nil {
		t.Fatalf("unowned-test preparation = %+v, %v", report, err)
	}
}

func newArchitectureReviewEngine(t *testing.T) *Engine {
	t.Helper()
	root := t.TempDir()
	writeEngineFile(t, root, ".gitignore", ".code-polishy-reports/\n", 0o600)
	writeEngineFile(t, root, "go.mod", "module example.test/app\n", 0o600)
	writeEngineFile(t, root, "app/value.go", "package app\n\nfunc Value() int { return 1 }\n", 0o600)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "app/value.go", "package app\n\nfunc Value() int { return 2 }\n", 0o600)
	commitEngineCandidate(t, root, "change value")
	config := policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"app/**"}}}, ModuleByName: map[string]int{"app": 0}}
	return &Engine{Repository: repository.Repository{Root: root, PolicyRoot: enginePolicyRoot(t), Config: config}, Runner: &recordingEngineRunner{}, Output: io.Discard}
}

func architectureGateController(t *testing.T, engine *Engine, gate gaterun.GateKind) *gateRunController {
	t.Helper()
	selection, err := engine.Repository.SelectBase("main")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := engine.Repository.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	decision, err := engine.behaviorReviewDecision(t.Context(), selection, BehaviorReviewMerge)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newGateRunController(engine, gate, "main", selection.Base, candidate, "recommended", []MergeGateExecutionCommand{}, decision.status, false)
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

func writeEngineArchitectureAcceptance(t *testing.T, engine *Engine, prepared behaviorreview.ArchitecturePrepareResult) {
	t.Helper()
	packetData, err := os.ReadFile(filepath.Join(engine.Repository.Root, filepath.FromSlash(prepared.PacketPath)))
	if err != nil {
		t.Fatal(err)
	}
	var packet struct {
		Protocol  string `json:"protocol"`
		Base      string `json:"base"`
		Candidate string `json:"candidate"`
		Topology  struct {
			Identity string `json:"identity"`
		} `json:"topology"`
	}
	if err := json.Unmarshal(packetData, &packet); err != nil {
		t.Fatal(err)
	}
	result := behaviorreview.ArchitectureReviewResult{
		Protocol: packet.Protocol, ReviewID: prepared.ReviewID, Base: packet.Base, Candidate: packet.Candidate, Topology: packet.Topology.Identity,
		Decision: "accept", Rationale: "The single value calculation has one cohesive owner.", Findings: []behaviorreview.ArchitectureReviewFinding{},
		Evidence: []behaviorreview.ArchitectureCitation{{Pointer: "/graph/nodes/0/path", Quote: "app/value.go", Rationale: "The calculation belongs to the app module."}},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, engine.Repository.Root, prepared.ResultPath, string(data), 0o600)
}
