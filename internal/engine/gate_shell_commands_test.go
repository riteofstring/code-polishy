package engine

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestGatesExecuteShellFilesWithUnusualNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native shell checks require Bash and ShellCheck on Unix")
	}
	for _, gate := range []string{"merge", "checkpoint"} {
		for _, broken := range []bool{false, true} {
			name := gate + "/valid"
			if broken {
				name = gate + "/invalid-syntax"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				checkGateShellFiles(t, gate, broken)
			})
		}
	}
}

func checkGateShellFiles(t *testing.T, gate string, broken bool) {
	t.Helper()
	files := []string{
		"content/Open Learn Portugal.command",
		"content/Olá lição.command",
		"content/lesson 'one' (v2) [draft] $&;.command",
		"content/a.b.command", "content/a-b.command", "content/a/b.command",
		"content/" + strings.Repeat("long", 45) + "-a.command",
		"content/" + strings.Repeat("long", 45) + "-b.command",
	}
	root := shellGateRepository(t, files, broken)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commands := &shellGateRunner{}
	policyEngine.Runner = commands
	var report Report
	if gate == "merge" {
		report, err = policyEngine.MergeGate(t.Context(), "main")
	} else {
		report, err = policyEngine.CheckpointGate(t.Context(), "main")
	}
	if err != nil || report.GateRunPolicy == nil {
		t.Fatalf("gate failed before reporting checks: %v, %+v", err, report)
	}
	wantStatus := "passed"
	if broken {
		wantStatus = "failed"
		if !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
			return finding.Check == "quality.shellSyntax" && finding.Path == files[1] && finding.Subject == files[1]
		}) {
			t.Errorf("missing syntax failure for %q: %+v", files[1], report.Findings)
		}
	} else if HasFindings(report) {
		t.Errorf("valid shell files produced findings: %+v", report.Findings)
	}
	if report.GateRunPolicy.Status != wantStatus {
		t.Errorf("gate status = %s, want %s", report.GateRunPolicy.Status, wantStatus)
	}
	assertShellGateCommands(t, root, report, commands.commands, files, broken)
}

func shellGateRepository(t *testing.T, files []string, broken bool) string {
	t.Helper()
	root := contentRepository(t, nil)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	for index, path := range files {
		source := "#!/usr/bin/env bash\nprintf '%s\\n' ready\n"
		if broken && index == 1 {
			source = "#!/usr/bin/env bash\nif then\n"
		}
		writeEngineFile(t, root, path, source, 0o700)
	}
	commitEngineCandidate(t, root, "add shell launchers")
	return root
}

func assertShellGateCommands(t *testing.T, root string, report Report, executed []policy.Command, files []string, broken bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(report.GateRunPolicy.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	var stored gaterun.Report
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if _, err := gaterun.LoadReport(root, stored.Identity); err != nil {
		t.Fatalf("gate evidence rejected its command identities: %v", err)
	}
	names := map[string]bool{}
	paths := []string{}
	for _, command := range executed {
		if !strings.HasPrefix(command.Name, "shell-syntax-") {
			continue
		}
		if names[command.Name] || len(command.Paths) != 1 {
			t.Fatalf("duplicate identity or missing original path: %+v", command)
		}
		names[command.Name] = true
		path := command.Paths[0]
		paths = append(paths, path)
		if !slices.Equal(command.Argv, []string{"bash", "-n", path}) {
			t.Errorf("shell syntax command changed the path: %+v", command)
		}
		outcome := readGateCommandOutcome(t, root, report.GateRunPolicy.ReportPath, command.Name)
		want := gaterun.Passed
		if broken && path == files[1] {
			want = gaterun.Failed
		}
		if outcome.Status != want || len(outcome.Attempts) != 1 {
			t.Errorf("syntax check for %q: %+v", path, outcome)
		}
	}
	wantPaths := slices.Clone(files)
	slices.Sort(wantPaths)
	slices.Sort(paths)
	if !slices.Equal(paths, wantPaths) {
		t.Errorf("checked %q, want %q", paths, wantPaths)
	}
	if !slices.ContainsFunc(executed, func(command policy.Command) bool { return command.Name == "shellcheck" }) {
		t.Error("gate omitted ShellCheck")
	}
}

type shellGateRunner struct {
	commands []policy.Command
}

func (commandRunner *shellGateRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, err := commandRunner.RunWithWriters(ctx, root, command, io.Discard, io.Discard)
	return err
}

func (commandRunner *shellGateRunner) RunWithWriters(ctx context.Context, root string, command policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	commandRunner.commands = append(commandRunner.commands, command)
	if strings.HasPrefix(command.Name, "shell-syntax-") || command.Name == "shellcheck" {
		return (runner.OSRunner{}).RunWithWriters(ctx, root, command, stdout, stderr)
	}
	return runner.Result{}, nil
}
