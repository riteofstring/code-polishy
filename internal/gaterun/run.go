package gaterun

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

func Start(options StartOptions) (*Run, error) {
	if err := validateIdentity(options.Identity); err != nil {
		return nil, err
	}
	root, directory, err := newRunDirectory(options.RepositoryRoot, options.Identity)
	if err != nil {
		return nil, err
	}
	reportPath, _, err := artifactFilePath(root, directory, reportFilename)
	if err != nil {
		return nil, err
	}
	if err := rejectUnsafeFileTarget(reportPath); err != nil {
		return nil, err
	}
	runSHA256, err := options.Identity.Digest()
	if err != nil {
		return nil, err
	}
	startedAt := options.StartedAt.UTC()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	return &Run{
		repositoryRoot: root, directory: directory, reportPath: reportPath,
		identity: cloneIdentity(options.Identity), runSHA256: runSHA256, startedAt: startedAt,
		commands: map[int]CommandOutcome{}, openLogs: map[int]bool{},
	}, nil
}

func (run *Run) Identity() Identity {
	if run == nil {
		return Identity{}
	}
	return cloneIdentity(run.identity)
}

func (run *Run) ReportPath() string {
	if run == nil || run.reportPath == "" {
		return ""
	}
	relative, err := filepath.Rel(run.repositoryRoot, run.reportPath)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(relative)
}

func (run *Run) RecordAttempt(index int, input AttemptInput, log LogResult) (CommandOutcome, error) {
	entry, reference, err := run.prepareAttempt(index, input)
	if err != nil {
		return CommandOutcome{}, err
	}
	attempt, err := run.validatedAttempt(reference, len(entry.Attempts)+1, input, log)
	if err != nil {
		return CommandOutcome{}, err
	}
	entry.Attempts = append(entry.Attempts, attempt)
	entry.Status = input.Status
	entry.ReceiptPath = ""
	entry.ReceiptSHA256 = ""
	if input.Status == Passed && reference.Spec.Category == OrdinaryTest {
		receipt, path, err := run.writePassedReceipt(reference, attempt)
		if err != nil {
			return CommandOutcome{}, err
		}
		entry.ReceiptPath = path
		entry.ReceiptSHA256 = receipt.SHA256
	}
	run.commands[index] = entry
	return cloneCommandOutcome(entry), nil
}

func (run *Run) prepareAttempt(index int, input AttemptInput) (CommandOutcome, CommandRef, error) {
	if !run.commandAvailable(index) {
		return CommandOutcome{}, CommandRef{}, fmt.Errorf("%w: gate run command is unavailable", ErrInvalidInput)
	}
	entry, err := run.commandOutcome(index)
	if err != nil {
		return CommandOutcome{}, CommandRef{}, err
	}
	if entry.Reused {
		return CommandOutcome{}, CommandRef{}, fmt.Errorf("%w: reused command %d cannot record an attempt", ErrInvalidInput, index)
	}
	if err := validateAttemptInput(input); err != nil {
		return CommandOutcome{}, CommandRef{}, err
	}
	reference, err := run.identity.Command(index)
	if err != nil {
		return CommandOutcome{}, CommandRef{}, err
	}
	return entry, reference, nil
}

func (run *Run) commandAvailable(index int) bool {
	return run != nil && !run.finalized && !run.openLogs[index]
}

func (run *Run) RecordReuse(index int, reusable ReusableReceipt) (CommandOutcome, error) {
	if run == nil || run.finalized || run.openLogs[index] {
		return CommandOutcome{}, fmt.Errorf("%w: gate run command is unavailable", ErrInvalidInput)
	}
	if run.identity.Gate != MergeGate {
		return CommandOutcome{}, fmt.Errorf("%w: only merge gate runs can reuse receipts", ErrIneligible)
	}
	entry, err := run.commandOutcome(index)
	if err != nil {
		return CommandOutcome{}, err
	}
	if len(entry.Attempts) != 0 || entry.Reused {
		return CommandOutcome{}, fmt.Errorf("%w: command %d already has an outcome", ErrInvalidInput, index)
	}
	reference, err := run.identity.Command(index)
	if err != nil {
		return CommandOutcome{}, err
	}
	if reference.Spec.Category != OrdinaryTest {
		return CommandOutcome{}, fmt.Errorf("%w: command %q is not an ordinary test", ErrIneligible, reference.Spec.Name)
	}
	if err := run.validateReusableReceipt(reference, reusable); err != nil {
		return CommandOutcome{}, err
	}
	entry.Status = Passed
	entry.Reused = true
	entry.ReceiptPath = reusable.Path
	entry.ReceiptSHA256 = reusable.Receipt.SHA256
	run.commands[index] = entry
	return cloneCommandOutcome(entry), nil
}

