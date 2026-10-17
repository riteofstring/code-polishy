package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDoctorFindsInstalledRustToolsWithoutAmbientPath(t *testing.T) {
	root := contentRepository(t, nil)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, "tools/rust-version.txt", "1.97.1\n", 0o600)
	tool := policyEngine.Repository.RustTool("cargo")
	if err := os.MkdirAll(filepath.Dir(tool), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, []byte("fixture executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	policyEngine.Repository.Config.Checks = []policy.Command{{Name: "native-build", Argv: []string{"cargo", "build"}}}
	t.Setenv("PATH", t.TempDir())
	report, err := policyEngine.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.tool" && finding.Subject == "cargo"
	}) {
		t.Fatalf("installed Cargo was reported missing: %+v", report.Findings)
	}
	if err := os.Remove(tool); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), filepath.Base(tool)), []byte("ambient executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	report, err = policyEngine.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.tool" && finding.Subject == "cargo"
	}) {
		t.Fatal("ambient Cargo concealed an incomplete governed installation")
	}
}

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
