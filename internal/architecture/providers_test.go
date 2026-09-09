package architecture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestProviderImportsParticipateInNativeGraphAndModulePolicy(t *testing.T) {
	repo := providerGraphRepository(t, "foreign")
	boundary := providerGraphRunner{}
	result := AnalyzeWithRunner(t.Context(), repo, []string{"foreign/main.fixture"}, boundary)
	if result.Graph == nil {
		t.Fatalf("provider/native graph is incomplete: %+v", result.Findings)
	}
	if !slices.ContainsFunc(result.Findings, func(finding policy.Finding) bool {
		return finding.Check == "architecture.moduleDependency" && finding.Path == "foreign/main.fixture" && finding.Subject == "native"
	}) {
		t.Fatalf("prohibited provider import disappeared: %+v", result.Findings)
	}
	repo.Config.Modules[0].DependsOn = []string{"native"}
	result = AnalyzeWithRunner(t.Context(), repo, []string{"foreign/main.fixture"}, boundary)
	if result.Graph == nil || len(result.Findings) != 0 {
		t.Fatalf("declared dependency failed: %+v", result.Findings)
	}
	found := false
	for _, edge := range result.Graph.Edges {
		if edge.Source == "foreign/main.fixture" && edge.Target == "native/value.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("provider-to-native edge was omitted")
	}
	result = AnalyzeWithRunner(t.Context(), repo, []string{"foreign/main.fixture"}, providerGraphRunner{omit: true})
	if result.Graph != nil || !slices.ContainsFunc(result.Findings, func(finding policy.Finding) bool { return finding.Check == "policy.packOperation" }) {
		t.Fatalf("omitted provider coverage passed: %+v", result)
	}
}

func TestProviderOwnedPythonDoesNotRequireANativeProject(t *testing.T) {
	repo := providerGraphRepository(t, "python")
	repo.Config.Modules[0].DependsOn = []string{"native"}
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"foreign/main.fixture"}, providerGraphRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 {
		t.Fatalf("provider-owned Python acquired native project requirements: %+v", analysis)
	}
}

func providerGraphRepository(t *testing.T, language string) repository.Repository {
	t.Helper()
	root, source, store := t.TempDir(), t.TempDir(), t.TempDir()
	t.Cleanup(func() {
		err := filepath.WalkDir(store, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return os.Chmod(path, 0o755)
			}
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	})
	writeArchitectureFile(t, root, "go.mod", "module example.test/mixed\n\ngo 1.26.6\n")
	writeArchitectureFile(t, root, "native/value.go", "package native\n\nfunc Value() int { return 1 }\n")
	writeArchitectureFile(t, root, "foreign/main.fixture", "load native/value.go\n")
	writeArchitectureFile(t, source, "README.md", "Graph provider fixture\n")
	writeArchitectureFile(t, source, "bin/analyze", "fixture analyzer entry\n")
	writeArchitectureFile(t, source, "fixtures/pass/main.fixture", "valid\n")
	writeArchitectureFile(t, source, "fixtures/fail/main.fixture", "invalid\n")
	manifest := fmt.Sprintf(`{"manifestVersion":2,"protocolVersion":3,"name":"graph-proof","version":"1.0.0","platforms":[%q],"languages":[{"id":%q,"sourcePatterns":["**/*.fixture"]}],"commands":[{"name":"analyze","argv":["bin/analyze"],"capabilities":["architecture"],"profiles":["check","gate"],"timeoutSeconds":10}],"fixtures":[{"name":"pass","command":"analyze","capability":"architecture","project":"fixtures/pass","files":["main.fixture"],"expectedStatus":"pass"},{"name":"fail","command":"analyze","capability":"architecture","project":"fixtures/fail","files":["main.fixture"],"expectedStatus":"findings","expectedRules":["unresolved"]}]}`, pack.CurrentPlatform(), language)
	writeArchitectureFile(t, source, pack.ManifestFilename, manifest)
	identity, _, err := pack.Install(source, store)
	if err != nil {
		t.Fatal(err)
	}
	config := policy.Config{Modules: []policy.Module{{Name: "foreign", Paths: []string{"foreign/**"}}, {Name: "native", Paths: []string{"native/**"}}}, ModuleByName: map[string]int{"foreign": 0, "native": 1}}
	pack.Apply(&config, pack.Resolve([]policy.PackSelection{{Name: identity.Name, Version: identity.Version, Digest: identity.Digest}}, store))
	repo, err := repository.Open(root, root, config)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

type providerGraphRunner struct{ omit bool }

func (boundary providerGraphRunner) Run(context.Context, string, policy.Command) error {
	return fmt.Errorf("structured analysis is required")
}

func (boundary providerGraphRunner) RunStructured(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	request := pack.Request{}
	if err := json.Unmarshal(command.Stdin, &request); err != nil {
		return runner.Result{}, runner.Output{}, err
	}
	analyzed := request.Files
	if boundary.omit {
		analyzed = []string{}
	}
	imports := []pack.ImportFact{{Path: "foreign/main.fixture", Line: 1, Column: 1, Specifier: "./native/value.go", Resolved: "native/value.go", Kind: "runtime"}}
	data, err := os.ReadFile(filepath.Join(request.ProjectRoot, "native/value.go"))
	if err != nil {
		return runner.Result{}, runner.Output{}, err
	}
	digest := sha256.Sum256(data)
	inputs := append(slices.Clone(request.Context), pack.InputFile{Path: "native/value.go", SHA256: hex.EncodeToString(digest[:])})
	response := pack.Response{ProtocolVersion: 3, Status: "pass", Evidence: []string{"fixture parser completed"}, Inputs: inputs, Coverage: &pack.Coverage{Analyzed: analyzed, Unsupported: []pack.Unsupported{}}, Facts: &pack.SourceFacts{Imports: &imports}}
	data, err = json.Marshal(response)
	return runner.Result{}, runner.Output{Stdout: data}, err
}

func TestFocusedProviderArchitectureDoesNotScheduleUnselectedLanguages(t *testing.T) {
	repo := providerGraphRepository(t, "foreign")
	files, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	if operations := providerOperations(repo, []string{"native/value.go"}, files); len(operations) != 0 {
		t.Fatalf("native selection scheduled foreign provider: %+v", operations)
	}
	operations := providerOperations(repo, []string{"foreign/main.fixture"}, files)
	if len(operations) != 1 || operations[0].selection.All {
		t.Fatalf("focused provider selection became global: %+v", operations)
	}
}
