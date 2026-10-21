package rustfacts

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustSourceFactsKeepLargeResultInventories(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	var source strings.Builder
	for index := range 1200 {
		fmt.Fprintf(&source, "pub fn function_%d() {}\n", index)
	}
	paths := make([]string, 20)
	for index := range paths {
		paths[index] = fmt.Sprintf("source_%02d.rs", index)
		write(t, repo.Root, paths[index], source.String())
	}
	result := ReadSources(t.Context(), repo, PlanSources(repo, paths), runner.OSRunner{})
	if len(result.Findings) != 0 || len(result.Files) != len(paths) {
		t.Fatalf("expanded syntax facts lost source coverage: files=%d findings=%+v", len(result.Files), result.Findings)
	}
	for index, file := range result.Files {
		if file.Path != paths[index] || len(file.Functions) != 1200 || file.Functions[1199].Name != "function_1199" {
			t.Fatalf("large result lost its original function inventory: path=%s functions=%d", file.Path, len(file.Functions))
		}
	}
}

func TestRustSourceFactsBatchLargeScopes(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name   string
		count  int
		source string
	}{
		{"source count", 4097, "\n"},
		{"source bytes", 3, "/*" + strings.Repeat("x", 3<<20) + "*/\npub fn f() {}\n"},
		{"encoded bytes", 2, "pub const TEXT: &str = \"" + strings.Repeat("&", 1<<20) + "\";\n"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := rustRepository(t)
			paths := make([]string, scenario.count)
			for index := range paths {
				paths[index] = fmt.Sprintf("source_%04d.rs", index)
				write(t, repo.Root, paths[index], scenario.source)
			}
			plan := PlanSources(repo, paths)
			if len(plan.Findings) != 0 || len(plan.Commands) < 2 {
				t.Fatalf("large source scope was not batched: commands=%d findings=%+v", len(plan.Commands), plan.Findings)
			}
			seen := []string{}
			for _, command := range plan.Commands {
				var request struct {
					Protocol string        `json:"protocol"`
					Sources  []SourceInput `json:"sources"`
				}
				program, payload, found := bytes.Cut(command.Stdin, []byte("\n"))
				if !found || len(program) > 1<<20 {
					t.Fatal("parser command omitted its bounded program record")
				}
				if err := json.Unmarshal(payload, &request); err != nil {
					t.Fatal(err)
				}
				if len(payload) > 8<<20 || len(request.Sources) > 4096 || len(request.Sources) == 0 || request.Protocol != SourceProtocol {
					t.Fatalf("unbounded or empty parser request: bytes=%d sources=%d protocol=%s", len(payload), len(request.Sources), request.Protocol)
				}
				for _, source := range request.Sources {
					seen = append(seen, source.Path)
				}
			}
			if !slices.Equal(seen, paths) {
				t.Fatal("batching omitted, repeated or reordered selected source")
			}
			result := ReadSources(t.Context(), repo, plan, runner.OSRunner{})
			if len(result.Findings) != 0 || len(result.Files) != len(paths) {
				t.Fatalf("large source scope lost syntax facts: files=%d findings=%+v", len(result.Files), result.Findings)
			}
		})
	}
}

func TestRustSourceFactsKeepSiblingsOfAnOversizedEncodedSource(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "before.rs", "pub fn before() {}\n")
	write(t, repo.Root, "large.rs", "pub const TEXT: &str = \""+strings.Repeat("&", 2<<20)+"\";\n")
	write(t, repo.Root, "after.rs", "pub fn after() {}\n")
	plan := PlanSources(repo, []string{"before.rs", "large.rs", "after.rs"})
	result := ReadSources(t.Context(), repo, plan, runner.OSRunner{})
	if len(result.Findings) != 1 || result.Findings[0].Path != "large.rs" || !strings.Contains(result.Findings[0].Message, "8388608") {
		t.Fatalf("oversized source lacks its own coverage failure: %+v", result.Findings)
	}
	if len(result.Files) != 2 || result.Files[0].Path != "before.rs" || result.Files[1].Path != "after.rs" {
		t.Fatalf("oversized source discarded valid siblings: files=%d", len(result.Files))
	}
}

