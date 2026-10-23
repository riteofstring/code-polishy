package supplychain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type osvInputSnapshot struct {
	data   string
	digest string
	path   string
}

func (snapshot *osvInputSnapshot) Identity(command policy.Command) (string, error) {
	input, present := osvInputArgument(command.Argv, "--lockfile")
	config, configured := osvInputArgument(command.Argv, "--config")
	if !present || input != "osv-scanner:"+snapshot.path || !configured || config != "osv-scanner.toml" || !command.SealedEnvironment || command.Cwd != "." || !slices.Contains(command.Argv, "--no-resolve") {
		return "", errors.New("OSV command does not match its isolated input snapshot")
	}
	if slices.Contains(command.Argv, "--offline") || slices.Contains(command.Argv, "--offline-vulnerabilities") {
		return "", nil
	}
	return snapshot.digest, nil
}

func osvInputArgument(arguments []string, name string) (string, bool) {
	index := slices.Index(arguments, name)
	if index < 0 || index+1 >= len(arguments) {
		return "", false
	}
	return arguments[index+1], true
}

func (snapshot *osvInputSnapshot) Execute(ctx context.Context, commandRunner runner.Runner, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if _, err := snapshot.Identity(invocation.Command); err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	root, cleanup, err := snapshot.prepare()
	if err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	defer cleanup()
	invocation.Root = root
	result, output, runErr := commandRunner.Execute(ctx, invocation)
	if len(output.Stdout) != 0 {
		normalized, err := normalizeOSVSnapshotReport(output.Stdout, root, snapshot.path)
		if err != nil {
			result.FailureCategory = runner.CaptureFailureCategory(ctx, result, runErr)
			return result, output, errors.Join(runErr, err)
		}
		output.Stdout = normalized
	}
	return result, output, runErr
}

func (snapshot *osvInputSnapshot) prepare() (string, func(), error) {
	root, err := os.MkdirTemp("", "code-polishy-osv-snapshot-")
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(root, filepath.FromSlash(snapshot.path))
	config := filepath.Join(root, "osv-scanner.toml")
	cleanup := func() {
		_ = os.Chmod(path, 0o600)
		_ = os.Chmod(config, 0o600)
		_ = os.RemoveAll(root)
	}
	err = os.MkdirAll(filepath.Dir(path), 0o700)
	if err == nil {
		err = os.WriteFile(path, []byte(snapshot.data), 0o400)
	}
	if err == nil {
		err = os.WriteFile(config, nil, 0o400)
	}
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create isolated OSV inputs: %w", err)
	}
	return root, cleanup, nil
}

func normalizeOSVSnapshotReport(payload []byte, root, inputPath string) ([]byte, error) {
	var report map[string]json.RawMessage
	if err := json.Unmarshal(payload, &report); err != nil {
		return nil, err
	}
	var results []map[string]json.RawMessage
	if err := json.Unmarshal(report["results"], &results); err != nil {
		return nil, err
	}
	for _, result := range results {
		var source map[string]json.RawMessage
		if err := json.Unmarshal(result["source"], &source); err != nil {
			return nil, err
		}
		var path string
		if err := json.Unmarshal(source["path"], &path); err != nil {
			return nil, err
		}
		scope, err := osvScope(repository.Repository{Root: root}, ".", path)
		if err != nil || scope != inputPath {
			return nil, errors.New("OSV result refers outside its isolated input snapshot")
		}
		source["path"], _ = json.Marshal(inputPath)
		result["source"], _ = json.Marshal(source)
	}
	report["results"], _ = json.Marshal(results)
	return json.Marshal(report)
}

func validateOSVInventoryReport(scan osvScan, report osvReport) error {
	if scan.InputPath == "" {
		return nil
	}
	if scan.Snapshot == nil {
		return errors.New("public package inventory snapshot is missing")
	}
	var projected osvReport
	if err := json.Unmarshal([]byte(scan.Snapshot.data), &projected); err != nil {
		return fmt.Errorf("planned public package projection is malformed")
	}
	expected := map[string]bool{}
	for _, result := range projected.Results {
		for _, pkg := range result.Packages {
			expected[pkg.Package.Ecosystem+"\x00"+pkg.Package.Name+"\x00"+pkg.Package.Version] = true
		}
	}
	for _, result := range report.Results {
		for _, pkg := range result.Packages {
			key := pkg.Package.Ecosystem + "\x00" + pkg.Package.Name + "\x00" + pkg.Package.Version
			if !expected[key] {
				return fmt.Errorf("scanner report repeats a package or refers outside its public package projection")
			}
			delete(expected, key)
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("scanner report omitted packages from its complete locked registry inventory")
	}
	return nil
}
