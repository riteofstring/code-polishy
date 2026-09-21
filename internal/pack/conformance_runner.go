package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/schema"
)

type ConformanceOptions struct {
	LedgerPath          string
	ReferenceExecutable string
	CandidateExecutable string
}

type ConformanceReport struct {
	Schema    string                        `json:"$schema"`
	Protocol  string                        `json:"protocol"`
	Ledger    ConformanceEvidenceIdentity   `json:"ledger"`
	Reference ConformanceExecutableIdentity `json:"reference"`
	Candidate ConformanceExecutableIdentity `json:"candidate"`
	Fixtures  []ConformanceFixtureEvidence  `json:"fixtures"`
	Summary   ConformanceReportSummary      `json:"summary"`
}

type ConformanceEvidenceIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ConformanceExecutableIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ConformanceFixtureEvidence struct {
	ID                string                  `json:"id"`
	BehaviorIDs       []string                `json:"behaviorIds"`
	Status            string                  `json:"status"`
	SkipReason        string                  `json:"skipReason,omitempty"`
	Reference         *ConformanceRunEvidence `json:"reference,omitempty"`
	Candidate         *ConformanceRunEvidence `json:"candidate,omitempty"`
	Differences       []ConformanceDifference `json:"differences,omitempty"`
	AssertionFailures []string                `json:"assertionFailures,omitempty"`
}

type ConformanceRunEvidence struct {
	ExitStatus int                       `json:"exitStatus"`
	Report     json.RawMessage           `json:"report"`
	Stderr     string                    `json:"stderr,omitempty"`
	Before     []ConformanceFileIdentity `json:"before"`
	After      []ConformanceFileIdentity `json:"after"`
}

type ConformanceFileIdentity struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

type ConformanceDifference struct {
	Path      string `json:"path"`
	Reference string `json:"reference"`
	Candidate string `json:"candidate"`
}

