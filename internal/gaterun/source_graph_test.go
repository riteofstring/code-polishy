package gaterun

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
)

func TestGateReportPersistsValidatedIndependentSourceGraph(t *testing.T) {
	t.Parallel()
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, t.TempDir(), identity)
	recordAttempt(t, run, 0, Passed, 0, "ok", "", 16)
	graph := gateReportTestGraph(t)
	expected := sourcegraph.Clone(&graph)
	prepared, err := run.PrepareFinalization(FinalizeOptions{Status: RunPassed, BehaviorReview: identity.BehaviorReview, SourceDependencyGraph: &graph})
	if err != nil {
		t.Fatal(err)
	}
	graph.Inputs[0].Paths[0] = "mutated.py"
	report, err := prepared.Commit()
	if err != nil || !reflect.DeepEqual(report.SourceDependencyGraph, expected) {
		t.Fatalf("published graph: %+v, %v", report.SourceDependencyGraph, err)
	}
	report.SourceDependencyGraph.Nodes[0].Module = "mutated"
	report.SourceDependencyGraph.Inputs[0].FactsSHA256 = strings.Repeat("f", 64)
	loaded, err := LoadReport(run.repositoryRoot, identity)
	if err != nil || !reflect.DeepEqual(loaded.SourceDependencyGraph, expected) {
		t.Fatalf("loaded graph: %+v, %v", loaded.SourceDependencyGraph, err)
	}
	changed := cloneReport(loaded)
	changed.SourceDependencyGraph.Inputs[0].PartitionsSHA256 = strings.Repeat("f", 64)
	if err := validateReport(changed, identity); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("accepted stale source graph: %v", err)
	}
	updated, err := sourcegraph.New(changed.SourceDependencyGraph.Nodes, changed.SourceDependencyGraph.Edges, changed.SourceDependencyGraph.Inputs, changed.SourceDependencyGraph.External)
	if err != nil {
		t.Fatal(err)
	}
	changed.SourceDependencyGraph = &updated
	digest, err := reportDigest(changed)
	if err != nil || digest == loaded.SHA256 {
		t.Fatalf("report digest omitted graph evidence: %q, %v", digest, err)
	}
	if !reflect.DeepEqual(loaded.SourceDependencyGraph, expected) {
		t.Fatal("clone mutation changed loaded evidence")
	}
}

func TestGateReportCannotPublishInvalidSourceGraph(t *testing.T) {
	t.Parallel()
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, t.TempDir(), identity)
	recordAttempt(t, run, 0, Passed, 0, "ok", "", 16)
	graph := gateReportTestGraph(t)
	graph.Inputs = nil
	if _, err := run.Finalize(FinalizeOptions{Status: RunPassed, BehaviorReview: identity.BehaviorReview, SourceDependencyGraph: &graph}); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("invalid graph published: %v", err)
	}
	if _, err := LoadReport(run.repositoryRoot, identity); !errors.Is(err, ErrMissingArtifact) {
		t.Fatalf("failed publication left readable acceptance: %v", err)
	}
}

func TestGateReportReloadsGraphLargerThanEightMiB(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, t.TempDir(), identity)
	recordAttempt(t, run, 0, Passed, 0, "ok", "", 16)
	graph := gateReportTestGraph(t)
	graph.Nodes = nil
	graph.Inputs[0].Paths = nil
	for index := range 6000 {
		path := fmt.Sprintf("%s/%s/%s/%04d.py", strings.Repeat("a", 200), strings.Repeat("b", 200), strings.Repeat("c", 200), index)
		graph.Nodes = append(graph.Nodes, sourcegraph.Node{Path: path, Language: "python", Root: ".", Module: "app", Resolution: "file:" + path})
		graph.Inputs[0].Paths = append(graph.Inputs[0].Paths, path)
	}
	canonical, err := sourcegraph.New(graph.Nodes, graph.Edges, graph.Inputs, graph.External)
	if err != nil {
		t.Fatal(err)
	}
	report, err := run.Finalize(FinalizeOptions{Status: RunPassed, BehaviorReview: identity.BehaviorReview, SourceDependencyGraph: &canonical})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(run.repositoryRoot, run.reportPath))
	if err != nil || info.Size() <= 8<<20 {
		t.Fatalf("fixture did not exceed the old report boundary: %v", err)
	}
	loaded, err := LoadReport(run.repositoryRoot, identity)
	if err != nil || loaded.SHA256 != report.SHA256 || !reflect.DeepEqual(loaded.SourceDependencyGraph, &canonical) {
		t.Fatalf("large graph reload lost evidence: %v", err)
	}
}

func gateReportTestGraph(t *testing.T) sourcegraph.Graph {
	t.Helper()
	graph, err := sourcegraph.New([]sourcegraph.Node{{Path: "app.py", Language: "python", Root: ".", Module: "app", Resolution: "file:app.py"}}, nil,
		[]sourcegraph.FactInput{{Analyzer: "python-facts", Protocol: "python-facts/v3", Project: "pyproject.toml", Root: ".", Paths: []string{"app.py"}, FactsSHA256: strings.Repeat("a", 64), PartitionsSHA256: strings.Repeat("b", 64), ResolutionSHA256: strings.Repeat("c", 64)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}
