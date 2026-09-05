package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGeneratedFormatSummaryNamesProtectedProducerWithoutClaimingARewrite(t *testing.T) {
	t.Parallel()
	output := &bytes.Buffer{}
	printFormattingOutcome(output, &engine.FormatOutcome{Protected: 1, Files: []engine.FormatFile{{Path: "app/client.generated.ts", State: "protected", Producer: "contracts", StyleExempt: true}}})
	text := output.String()
	if !strings.Contains(text, "0 rewritten, 0 unchanged, 1 protected and untouched") || !strings.Contains(text, "contracts") || !strings.Contains(text, "style-exempt") {
		t.Fatalf("format summary = %q", text)
	}
}

func TestGeneratedRemediationPreservesLiteralCommandsAndBoundsTerminalOutput(t *testing.T) {
	t.Parallel()
	output := &bytes.Buffer{}
	generation := &policy.GenerationRemediation{
		Producer: "contracts", Inputs: []string{"source/schema.json"},
		Generate: policy.GenerationCommand{Argv: []string{"node", "scripts/with space.js", "a'b"}, Cwd: "frontend", TimeoutSeconds: 900},
		Verify:   policy.GenerationCommand{Argv: []string{"node", "scripts/verify.js"}, Cwd: "frontend", TimeoutSeconds: 900},
	}
	printGenerationRemediation(output, generation)
	text := output.String()
	if !strings.Contains(text, "source/schema.json") || !strings.Contains(text, "generate (cwd frontend): 'node' 'scripts/with space.js' 'a'\\''b'") || !strings.Contains(text, "verify (cwd frontend)") {
		t.Fatalf("generated commands = %q", text)
	}
	if command := displayGenerationCommand([]string{"node", strings.Repeat("x", 4096)}); !strings.Contains(command, "complete report") || len(command) > 128 {
		t.Fatalf("unbounded command display = %q", command)
	}
}