func TestRustSourceFactsRetainValidBatchesAfterAParserFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"command", "wrong inventory"} {
		t.Run(failure, func(t *testing.T) {
			repo := rustRepository(t)
			paths := []string{"before.rs", "broken.rs", "after.rs"}
			for _, path := range paths {
				write(t, repo.Root, path, "pub const TEXT: &str = \""+strings.Repeat("&", 1<<20)+"\";\n")
			}
			plan := PlanSources(repo, paths)
			var first runner.Output
			boundary := &sourceBatchRunner{after: func(index int, output runner.Output) (runner.Output, error) {
				if index == 1 {
					first = output
				}
				if index != 2 {
					return output, nil
				}
				if failure == "command" {
					return output, errors.New("parser worker failed")
				}
				return first, nil
			}}
			result := ReadSources(t.Context(), repo, plan, boundary)
			if len(result.Files) != 2 || result.Files[0].Path != "before.rs" || result.Files[1].Path != "after.rs" || len(result.Findings) != 1 || result.Findings[0].Path != "broken.rs" {
				t.Fatalf("a failed batch discarded or fabricated sibling coverage: files=%d findings=%+v", len(result.Files), result.Findings)
			}
			if len(boundary.commands) != len(plan.Commands) {
				t.Fatalf("execution omitted planned batches: ran=%d planned=%d", len(boundary.commands), len(plan.Commands))
			}
			for index, command := range boundary.commands {
				if command.Name != plan.Commands[index].Name {
					t.Fatalf("batch execution departed from its command plan: %q != %q", command.Name, plan.Commands[index].Name)
				}
			}
		})
	}
}

func TestRustSourceFactsStopStartingBatchesAfterCancellation(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	paths := []string{"first.rs", "second.rs", "third.rs"}
	for _, path := range paths {
		write(t, repo.Root, path, "pub const TEXT: &str = \""+strings.Repeat("&", 1<<20)+"\";\n")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	boundary := &sourceBatchRunner{after: func(_ int, output runner.Output) (runner.Output, error) {
		cancel()
		return output, nil
	}}
	result := ReadSources(ctx, repo, PlanSources(repo, paths), boundary)
	if len(boundary.commands) != 1 || len(result.Files) != 1 || result.Files[0].Path != "first.rs" || len(result.Findings) != 2 {
		t.Fatalf("cancellation executed or lost unparsed batches: commands=%d files=%d findings=%+v", len(boundary.commands), len(result.Files), result.Findings)
	}
	for index, finding := range result.Findings {
		if finding.Path != paths[index+1] || !strings.Contains(finding.Message, "canceled") {
			t.Fatalf("canceled source lacks its own coverage failure: %+v", finding)
		}
	}
}

type sourceBatchRunner struct {
	runner.OSRunner
	commands []policy.Command
	after    func(int, runner.Output) (runner.Output, error)
}

func (boundary *sourceBatchRunner) runStreamed(ctx context.Context, root string, command policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	boundary.commands = append(boundary.commands, command)
	result, output, err := runner.RunCaptured(ctx, boundary.OSRunner, root, command, runner.CaptureStructured)
	if err == nil {
		output, err = boundary.after(len(boundary.commands), output)
	}
	if err != nil {
		return result, err
	}
	if _, err := stderr.Write(output.Stderr); err != nil {
		return result, err
	}
	_, err = stdout.Write(output.Stdout)
	return result, err
}

func (double *sourceBatchRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Stdout != nil || invocation.Stderr != nil {
		var stdout, stderr bytes.Buffer
		result, err := double.runStreamed(ctx, invocation.Root, invocation.Command, io.MultiWriter(&stdout, cmp.Or[io.Writer](invocation.Stdout, io.Discard)), io.MultiWriter(&stderr, cmp.Or[io.Writer](invocation.Stderr, io.Discard)))
		return result, runner.Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
	}
	return double.OSRunner.Execute(ctx, invocation)
}
