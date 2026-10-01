package agents

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestProjectPrinciplesAcceptBoundedFlatNumberedList(t *testing.T) {
	t.Parallel()
	suffix := []byte("\n## Project principles\n\n1. **Prefer observable behavior.** Verify results through public\n   boundaries without hidden state.\n2. **Keep ownership explicit.** Give every responsibility one clear owner.\n")
	if err := validateProjectPrinciples(suffix); err != nil {
		t.Fatal(err)
	}
	if err := validateProjectPrinciples(bytes.ReplaceAll(suffix, []byte("\n"), []byte("\r\n"))); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalAgentGuidanceRejectsCRLFExpansionBeyondBudget(t *testing.T) {
	t.Parallel()
	template := bytes.Repeat([]byte("x\n"), 1710)
	if len(template) > 5<<10 {
		t.Fatalf("test fixture already exceeds the LF budget: %d", len(template))
	}
	err := validateCanonicalAgents(template)
	if err == nil || !strings.Contains(err.Error(), "with CRLF") {
		t.Fatalf("error = %v, want CRLF budget failure", err)
	}
}

func TestProjectPrinciplesRejectInvalidStructureAndBounds(t *testing.T) {
	t.Parallel()
	itemPrefix := "1. **Keep principles bounded.** "
	exactItem := itemPrefix + strings.Repeat("a", maximumProjectPrincipleBytes-len(itemPrefix)-1) + "\n"
	if err := validateProjectPrinciples([]byte("\n## Project principles\n\n" + exactItem)); err != nil {
		t.Fatalf("maximum-size item was rejected: %v", err)
	}
	tests := map[string]struct {
		suffix string
		want   string
	}{
		"section size": {
			suffix: "\n## Project principles\n\n1. **Keep principles bounded.** " + strings.Repeat("a", maximumProjectPrinciplesBytes) + "\n",
			want:   "project principles use",
		},
		"item size": {
			suffix: "\n## Project principles\n\n" + itemPrefix + strings.Repeat("a", maximumProjectPrincipleBytes-len(itemPrefix)) + "\n",
			want:   "maximum 400",
		},
		"item count": {
			suffix: projectPrinciplesWithItems(13),
			want:   "more than 12",
		},
		"numbering": {
			suffix: "\n## Project principles\n\n1. **Keep order stable.** First.\n3. **Append deliberately.** Third.\n",
			want:   "expected 2, found 3",
		},
		"title length": {
			suffix: "\n## Project principles\n\n1. **Keep every repository principle title deliberately short and direct.** Apply it.\n",
			want:   "maximum 8",
		},
		"missing bold title": {
			suffix: "\n## Project principles\n\n1. Keep the title bold.\n",
			want:   "bold title",
		},
		"escaped bold title": {
			suffix: "\n## Project principles\n\n1. **Keep state visible\\** Explain the decision.\n",
			want:   "invalid bold title",
		},
		"link in title": {
			suffix: "\n## Project principles\n\n1. **<https://example.com>** Follow this source.\n",
			want:   "invalid bold title",
		},
		"bare email link in title": {
			suffix: "\n## Project principles\n\n1. **Email owner@example.com.** Keep ownership explicit.\n",
			want:   "invalid bold title",
		},
		"nested bullet list": {
			suffix: "\n## Project principles\n\n1. **Keep one level.** Apply it.\n   - Nested detail.\n",
			want:   "nested list",
		},
		"nested parenthesized list": {
			suffix: "\n## Project principles\n\n1. **Keep one level.** Apply it.\n   1) Nested detail.\n",
			want:   "nested list",
		},
		"nested tabbed bullet list": {
			suffix: "\n## Project principles\n\n1. **Keep one level.** Apply it.\n   -\tNested detail.\n",
			want:   "nested list",
		},
		"second paragraph": {
			suffix: "\n## Project principles\n\n1. **Keep one paragraph.** Apply it.\n\n2. **Keep another principle.** Apply it.\n",
			want:   "one paragraph",
		},
		"link": {
			suffix: "\n## Project principles\n\n1. **Keep principles complete.** Read [the design](design.md).\n",
			want:   "link",
		},
		"bare email link": {
			suffix: "\n## Project principles\n\n1. **Keep ownership explicit.** Contact owner@example.com.\n",
			want:   "link",
		},
		"image": {
			suffix: "\n## Project principles\n\n1. **Keep principles textual.** See ![diagram](diagram.png).\n",
			want:   "link",
		},
		"table": {
			suffix: "\n## Project principles\n\n1. **Avoid tables.** A | B.\n",
			want:   "unsupported Markdown",
		},
		"single column table": {
			suffix: "\n## Project principles\n\n1. **Avoid tables.** Header\n   :---:\n   Cell\n",
			want:   "nested Markdown",
		},
		"extra heading": {
			suffix: "\n## Project principles\n\n1. **Keep one section.** Apply it.\n   ### Detail\n",
			want:   "nested Markdown",
		},
		"setext heading": {
			suffix: "\n## Project principles\n\n1. **Keep one section.** Apply it.\n   ===\n",
			want:   "nested Markdown",
		},
		"hyphen heading": {
			suffix: "\n## Project principles\n\n1. **Keep one section.** Apply it.\n   ---\n",
			want:   "nested Markdown",
		},
		"thematic break": {
			suffix: "\n## Project principles\n\n1. **Keep one paragraph.** Apply it.\n   _ _ _\n",
			want:   "nested Markdown",
		},
		"code block": {
			suffix: "\n## Project principles\n\n1. **Keep principles declarative.** Apply it.\n   ```text\n",
			want:   "nested Markdown",
		},
		"compact block quote": {
			suffix: "\n## Project principles\n\n1. **Keep principles declarative.** Apply it.\n   >```text\n",
			want:   "nested Markdown",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateProjectPrinciples([]byte(test.suffix))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRenderSynchronizedAgentsPreservesProjectPrinciplesBytes(t *testing.T) {
	t.Parallel()
	existing := []byte("# Stale guidance\n" + projectPrinciplesText)
	updated, changed, err := renderSynchronizedAgents(existing, []byte(canonicalAgentsText))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("stale canonical guidance was not synchronized")
	}
	want := []byte(canonicalAgentsText + projectPrinciplesText)
	if !bytes.Equal(updated, want) {
		t.Fatalf("updated guidance = %q, want %q", updated, want)
	}
}

func projectPrinciplesWithItems(count int) string {
	var result strings.Builder
	result.WriteString("\n## Project principles\n\n")
	for number := 1; number <= count; number++ {
		result.WriteString(strconv.Itoa(number))
		result.WriteString(". **Keep principles focused.** Apply this decision.\n")
	}
	return result.String()
}
