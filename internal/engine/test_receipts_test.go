package engine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestSuiteReceiptIdentityTracksBoundedInputs(t *testing.T) {
	t.Parallel()
	root := reusableContentRepository(t, nil)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policyEngine.Repository.Config.Tests.Suites[0]
	initial := receiptIdentityDigest(t, policyEngine, suite)
	writeEngineFile(t, root, "README.md", "unrelated prose\n", 0o600)
	if afterDocumentation := receiptIdentityDigest(t, policyEngine, suite); afterDocumentation != initial {
		t.Fatalf("unrelated documentation changed suite identity: %s != %s", afterDocumentation, initial)
	}
	writeEngineFile(t, root, "content/data.json", "{\"changed\":true}\n", 0o600)
	if afterSource := receiptIdentityDigest(t, policyEngine, suite); afterSource == initial {
		t.Fatal("owned production change did not invalidate suite identity")
	}
}

func TestSuiteReceiptIdentityTracksContainedRunnerAndExtraInputs(t *testing.T) {
	t.Parallel()
	root := reusableContentRepository(t, nil)
	writeEngineFile(t, root, "scripts/test.sh", "#!/bin/sh\nexit 0\n", 0o700)
	writeEngineFile(t, root, "fixtures/input.txt", "first\n", 0o600)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policyEngine.Repository.Config.Tests.Suites[0]
	suite.Argv = []string{"./scripts/test.sh"}
	suite.ExtraInputs = []string{"fixtures/**"}
	initial := receiptIdentityDigest(t, policyEngine, suite)
	writeEngineFile(t, root, "scripts/test.sh", "#!/bin/sh\nexit 1\n", 0o700)
	if changedRunner := receiptIdentityDigest(t, policyEngine, suite); changedRunner == initial {
		t.Fatal("runner change did not invalidate suite identity")
	}
	writeEngineFile(t, root, "scripts/test.sh", "#!/bin/sh\nexit 0\n", 0o700)
	writeEngineFile(t, root, "fixtures/input.txt", "second\n", 0o600)
	if changedInput := receiptIdentityDigest(t, policyEngine, suite); changedInput == initial {
		t.Fatal("extra input change did not invalidate suite identity")
	}
}

func TestSuiteReceiptIdentityRejectsUnversionedExternalTool(t *testing.T) {
	t.Parallel()
	root := reusableContentRepository(t, nil)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policyEngine.Repository.Config.Tests.Suites[0]
	suite.Argv = []string{"unknown-test-tool"}
	_, reason, err := policyEngine.suiteReceiptIdentity(suite)
	if err != nil || reason == "" {
		t.Fatalf("reason = %q, error = %v", reason, err)
	}
}

func TestSuiteReceiptIdentityRequiresExplicitReuse(t *testing.T) {
	t.Parallel()
	root := reusableContentRepository(t, nil)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policyEngine.Repository.Config.Tests.Suites[0]
	suite.Reusable = false
	_, reason, err := policyEngine.suiteReceiptIdentity(suite)
	if err != nil || reason != "suite is not explicitly reusable" {
		t.Fatalf("reason = %q, error = %v", reason, err)
	}
}

