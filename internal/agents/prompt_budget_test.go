package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalAgentGuidanceFitsPersistentPromptBudget(t *testing.T) {
	path := filepath.Join("..", "..", "templates", "AGENTS.md")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const maximumBytes = 5 << 10
	if len(contents) > maximumBytes {
		t.Fatalf("canonical AGENTS.md uses %d bytes, maximum %d", len(contents), maximumBytes)
	}
}
