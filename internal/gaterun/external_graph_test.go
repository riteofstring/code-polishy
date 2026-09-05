package gaterun

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
)

func TestGateReportBindsExternalCompositionAndRejectsChangedProof(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, t.TempDir(), identity)
	recordAttempt(t, run, 0, Passed, 0, "ok", "", 16)
	base := gateReportTestGraph(t)
	edge := sourcegraph.ExternalComposition{Source: "app.py", SourceResolution: "file:app.py", Line: 4, Column: 14, Dependency: sourcegraph.ExternalDependency{Project: "pyproject.toml", Lock: "uv.lock", ManifestSHA256: strings.Repeat("a", 64), LockSHA256: strings.Repeat("b", 64), Distribution: "plug-dist", Version: "1.0", Kind: "registry", Source: "https://pypi.org/simple", Namespace: "third_party.plugins"}, Contract: sourcegraph.ExternalContract{InputGrammar: "python-module-object/v1", CheckKind: "issubclass", Protocol: "app.Contract", RuntimeType: "app.Contract", CheckLine: 5, CheckColumn: 12, SourceSHA256: strings.Repeat("c", 64), RuntimeSHA256: strings.Repeat("d", 64), InputSHA256: strings.Repeat("e", 64)}}
	graph, err := sourcegraph.New(base.Nodes, base.Edges, base.Inputs, []sourcegraph.ExternalComposition{edge})
	if err != nil {
		t.Fatal(err)
	}
	expected := sourcegraph.Clone(&graph)
	prepared, err := run.PrepareFinalization(FinalizeOptions{Status: RunPassed, BehaviorReview: identity.BehaviorReview, SourceDependencyGraph: &graph})
	if err != nil {
		t.Fatal(err)
	}
	graph.External[0].Dependency.Namespace = "another.namespace"
	report, err := prepared.Commit()
	if err != nil || !reflect.DeepEqual(report.SourceDependencyGraph, expected) {
		t.Fatalf("published report did not freeze external evidence: %v", err)
	}
	loaded, err := LoadReport(run.repositoryRoot, identity)
	if err != nil || !reflect.DeepEqual(loaded.SourceDependencyGraph, expected) {
		t.Fatalf("saved report lost external evidence: %v", err)
	}
	changed := cloneReport(loaded)
	changed.SourceDependencyGraph.External[0].Contract.RuntimeSHA256 = strings.Repeat("f", 64)
	if err := validateReport(changed, identity); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("changed runtime proof retained gate acceptance: %v", err)
	}
	canonical, err := sourcegraph.New(changed.SourceDependencyGraph.Nodes, changed.SourceDependencyGraph.Edges, changed.SourceDependencyGraph.Inputs, changed.SourceDependencyGraph.External)
	if err != nil {
		t.Fatal(err)
	}
	changed.SourceDependencyGraph = &canonical
	digest, err := reportDigest(changed)
	if err != nil || digest == loaded.SHA256 || !reflect.DeepEqual(loaded.SourceDependencyGraph, expected) {
		t.Fatalf("external evidence was omitted from report identity or aliased: %v", err)
	}
}