func TestReusableExecutionViewExposesOnlyDeclaredInputsAndRejectsWrites(t *testing.T) {
	t.Parallel()
	repositoryRoot := t.TempDir()
	writeEngineFile(t, repositoryRoot, "declared/input.txt", "declared\n", 0o600)
	writeEngineFile(t, repositoryRoot, "undeclared/input.txt", "ambient\n", 0o600)
	repository := repositoryFixture(t, repositoryRoot)
	inputs, reason, err := hashSuiteReceiptInputs(repository, []string{"declared/input.txt"})
	if err != nil || reason != "" {
		t.Fatalf("inputs reason = %q, error = %v", reason, err)
	}
	view, err := createSuiteExecutionView(repositoryRoot, ".", inputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(view.root, "declared", "input.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(view.root, "undeclared", "input.txt")); !os.IsNotExist(err) {
		t.Fatalf("undeclared input was exposed: %v", err)
	}
	if err := os.Chmod(filepath.Join(view.root, "declared"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(view.root, "declared", "written.txt"), []byte("write\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := view.close(); err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("view write error = %v", err)
	}
}

func repositoryFixture(t *testing.T, root string) repository.Repository {
	t.Helper()
	return repository.Repository{Root: root}
}

func receiptIdentityDigest(t *testing.T, policyEngine *Engine, suite policy.TestSuite) string {
	t.Helper()
	identity, reason, err := policyEngine.suiteReceiptIdentity(suite)
	if err != nil || reason != "" {
		t.Fatalf("identity reason = %q, error = %v", reason, err)
	}
	digest, err := identity.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestSuiteReceiptInputRejectsLinkedRunner(t *testing.T) {
	t.Parallel()
	root := reusableContentRepository(t, nil)
	outside := filepath.Join(t.TempDir(), "runner")
	if err := os.WriteFile(outside, []byte("runner\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "scripts", "test.sh")); err != nil {
		t.Fatal(err)
	}
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policyEngine.Repository.Config.Tests.Suites[0]
	suite.Argv = []string{"./scripts/test.sh"}
	_, reason, err := policyEngine.suiteReceiptIdentity(suite)
	if err != nil || reason == "" {
		t.Fatalf("reason = %q, error = %v", reason, err)
	}
}

func TestSupplementalResumeReusesExactPassedSuite(t *testing.T) {
	root := reusableContentRepository(t, nil)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policy.TestSuite{
		Name: "content-mutation", Kind: "mutation", Scope: "module", Reusable: true, Cost: "expensive", Modules: []string{"content"},
		Argv: []string{"go", "test", "./content/..."}, Cwd: ".", RunOn: []string{"supplemental"},
		ExclusiveResources: []string{}, TimeoutSeconds: 3600,
	}
	policyEngine.Repository.Config.Tests.Suites = append(policyEngine.Repository.Config.Tests.Suites, suite)
	commandRunner := &countingReceiptRunner{}
	policyEngine.Runner = commandRunner
	policyEngine.Output = io.Discard
	first, err := policyEngine.Test(t.Context(), testpolicy.Request{Supplemental: true})
	if err != nil || len(first.Findings) != 0 || commandRunner.runs != 1 {
		t.Fatalf("first report = %+v, runs = %d, error = %v", first, commandRunner.runs, err)
	}
	second, err := policyEngine.Test(t.Context(), testpolicy.Request{Supplemental: true, Resume: true})
	if err != nil || len(second.Findings) != 0 || commandRunner.runs != 1 || len(second.TestCommands) != 1 ||
		!second.TestCommands[0].Reused || second.TestCommands[0].ReceiptSHA256 == "" {
		t.Fatalf("second report = %+v, runs = %d, error = %v", second, commandRunner.runs, err)
	}
}

func TestExactSuitePassComposesWithSupplementalResume(t *testing.T) {
	root := reusableContentRepository(t, nil)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policy.TestSuite{
		Name: "content-mutation", Kind: "mutation", Scope: "module", Reusable: true, Cost: "expensive", Modules: []string{"content"},
		Argv: []string{"go", "test", "./content/..."}, Cwd: ".", RunOn: []string{"supplemental"},
		ExclusiveResources: []string{}, TimeoutSeconds: 3600,
	}
	policyEngine.Repository.Config.Tests.Suites = append(policyEngine.Repository.Config.Tests.Suites, suite)
	commandRunner := &countingReceiptRunner{}
	policyEngine.Runner = commandRunner
	policyEngine.Output = io.Discard
	first, err := policyEngine.Test(t.Context(), testpolicy.Request{Suites: []string{suite.Name}})
	if err != nil || len(first.Findings) != 0 || commandRunner.runs != 1 {
		t.Fatalf("exact report = %+v, runs = %d, error = %v", first, commandRunner.runs, err)
	}
	second, err := policyEngine.Test(t.Context(), testpolicy.Request{Supplemental: true, Resume: true})
	if err != nil || len(second.Findings) != 0 || commandRunner.runs != 1 || len(second.TestCommands) != 1 ||
		!second.TestCommands[0].Reused {
		t.Fatalf("resume report = %+v, runs = %d, error = %v", second, commandRunner.runs, err)
	}
}

func TestMergeGateReturnsAlreadyPassedForExactSuccessfulIdentity(t *testing.T) {
	root := reusableContentRepository(t, nil)
	installRequiredBehaviorReviewPolicy(t, root, "checkpoint")
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	captureEngineBehaviorIntent(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	commitEngineCandidate(t, root, "candidate")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	policyEngine.Output = io.Discard
	first, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || len(first.Findings) != 0 || first.GateRunPolicy == nil || first.GateRunPolicy.Status != "passed" {
		t.Fatalf("first report = %+v, error = %v", first, err)
	}
	firstCommands := append([]string{}, commandRunner.commands...)
	second, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || len(second.Findings) != 0 || second.GateRunPolicy == nil || second.GateRunPolicy.Status != "already-passed" {
		t.Fatalf("second report = %+v, error = %v", second, err)
	}
	if !slices.Equal(commandRunner.commands, firstCommands) || second.GateRunPolicy.ReportPath == "" ||
		!slices.Equal(second.GateRunPolicy.ReusedPhases, []string{"focused"}) {
		t.Fatalf("commands = %v, first = %v, policy = %+v", commandRunner.commands, firstCommands, second.GateRunPolicy)
	}
}

func TestMergeGateReusesOnlyUnchangedSuitesAcrossCandidates(t *testing.T) {
	root := reusableContentRepository(t, nil)
	configPath := filepath.Join(root, policy.ConfigFilename)
	configuration, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(configuration),
		`"modules": [{"name":"content","paths":["content/**"]}]`,
		`"modules": [{"name":"content","paths":["content/**"]},{"name":"a","paths":["a/**"]},{"name":"b","paths":["b/**"]}]`, 1)
	configured = strings.Replace(configured,
		`{"name":"focused","kind":"content","scope":"module","modules":["content"],"reusable":true,"argv":["go","test","./..."]},`,
		`{"name":"focused","kind":"content","scope":"module","modules":["content"],"reusable":true,"argv":["go","test","./..."]},
    {"name":"a-focused","kind":"content","scope":"module","modules":["a"],"reusable":true,"argv":["go","test","./a/..."]},
    {"name":"b-focused","kind":"content","scope":"module","modules":["b"],"reusable":true,"argv":["go","test","./b/..."]},`, 1)
	if configured == string(configuration) {
		t.Fatal("content fixture configuration did not change")
	}
	writeEngineFile(t, root, policy.ConfigFilename, configured, 0o600)
	writeEngineFile(t, root, "a/data.json", "{}\n", 0o600)
	writeEngineFile(t, root, "b/data.json", "{}\n", 0o600)
	installRequiredBehaviorReviewPolicy(t, root, "checkpoint")
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	captureEngineBehaviorIntent(t, root)
	configuration, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, policy.ConfigFilename, string(configuration)+"\n", 0o600)
	writeEngineFile(t, root, "a/data.json", "{\"candidate\":1}\n", 0o600)
	commitEngineCandidate(t, root, "first candidate")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	firstRunner := &recordingEngineRunner{}
	policyEngine.Runner = firstRunner
	policyEngine.Output = io.Discard
	first, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || len(first.Findings) != 0 || first.GateRunPolicy == nil || first.GateRunPolicy.Status != "passed" {
		t.Fatalf("first report = %+v, commands = %v, error = %v", first, firstRunner.commands, err)
	}
	writeEngineFile(t, root, "b/data.json", "{\"candidate\":2}\n", 0o600)
	gitBehaviorReview(t, root, "add", "b/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "second candidate")
	policyEngine, err = Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	secondRunner := &recordingEngineRunner{}
	policyEngine.Runner = secondRunner
	policyEngine.Output = io.Discard
	second, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || len(second.Findings) != 0 || second.GateRunPolicy == nil || second.GateRunPolicy.Status != "passed" {
		t.Fatalf("second report = %+v, commands = %v, error = %v", second, secondRunner.commands, err)
	}
	if slices.Contains(secondRunner.commands, "a-focused") || !slices.Contains(second.GateRunPolicy.ReusedPhases, "a-focused") {
		t.Fatalf("unchanged suite was not reused: commands = %v, policy = %+v", secondRunner.commands, second.GateRunPolicy)
	}
	for _, name := range []string{"b-focused", "full"} {
		if !slices.Contains(secondRunner.commands, name) {
			t.Fatalf("changed suite %q was reused: commands = %v", name, secondRunner.commands)
		}
	}
}

type countingReceiptRunner struct{ runs int }

func (commandRunner *countingReceiptRunner) Run(context.Context, string, policy.Command) error {
	commandRunner.runs++
	return nil
}

func reusableContentRepository(t *testing.T, excludes []string) string {
	t.Helper()
	root := contentRepository(t, excludes)
	enableReusableContentSuites(t, root)
	return root
}

func enableReusableContentSuites(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.ReplaceAll(string(data), `"scope":"module","modules":["content"],"argv"`, `"scope":"module","modules":["content"],"reusable":true,"argv"`)
	configured = strings.ReplaceAll(configured, `"scope":"repository","argv"`, `"scope":"repository","reusable":true,"argv"`)
	if configured == string(data) {
		t.Fatal("content suites were not made reusable")
	}
	writeEngineFile(t, root, policy.ConfigFilename, configured, 0o600)
}
