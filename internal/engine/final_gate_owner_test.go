package engine

import (
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCIFinalGateOwnerRequiresLiteralCheckedInWorkflowCommand(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, ".github/workflows/ci.yml", "jobs:\n  verify:\n    run: code-polishy test --all\n", 0o600)
	repo := repository.Repository{
		Root:   root,
		Config: policy.Config{Version: 3, Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}},
	}
	if findings := finalGateOwnerFindings(repo, []string{".github/workflows/ci.yml"}); len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	writeEngineFile(t, root, ".github/workflows/ci.yml", "jobs:\n  verify:\n    run: ./bin/code-polishy merge-gate --base main\n", 0o600)
	if findings := finalGateOwnerFindings(repo, []string{".github/workflows/ci.yml"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCIFinalGateOwnerAcceptsConfiguredGitLabFragment(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "ci/policy.yml", "verify:\n  script: code-polishy merge-gate --base main\n", 0o600)
	repo := repository.Repository{
		Root: root, DynamicControlInputs: []string{"ci/policy.yml"},
		Config: policy.Config{Version: 3, Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerCI}},
	}
	if findings := finalGateOwnerFindings(repo, []string{"ci/policy.yml"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestLocalFinalGateOwnerNeedsNoWorkflow(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir(), Config: policy.Config{Verification: policy.Verification{FinalGateOwner: policy.FinalGateOwnerLocal}}}
	if findings := finalGateOwnerFindings(repo, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}
