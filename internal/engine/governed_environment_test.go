package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestGovernedEnvironmentKeepsRustCompanionOptionalAndComplete(t *testing.T) {
	root := contentRepository(t, nil)
	repo := repository.Repository{Root: root, PolicyRoot: root}
	writeEngineFile(t, root, "tools/cargo-nextest-version.txt", "0.9.138\n", 0o600)
	writeEngineFile(t, root, "tools/rust-version.txt", "1.97.1\n", 0o600)
	if err := os.MkdirAll(filepath.Dir(repo.PolicyTool("cargo-nextest")), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := FreezeGovernedEnvironment(repo); err != nil {
		t.Fatalf("core-only task environment requires Rust: %v", err)
	}
	for _, name := range []string{"cargo", "rustc", "rustdoc", "rustfmt", "cargo-fmt", "cargo-clippy", "clippy-driver"} {
		path, err := filepath.Rel(root, repo.RustTool(name))
		if err != nil {
			t.Fatal(err)
		}
		writeEngineFile(t, root, path, "tool", 0o700)
	}
	if _, err := FreezeGovernedEnvironment(repo); err == nil || !strings.Contains(err.Error(), `required locked tool "cargo-nextest"`) {
		t.Fatalf("partial Rust companion was accepted: %v", err)
	}
	if err := os.WriteFile(repo.PolicyTool("cargo-nextest"), []byte("tool"), 0o700); err != nil {
		t.Fatal(err)
	}
	receipt, err := FreezeGovernedEnvironment(repo)
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalGovernedEnvironment(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseGovernedEnvironment(data); err != nil {
		t.Fatalf("complete Rust companion receipt: %v", err)
	}
}

func TestGovernedEnvironmentFailsClosedWhenTheLockedToolIsMissing(t *testing.T) {
	root := contentRepository(t, nil)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := FreezeGovernedEnvironment(policyEngine.Repository)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Digest == "" || len(receipt.PathEntries) == 0 || len(receipt.Tools) != 2 {
		t.Fatalf("receipt = %+v", receipt)
	}
	if _, err := MarshalGovernedEnvironment(receipt); err != nil {
		t.Fatal(err)
	}

	ambient := t.TempDir()
	if err := os.WriteFile(filepath.Join(ambient, "go"), []byte("ambient"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", ambient)
	if err := os.Remove(policyEngine.Repository.GoTool("go")); err != nil {
		t.Fatal(err)
	}
	if _, err := FreezeGovernedEnvironment(policyEngine.Repository); err == nil ||
		!strings.Contains(err.Error(), `required locked tool "go" is unavailable`) {
		t.Fatalf("missing locked Go did not fail closed: %v", err)
	}
}
