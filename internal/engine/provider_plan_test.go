package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/quality"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type providerPlanRunner struct {
	capabilities []string
	roots        []string
}

func (boundary *providerPlanRunner) Run(context.Context, string, policy.Command) error {
	return fmt.Errorf("provider requires structured execution")
}

func (boundary *providerPlanRunner) RunStructured(_ context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	request := pack.Request{}
	if err := json.Unmarshal(command.Stdin, &request); err != nil {
		return runner.Result{}, runner.Output{}, err
	}
	boundary.capabilities = append(boundary.capabilities, request.Capability)
	boundary.roots = append(boundary.roots, root)
	imports, comments, functions := []pack.ImportFact{}, []pack.CommentFact{}, []pack.FunctionFact{}
	facts := &pack.SourceFacts{}
	switch request.Capability {
	case "architecture":
		facts.Imports = &imports
	case "lint":
		facts.Comments = &comments
	case "complexity":
		facts.Functions = &functions
	}
	response := pack.Response{ProtocolVersion: 3, Status: "pass", Evidence: []string{"fixture parsed"}, Coverage: &pack.Coverage{Analyzed: request.Files, Unsupported: []pack.Unsupported{}}, Inputs: request.Context, Facts: facts}
	data, err := json.Marshal(response)
	return runner.Result{ExitStatus: 0}, runner.Output{Stdout: data}, err
}

func TestPlannedPackExecutionAccountsForEveryCapability(t *testing.T) {
	repo := providerPlanRepository(t)
	selection := repository.Selection{Files: []string{"src/main.fixture"}, All: true}
	plan := mergeGateCheckCommands(gaterun.Check, plannedPolicyCheckCommands(repo, selection, "gate"))
	delegate := &providerPlanRunner{}
	boundary := &mergeGatePlannedRunner{root: repo.Root, expected: plan, delegate: delegate}
	findings := quality.Check(t.Context(), repo, selection, boundary, "gate")
	analysis := architecture.AnalyzeWithRunner(t.Context(), repo.WithAnalysisProfile("gate"), selection.Files, boundary)
	findings = append(findings, analysis.Findings...)
	if len(findings) != 0 || boundary.err != nil || boundary.next != len(plan) {
		t.Fatalf("provider gate did not complete: %v, %+v, %d/%d", boundary.err, findings, boundary.next, len(plan))
	}
	want := []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}
	if !slices.Equal(delegate.capabilities, want) || analysis.Graph == nil {
		t.Fatalf("executed %v; graph=%+v", delegate.capabilities, analysis.Graph)
	}
	for index, root := range delegate.roots {
		if root == repo.Root || root != plan[index].Root {
			t.Fatalf("wrong provider root %q", root)
		}
		if gateRunCommandSpec(plan[index], nil).InputSHA256 == "" {
			t.Fatal("provider input was not bound to the gate identity")
		}
	}
	repo.Config.Scope.Generated = []string{"src/main.fixture"}
	generated := plannedPolicyCheckCommands(repo, selection, "gate")
	for _, command := range generated {
		if command.Adapter.Capability == "format" || command.Adapter.Capability == "complexity" {
			t.Fatalf("empty provider operation was planned: %s", command.Name)
		}
	}
}

func TestPlannedPackExecutionRejectsIdentityChanges(t *testing.T) {
	repo := providerPlanRepository(t)
	selection := repository.Selection{Files: []string{"src/main.fixture"}, All: true}
	planned := mergeGateCheckCommands(gaterun.Check, plannedPolicyCheckCommands(repo, selection, "gate"))[0]
	cases := []struct {
		name   string
		mutate func(*MergeGateExecutionCommand)
	}{
		{"root", func(value *MergeGateExecutionCommand) { value.Root = repo.Root }},
		{"arguments", func(value *MergeGateExecutionCommand) { value.Command.Argv = []string{"other"} }},
		{"request", func(value *MergeGateExecutionCommand) { value.Command.Stdin = []byte("different capability or input") }},
		{"environment", func(value *MergeGateExecutionCommand) { value.Command.Environment = []string{"UNPLANNED"} }},
		{"provider", func(value *MergeGateExecutionCommand) {
			copy := *value.Command.Adapter
			copy.PackDigest = "changed"
			value.Command.Adapter = &copy
		}},
	}
	for _, example := range cases {
		t.Run(example.name, func(t *testing.T) {
			actual := planned
			example.mutate(&actual)
			boundary := &mergeGatePlannedRunner{root: repo.Root, expected: []MergeGateExecutionCommand{planned}, delegate: &providerPlanRunner{}}
			if _, _, err := boundary.RunStructured(t.Context(), actual.Root, actual.Command); err == nil {
				t.Fatal("changed provider execution was accepted")
			}
			if boundary.next != 0 {
				t.Fatal("failed command advanced acceptance")
			}
		})
	}
}

func providerPlanRepository(t *testing.T) repository.Repository {
	t.Helper()
	source := t.TempDir()
	capabilities := []string{"format", "lint", "typecheck", "complexity", "dead-code", "architecture"}
	manifest := pack.Manifest{ManifestVersion: 2, ProtocolVersion: 3, Name: "fixture-language", Version: "1.0.0", Platforms: []string{pack.CurrentPlatform()}, Languages: []pack.Language{{ID: "fixture", SourcePatterns: []string{"**/*.fixture"}}}, Commands: []pack.Command{{Name: "analyze", Argv: []string{"adapter"}, Capabilities: capabilities, Profiles: []string{"check", "gate"}, TimeoutSeconds: 30}}}
	for _, capability := range capabilities {
		for _, status := range []string{"pass", "findings"} {
			fixture := pack.Fixture{Name: capability + "-" + status, Command: "analyze", Capability: capability, Project: "fixtures/" + status, Files: []string{"main.fixture"}, ExpectedStatus: status}
			if status == "findings" {
				fixture.ExpectedRules = []string{"invalid"}
			}
			manifest.Fixtures = append(manifest.Fixtures, fixture)
			writeEngineFile(t, source, fixture.Project+"/main.fixture", "fixture\n", 0o600)
		}
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, source, pack.ManifestFilename, string(data), 0o600)
	writeEngineFile(t, source, "README.md", "# Fixture provider\n", 0o600)
	writeEngineFile(t, source, "adapter", "fixture adapter\n", 0o755)
	store := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(store, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				_ = os.Chmod(path, 0o755)
			}
			return nil
		})
	})
	identity, _, err := pack.Install(source, store)
	if err != nil {
		t.Fatal(err)
	}
	config := policy.Config{Quality: policy.EffectiveQuality(policy.Quality{}), Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}}, ModuleByName: map[string]int{"application": 0}}
	pack.Apply(&config, pack.Resolve([]policy.PackSelection{{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}}, store))
	root := t.TempDir()
	writeEngineFile(t, root, "src/main.fixture", "fixture\n", 0o600)
	repo, err := repository.Open(root, enginePolicyRoot(t), config)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
