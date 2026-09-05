package architecture

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestReviewSignalsSelectOneSmallModuleWithoutSizeThresholds(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"src/**"}}}}}
	graph, err := sourcegraph.New([]sourcegraph.Node{{Path: "src/app.ts", Language: "typescript", Root: ".", Module: "app", Resolution: "file:src/app.ts"}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := BuildReviewTopology(repo, graph)
	if err != nil {
		t.Fatal(err)
	}
	signals := ReviewSignals(topology)
	if len(signals) != 1 || signals[0].ID != "single-production-module" || !slices.Equal(signals[0].Paths, []string{"src/app.ts"}) {
		t.Fatalf("signals = %+v", signals)
	}
}

func TestReviewSignalsExposeCatchAllProjectsAndDisconnectedResponsibilities(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: policy.Config{Modules: []policy.Module{{Name: "app", Paths: []string{"**/*.ts"}}}}}
	nodes := []sourcegraph.Node{
		{Path: "web/app.ts", Language: "typescript", Root: "web", Module: "app", Resolution: "file:web/app.ts"},
		{Path: "tools/build.ts", Language: "typescript", Root: "tools", Module: "app", Resolution: "file:tools/build.ts"},
	}
	graph, err := sourcegraph.New(nodes, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := BuildReviewTopology(repo, graph)
	if err != nil {
		t.Fatal(err)
	}
	signals := ReviewSignals(topology)
	for _, id := range []string{"single-production-module", "multiple-projects-or-packages", "catch-all-ownership", "disconnected-responsibilities"} {
		if !slices.ContainsFunc(signals, func(signal ReviewSignal) bool { return signal.ID == id }) {
			t.Fatalf("missing %s in %+v", id, signals)
		}
	}
	edge := sourcegraph.Edge{Source: nodes[0].Path, Target: nodes[1].Path, SourceResolution: nodes[0].Resolution, TargetResolution: nodes[1].Resolution, Line: 2, Column: 1, Ecosystem: "javascript", Kind: sourcegraph.EdgeRuntime}
	graph, err = sourcegraph.New(nodes, []sourcegraph.Edge{edge}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	topology, err = BuildReviewTopology(repo, graph)
	if err != nil {
		t.Fatal(err)
	}
	signals = ReviewSignals(topology)
	if slices.ContainsFunc(signals, func(signal ReviewSignal) bool { return signal.ID == "disconnected-responsibilities" }) || !slices.ContainsFunc(signals, func(signal ReviewSignal) bool { return signal.ID == "empty-declared-graph" }) {
		t.Fatalf("connected internal graph signals = %+v", signals)
	}
}

func TestReviewTopologyIgnoresOrderingAndLocationsButBindsOwnershipAndEdges(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: policy.Config{Modules: []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}, {Name: "api", Paths: []string{"api/**"}, DependsOn: []string{"domain"}}}, Tests: policy.Testing{Ownership: []policy.TestOwnership{{Paths: []string{"api/contract_test.go"}, Module: "api", FocusedSuite: "api-unit"}}}}}
	nodes := []sourcegraph.Node{
		{Path: "domain/model.go", Language: "go", Root: ".", Module: "domain", Resolution: "go:domain"},
		{Path: "api/handler.go", Language: "go", Root: ".", Module: "api", Resolution: "go:api"},
	}
	edge := sourcegraph.Edge{Source: nodes[1].Path, Target: "example.test/domain", SourceResolution: "go:api", TargetResolution: "go:domain", Line: 1, Column: 1, Ecosystem: "go", Kind: sourcegraph.EdgeRuntime}
	build := func() ReviewTopology {
		graph, err := sourcegraph.New(nodes, []sourcegraph.Edge{edge}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		topology, err := BuildReviewTopology(repo, graph)
		if err != nil {
			t.Fatal(err)
		}
		return topology
	}
	first := build()
	slices.Reverse(nodes)
	slices.Reverse(repo.Config.Modules)
	edge.Line = 25
	second := build()
	if first.Identity != second.Identity || len(ReviewSignals(second)) != 0 {
		t.Fatalf("equivalent topology changed: %+v", second)
	}
	repo.Config.Tests.Ownership[0].Module = "domain"
	third := build()
	difference := CompareReviewTopology(first, third)
	if first.Identity == third.Identity || !difference.OwnershipChanged || len(difference.AddedEdges) != 0 {
		t.Fatalf("ownership difference = %+v", difference)
	}
	edge.Kind = sourcegraph.EdgeTypeOnly
	fourth := build()
	difference = CompareReviewTopology(third, fourth)
	if len(difference.AddedEdges) != 1 || len(difference.RemovedEdges) != 1 {
		t.Fatalf("edge difference = %+v", difference)
	}
}
