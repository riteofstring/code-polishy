package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
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
		return usageError("task-session runs a workflow supervisor rather than reading the configuration; spell --config after the command")
	}
	options, err := parseTaskSessionWorkflowOptions(invocation.repoRoot, invocation.arguments)
	if err != nil {
		return usageError(err.Error())
	}
	if options.showHelp {
		fmt.Print(taskSessionHelp)
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

const taskSessionHelp = `Usage: code-polishy task-session --module NAME... [options] -- COMMAND [ARG...]

Runs one task command in a disposable Git worktree, freezes its authority and
governed environment, and validates every change before optional promotion.

Options:
  --repo-root PATH       Clean target Git worktree. Default: current directory.
  --config PATH          Target config path. Default: .code-polishy.json.
  --module NAME          Allowed module. Repeat as needed.
  --allow-path PATH      Allowed exact tracked path. Repeat as needed.
  --allow-new-path PATH  Allowed exact new path. Repeat as needed.
  --output-dir PATH      New external artifact directory.
  --promote              Fast-forward the original branch to verified commits.
  --help                 Show this help.
`

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

const (
	taskSessionScopeArtifact   = "scope.argv"
	taskSessionCommandArtifact = "command.argv"
)

type taskSessionArtifactOptions struct {
	operation     string
	outputDir     string
	config        string
	scopeDigest   string
	commandDigest string
	scope         []string
	command       []string
}

type taskSessionScopeOption struct {
	name   string
	values *[]string
}

func (option taskSessionScopeOption) String() string { return "" }
func (option taskSessionScopeOption) Set(value string) error {
	if value == "" {
		return fmt.Errorf("%s requires a non-empty value", option.name)
	}
	*option.values = append(*option.values, option.name, value)
	return nil
}

func handleTaskSessionArtifactsMeta(invocation invocation) int {
	options, err := parseTaskSessionArtifactOptions(invocation.arguments)
	if err != nil {
		return usageError(err.Error())
	}
	switch options.operation {
	case "freeze":
		scopeDigest, commandDigest, err := freezeTaskSessionArtifacts(options)
		if err != nil {
			return operationalError(err)
		}
		fmt.Printf("%s %s\n", scopeDigest, commandDigest)
	case "validate":
		if err := validateTaskSessionArtifacts(options); err != nil {
			return operationalError(err)
		}
	}
	return 0
}

func parseTaskSessionArtifactOptions(arguments []string) (taskSessionArtifactOptions, error) {
	if len(arguments) == 0 || (arguments[0] != "freeze" && arguments[0] != "validate") {
		return taskSessionArtifactOptions{}, errors.New("task-session-artifacts requires freeze or validate")
	}
	options := taskSessionArtifactOptions{operation: arguments[0]}
	flags := taskSessionArtifactFlagSet(&options)
	separator := taskSessionArtifactSeparator(arguments)
	if err := flags.Parse(arguments[1:separator]); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, errors.New("task-session-artifacts accepts no positional arguments before --")
	}
	if separator < len(arguments) {
		options.command = append([]string(nil), arguments[separator+1:]...)
	}
	if err := resolveTaskSessionArtifactOutput(&options); err != nil {
		return options, err
	}
	return options, validateTaskSessionArtifactOperation(options)
}

func taskSessionArtifactFlagSet(options *taskSessionArtifactOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("task-session-artifacts "+options.operation, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&options.outputDir, "output-dir", "", "canonical task-session artifact directory")
	flags.StringVar(&options.config, "config", "", "frozen configuration path")
	flags.StringVar(&options.scopeDigest, "scope-digest", "", "expected frozen scope digest")
	flags.StringVar(&options.commandDigest, "command-digest", "", "expected frozen command digest")
	flags.Var(taskSessionScopeOption{name: "--module", values: &options.scope}, "module", "frozen module")
	flags.Var(taskSessionScopeOption{name: "--allow-path", values: &options.scope}, "allow-path", "frozen tracked path")
	flags.Var(taskSessionScopeOption{name: "--allow-new-path", values: &options.scope}, "allow-new-path", "frozen new path")
	return flags
}

func taskSessionArtifactSeparator(arguments []string) int {
	for index, argument := range arguments[1:] {
		if argument == "--" {
			return index + 1
		}
	}
	return len(arguments)
}

