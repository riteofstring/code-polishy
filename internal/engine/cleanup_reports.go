package engine

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"

	"github.com/riteofstring/code-polishy/internal/gaterun"
)

var cleanupDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func reportCleanupCandidates(root *os.Root) ([]string, []CleanupKept, error) {
	const directory = ".code-polishy-reports"
	reports, err := openOutputCleanupRoot(root, directory)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer reports.Close()
	entries, err := fs.ReadDir(reports.FS(), ".")
	if err != nil {
		return nil, nil, err
	}
	candidates := []string{}
	kept := []CleanupKept{}
	for _, entry := range entries {
		command := entry.Name()
		relative := path.Join(directory, command)
		if !entry.IsDir() || !validReportCommand(command) || protectedReportDirectory(command) {
			kept = append(kept, CleanupKept{Path: relative, Reason: "user files or retained review and upgrade evidence"})
			continue
		}
		switch command {
		case "test-receipts", "security-scans", "security-refresh":
			candidates = append(candidates, relative)
			continue
		}
		paths, retained, err := commandReportCleanupCandidates(reports, command)
		if err != nil {
			return nil, nil, err
		}
		candidates = append(candidates, paths...)
		kept = append(kept, retained...)
	}
	return candidates, kept, nil
}

func commandReportCleanupCandidates(reports *os.Root, command string) ([]string, []CleanupKept, error) {
	children, err := fs.ReadDir(reports.FS(), command)
	if err != nil {
		return nil, nil, err
	}
	candidates := []string{}
	kept := []CleanupKept{}
	for _, child := range children {
		candidate := path.Join(".code-polishy-reports", command, child.Name())
		if child.IsDir() && cleanupDigestPattern.MatchString(child.Name()) && ownedReportDirectory(reports, command, child.Name()) {
			candidates = append(candidates, candidate)
		} else {
			kept = append(kept, CleanupKept{Path: candidate, Reason: "not a completed managed report"})
		}
	}
	return candidates, kept, nil
}

func protectedReportDirectory(name string) bool {
	switch name {
	case "behavior-review", "architecture-review", "capability-upgrades":
		return true
	default:
		return false
	}
}

func ownedReportDirectory(reports *os.Root, command, digest string) bool {
	relative := path.Join(command, digest, "report.json")
	header := struct {
		Protocol   string `json:"protocol"`
		Command    string `json:"command"`
		ReportPath string `json:"reportPath"`
	}{}
	if readCleanupJSON(reports, relative, &header) == nil && header.Protocol == ReportProtocol &&
		header.Command == command && header.ReportPath == ".code-polishy-reports/"+relative {
		return true
	}
	if command != string(gaterun.MergeGate) && command != string(gaterun.CheckpointGate) {
		return false
	}
	return completedGateCandidate(reports, command, digest)
}

func completedGateCandidate(reports *os.Root, command, digest string) bool {
	executions := path.Join(command, digest, "executions")
	entries, err := fs.ReadDir(reports.FS(), executions)
	if err != nil || len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		report := gaterun.Report{}
		if !entry.IsDir() || readCleanupJSON(reports, path.Join(executions, entry.Name(), "report.json"), &report) != nil ||
			report.Version != gaterun.Version || report.IdentitySHA256 != digest || string(report.Identity.Gate) != command ||
			report.ExecutionID != entry.Name() || report.CompletedAt.IsZero() {
			return false
		}
	}
	return true
}

func readCleanupJSON(root *os.Root, path string, value any) error {
	info, err := root.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return fmt.Errorf("%s is not a bounded regular report", path)
	}
	data, err := root.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
