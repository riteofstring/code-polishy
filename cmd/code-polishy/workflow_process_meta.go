package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

type taskSessionWorkflowOptions struct {
	repoRoot        string
	config          string
	modules         []string
	allowedPaths    []string
	allowedNewPaths []string
	outputDir       string
	promote         bool
	showHelp        bool
	command         []string
}

func handleTaskSessionWorkflowMeta(invocation invocation) int {
	if invocation.configPath != "" {
		return commandUsageError("task-session", "task-session runs a workflow supervisor rather than reading the configuration; spell --config after the command")
	}
	options, err := parseTaskSessionWorkflowOptions(invocation.repoRoot, invocation.arguments)
	if err != nil {
		return commandUsageError("task-session", err.Error())
	}
	if options.showHelp {
		printCommandHelp(os.Stdout, "task-session")
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, runErr := engine.RunTaskSession(ctx, engine.TaskSessionOptions{
		RepoRoot: options.repoRoot, PolicyRoot: invocation.policyRoot, ConfigPath: options.config,
		Modules: options.modules, AllowedPaths: options.allowedPaths, AllowedNewPaths: options.allowedNewPaths,
		OutputDir: options.outputDir, Promote: options.promote, Command: options.command,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	})
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		if result.ExitStatus != 0 {
			return result.ExitStatus
		}
		return 1
	}
	fmt.Printf("Task session %s; disposable worktree removed. Artifacts: %s\n", result.Status, result.OutputDir)
	return 0
}

func parseTaskSessionWorkflowOptions(defaultRoot string, arguments []string) (taskSessionWorkflowOptions, error) {
	working, err := os.Getwd()
	if err != nil {
		return taskSessionWorkflowOptions{}, fmt.Errorf("resolve working directory: %w", err)
	}
	options := taskSessionWorkflowOptions{repoRoot: defaultRoot}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" {
			options.command = append([]string(nil), arguments[index+1:]...)
			break
		}
		name, value, joined := strings.Cut(argument, "=")
		switch name {
		case "--repo-root", "--config", "--module", "--allow-path", "--allow-new-path", "--output-dir":
			index, err = applyTaskSessionWorkflowValue(&options, working, arguments, index, name, value, joined)
			if err != nil {
				return options, err
			}
		case "--promote":
			if joined {
				return options, errors.New("--promote does not accept a value")
			}
			options.promote = true
		case "--help", "-h":
			options.showHelp = true
		default:
			return options, fmt.Errorf("unknown task-session option: %s", argument)
		}
	}
	options.repoRoot, err = resolveExistingDirectory(working, options.repoRoot, "--repo-root")
	if err != nil {
		return options, err
	}
	return options, validateTaskSessionWorkflowOptions(options)
}

func applyTaskSessionWorkflowValue(options *taskSessionWorkflowOptions, working string, arguments []string, index int, name, value string, joined bool) (int, error) {
	if !joined {
		index++
		if index >= len(arguments) {
			return index, fmt.Errorf("%s requires a non-empty value", name)
		}
		value = arguments[index]
	}
	if value == "" {
		return index, fmt.Errorf("%s requires a non-empty value", name)
	}
	switch name {
	case "--repo-root":
		resolved, err := resolveExistingDirectory(working, value, name)
		options.repoRoot = resolved
		return index, err
	case "--config":
		options.config = value
	case "--module":
		options.modules = append(options.modules, value)
	case "--allow-path":
		options.allowedPaths = append(options.allowedPaths, value)
	case "--allow-new-path":
		options.allowedNewPaths = append(options.allowedNewPaths, value)
	case "--output-dir":
		resolved, err := resolveTaskSessionOutput(working, value)
		options.outputDir = resolved
		return index, err
	}
	return index, nil
}

func validateTaskSessionWorkflowOptions(options taskSessionWorkflowOptions) error {
	if options.showHelp {
		return nil
	}
	if len(options.command) == 0 {
		return errors.New("a command is required after --")
	}
	if len(options.modules) == 0 {
		return errors.New("at least one --module is required")
	}
	return nil
}

func resolveTaskSessionOutput(working, candidate string) (string, error) {
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(working, candidate)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(candidate))
	if err != nil {
		return "", fmt.Errorf("--output-dir parent directory does not exist: %s", filepath.Dir(candidate))
	}
	return filepath.Join(parent, filepath.Base(candidate)), nil
}

func resolveExistingDirectory(working, candidate, option string) (string, error) {
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(working, candidate)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("%s directory does not exist: %s", option, candidate)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s directory does not exist: %s", option, candidate)
	}
	return resolved, nil
}
