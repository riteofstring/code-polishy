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
	want := hostReportCommand("'node' 'scripts/with space.js' 'a'\\''b'", "& 'node' 'scripts/with space.js' 'a''b'")
	if !strings.Contains(text, "source/schema.json") || !strings.Contains(text, "generate (cwd frontend): "+want) || !strings.Contains(text, "verify (cwd frontend)") {
		t.Fatalf("generated commands = %q", text)
	}
	if command := displayRemediationCommand([]string{"node", strings.Repeat("x", 4096)}); !strings.Contains(command, "complete report") || len(command) > 128 {
		t.Fatalf("unbounded command display = %q", command)
	}
}

func TestGeneratedReportGroupsCompleteProducerInstructions(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*policy.GenerationRemediation){
		"producer":      func(value *policy.GenerationRemediation) { value.Producer = "models" },
		"hidden input":  func(value *policy.GenerationRemediation) { value.Inputs[8] = "source/other.json" },
		"generate args": func(value *policy.GenerationRemediation) { value.Generate.Argv = []string{"node", "other.js"} },
		"generate cwd":  func(value *policy.GenerationRemediation) { value.Generate.Cwd = "backend" },
		"verify args":   func(value *policy.GenerationRemediation) { value.Verify.Argv = []string{"node", "other.js"} },
		"verify cwd":    func(value *policy.GenerationRemediation) { value.Verify.Cwd = "backend" },
		"prerequisite":  func(value *policy.GenerationRemediation) { value.Prerequisites[0].Message = "Install another tool." },
	} {
		t.Run(name, func(t *testing.T) {
			findings := []policy.Finding{}
			for index := range 3 {
				generation := &policy.GenerationRemediation{
					Producer: "contracts", Inputs: []string{"a", "b", "c", "d", "e", "f", "g", "h", "source/schema.json"},
					Generate:      policy.GenerationCommand{Argv: []string{"node", "generate.js"}, Cwd: "frontend"},
					Verify:        policy.GenerationCommand{Argv: []string{"node", "verify.js"}, Cwd: "frontend"},
					Prerequisites: []policy.GenerationPrerequisite{{Operation: "generate", Message: "Install the declared tool."}},
				}
				if index == 2 {
					change(generation)
				}
				findings = append(findings, policy.Finding{
					Check: "quality.typecheck", Path: "client.generated.ts", Line: index + 1,
					Remediation: policy.FindingRemediation{Generation: generation},
				})
			}
			var output bytes.Buffer
			printFindings(&output, &output, findings)
			text := output.String()
			for _, expected := range []string{
				"client.generated.ts:1 [] relation=global generation=G1",
				"client.generated.ts:2 [] relation=global generation=G1",
				"client.generated.ts:3 [] relation=global generation=G2",
				"GENERATION G1 (2 findings)", "GENERATION G2 (1 finding)",
				"generate (cwd frontend): " + hostReportCommand("'node' 'generate.js'", "& 'node' 'generate.js'"),
				"verify (cwd frontend): " + hostReportCommand("'node' 'verify.js'", "& 'node' 'verify.js'"), "generate prerequisite: Install the declared tool.",
			} {
				if !strings.Contains(text, expected) {
					t.Errorf("missing %q: %s", expected, text)
				}
			}
			if strings.Count(text, "producer:") != 2 || strings.Count(text, "inputs:") != 2 || strings.Count(text, "1 more producer input(s)") != 2 {
				t.Fatalf("producer groups lost identity or bounds: %s", text)
			}
		})
	}
}
