package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
