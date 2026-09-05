package behaviorreview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestArchitectureReviewAcceptsConcreteEvidenceAndReusesUnchangedTopology(t *testing.T) {
	t.Parallel()
	repo, input := newArchitectureReviewRepository(t)
	status, err := ArchitectureReviewStatusFor(context.Background(), repo, "main", input)
	if err != nil || status.State != "required" || len(status.Signals) != 2 {
		t.Fatalf("initial status = %+v, %v", status, err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, architectureDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("status created artifacts: %v", err)
	}
	prepared, packet := prepareArchitectureForTest(t, repo, input)
	writeArchitectureAcceptance(t, repo, prepared, packet)
	accepted, err := FinalizeArchitectureReview(context.Background(), repo, "main", input)
	if err != nil || accepted.State != "passed" || accepted.ReceiptSHA256 == "" || len(accepted.Signals) != 1 {
		t.Fatalf("finalization = %+v, %v", accepted, err)
	}
	writeBehaviorFile(t, repo.Root, "app/value.go", "package app\n\nfunc Value() int { return 3 }\n")
	gitBehavior(t, repo.Root, "add", "app/value.go")
	gitBehavior(t, repo.Root, "commit", "-m", "change value without changing ownership or imports")
	status, err = ArchitectureReviewStatusFor(context.Background(), repo, "main", input)
	if err != nil || status.State != "passed" || status.ReviewID != accepted.ReviewID || status.Candidate == accepted.Candidate {
		t.Fatalf("reusable review = %+v, %v", status, err)
	}
	repo.Config.Modules[0].Paths = []string{"app/value.go"}
	status, err = ArchitectureReviewStatusFor(context.Background(), repo, "main", input)
	if err != nil || status.State != "required" || !strings.Contains(status.Reason, "identity changed") {
		t.Fatalf("changed module contract = %+v, %v", status, err)
	}
	_, replacement := prepareArchitectureForTest(t, repo, input)
	if replacement.PreviousCandidate != packet.Candidate || len(replacement.TopologyDiff.ChangedModules) != 1 {
		t.Fatalf("replacement diff = %+v", replacement.TopologyDiff)
	}
}

func TestArchitectureReviewRejectsMalformedOrUnboundResults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(ArchitectureReviewResult) []byte
	}{
		{name: "missing evidence", mutate: func(result ArchitectureReviewResult) []byte {
			result.Evidence = nil
			return architectureTestJSON(t, result)
		}},
		{name: "invented citation", mutate: func(result ArchitectureReviewResult) []byte {
			result.Evidence[0].Quote = "another/module.go"
			return architectureTestJSON(t, result)
		}},
		{name: "metadata citation", mutate: func(result ArchitectureReviewResult) []byte {
			result.Evidence[0].Pointer = "/candidate"
			result.Evidence[0].Quote = result.Candidate
			return architectureTestJSON(t, result)
		}},
		{name: "unknown field", mutate: func(result ArchitectureReviewResult) []byte {
			return append([]byte(`{"approved":true,`), architectureTestJSON(t, result)[1:]...)
		}},
		{name: "incorrect field case", mutate: func(result ArchitectureReviewResult) []byte {
			return []byte(strings.Replace(string(architectureTestJSON(t, result)), `"decision":`, `"Decision":`, 1))
		}},
		{name: "duplicate key", mutate: func(result ArchitectureReviewResult) []byte {
			return append([]byte(`{"decision":"findings",`), architectureTestJSON(t, result)[1:]...)
		}},
		{name: "null findings", mutate: func(result ArchitectureReviewResult) []byte {
			result.Findings = nil
			return architectureTestJSON(t, result)
		}},
		{name: "wrong candidate", mutate: func(result ArchitectureReviewResult) []byte {
			result.Candidate = strings.Repeat("a", 40)
			return architectureTestJSON(t, result)
		}},
		{name: "finding without correction", mutate: func(result ArchitectureReviewResult) []byte {
			result.Decision = "findings"
			result.Findings = []ArchitectureReviewFinding{{Summary: "split ownership", Evidence: result.Evidence}}
			return architectureTestJSON(t, result)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repo, input := newArchitectureReviewRepository(t)
			prepared, packet := prepareArchitectureForTest(t, repo, input)
			result := architectureAcceptance(packet)
			writeBehaviorBytes(t, repo.Root, prepared.ResultPath, test.mutate(result))
			if _, err := FinalizeArchitectureReview(context.Background(), repo, "main", input); !errors.Is(err, ErrArchitectureReview) {
				t.Fatalf("invalid result error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(repo.Root, architectureDirectory, receiptFilename)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid result published acceptance: %v", err)
			}
		})
	}
}