func resolveTaskSessionArtifactOutput(options *taskSessionArtifactOptions) error {
	if options.outputDir == "" {
		return errors.New("task-session-artifacts requires --output-dir")
	}
	resolved, err := filepath.EvalSymlinks(options.outputDir)
	if err != nil {
		return fmt.Errorf("resolve task-session artifact directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("task-session artifact directory does not exist: %s", options.outputDir)
	}
	options.outputDir = resolved
	return nil
}

func validateTaskSessionArtifactOperation(options taskSessionArtifactOptions) error {
	if options.operation == "freeze" {
		if len(options.scope) == 0 || len(options.command) == 0 {
			return errors.New("task-session-artifacts freeze requires scope options and a command after --")
		}
		if options.scopeDigest != "" || options.commandDigest != "" {
			return errors.New("task-session-artifacts freeze does not accept expected digests")
		}
		return nil
	}
	if len(options.scope) != 0 || len(options.command) != 0 || !validSHA256(options.scopeDigest) || !validSHA256(options.commandDigest) {
		return errors.New("task-session-artifacts validate requires exact SHA-256 scope and command digests and no scope or command arguments")
	}
	return nil
}

func freezeTaskSessionArtifacts(options taskSessionArtifactOptions) (string, string, error) {
	scopeData := nulDelimited(options.scope)
	commandData := nulDelimited(options.command)
	if err := writeNewPrivateArtifacts(options.outputDir, []taskSessionArtifact{
		{name: taskSessionScopeArtifact, data: scopeData},
		{name: taskSessionCommandArtifact, data: commandData},
	}); err != nil {
		return "", "", err
	}
	return digestTaskSessionScope(options.config, scopeData), digestBytes(commandData), nil
}

func validateTaskSessionArtifacts(options taskSessionArtifactOptions) error {
	scopeData, err := readPrivateRegularArtifact(filepath.Join(options.outputDir, taskSessionScopeArtifact))
	if err != nil {
		return err
	}
	if _, err := parseScopeArtifact(scopeData); err != nil {
		return err
	}
	commandData, err := readPrivateRegularArtifact(filepath.Join(options.outputDir, taskSessionCommandArtifact))
	if err != nil {
		return err
	}
	if len(commandData) == 0 || commandData[len(commandData)-1] != 0 {
		return errors.New("retained task command artifact is malformed")
	}
	if digestTaskSessionScope(options.config, scopeData) != options.scopeDigest {
		return errors.New("retained task scope changed")
	}
	if digestBytes(commandData) != options.commandDigest {
		return errors.New("retained task worker argv changed")
	}
	return nil
}

func nulDelimited(values []string) []byte {
	var data []byte
	for _, value := range values {
		data = append(data, value...)
		data = append(data, 0)
	}
	return data
}

func parseScopeArtifact(data []byte) ([]string, error) {
	if len(data) == 0 || data[len(data)-1] != 0 {
		return nil, errors.New("retained task scope artifact is malformed")
	}
	parts := strings.Split(string(data[:len(data)-1]), "\x00")
	if len(parts)%2 != 0 {
		return nil, errors.New("retained task scope artifact is malformed")
	}
	for index := 0; index < len(parts); index += 2 {
		if (parts[index] != "--module" && parts[index] != "--allow-path" && parts[index] != "--allow-new-path") || parts[index+1] == "" {
			return nil, errors.New("retained task scope artifact contains an unsupported option")
		}
	}
	return parts, nil
}

type taskSessionArtifact struct {
	name string
	data []byte
}

func writeNewPrivateArtifacts(directory string, artifacts []taskSessionArtifact) error {
	temporaryPaths := make(map[string]string, len(artifacts))
	defer func() {
		for _, temporary := range temporaryPaths {
			_ = os.Remove(temporary)
		}
	}()
	if err := stagePrivateArtifacts(directory, artifacts, temporaryPaths); err != nil {
		return err
	}
	return publishPrivateArtifacts(directory, artifacts, temporaryPaths)
}

func stagePrivateArtifacts(directory string, artifacts []taskSessionArtifact, temporaryPaths map[string]string) error {
	for _, artifact := range artifacts {
		path := filepath.Join(directory, artifact.name)
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("task-session artifact already exists: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		temporary, err := os.CreateTemp(directory, ".task-session-artifact-*")
		if err != nil {
			return err
		}
		temporaryPaths[artifact.name] = temporary.Name()
		if err := writePrivateArtifact(temporary, artifact.data); err != nil {
			return err
		}
	}
	return nil
}

func writePrivateArtifact(file *os.File, data []byte) error {
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func publishPrivateArtifacts(directory string, artifacts []taskSessionArtifact, temporaryPaths map[string]string) error {
	committedPaths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		temporary := temporaryPaths[artifact.name]
		path := filepath.Join(directory, artifact.name)
		// Linking publishes a fully written file and, unlike Rename on POSIX,
		// cannot replace a target created after the validation pass above.
		if err := os.Link(temporary, path); err != nil {
			removeArtifacts(committedPaths)
			return err
		}
		committedPaths = append(committedPaths, path)
		if err := os.Remove(temporary); err != nil {
			removeArtifacts(committedPaths)
			return err
		}
		delete(temporaryPaths, artifact.name)
	}
	return nil
}

func removeArtifacts(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func readPrivateRegularArtifact(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("retained task artifact is not a regular file: %s", path)
	}
	return os.ReadFile(path)
}

func digestTaskSessionScope(config string, scope []byte) string {
	return digestBytes(append(append([]byte(config), 0), scope...))
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
