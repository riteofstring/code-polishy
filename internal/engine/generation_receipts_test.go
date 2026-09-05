package engine

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
	"github.com/riteofstring/code-polishy/internal/testreceipt"
)

func TestGenerationVerificationRejectsDriftAndInvalidatesProducerEvidence(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"input", "output", "producer", "dependency", "toolchain"} {
		t.Run(change, func(t *testing.T) {
			policyEngine, suite, generated := generationReceiptEngine(t)
			identity := generationReceiptIdentity(t, policyEngine, suite)
			var output bytes.Buffer
			commandRunner := runner.OSRunner{Stdout: &output, Stderr: &output}
			execution := testpolicy.ExecuteSuite(t.Context(), policyEngine.Repository.Root, commandRunner, suite, 1)
			if execution.Failed() {
				t.Fatalf("initial generation verification failed: %+v\n%s", execution, output.String())
			}
			if _, err := testreceipt.RecordPassed(policyEngine.Repository.Root, identity, execution.Result.ExecutionDuration, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := testreceipt.Load(policyEngine.Repository.Root, identity); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "input":
				writeEngineFile(t, policyEngine.Repository.Root, "source/template.txt", "changed input\n", 0o600)
			case "output":
				writeEngineFile(t, policyEngine.Repository.Root, generated, "changed output\n", 0o600)
			case "producer":
				writeEngineFile(t, policyEngine.Repository.Root, "scripts/generate.sh", "#!/bin/sh\ncp source/template.txt app/value.generated.ts\nprintf changed\n", 0o700)
			case "dependency":
				writeEngineFile(t, policyEngine.Repository.Root, "package.json", "{\"name\":\"fixture\",\"version\":\"2.0.0\"}\n", 0o600)
			case "toolchain":
				writeEngineFile(t, policyEngine.Repository.PolicyRoot, "scripts/go_version.txt", "1.26.6\n", 0o600)
			}
			changed := generationReceiptIdentity(t, policyEngine, suite)
			if _, err := testreceipt.Load(policyEngine.Repository.Root, changed); !errors.Is(err, testreceipt.ErrMissing) {
				t.Fatalf("changed %s retained verification evidence: %v", change, err)
			}
			if change == "input" || change == "output" {
				execution := testpolicy.ExecuteSuite(t.Context(), policyEngine.Repository.Root, runner.OSRunner{}, suite, 1)
				if !execution.Failed() {
					t.Fatal("generation drift passed declared verification")
				}
			}
		})
	}
}

func TestGenerationReceiptIncludesProducerEnvironmentAndTransitiveInputs(t *testing.T) {
	policyEngine, suite, generated := generationReceiptEngine(t)
	command := policyEngine.Repository.Config.Generation.Producers[0].Generate.Clone()
	producer := policy.GenerationProducer{Name: "intermediate", Inputs: []string{"source/template.txt"}, Outputs: []string{"wire/schema.generated.json"}, Generate: command, Verify: command}
	writeEngineFile(t, policyEngine.Repository.Root, producer.Outputs[0], "{}\n", 0o600)
	policyEngine.Repository.Config.Generation.Producers[0].Inputs = slices.Clone(producer.Outputs)
	policyEngine.Repository.Config.Generation.Producers[0].Generate.Environment = []string{"CODE_POLISHY_GENERATOR_TEST_INPUT"}
	policyEngine.Repository.Config.Generation.Producers = append(policyEngine.Repository.Config.Generation.Producers, producer)
	t.Setenv("CODE_POLISHY_GENERATOR_TEST_INPUT", "one")
	identity := generationReceiptIdentity(t, policyEngine, suite)
	for _, path := range []string{generated, "wire/schema.generated.json", "source/template.txt", "scripts/generate.sh", "scripts/verify.sh"} {
		if !slices.ContainsFunc(identity.Inputs, func(input testreceipt.Input) bool { return input.Path == path }) {
			t.Fatalf("verification identity omitted %s", path)
		}
	}
	initial, err := identity.Digest()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODE_POLISHY_GENERATOR_TEST_INPUT", "two")
	changed := generationReceiptIdentity(t, policyEngine, suite)
	actual, err := changed.Digest()
	if err != nil || actual == initial {
		t.Fatalf("producer environment did not invalidate identity: %v", err)
	}
}

func TestGenerationReceiptRejectsUnversionedToolsAndAmbientProducerInputs(t *testing.T) {
	t.Parallel()
	for _, argv := range [][]string{{"unknown-generation-tool"}, {"./scripts/generate.sh", "/untracked/input.json"}} {
		policyEngine, suite, _ := generationReceiptEngine(t)
		policyEngine.Repository.Config.Generation.Producers[0].Generate.Argv = argv
		_, reason, err := policyEngine.suiteReceiptIdentity(suite)
		if err != nil || reason == "" {
			t.Fatalf("unbounded producer received reusable evidence: %q, %v", reason, err)
		}
	}
}

func generationReceiptIdentity(t *testing.T, policyEngine *Engine, suite policy.TestSuite) testreceipt.Identity {
	t.Helper()
	identity, reason, err := policyEngine.suiteReceiptIdentity(suite)
	if err != nil || reason != "" {
		t.Fatalf("generation identity = %q, %v", reason, err)
	}
	return identity
}

func generationReceiptEngine(t *testing.T) (*Engine, policy.TestSuite, string) {
	t.Helper()
	policyEngine, generated, original := generatedFormatEngine(t)
	policyEngine.Repository.Config.Version = 4
	policyEngine.Repository.PolicyRoot = t.TempDir()
	writeEngineFile(t, policyEngine.Repository.PolicyRoot, "scripts/go_version.txt", "1.26.5\n", 0o600)
	writeEngineFile(t, policyEngine.Repository.Root, "package.json", "{\"name\":\"fixture\",\"version\":\"1.0.0\"}\n", 0o600)
	writeEngineFile(t, policyEngine.Repository.Root, "source/template.txt", original, 0o600)
	writeEngineFile(t, policyEngine.Repository.Root, "scripts/verify.sh", "#!/bin/sh\nset -eu\nIFS= read -r expected < source/template.txt\nIFS= read -r actual < app/value.generated.ts\ntest \"$expected\" = \"$actual\"\n", 0o700)
	writeEngineFile(t, policyEngine.Repository.Root, "scripts/generate.sh", "#!/bin/sh\nset -eu\nIFS= read -r source < source/template.txt\nprintf '%s\\n' \"$source\" > app/value.generated.ts\n", 0o700)
	for _, path := range []string{"scripts/generate.sh", "scripts/verify.sh"} {
		if err := os.Chmod(filepath.Join(policyEngine.Repository.Root, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(policyEngine.Repository.Root, "source/schema.json")); err != nil {
		t.Fatal(err)
	}
	producer := &policyEngine.Repository.Config.Generation.Producers[0]
	producer.Inputs = []string{"source/template.txt"}
	producer.Generate.Argv = []string{"./scripts/generate.sh"}
	producer.Verify.Argv = []string{"./scripts/verify.sh"}
	suite := policy.TestSuite{Name: "contracts", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{"app"}, Reusable: true, Argv: slices.Clone(producer.Verify.Argv), Cwd: ".", TimeoutSeconds: 900}
	return policyEngine, suite, generated
}
