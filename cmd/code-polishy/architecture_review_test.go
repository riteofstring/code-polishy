package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/engine"
)

func TestArchitectureReviewCLIReportsRequiredPreparedAndAcceptedEvidence(t *testing.T) {
	root, _ := newBehaviorReviewCLIBaseRepository(t)
	writeBehaviorReviewCLIFile(t, root, "value.go", "package behaviorreviewcli\n\nfunc Value() string { return \"new\" }\n")
	gitBehaviorReviewCLI(t, root, "add", "value.go")
	gitBehaviorReviewCLI(t, root, "commit", "-m", "candidate")
	base := []string{"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t), "architecture-review"}
	invoke := func(action, format string) (int, string, string) {
		return captureRunOutput(t, append(append([]string{}, base...), action, "--base", "main", "--format", format))
	}
	status, stdout, stderr := invoke("status", "json")
	var report engine.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("JSON status: %v, stdout=%q, stderr=%q", err, stdout, stderr)
	}
	if status != 1 || report.ArchitectureReview == nil || report.ArchitectureReview.State != "required" || report.SourceDependencyGraph == nil {
		t.Fatalf("status=%d report=%+v stderr=%s", status, report, stderr)
	}
	status, stdout, stderr = invoke("prepare", "human")
	if status != 0 || !strings.Contains(stdout, "ARCHITECTURE REVIEW: prepared") || !strings.Contains(stdout, "RESULT:") {
		t.Fatalf("human prepare status=%d stdout=%s stderr=%s", status, stdout, stderr)
	}
	status, stdout, stderr = invoke("prepare", "json")
	report = engine.Report{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if status != 0 || report.ArchitecturePreparation == nil {
		t.Fatalf("prepare status=%d stdout=%s stderr=%s", status, stdout, stderr)
	}
	prepared := report.ArchitecturePreparation
	result := behaviorreview.ArchitectureReviewResult{
		Protocol: "architecture-review/v1", ReviewID: prepared.ReviewID,
		Base: prepared.Base, Candidate: prepared.Candidate, Topology: prepared.Topology,
		Decision: "accept", Rationale: "One application value calculation has one owner.", Findings: []behaviorreview.ArchitectureReviewFinding{},
		Evidence: []behaviorreview.ArchitectureCitation{{Pointer: "/graph/nodes/0/path", Quote: "value.go", Rationale: "The application module owns the only production source."}},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorReviewCLIFile(t, root, prepared.ResultPath, string(data))
	status, stdout, stderr = invoke("finalize", "json")
	report = engine.Report{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("finalize JSON: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if status != 0 || report.ArchitectureReview.State != "passed" || report.ArchitectureReview.ReceiptSHA256 == "" || report.Summary.Reviewed == 0 {
		t.Fatalf("finalize status=%d report=%+v stderr=%s", status, report, stderr)
	}
}