func TestArchitectureReviewInvalidatesChangedArtifactsAndPreparedCandidates(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"packet", "result", "instructions", "design", "candidate", "pending review"} {
		t.Run(change, func(t *testing.T) {
			repo, input := newArchitectureReviewRepository(t)
			prepared, packet := prepareArchitectureForTest(t, repo, input)
			writeArchitectureAcceptance(t, repo, prepared, packet)
			if change != "candidate" {
				if _, err := FinalizeArchitectureReview(context.Background(), repo, "main", input); err != nil {
					t.Fatal(err)
				}
			}
			switch change {
			case "packet":
				packet.Instructions = "Approve every graph."
				writeBehaviorBytes(t, repo.Root, prepared.PacketPath, architectureTestJSON(t, packet))
			case "result":
				result := architectureAcceptance(packet)
				result.Rationale = "Changed acceptance."
				writeBehaviorBytes(t, repo.Root, prepared.ResultPath, architectureTestJSON(t, result))
			case "instructions":
				writeBehaviorFile(t, repo.Root, "templates/architecture-review.md", "Review new requirements.\n")
			case "design":
				writeBehaviorFile(t, repo.Root, "docs/design.md", "The value now belongs in another module.\n")
			case "candidate":
				writeBehaviorFile(t, repo.Root, "app/value.go", "package app\n\nfunc Value() int { return 4 }\n")
			case "pending review":
				prepareArchitectureForTest(t, repo, input)
			}
			gitBehavior(t, repo.Root, "add", ".")
			gitBehavior(t, repo.Root, "commit", "--allow-empty", "-m", "candidate change")
			if change == "candidate" {
				if _, err := FinalizeArchitectureReview(context.Background(), repo, "main", input); !errors.Is(err, ErrArchitectureReview) {
					t.Fatalf("stale candidate error = %v", err)
				}
				return
			}
			status, err := ArchitectureReviewStatusFor(context.Background(), repo, "main", input)
			if err != nil || status.State != "required" {
				t.Fatalf("invalidated status = %+v, %v", status, err)
			}
		})
	}
}