func (run *Run) Finalize(options FinalizeOptions) (Report, error) {
	report, err := run.finalReport(options)
	if err != nil {
		return Report{}, err
	}
	data, err := marshalArtifact(report, "encode gate run report")
	if err != nil {
		return Report{}, err
	}
	if err := writeArtifactAtomic(run.reportPath, data); err != nil {
		return Report{}, err
	}
	run.finalized = true
	return cloneReport(report), nil
}

func (run *Run) finalReport(options FinalizeOptions) (Report, error) {
	commands, completedAt, err := run.finalizationState(options)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Version: Version, Identity: cloneIdentity(run.identity), IdentitySHA256: run.runSHA256,
		Status: options.Status, StartedAt: run.startedAt, CompletedAt: completedAt,
		Commands: commands, Findings: cloneFindings(options.Findings), Notes: cloneStrings(options.Notes),
	}
	if err := validateReport(report, run.identity); err != nil {
		return Report{}, err
	}
	digest, err := reportDigest(report)
	if err != nil {
		return Report{}, err
	}
	report.SHA256 = digest
	return report, nil
}

func (run *Run) finalizationState(options FinalizeOptions) ([]CommandOutcome, time.Time, error) {
	if run == nil || run.finalized || len(run.openLogs) != 0 {
		return nil, time.Time{}, fmt.Errorf("%w: gate run cannot finalize", ErrInvalidInput)
	}
	if !validRunStatus(options.Status) {
		return nil, time.Time{}, fmt.Errorf("%w: final gate run status is invalid", ErrInvalidInput)
	}
	commands, err := run.sortedOutcomes()
	if err != nil {
		return nil, time.Time{}, err
	}
	if options.Status == RunPassed && !completedSuccessfully(run.identity.Commands, commands) {
		return nil, time.Time{}, fmt.Errorf("%w: a passed gate run has incomplete command outcomes", ErrInvalidInput)
	}
	completedAt := completedRunTime(options.CompletedAt)
	if completedAt.Before(run.startedAt) {
		return nil, time.Time{}, fmt.Errorf("%w: final gate run time precedes its start", ErrInvalidInput)
	}
	return commands, completedAt, nil
}

func completedRunTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func (run *Run) commandOutcome(index int) (CommandOutcome, error) {
	reference, err := run.identity.Command(index)
	if err != nil {
		return CommandOutcome{}, err
	}
	entry, exists := run.commands[index]
	if exists {
		return entry, nil
	}
	return CommandOutcome{
		CommandIndex: index, CommandSHA256: reference.SHA256, Name: reference.Spec.Name,
		Category: reference.Spec.Category, Attempts: []Attempt{},
	}, nil
}

