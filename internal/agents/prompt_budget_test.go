package agents

import (
	"bytes"
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
	variants := map[string][]byte{
		"LF":   contents,
		"CRLF": bytes.ReplaceAll(contents, []byte("\n"), []byte("\r\n")),
	}
	for name, variant := range variants {
		if len(variant) > maximumCanonicalAgentsBytes {
			t.Fatalf("canonical AGENTS.md with %s uses %d bytes, maximum %d", name, len(variant), maximumCanonicalAgentsBytes)
		}
	}
}
