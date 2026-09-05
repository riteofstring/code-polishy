package architecture

import (
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
)

func TestReviewTopologyDistinguishesExternalContractChangesFromProofRefresh(t *testing.T) {
	repo, graphRunner := externalArchitectureFixture(t, false)
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner)
	if analysis.Graph == nil || len(analysis.Findings) != 0 {
		t.Fatalf("external graph: %+v", analysis)
	}
	first, err := BuildReviewTopology(repo, *analysis.Graph)
	if err != nil || len(first.External) != 1 {
		t.Fatalf("review omitted external composition: %+v, %v", first, err)
	}
	changed := sourcegraph.Clone(analysis.Graph)
	changed.External[0].Contract.RuntimeSHA256 = strings.Repeat("f", 64)
	refreshed, err := sourcegraph.New(changed.Nodes, changed.Edges, changed.Inputs, changed.External)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildReviewTopology(repo, refreshed)
	if err != nil || first.Identity != second.Identity || refreshed.Identity == analysis.Graph.Identity {
		t.Fatalf("proof refresh changed topology or retained old graph identity: %+v, %v", second, err)
	}
	changed.External[0].Dependency.Namespace = "another.namespace"
	replaced, err := sourcegraph.New(changed.Nodes, changed.Edges, changed.Inputs, changed.External)
	if err != nil {
		t.Fatal(err)
	}
	third, err := BuildReviewTopology(repo, replaced)
	if err != nil {
		t.Fatal(err)
	}
	difference := CompareReviewTopology(first, third)
	if first.Identity == third.Identity || len(difference.AddedExternal) != 1 || len(difference.RemovedExternal) != 1 || len(difference.AddedEdges) != 0 || len(difference.RemovedEdges) != 0 {
		t.Fatalf("external contract change did not remain distinct from local dependencies: %+v", difference)
	}
}
