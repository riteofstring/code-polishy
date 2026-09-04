package quality

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCommandPathsConstrainSharedModuleAndProjectExpansion(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "application", Paths: []string{"**"}}}
	repo.Config.Checks = []policy.Command{{
		Name: "project-format", Provides: []string{"format"}, Modules: []string{"application"},
		Paths: []string{"src/app.py", "src/second.py", "pyproject.toml"}, RunOn: []string{"check"},
		Argv: []string{"formatter", "--check"}, PassFiles: true, PassFilePaths: []string{"src/app.py", "src/second.py"},
	}}
	for _, testCase := range []struct {
		path string
		argv []string
	}{
		{path: "AGENTS.md"},
		{path: "apps/other/pyproject.toml"},
		{path: "src/app.py", argv: []string{"formatter", "--check", "src/app.py"}},
		{path: "pyproject.toml", argv: []string{"formatter", "--check", "src/app.py", "src/second.py"}},
	} {
		t.Run(testCase.path, func(t *testing.T) {
			writeQualityFile(t, repo.Root, testCase.path, "fixture\n")
			commandRunner := &recordingQualityRunner{}
			selection := repository.Selection{Files: []string{testCase.path}}
			if findings := RunCommands(t.Context(), repo, selection, commandRunner, "check"); len(findings) != 0 {
				t.Fatalf("command execution failed: %+v", findings)
			}
			if len(testCase.argv) == 0 {
				if len(commandRunner.commands) != 0 {
					t.Fatalf("unselected project command ran: %+v", commandRunner.commands)
				}
				return
			}
			if len(commandRunner.commands) != 1 || !slices.Equal(commandRunner.commands[0].Argv, testCase.argv) {
				t.Fatalf("selected command = %+v, want arguments %v", commandRunner.commands, testCase.argv)
			}
		})
	}
}

func TestRepositoryCommandRunsOnlyWhenExplicitlySelected(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	writeQualityFile(t, repo.Root, "README.md", "# Repository\n")
	repo.Config.Checks = []policy.Command{{Name: "whole-repository", Argv: []string{"repository-check"}, RunOn: []string{"check"}}}
	commandRunner := &recordingQualityRunner{}
	selection := repository.Selection{Files: []string{"README.md"}}
	if findings := RunCommands(t.Context(), repo, selection, commandRunner, "check"); len(findings) != 0 || len(commandRunner.commands) != 0 {
		t.Fatalf("repository command ran for a focused selection: findings=%+v commands=%+v", findings, commandRunner.commands)
	}
	selection.All = true
	if findings := RunCommands(t.Context(), repo, selection, commandRunner, "check"); len(findings) != 0 || len(commandRunner.commands) != 1 {
		t.Fatalf("repository selection did not execute the check: findings=%+v commands=%+v", findings, commandRunner.commands)
	}
	commandRunner = &recordingQualityRunner{}
	findings, err := RunNamedCommands(t.Context(), repo, commandRunner, []string{"whole-repository"})
	if err != nil || len(findings) != 0 || len(commandRunner.commands) != 1 || commandRunner.commands[0].Name != "whole-repository" {
		t.Fatalf("named check was not executed: error=%v findings=%+v commands=%+v", err, findings, commandRunner.commands)
	}
}