type ConformanceReportSummary struct {
	Status  string `json:"status"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
}

type conformanceExecution struct {
	ExitStatus int
	Stdout     []byte
	Stderr     []byte
}

type conformanceExecutor interface {
	Run(context.Context, string, string, []string, int) (conformanceExecution, error)
}

type osConformanceExecutor struct{}

func RunConformance(ctx context.Context, options ConformanceOptions) (ConformanceReport, error) {
	return runConformance(ctx, options, osConformanceExecutor{})
}

func runConformance(ctx context.Context, options ConformanceOptions, executor conformanceExecutor) (ConformanceReport, error) {
	ledger, err := LoadConformanceLedger(options.LedgerPath)
	if err != nil {
		return ConformanceReport{}, err
	}
	reference, err := conformanceExecutableIdentity(options.ReferenceExecutable)
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("reference executable: %w", err)
	}
	candidate, err := conformanceExecutableIdentity(options.CandidateExecutable)
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("candidate executable: %w", err)
	}
	report := ConformanceReport{
		Schema:    ConformanceReportSchema,
		Protocol:  ConformanceReportProtocol,
		Ledger:    ConformanceEvidenceIdentity{Path: filepath.ToSlash(ledger.Path), SHA256: ledger.SHA256},
		Reference: reference,
		Candidate: candidate,
		Fixtures:  []ConformanceFixtureEvidence{},
		Summary:   ConformanceReportSummary{Status: "passed"},
	}
	for _, fixture := range ledger.Fixtures {
		if !slices.Contains(fixture.Platforms, CurrentPlatform()) {
			report.Fixtures = append(report.Fixtures, ConformanceFixtureEvidence{ID: fixture.ID, BehaviorIDs: slices.Clone(fixture.BehaviorIDs), Status: "skipped", SkipReason: "platform " + CurrentPlatform() + " is not declared by the fixture"})
			report.Summary.Skipped++
			continue
		}
		evidence, runErr := runConformanceFixture(ctx, fixture, reference.Path, candidate.Path, executor)
		if runErr != nil {
			return ConformanceReport{}, fmt.Errorf("fixture %s: %w", fixture.ID, runErr)
		}
		report.Fixtures = append(report.Fixtures, evidence)
		if evidence.Status == "passed" {
			report.Summary.Passed++
		} else {
			report.Summary.Failed++
			report.Summary.Status = "failed"
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		return ConformanceReport{}, err
	}
	if err := schema.NewValidator(ConformanceReportSchema).Validate(data); err != nil {
		return ConformanceReport{}, fmt.Errorf("validate conformance report: %w", err)
	}
	return report, nil
}

func conformanceExecutableIdentity(name string) (ConformanceExecutableIdentity, error) {
	if strings.TrimSpace(name) == "" {
		return ConformanceExecutableIdentity{}, errors.New("path is required")
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return ConformanceExecutableIdentity{}, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return ConformanceExecutableIdentity{}, err
	}
	file, err := os.Open(canonical)
	if err != nil {
		return ConformanceExecutableIdentity{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return ConformanceExecutableIdentity{}, errors.New("path must resolve to an executable regular file")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(file, 512<<20)); err != nil {
		return ConformanceExecutableIdentity{}, err
	}
	if info.Size() > 512<<20 {
		return ConformanceExecutableIdentity{}, errors.New("executable exceeds 512 MiB")
	}
	return ConformanceExecutableIdentity{Path: canonical, SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

func runConformanceFixture(ctx context.Context, fixture ConformanceFixture, referenceExecutable, candidateExecutable string, executor conformanceExecutor) (ConformanceFixtureEvidence, error) {
	temporary, err := os.MkdirTemp("", "code-polishy-conformance-")
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	defer os.RemoveAll(temporary)
	referenceRoot := filepath.Join(temporary, "reference")
	candidateRoot := filepath.Join(temporary, "candidate")
	if err := materializeConformanceFixture(referenceRoot, fixture); err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	if err := materializeConformanceFixture(candidateRoot, fixture); err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	referenceBefore, err := conformanceSnapshot(referenceRoot)
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	candidateBefore, err := conformanceSnapshot(candidateRoot)
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	if !slices.Equal(referenceBefore, candidateBefore) {
		return ConformanceFixtureEvidence{}, errors.New("materialized repositories are not identical")
	}
	referenceRun, err := executeConformanceLane(ctx, fixture, referenceExecutable, referenceRoot, referenceBefore, executor)
	if err != nil {
		return ConformanceFixtureEvidence{}, fmt.Errorf("reference: %w", err)
	}
	candidateRun, err := executeConformanceLane(ctx, fixture, candidateExecutable, candidateRoot, candidateBefore, executor)
	if err != nil {
		return ConformanceFixtureEvidence{}, fmt.Errorf("candidate: %w", err)
	}
	differences, err := compareConformanceRuns(referenceRun, referenceRoot, candidateRun, candidateRoot)
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	failures := append(assertConformanceOutcome("reference", fixture.Expected, referenceRun), assertConformanceOutcome("candidate", fixture.Expected, candidateRun)...)
	evidence := ConformanceFixtureEvidence{
		ID:                fixture.ID,
		BehaviorIDs:       slices.Clone(fixture.BehaviorIDs),
		Status:            "passed",
		Reference:         &referenceRun,
		Candidate:         &candidateRun,
		Differences:       differences,
		AssertionFailures: failures,
	}
	if len(differences) != 0 || len(failures) != 0 {
		evidence.Status = "failed"
	}
	return evidence, nil
}

func materializeConformanceFixture(root string, fixture ConformanceFixture) error {
	if err := os.Mkdir(root, 0o700); err != nil {
		return err
	}
	for _, file := range fixture.Files {
		data, err := conformanceFixtureBytes(file)
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if file.Mode == "0755" {
			mode = 0o755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return err
		}
		if err := os.Chmod(target, mode); err != nil {
			return err
		}
	}
	return nil
}

func executeConformanceLane(ctx context.Context, fixture ConformanceFixture, executable, root string, before []ConformanceFileIdentity, executor conformanceExecutor) (ConformanceRunEvidence, error) {
	execution, err := executor.Run(ctx, executable, root, slices.Clone(fixture.Arguments), fixture.TimeoutSeconds)
	if err != nil {
		return ConformanceRunEvidence{}, err
	}
	if len(execution.Stdout) == 0 || len(execution.Stdout) > 8<<20 {
		return ConformanceRunEvidence{}, errors.New("CLI report is empty or exceeds 8 MiB")
	}
	if err := schema.NewValidator("https://code-polishy.dev/schema/code-polishy-report.schema.json").Validate(execution.Stdout); err != nil {
		return ConformanceRunEvidence{}, fmt.Errorf("invalid CLI report: %w", err)
	}
	after, err := conformanceSnapshot(root)
	if err != nil {
		return ConformanceRunEvidence{}, err
	}
	return ConformanceRunEvidence{
		ExitStatus: execution.ExitStatus,
		Report:     append(json.RawMessage(nil), execution.Stdout...),
		Stderr:     string(execution.Stderr),
		Before:     before,
		After:      after,
	}, nil
}

func (osConformanceExecutor) Run(ctx context.Context, executable, root string, arguments []string, timeout int) (conformanceExecution, error) {
	command := policy.Command{
		Name:               "language-conformance",
		Argv:               append([]string{executable, "--repo-root", root}, arguments...),
		Cwd:                ".",
		TimeoutSeconds:     timeout,
		ExclusiveResources: []string{},
	}
	result, output, err := (runner.OSRunner{}).RunStructured(ctx, root, command)
	if err != nil && result.FailureCategory != runner.FailureCommandExit {
		return conformanceExecution{}, err
	}
	return conformanceExecution{ExitStatus: result.ExitStatus, Stdout: output.Stdout, Stderr: output.Stderr}, nil
}

func conformanceSnapshot(root string) ([]ConformanceFileIdentity, error) {
	files := []ConformanceFileIdentity{}
	total := int64(0)
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative == ".code-polishy-reports" || relative == ".code-polishy-artifacts" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("conformance repository contains a non-regular file at %s", relative)
		}
		total += info.Size()
		if len(files) >= maximumConformanceFiles || total > maximumConformanceFixtureBytes {
			return errors.New("conformance repository exceeds its file or byte limit")
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		files = append(files, ConformanceFileIdentity{Path: relative, Mode: fmt.Sprintf("%04o", info.Mode().Perm()), SHA256: hex.EncodeToString(digest[:])})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(left, right ConformanceFileIdentity) int { return strings.Compare(left.Path, right.Path) })
	return files, nil
}

func assertConformanceOutcome(lane string, expected ConformanceExpectedOutcome, run ConformanceRunEvidence) []string {
	failures := []string{}
	if run.ExitStatus != expected.ExitStatus {
		failures = append(failures, fmt.Sprintf("%s exitStatus: expected %d, received %d", lane, expected.ExitStatus, run.ExitStatus))
	}
	envelope := struct {
		Summary struct {
			Status string `json:"status"`
		} `json:"summary"`
		Findings []struct {
			Check string `json:"ruleId"`
		} `json:"findings"`
		AnalysisContext []struct {
			Paths []string `json:"paths"`
		} `json:"analysisContext"`
	}{}
	if err := json.Unmarshal(run.Report, &envelope); err != nil {
		return append(failures, lane+" report could not be decoded after schema validation")
	}
	if envelope.Summary.Status != expected.ReportStatus {
		failures = append(failures, fmt.Sprintf("%s summary.status: expected %s, received %s", lane, expected.ReportStatus, envelope.Summary.Status))
	}
	for _, rule := range expected.RequiredRules {
		if !slices.ContainsFunc(envelope.Findings, func(finding struct {
			Check string `json:"ruleId"`
		}) bool {
			return finding.Check == rule
		}) {
			failures = append(failures, fmt.Sprintf("%s findings: required rule %s was absent", lane, rule))
		}
	}
	coverage := []string{}
	for _, context := range envelope.AnalysisContext {
		coverage = append(coverage, context.Paths...)
	}
	for _, path := range expected.RequiredCoverage {
		if !slices.Contains(coverage, path) {
			failures = append(failures, fmt.Sprintf("%s analysisContext: required path %s was absent", lane, path))
		}
	}
	before := conformanceFilesByPath(run.Before)
	after := conformanceFilesByPath(run.After)
	allowedWrites := map[string]bool{}
	for _, write := range expected.Writes {
		allowedWrites[write.Path] = true
		file, exists := after[write.Path]
		if !exists || file.SHA256 != write.SHA256 {
			failures = append(failures, fmt.Sprintf("%s writes[%s]: expected SHA-256 %s", lane, write.Path, write.SHA256))
		}
	}
	for _, path := range expected.Protected {
		if before[path] != after[path] {
			failures = append(failures, fmt.Sprintf("%s protected[%s]: file identity changed", lane, path))
		}
	}
	for path, initial := range before {
		if final, exists := after[path]; (!exists || final != initial) && !allowedWrites[path] {
			failures = append(failures, fmt.Sprintf("%s filesystem[%s]: unauthorized change", lane, path))
		}
	}
	for path := range after {
		if _, exists := before[path]; !exists && !allowedWrites[path] {
			failures = append(failures, fmt.Sprintf("%s filesystem[%s]: unauthorized addition", lane, path))
		}
	}
	return failures
}

func conformanceFilesByPath(files []ConformanceFileIdentity) map[string]ConformanceFileIdentity {
	result := make(map[string]ConformanceFileIdentity, len(files))
	for _, file := range files {
		result[file.Path] = file
	}
	return result
}
