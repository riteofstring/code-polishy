package engine

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestArchitectureReviewGateRequiresUnreviewedGraph(t *testing.T) {
	t.Parallel()
	engine := newArchitectureReviewEngine(t)
	controller := architectureGateController(t, engine)
	report, err := controller.checkArchitectureReview(t.Context(), engine, "main", false)
	if err != nil || !HasFindings(report) || report.ArchitectureReview == nil || report.ArchitectureReview.State != "required" {
		t.Fatalf("unreviewed gate = %+v, %v", report, err)
	}
	failed, err := controller.finalize(engine, report, nil)
	if err != nil || failed.GateRunPolicy.Status != "failed" {
		t.Fatalf("failed gate = %+v, %v", failed, err)
	}
}

func TestArchitectureReviewGateBindsAcceptanceAndPreservesVisibleSignals(t *testing.T) {
	t.Parallel()
	engine := newArchitectureReviewEngine(t)
	prepared := acceptEngineArchitecture(t, engine)
	controller := architectureGateController(t, engine)
	if controller.alreadyPassed != nil {
		t.Fatal("a gate with no previous pass was reused")
	}
	report, err := controller.checkArchitectureReview(t.Context(), engine, "main", false)
	if err != nil || HasFindings(report) {
		t.Fatalf("accepted gate = %+v, %v", report, err)
	}
	passed, err := controller.finalize(engine, Report{}, nil)
	if err != nil || passed.GateRunPolicy.Status != "passed" {
		t.Fatalf("passed gate = %+v, %v", passed, err)
	}
	assertArchitectureGateEvidence(t, engine, passed)
	controller = architectureGateController(t, engine)
	if controller.alreadyPassed == nil {
		t.Fatal("exact accepted gate did not reuse its passed identity")
	}
	reused, err := engine.alreadyPassedMergeGate(t.Context(), "main", MergeGateExecutionPlan{Level: "recommended"}, controller)
	if err != nil {
		t.Fatal(err)
	}
	assertArchitectureGateEvidence(t, engine, reused)
	resultPath := filepath.Join(engine.Repository.Root, filepath.FromSlash(prepared.ResultPath))
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, append(resultData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	controller = architectureGateController(t, engine)
	if controller.alreadyPassed != nil {
		t.Fatal("changed review evidence reused a passed gate")
	}
	report, err = controller.checkArchitectureReview(t.Context(), engine, "main", false)
	if err != nil || !HasFindings(report) {
		t.Fatalf("edited evidence gate = %+v, %v", report, err)
	}
	if _, err := controller.finalize(engine, report, nil); err != nil {
		t.Fatal(err)
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

func assertArchitectureGateEvidence(t *testing.T, engine *Engine, passed Report) {
	t.Helper()
	if !slices.ContainsFunc(passed.Findings, func(finding policy.Finding) bool {
		return finding.Check == "architecture.reviewSignal" && finding.Status == policy.FindingReviewed && finding.Severity == policy.FindingInformation
	}) {
		t.Fatalf("review signal was lost: %+v", passed.Findings)
	}
	data, err := os.ReadFile(filepath.Join(engine.Repository.Root, filepath.FromSlash(passed.GateRunPolicy.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	var durable gaterun.Report
	if err := json.Unmarshal(data, &durable); err != nil {
		t.Fatal(err)
	}
	if len(durable.Findings) != len(passed.Findings) || durable.Findings[0].Fingerprint != passed.Findings[0].Fingerprint || durable.Findings[0].Remediation.NextCommand == nil {
		t.Fatalf("gate lost canonical evidence: %+v", durable.Findings)
	}
}

func TestArchitectureReviewRejectsSourceOwnershipAndEvidenceChangesDuringGate(t *testing.T) {
	t.Parallel()
	engine := newArchitectureReviewEngine(t)
	prepared, err := engine.ArchitectureReview(t.Context(), "main", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	writeEngineArchitectureAcceptance(t, engine, *prepared.ArchitecturePreparation)
	if _, err := engine.ArchitectureReview(t.Context(), "main", "finalize"); err != nil {
		t.Fatal(err)
	}
	controller := architectureGateController(t, engine)
	if _, err := controller.checkArchitectureReview(t.Context(), engine, "main", false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(engine.Repository.Root, filepath.FromSlash(prepared.ArchitecturePreparation.ResultPath))); err != nil {
		t.Fatal(err)
	}
	_, err = controller.finalize(engine, Report{}, nil)
	if err == nil || !strings.Contains(err.Error(), "evidence changed") {
		t.Fatalf("gate publication error = %v", err)
	}
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

func architectureGateController(t *testing.T, engine *Engine) *gateRunController {
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
	controller, err := newGateRunController(engine, gaterun.MergeGate, "main", selection.Base, candidate, "recommended", []MergeGateExecutionCommand{}, decision.status, false)
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
