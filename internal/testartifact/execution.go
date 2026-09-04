package testartifact

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

var executionIDPattern = regexp.MustCompile(`^run-[0-9a-f]{32}$`)

type pruneCandidate struct {
	name      string
	completed time.Time
}

func Start(repositoryRoot, requestedID string) (*Execution, error) {
	root, err := prepareRoot(repositoryRoot)
	if err != nil {
		return nil, err
	}
	id := requestedID
	if id == "" {
		id, err = newExecutionID()
		if err != nil {
			return nil, err
		}
	}
	if !executionIDPattern.MatchString(id) {
		return nil, errors.New("test artifact execution ID is invalid")
	}
	directory := filepath.Join(root, id)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create test artifact execution: %w", err)
	}
	createdAt := time.Now().UTC()
	execution := &Execution{repositoryRoot: repositoryRoot, root: root, directory: directory, id: id, createdAt: createdAt}
	if err := execution.writeLease(); err != nil {
		_ = os.Remove(directory)
		return nil, err
	}
	if err := writeManifest(directory, manifest{Version: manifestVersion, ExecutionID: id, Status: "active", CreatedAt: createdAt}); err != nil {
		_ = os.Remove(filepath.Join(directory, leaseFilename))
		_ = os.Remove(directory)
		return nil, err
	}
	return execution, nil
}

func prepareRoot(repositoryRoot string) (string, error) {
	absolute, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", err
	}
	root := filepath.Join(absolute, RootName)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(root, 0o700); err != nil {
			return "", fmt.Errorf("create test artifact root: %w", err)
		}
		return root, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("test artifact root is not a regular directory")
	}
	return root, nil
}

func newExecutionID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "run-" + hex.EncodeToString(random), nil
}

func (execution *Execution) ID() string {
	if execution == nil {
		return ""
	}
	return execution.id
}

func (execution *Execution) Directory() string {
	if execution == nil {
		return ""
	}
	return execution.directory
}

func (execution *Execution) PrepareCommand(suite policy.TestSuite, command policy.Command) (policy.Command, error) {
	if execution == nil || execution.completed {
		return policy.Command{}, errors.New("test artifact execution is unavailable")
	}
	suiteDirectory := ""
	for attempt := 1; ; attempt++ {
		name := suite.Name
		if attempt > 1 {
			name = fmt.Sprintf("%s-attempt-%d", suite.Name, attempt)
		}
		candidate := filepath.Join(execution.directory, name)
		if err := os.Mkdir(candidate, 0o700); err == nil {
			suiteDirectory = candidate
			break
		} else if !errors.Is(err, os.ErrExist) {
			return policy.Command{}, fmt.Errorf("create suite artifact directory: %w", err)
		}
	}
	command.Argv = substituteArguments(command.Argv, suiteDirectory, execution.id)
	command.EnvironmentOverrides = []string{
		EnvironmentDirectory + "=" + suiteDirectory,
		EnvironmentID + "=" + execution.id,
	}
	command.TestArtifacts = append([]policy.TestArtifact{}, suite.Artifacts...)
	command.TestArtifactSuite = suite.Name
	command.TestArtifactDirectory = suiteDirectory
	return command, nil
}

func substituteArguments(argv []string, directory, executionID string) []string {
	result := make([]string, len(argv))
	for index, argument := range argv {
		argument = strings.ReplaceAll(argument, DirectoryToken, directory)
		result[index] = strings.ReplaceAll(argument, ExecutionIDToken, executionID)
	}
	return result
}

func (execution *Execution) ValidateCommand(command policy.Command) ([]Record, error) {
	if command.TestArtifactDirectory == "" || command.TestArtifactSuite == "" {
		if len(command.TestArtifacts) == 0 {
			return []Record{}, nil
		}
		return nil, errors.New("test artifact command has no managed directory")
	}
	return validateDeclared(command.TestArtifactDirectory, command.TestArtifactSuite, command.TestArtifacts)
}

func (execution *Execution) Complete() error {
	if execution == nil || execution.completed {
		return errors.New("test artifact execution is unavailable")
	}
	completedAt := time.Now().UTC()
	if err := writeManifest(execution.directory, manifest{
		Version: manifestVersion, ExecutionID: execution.id, Status: "completed",
		CreatedAt: execution.createdAt, CompletedAt: completedAt,
	}); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(execution.directory, leaseFilename)); err != nil {
		return fmt.Errorf("release test artifact lease: %w", err)
	}
	execution.completed = true
	return prune(execution.root, execution.id)
}

func (execution *Execution) Abandon() error {
	if execution == nil || execution.completed {
		return nil
	}
	execution.completed = true
	return nil
}

func (execution *Execution) writeLease() error {
	lease, err := os.OpenFile(filepath.Join(execution.directory, leaseFilename), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create test artifact lease: %w", err)
	}
	_, writeErr := lease.WriteString(execution.id + "\n")
	return errors.Join(writeErr, lease.Close())
}

func prune(root, current string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	candidates := []pruneCandidate{}
	for _, entry := range entries {
		candidate, eligible := completedPruneCandidate(root, current, entry)
		if eligible {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].completed.After(candidates[right].completed)
	})
	retained := retainedCandidateCount()
	if retained > len(candidates) {
		retained = len(candidates)
	}
	for _, stale := range candidates[retained:] {
		if err := os.RemoveAll(filepath.Join(root, stale.name)); err != nil {
			return fmt.Errorf("prune completed test artifacts: %w", err)
		}
	}
	return nil
}

func completedPruneCandidate(root, current string, entry os.DirEntry) (pruneCandidate, bool) {
	if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || entry.Name() == current {
		return pruneCandidate{}, false
	}
	directory := filepath.Join(root, entry.Name())
	owned, err := readManifest(directory)
	if err != nil || owned.Status != "completed" || owned.ExecutionID != entry.Name() || owned.CompletedAt.IsZero() {
		return pruneCandidate{}, false
	}
	if _, err := os.Lstat(filepath.Join(directory, leaseFilename)); !errors.Is(err, os.ErrNotExist) {
		return pruneCandidate{}, false
	}
	if err := removableTree(directory); err != nil {
		return pruneCandidate{}, false
	}
	return pruneCandidate{name: entry.Name(), completed: owned.CompletedAt}, true
}

func retainedCandidateCount() int {
	if retainedExecutions <= 1 {
		return 0
	}
	return retainedExecutions - 1
}

func removableTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("owned artifact tree contains a linked or special entry")
		}
		return nil
	})
}