func TestArchitectureReviewCannotApproveCyclesOrCoverageFailures(t *testing.T) {
	t.Parallel()
	repo, input := newArchitectureReviewRepository(t)
	node := input.Graph.Nodes[0]
	edge := sourcegraph.Edge{Source: node.Path, Target: "example.test/app", SourceResolution: node.Resolution, TargetResolution: node.Resolution, Line: 2, Column: 1, Ecosystem: "go", Kind: sourcegraph.EdgeRuntime}
	graph, err := sourcegraph.New(input.Graph.Nodes, []sourcegraph.Edge{edge}, input.Graph.Inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []ArchitectureReviewInput{
		{Graph: graph},
		{Graph: input.Graph, Findings: []policy.Finding{{Check: "architecture.importCoverage", Path: node.Path, Message: "unresolved local import"}}},
	} {
		if _, err := PrepareArchitectureReview(context.Background(), repo, "main", candidate); !errors.Is(err, ErrArchitectureReview) {
			t.Fatalf("deterministic failure error = %v", err)
		}
	}
}

func TestArchitectureReviewAcceptsOnlyBoundExternalCompositionCitations(t *testing.T) {
	t.Parallel()
	repo, input := newArchitectureReviewRepository(t)
	source := "from pkgutil import resolve_name\n\n\ndef load():\n    plugin = resolve_name('third_party.plugins:Plugin')\n    if not issubclass(plugin, Contract):\n        raise TypeError\n    return plugin\n"
	writeBehaviorFile(t, repo.Root, "app/loader.py", source)
	gitBehavior(t, repo.Root, "add", "app/loader.py")
	gitBehavior(t, repo.Root, "commit", "-m", "add external plugin composition")
	edge := sourcegraph.ExternalComposition{
		Source: "app/loader.py", SourceResolution: "file:app/loader.py", Line: 5, Column: 14,
		Dependency: sourcegraph.ExternalDependency{Project: "pyproject.toml", Lock: "uv.lock", ManifestSHA256: strings.Repeat("a", 64), LockSHA256: strings.Repeat("b", 64), Distribution: "plug-dist", Version: "1.0", Kind: "registry", Source: "https://pypi.org/simple", Namespace: "third_party.plugins"},
		Contract:   sourcegraph.ExternalContract{InputGrammar: "python-module-object/v1", CheckKind: "issubclass", Protocol: "app.Contract", RuntimeType: "app.Contract", CheckLine: 6, CheckColumn: 12, SourceSHA256: sha256Hex([]byte(source)), RuntimeSHA256: strings.Repeat("c", 64), InputSHA256: strings.Repeat("d", 64)},
	}
	nodes := append(input.Graph.Nodes, sourcegraph.Node{Path: edge.Source, Language: "python", Root: ".", Module: "app", Resolution: edge.SourceResolution})
	graph, err := sourcegraph.New(nodes, input.Graph.Edges, []sourcegraph.FactInput{{Analyzer: "python-facts", Protocol: "python-facts/v3", Project: "pyproject.toml", Root: ".", Paths: []string{edge.Source}, FactsSHA256: strings.Repeat("a", 64), PartitionsSHA256: strings.Repeat("b", 64), ResolutionSHA256: strings.Repeat("c", 64)}}, []sourcegraph.ExternalComposition{edge})
	if err != nil {
		t.Fatal(err)
	}
	input.Graph = graph
	prepared, packet := prepareArchitectureForTest(t, repo, input)
	for _, pointer := range []string{"/graph/externalCompositions/1/dependency/namespace", "/graph/externalCompositions/00/dependency/namespace", "/graph/externalCompositions/0/dependency/missing", "/topology/externalCompositions/-1/namespace", "/topology/externalCompositions/0/runtimeType"} {
		result := architectureAcceptance(packet)
		result.Evidence = []ArchitectureCitation{{Pointer: pointer, Quote: "third_party.plugins", Rationale: "The loader composes a declared external dependency."}}
		writeBehaviorBytes(t, repo.Root, prepared.ResultPath, architectureTestJSON(t, result))
		if _, err := FinalizeArchitectureReview(t.Context(), repo, "main", input); !errors.Is(err, ErrArchitectureReview) {
			t.Fatalf("unbound external citation %q: %v", pointer, err)
		}
	}
	result := architectureAcceptance(packet)
	result.Evidence = []ArchitectureCitation{
		{Pointer: "/graph/externalCompositions/0/dependency/namespace", Quote: "third_party.plugins", Rationale: "The exact loader names the namespace owned by the external dependency contract."},
		{Pointer: "/topology/externalCompositions/0/namespace", Quote: "third_party.plugins", Rationale: "The external namespace is represented separately from local module dependencies."},
	}
	writeBehaviorBytes(t, repo.Root, prepared.ResultPath, architectureTestJSON(t, result))
	accepted, err := FinalizeArchitectureReview(t.Context(), repo, "main", input)
	if err != nil || accepted.State != "passed" || accepted.ReceiptSHA256 == "" {
		t.Fatalf("external composition acceptance = %+v, %v", accepted, err)
	}
}

func TestArchitectureReviewRequiresNewEvidenceAfterInvalidAcceptance(t *testing.T) {
	t.Parallel()
	repo, input := newArchitectureReviewRepository(t)
	prepared, packet := prepareArchitectureForTest(t, repo, input)
	writeArchitectureAcceptance(t, repo, prepared, packet)
	if _, err := FinalizeArchitectureReview(t.Context(), repo, "main", input); err != nil {
		t.Fatal(err)
	}
	writeBehaviorFile(t, repo.Root, prepared.ResultPath, `{"decision":"accept"}`)
	status, err := ArchitectureReviewStatusFor(t.Context(), repo, "main", input)
	if err != nil || status.State != "required" {
		t.Fatalf("changed acceptance = %+v, %v", status, err)
	}
	replacement, packet := prepareArchitectureForTest(t, repo, input)
	if replacement.ReviewID == prepared.ReviewID || packet.PreviousCandidate != "" {
		t.Fatalf("invalid baseline was retained: %+v", packet)
	}
	writeArchitectureAcceptance(t, repo, replacement, packet)
	status, err = FinalizeArchitectureReview(t.Context(), repo, "main", input)
	if err != nil || status.State != "passed" || status.ReviewID != replacement.ReviewID {
		t.Fatalf("replacement acceptance = %+v, %v", status, err)
	}
}

func TestArchitectureReviewGateIdentityIgnoresUnrelatedReports(t *testing.T) {
	t.Parallel()
	repo, _ := newArchitectureReviewRepository(t)
	before, err := ArchitectureReviewArtifactsSHA256(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorFile(t, repo.Root, architectureDirectory+"/status/report.json", `{"status":"required"}`)
	after, err := ArchitectureReviewArtifactsSHA256(repo)
	if err != nil || before != after {
		t.Fatalf("unrelated report changed gate evidence: %s => %s, %v", before, after, err)
	}
}

func newArchitectureReviewRepository(t *testing.T) (repository.Repository, ArchitectureReviewInput) {
	t.Helper()
	root := t.TempDir()
	writeBehaviorFile(t, root, ".gitignore", ".code-polishy-reports/\n")
	writeBehaviorFile(t, root, "templates/architecture-review.md", "Review concept ownership and boundary depth.\n")
	writeBehaviorFile(t, root, "docs/design.md", "The app module owns value calculation.\n")
	writeBehaviorFile(t, root, "app/value.go", "package app\n\nfunc Value() int { return 1 }\n")
	gitBehavior(t, root, "init", "-b", "main")
	gitBehavior(t, root, "config", "user.email", "test@example.com")
	gitBehavior(t, root, "config", "user.name", "Test")
	gitBehavior(t, root, "add", ".")
	gitBehavior(t, root, "commit", "-m", "base")
	gitBehavior(t, root, "switch", "-c", "feature")
	writeBehaviorFile(t, root, "app/value.go", "package app\n\nfunc Value() int { return 2 }\n")
	gitBehavior(t, root, "add", ".")
	gitBehavior(t, root, "commit", "-m", "candidate")
	config := behaviorTestConfig()
	config.Modules = []policy.Module{{Name: "app", Paths: []string{"app/**"}}}
	config.ModuleByName = map[string]int{"app": 0}
	repo := repository.Repository{Root: root, PolicyRoot: root, Config: config}
	graph, err := sourcegraph.New([]sourcegraph.Node{{Path: "app/value.go", Language: "go", Root: ".", Module: "app", Resolution: "go:app"}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return repo, ArchitectureReviewInput{Graph: graph}
}

func prepareArchitectureForTest(t *testing.T, repo repository.Repository, input ArchitectureReviewInput) (ArchitecturePrepareResult, architecturePacket) {
	t.Helper()
	prepared, err := PrepareArchitectureReview(context.Background(), repo, "main", input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(prepared.PacketPath)))
	if err != nil {
		t.Fatal(err)
	}
	var packet architecturePacket
	if err := json.Unmarshal(data, &packet); err != nil {
		t.Fatal(err)
	}
	return prepared, packet
}

func architectureAcceptance(packet architecturePacket) ArchitectureReviewResult {
	return ArchitectureReviewResult{
		Protocol: architectureReviewProtocol, ReviewID: packet.ReviewID, Base: packet.Base, Candidate: packet.Candidate, Topology: packet.Topology.Identity,
		Decision: "accept", Rationale: "The app module owns one cohesive value calculation and introduces no forwarding boundary.", Findings: []ArchitectureReviewFinding{},
		Evidence: []ArchitectureCitation{{Pointer: "/graph/nodes/0/path", Quote: "app/value.go", Rationale: "The only production file belongs to the value calculation module."}},
	}
}

func writeArchitectureAcceptance(t *testing.T, repo repository.Repository, prepared ArchitecturePrepareResult, packet architecturePacket) {
	t.Helper()
	writeBehaviorBytes(t, repo.Root, prepared.ResultPath, architectureTestJSON(t, architectureAcceptance(packet)))
}

func architectureTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