func validateAttemptInput(input AttemptInput) error {
	if input.Duration < 0 || input.ResourceWait < 0 {
		return fmt.Errorf("%w: command duration is invalid", ErrInvalidInput)
	}
	switch input.Status {
	case Passed:
		if input.ExitStatus != 0 || input.FailureCategory != "" {
			return fmt.Errorf("%w: passed command outcome is invalid", ErrInvalidInput)
		}
	case Failed:
		if !validFailureCategory(input.FailureCategory) {
			return fmt.Errorf("%w: failed command category is invalid", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: command status is invalid", ErrInvalidInput)
	}
	return nil
}

func validFailureCategory(category FailureCategory) bool {
	switch category {
	case CommandExit, Timeout, Canceled, Environment, Resource, Operational:
		return true
	default:
		return false
	}
}

func (run *Run) validatedAttempt(reference CommandRef, number int, input AttemptInput, result LogResult) (Attempt, error) {
	path, display, err := run.logPath(reference.SHA256, number, false)
	if err != nil {
		return Attempt{}, err
	}
	if result.Path != display || !validSHA256(result.SHA256) {
		return Attempt{}, fmt.Errorf("%w: command log does not match its planned path", ErrInvalidArtifact)
	}
	document, data, err := readCommandLog(path)
	if err != nil {
		return Attempt{}, err
	}
	if ContentSHA256(data) != result.SHA256 || !sameLogResult(document, result) {
		return Attempt{}, fmt.Errorf("%w: command log does not match its recorded digest", ErrInvalidArtifact)
	}
	return Attempt{
		Number: number, Status: input.Status, FailureCategory: input.FailureCategory, ExitStatus: input.ExitStatus,
		DurationMilliseconds: input.Duration.Milliseconds(), ResourceWaitMilliseconds: input.ResourceWait.Milliseconds(),
		LogPath: display, LogSHA256: result.SHA256, StdoutTruncated: document.StdoutTruncated, StderrTruncated: document.StderrTruncated,
	}, nil
}

func sameLogResult(document commandLogDocument, result LogResult) bool {
	return document.StreamLimit == result.StreamLimit && document.StdoutTruncated == result.StdoutTruncated &&
		document.StderrTruncated == result.StderrTruncated && string(document.Stdout) == string(result.Stdout) && string(document.Stderr) == string(result.Stderr)
}

func (run *Run) writePassedReceipt(reference CommandRef, attempt Attempt) (Receipt, string, error) {
	directory, err := secureRunSubdirectory(run.repositoryRoot, run.directory, receiptsDirectory, true)
	if err != nil {
		return Receipt{}, "", err
	}
	path, display, err := artifactFilePath(run.repositoryRoot, directory, reference.SHA256+".json")
	if err != nil {
		return Receipt{}, "", err
	}
	receipt := Receipt{
		Version: Version, Gate: run.identity.Gate, RunSHA256: run.runSHA256, CommandSHA256: reference.SHA256,
		Category: OrdinaryTest, Status: Passed, LogSHA256: attempt.LogSHA256,
	}
	digest, err := receiptDigest(receipt)
	if err != nil {
		return Receipt{}, "", err
	}
	receipt.SHA256 = digest
	data, err := marshalArtifact(receipt, "encode gate run receipt")
	if err != nil {
		return Receipt{}, "", err
	}
	if err := writeArtifactAtomic(path, data); err != nil {
		return Receipt{}, "", err
	}
	return receipt, display, nil
}

func (run *Run) receiptPath(reference CommandRef, create bool) (string, string, error) {
	directory, err := secureRunSubdirectory(run.repositoryRoot, run.directory, receiptsDirectory, create)
	if err != nil {
		return "", "", err
	}
	return artifactFilePath(run.repositoryRoot, directory, reference.SHA256+".json")
}

func (run *Run) validateReusableReceipt(reference CommandRef, reusable ReusableReceipt) error {
	stored, err := LoadReusableReceipt(run.repositoryRoot, run.identity, reference.Index)
	if err != nil {
		return err
	}
	if stored != reusable {
		return fmt.Errorf("%w: receipt changed after validation", ErrStaleArtifact)
	}
	return nil
}

func (run *Run) sortedOutcomes() ([]CommandOutcome, error) {
	indexes := make([]int, 0, len(run.commands))
	for index := range run.commands {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	result := make([]CommandOutcome, 0, len(indexes))
	for _, index := range indexes {
		entry := run.commands[index]
		if err := validateOutcome(run.identity, entry); err != nil {
			return nil, err
		}
		result = append(result, cloneCommandOutcome(entry))
	}
	return result, nil
}

func completedSuccessfully(commands []CommandSpec, outcomes []CommandOutcome) bool {
	if len(commands) != len(outcomes) {
		return false
	}
	for index, outcome := range outcomes {
		if outcome.CommandIndex != index || outcome.Status != Passed {
			return false
		}
	}
	return true
}

func validRunStatus(status RunStatus) bool {
	return status == RunPassed || status == RunFailed || status == RunOperational
}

func reportDigest(report Report) (string, error) {
	report.SHA256 = ""
	data, err := json.Marshal(report)
	if err != nil {
		return "", operational("encode gate run report digest", err)
	}
	return ContentSHA256(data), nil
}

func receiptDigest(receipt Receipt) (string, error) {
	receipt.SHA256 = ""
	data, err := json.Marshal(receipt)
	if err != nil {
		return "", operational("encode gate run receipt digest", err)
	}
	return ContentSHA256(data), nil
}
