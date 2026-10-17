package rustfacts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustSourceStreamRetainsSiblingsOfOversizedFacts(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	var large strings.Builder
	for index := range 30000 {
		fmt.Fprintf(&large, "pub fn function_%d() {}\n", index)
	}
	write(t, repo.Root, "before.rs", "pub fn before() {}\n")
	write(t, repo.Root, "large.rs", large.String())
	write(t, repo.Root, "after.rs", "pub fn after() {}\n")
	result := ReadSources(t.Context(), repo, PlanSources(repo, []string{"before.rs", "large.rs", "after.rs"}), runner.OSRunner{})
	if len(result.Files) != 2 || result.Files[0].Path != "before.rs" || result.Files[1].Path != "after.rs" {
		t.Fatalf("oversized facts discarded valid siblings: files=%d findings=%+v", len(result.Files), result.Findings)
	}
	if len(result.Findings) != 1 || result.Findings[0].Path != "large.rs" || !strings.Contains(result.Findings[0].Message, "source record exceeds 8388608") {
		t.Fatalf("oversized source lacks an explicit coverage failure: %+v", result.Findings)
	}
}

func TestRustSourceStreamValidatesCompleteBoundedEvidence(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "first.rs", "pub fn café() {}\n")
	write(t, repo.Root, "second.rs", "pub fn second() {}\n")
	plan := PlanSources(repo, []string{"first.rs", "second.rs"})
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, plan.Commands[0])
	if err != nil {
		t.Fatal(err)
	}
	frames := bytes.SplitAfter(output.Stdout, []byte("\n"))
	for _, scenario := range []struct {
		name   string
		output []byte
		stderr []byte
		err    error
		valid  bool
	}{
		{name: "chunked records", output: output.Stdout, valid: true},
		{name: "empty stream"},
		{name: "missing record", output: frames[0]},
		{name: "repeated record", output: bytes.Repeat(frames[0], 2)},
		{name: "extra record", output: append(bytes.Clone(output.Stdout), frames[0]...)},
		{name: "unterminated record", output: bytes.TrimSuffix(output.Stdout, []byte("\n"))},
		{name: "malformed record", output: []byte("{\n")},
		{name: "oversized record", output: bytes.Repeat([]byte(" "), 8<<20)},
		{name: "oversized diagnostics", output: output.Stdout, stderr: bytes.Repeat([]byte("x"), (64<<10)+1)},
		{name: "late worker failure", output: output.Stdout, err: errors.New("worker failed after output")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			boundary := sourceStreamRunner{stdout: scenario.output, stderr: scenario.stderr, err: scenario.err}
			result := ReadSources(t.Context(), repo, plan, boundary)
			if scenario.valid {
				if len(result.Findings) != 0 || len(result.Files) != 2 || result.Files[0].Functions[0].Name != "café" {
					t.Fatalf("chunk boundaries changed valid syntax facts: %+v", result.Findings)
				}
			} else if len(result.Files) != 0 || len(result.Findings) != 2 {
				t.Fatalf("invalid stream established source coverage: files=%d findings=%+v", len(result.Files), result.Findings)
			}
		})
	}
}

type sourceStreamRunner struct {
	runner.OSRunner
	stdout []byte
	stderr []byte
	err    error
}

func (boundary sourceStreamRunner) RunWithWriters(_ context.Context, _ string, _ policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	for remaining := boundary.stdout; len(remaining) > 0; {
		count := min(31, len(remaining))
		if _, err := stdout.Write(remaining[:count]); err != nil {
			return runner.Result{}, err
		}
		remaining = remaining[count:]
	}
	if _, err := stderr.Write(boundary.stderr); err != nil {
		return runner.Result{}, err
	}
	return runner.Result{}, boundary.err
}
