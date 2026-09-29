package gaterun

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type StoredTestOutcome struct {
	StartedAt time.Time
	Candidate string
	Command   CommandOutcome
}

type StoredTestHistory struct {
	Outcomes    []StoredTestOutcome
	OtherFormat int
	Invalid     int
}

func StoredTestOutcomes(repositoryRoot string) (StoredTestHistory, error) {
	history := StoredTestHistory{Outcomes: []StoredTestOutcome{}}
	for _, gate := range []GateKind{CheckpointGate, MergeGate} {
		runs, err := storedRunNames(filepath.Join(repositoryRoot, reportsDirectory, string(gate)), validSHA256)
		if err != nil {
			return StoredTestHistory{}, err
		}
		for _, runSHA256 := range runs {
			history = appendStoredRunOutcomes(history, repositoryRoot, gate, runSHA256)
		}
	}
	return history, nil
}

func appendStoredRunOutcomes(history StoredTestHistory, repositoryRoot string, gate GateKind, runSHA256 string) StoredTestHistory {
	executions, err := storedRunNames(filepath.Join(repositoryRoot, reportsDirectory, string(gate), runSHA256, executionsDirectory), validExecutionID)
	if err != nil {
		history.Invalid++
		return history
	}
	for _, executionID := range executions {
		report, otherFormat, err := loadStoredExecution(repositoryRoot, gate, runSHA256, executionID)
		switch {
		case otherFormat:
			history.OtherFormat++
		case err != nil:
			history.Invalid++
		default:
			history.Outcomes = append(history.Outcomes, storedTestOutcomes(report)...)
		}
	}
	return history
}

func loadStoredExecution(repositoryRoot string, gate GateKind, runSHA256, executionID string) (Report, bool, error) {
	_, runDirectory, err := managedRunDirectory(repositoryRoot, gate, runSHA256, false)
	if err != nil {
		return Report{}, false, err
	}
	directory, err := existingExecutionDirectory(runDirectory, executionID)
	if err != nil {
		return Report{}, false, err
	}
	data, err := readExecutionReportBytes(directory)
	if err != nil {
		return Report{}, false, err
	}
	var header struct {
		Version *int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err == nil && header.Version != nil && *header.Version != Version {
		return Report{}, true, nil
	}
	report, err := decodeExecutionEvidenceReport(data, executionID)
	if err != nil {
		return Report{}, false, err
	}
	if report.Identity.Gate != gate || report.IdentitySHA256 != runSHA256 {
		return Report{}, false, fmt.Errorf("%w: gate run report is stored under another identity", ErrStaleArtifact)
	}
	return report, false, nil
}

func storedTestOutcomes(report Report) []StoredTestOutcome {
	outcomes := []StoredTestOutcome{}
	for _, command := range report.Commands {
		if command.Category == OrdinaryTest && !command.Reused && len(command.Attempts) > 0 {
			outcomes = append(outcomes, StoredTestOutcome{StartedAt: report.StartedAt, Candidate: report.Identity.Candidate, Command: command})
		}
	}
	return outcomes
}

func storedRunNames(path string, valid func(string) bool) ([]string, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, operational("list gate run history", err)
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() && valid(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}
