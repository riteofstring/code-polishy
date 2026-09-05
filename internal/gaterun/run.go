package gaterun

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/testreceipt"
)

type reportPointer struct {
	Version      int    `json:"version"`
	ExecutionID  string `json:"execution_id"`
	ReportSHA256 string `json:"report_sha256"`
}

func Start(options StartOptions) (*Run, error) {
	if err := validateIdentity(options.Identity); err != nil {
		return nil, err
	}
	root, runDirectory, directory, executionID, err := newRunDirectory(options.RepositoryRoot, options.Identity)
	if err != nil {
		return nil, err
	}
	report, display, err := artifactFilePath(directory, reportFilename)
	if err != nil {
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
		repositoryRoot: root.path, runDirectory: runDirectory, directory: directory, report: report, reportPath: display,
		executionID: executionID, identity: cloneIdentity(options.Identity), runSHA256: runSHA256, startedAt: startedAt,
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
	if run == nil {
		return ""
	}
	return run.reportPath
}

func (run *Run) ExecutionID() string {
	if run == nil {
		return ""
	}
	return run.executionID
}

func StoredReportPath(report Report) (string, error) {
	identitySHA256, err := report.Identity.Digest()
	if err != nil || report.IdentitySHA256 != identitySHA256 || !validExecutionID(report.ExecutionID) {
		return "", fmt.Errorf("%w: gate report identity is invalid", ErrInvalidInput)
	}
	return reportsDirectory + "/" + string(report.Identity.Gate) + "/" + identitySHA256 + "/" +
		executionsDirectory + "/" + report.ExecutionID + "/" + reportFilename, nil
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
	if !input.Diagnostic {
		entry.Status = input.Status
		entry.ReceiptPath = ""
		entry.ReceiptSHA256 = ""
		if input.Status == Passed && reference.Spec.Category == OrdinaryTest {
			receipt, path, receiptErr := run.writePassedReceipt(reference, attempt)
			if receiptErr != nil {
				return CommandOutcome{}, receiptErr
			}
			entry.ReceiptPath = path
			entry.ReceiptSHA256 = receipt.SHA256
		}
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
	if err := validateDiagnosticInput(entry, input); err != nil {
		return CommandOutcome{}, CommandRef{}, err
	}
	reference, err := run.identity.Command(index)
	if err != nil {
		return CommandOutcome{}, CommandRef{}, err
	}
	return entry, reference, nil
}

func validateDiagnosticInput(entry CommandOutcome, input AttemptInput) error {
	if !input.Diagnostic {
		return nil
	}
	planned, found := latestPlannedAttempt(entry.Attempts)
	if !found || planned.Status != Failed {
		return fmt.Errorf("%w: diagnostic attempt requires a failed planned command", ErrInvalidInput)
	}
	return nil
}

func latestPlannedAttempt(attempts []Attempt) (Attempt, bool) {
	for index := len(attempts) - 1; index >= 0; index-- {
		if !attempts[index].Diagnostic {
			return attempts[index], true
		}
	}
	return Attempt{}, false
}

func (run *Run) commandAvailable(index int) bool {
	return run != nil && !run.finalized && !run.openLogs[index]
}

func (run *Run) RecordReuse(index int, reusable ReusableReceipt) (CommandOutcome, error) {
	entry, reference, err := run.reusableEntry(index)
	if err != nil {
		return CommandOutcome{}, err
	}
	if err := run.validateReusableReceipt(reference, reusable); err != nil {
		return CommandOutcome{}, err
	}
	receipt, path, err := run.writeReusedReceipt(reference, reusable)
	if err != nil {
		return CommandOutcome{}, err
	}
	entry.Status = Passed
	entry.Reused = true
	entry.ReceiptPath = path
	entry.ReceiptSHA256 = receipt.SHA256
	if reusable.Receipt.ExternalSourcePath != "" {
		entry.ReuseSourcePath = reusable.Receipt.ExternalSourcePath
		entry.ReuseSourceSHA256 = reusable.Receipt.ExternalSourceSHA256
		entry.PriorDurationMilliseconds = reusable.Receipt.ExternalSourceDurationMilliseconds
	}
	run.commands[index] = entry
	return cloneCommandOutcome(entry), nil
}

func (run *Run) RecordSuiteReuse(index int, source SuiteReuse) (CommandOutcome, error) {
	entry, reference, err := run.suiteReusableEntry(index)
	if err != nil {
		return CommandOutcome{}, err
	}
	if reference.Spec.SuiteIdentitySHA256 == "" || source.IdentitySHA256 != reference.Spec.SuiteIdentitySHA256 ||
		source.DurationMillis < 0 || !validSHA256(source.ReceiptSHA256) {
		return CommandOutcome{}, fmt.Errorf("%w: suite receipt reuse input is invalid", ErrInvalidInput)
	}
	reusable, err := testreceipt.LoadDigest(run.repositoryRoot, source.IdentitySHA256)
	if err != nil {
		return CommandOutcome{}, err
	}
	if reusable.Path != source.ReceiptPath || reusable.Receipt.SHA256 != source.ReceiptSHA256 ||
		reusable.Receipt.DurationMillis != source.DurationMillis {
		return CommandOutcome{}, fmt.Errorf("%w: suite receipt changed after validation", ErrStaleArtifact)
	}
	receipt, path, err := run.writeSuiteReusedReceipt(reference, source)
	if err != nil {
		return CommandOutcome{}, err
	}
	entry.Status = Passed
	entry.Reused = true
	entry.ReceiptPath = path
	entry.ReceiptSHA256 = receipt.SHA256
	entry.ReuseSourcePath = source.ReceiptPath
	entry.ReuseSourceSHA256 = source.ReceiptSHA256
	entry.PriorDurationMilliseconds = source.DurationMillis
	run.commands[index] = entry
	return cloneCommandOutcome(entry), nil
}

func (run *Run) reusableEntry(index int) (CommandOutcome, CommandRef, error) {
	entry, reference, err := run.suiteReusableEntry(index)
	if err != nil {
		return CommandOutcome{}, CommandRef{}, err
	}
	if run.identity.Gate != MergeGate {
		return CommandOutcome{}, CommandRef{}, fmt.Errorf("%w: only merge gate runs can reuse receipts", ErrIneligible)
	}
	return entry, reference, nil
}

func (run *Run) suiteReusableEntry(index int) (CommandOutcome, CommandRef, error) {
	if err := run.reuseAvailabilityError(index); err != nil {
		return CommandOutcome{}, CommandRef{}, err
	}
	entry, err := run.commandOutcome(index)
	if err != nil {
		return CommandOutcome{}, CommandRef{}, err
	}
	if len(entry.Attempts) != 0 || entry.Reused {
		return CommandOutcome{}, CommandRef{}, fmt.Errorf("%w: command %d already has an outcome", ErrInvalidInput, index)
	}
	reference, err := run.identity.Command(index)
	if err != nil {
		return CommandOutcome{}, CommandRef{}, err
	}
	if reference.Spec.Category != OrdinaryTest {
		return CommandOutcome{}, CommandRef{}, fmt.Errorf("%w: command %q is not an ordinary test", ErrIneligible, reference.Spec.Name)
	}
	return entry, reference, nil
}

func (run *Run) reuseAvailabilityError(index int) error {
	if run == nil {
		return fmt.Errorf("%w: gate run command is unavailable", ErrInvalidInput)
	}
	if run.finalized || run.openLogs[index] {
		return fmt.Errorf("%w: gate run command is unavailable", ErrInvalidInput)
	}
	return nil
}

func (run *Run) Finalize(options FinalizeOptions) (Report, error) {
	prepared, err := run.PrepareFinalization(options)
	if err != nil {
		return Report{}, err
	}
	report, err := prepared.Commit()
	if err != nil {
		return Report{}, errors.Join(err, prepared.Discard())
	}
	return report, nil
}

func (run *Run) PrepareFinalization(options FinalizeOptions) (*PreparedFinalization, error) {
	report, err := run.finalReport(options)
	if err != nil {
		return nil, err
	}
	prepared := &PreparedFinalization{run: run, report: report}
	run.preparation = prepared
	return prepared, nil
}

func (prepared *PreparedFinalization) Evidence() ExecutionEvidence {
	if prepared == nil {
		return ExecutionEvidence{}
	}
	return ExecutionEvidence{
		Gate: prepared.report.Identity.Gate, IdentitySHA256: prepared.report.IdentitySHA256,
		ExecutionID: prepared.report.ExecutionID, ReportSHA256: prepared.report.SHA256,
	}
}

func (prepared *PreparedFinalization) Commit() (Report, error) {
	if err := prepared.active(); err != nil {
		return Report{}, err
	}
	data, err := marshalArtifact(prepared.report, "encode gate run report")
	if err != nil {
		return Report{}, err
	}
	if len(data) > maximumReportBytes {
		return Report{}, fmt.Errorf("%w: gate run report exceeds the %d byte limit", ErrInvalidArtifact, maximumReportBytes)
	}
	if err := writeArtifactAtomic(prepared.run.report, data); err != nil {
		return Report{}, err
	}
	if err := prepared.run.writeLatestReportPointer(prepared.report); err != nil {
		return Report{}, err
	}
	prepared.complete()
	return cloneReport(prepared.report), nil
}

func (prepared *PreparedFinalization) Abort() error {
	if err := prepared.active(); err != nil {
		return err
	}
	prepared.release()
	return nil
}

func (prepared *PreparedFinalization) Discard() error {
	if err := prepared.active(); err != nil {
		return err
	}
	prepared.release()
	return errors.Join(prepared.removeCurrentPointer(), prepared.removeCurrentReport())
}

func (prepared *PreparedFinalization) active() error {
	if prepared == nil || prepared.run == nil || prepared.completed || prepared.run.preparation != prepared {
		return fmt.Errorf("%w: gate run finalization is unavailable", ErrInvalidInput)
	}
	return nil
}

func (prepared *PreparedFinalization) complete() {
	prepared.run.preparation = nil
	prepared.run.finalized = true
	prepared.completed = true
}

func (prepared *PreparedFinalization) release() {
	prepared.run.preparation = nil
	prepared.completed = true
}

func (prepared *PreparedFinalization) removeCurrentPointer() error {
	file, _, err := artifactFilePath(prepared.run.runDirectory, latestFilename)
	if err != nil {
		return err
	}
	pointer, err := loadLatestReportPointer(prepared.run.runDirectory)
	if errors.Is(err, ErrMissingArtifact) {
		return nil
	}
	if err != nil {
		return err
	}
	if pointer.ExecutionID != prepared.report.ExecutionID || pointer.ReportSHA256 != prepared.report.SHA256 {
		return nil
	}
	return removeArtifact(file)
}

func (prepared *PreparedFinalization) removeCurrentReport() error {
	stored, err := readStoredReport(prepared.run.directory, prepared.run.identity, prepared.run.executionID)
	if errors.Is(err, ErrMissingArtifact) {
		return nil
	}
	if err != nil {
		return err
	}
	if stored.SHA256 != prepared.report.SHA256 {
		return fmt.Errorf("%w: gate run report changed during finalization cleanup", ErrStaleArtifact)
	}
	return removeArtifact(prepared.run.report)
}

func (run *Run) finalReport(options FinalizeOptions) (Report, error) {
	commands, completedAt, err := run.finalizationState(options)
	if err != nil {
		return Report{}, err
	}
	satisfactions, err := deriveSuiteSatisfactions(options.SuiteSatisfactions, commands)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		SourceDependencyGraph: sourcegraph.Clone(options.SourceDependencyGraph),
		Version:               Version, Identity: cloneIdentity(run.identity), IdentitySHA256: run.runSHA256, ExecutionID: run.executionID,
		Status: options.Status, StartedAt: run.startedAt, CompletedAt: completedAt,
		Commands: commands, Findings: cloneFindings(options.Findings), Notes: cloneStrings(options.Notes),
		Suppressed: cloneSuppressedOutcomes(options.Suppressed), Assessed: cloneVulnerabilityOutcomes(options.Assessed),
		ReleaseAges:  cloneReleaseAgeOutcomes(options.ReleaseAges),
		TestEvidence: cloneTestEvidence(options.TestEvidence), TestDiagnostics: cloneTestDiagnostics(options.TestDiagnostics),
		SuiteSatisfactions: satisfactions,
		BehaviorReview:     cloneBehaviorReview(options.BehaviorReview),
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

func deriveSuiteSatisfactions(inputs []SuiteSatisfactionInput, commands []CommandOutcome) ([]SuiteSatisfaction, error) {
	result := make([]SuiteSatisfaction, 0, len(inputs))
	for _, input := range inputs {
		outcome, found := outcomeNamed(commands, input.ExecutedBy)
		if !found || outcome.Category != OrdinaryTest {
			return nil, fmt.Errorf("%w: suite satisfaction has no ordinary test representative", ErrInvalidInput)
		}
		if outcome.Status != Passed {
			continue
		}
		result = append(result, SuiteSatisfaction{
			Suite: input.Suite, ExecutedBy: input.ExecutedBy, Reason: input.Reason,
			ReceiptPath: outcome.ReceiptPath, ReceiptSHA256: outcome.ReceiptSHA256,
		})
	}
	return result, nil
}

func outcomeNamed(commands []CommandOutcome, name string) (CommandOutcome, bool) {
	for _, command := range commands {
		if command.Name == name {
			return command, true
		}
	}
	return CommandOutcome{}, false
}

func (run *Run) writeLatestReportPointer(report Report) error {
	file, _, err := artifactFilePath(run.runDirectory, latestFilename)
	if err != nil {
		return err
	}
	pointer := reportPointer{Version: Version, ExecutionID: report.ExecutionID, ReportSHA256: report.SHA256}
	data, err := marshalArtifact(pointer, "encode gate run report pointer")
	if err != nil {
		return err
	}
	return writeArtifactAtomic(file, data)
}

func (run *Run) finalizationState(options FinalizeOptions) ([]CommandOutcome, time.Time, error) {
	if run == nil || run.finalized || run.preparation != nil || len(run.openLogs) != 0 {
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
	file, display, err := run.logPath(reference.SHA256, number, false)
	if err != nil {
		return Attempt{}, err
	}
	if result.Path != display || !validSHA256(result.SHA256) {
		return Attempt{}, fmt.Errorf("%w: command log does not match its planned path", ErrInvalidArtifact)
	}
	document, data, err := readCommandLog(file)
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
		Diagnostic: input.Diagnostic,
	}, nil
}

func sameLogResult(document commandLogDocument, result LogResult) bool {
	stdout := capturedStream{head: document.Stdout, tail: document.StdoutTail, total: document.StdoutBytes, truncated: document.StdoutTruncated}
	stderr := capturedStream{head: document.Stderr, tail: document.StderrTail, total: document.StderrBytes, truncated: document.StderrTruncated}
	return document.StreamLimit == result.StreamLimit && document.StdoutTruncated == result.StdoutTruncated && document.StderrTruncated == result.StderrTruncated &&
		document.StdoutBytes == result.StdoutBytes && document.StderrBytes == result.StderrBytes && string(stdout.rendered()) == string(result.Stdout) &&
		string(stderr.rendered()) == string(result.Stderr) && string(document.StdoutTail) == string(result.StdoutTail) && string(document.StderrTail) == string(result.StderrTail)
}

func (run *Run) writePassedReceipt(reference CommandRef, attempt Attempt) (Receipt, string, error) {
	receipt := Receipt{
		Version: Version, Gate: run.identity.Gate, RunSHA256: run.runSHA256, ExecutionID: run.executionID, CommandSHA256: reference.SHA256,
		Category: OrdinaryTest, Status: Passed, LogSHA256: attempt.LogSHA256,
	}
	return run.writeReceipt(reference, receipt)
}

func (run *Run) writeReusedReceipt(reference CommandRef, reusable ReusableReceipt) (Receipt, string, error) {
	if reusable.Receipt.ExternalSourcePath != "" {
		receipt := Receipt{
			Version: Version, Gate: run.identity.Gate, RunSHA256: run.runSHA256, ExecutionID: run.executionID, CommandSHA256: reference.SHA256,
			Category: OrdinaryTest, Status: Passed, ExternalSourcePath: reusable.Receipt.ExternalSourcePath,
			ExternalSourceSHA256:               reusable.Receipt.ExternalSourceSHA256,
			ExternalSourceDurationMilliseconds: reusable.Receipt.ExternalSourceDurationMilliseconds,
		}
		return run.writeReceipt(reference, receipt)
	}
	sourceExecutionID, sourceReceiptSHA256 := sourceReceiptIdentity(reusable.Receipt)
	receipt := Receipt{
		Version: Version, Gate: run.identity.Gate, RunSHA256: run.runSHA256, ExecutionID: run.executionID, CommandSHA256: reference.SHA256,
		Category: OrdinaryTest, Status: Passed, LogSHA256: reusable.Receipt.LogSHA256,
		SourceExecutionID: sourceExecutionID, SourceReceiptSHA256: sourceReceiptSHA256,
	}
	return run.writeReceipt(reference, receipt)
}

func (run *Run) writeSuiteReusedReceipt(reference CommandRef, source SuiteReuse) (Receipt, string, error) {
	receipt := Receipt{
		Version: Version, Gate: run.identity.Gate, RunSHA256: run.runSHA256, ExecutionID: run.executionID,
		CommandSHA256: reference.SHA256, Category: OrdinaryTest, Status: Passed,
		ExternalSourcePath: source.ReceiptPath, ExternalSourceSHA256: source.ReceiptSHA256,
		ExternalSourceDurationMilliseconds: source.DurationMillis,
	}
	return run.writeReceipt(reference, receipt)
}

func sourceReceiptIdentity(receipt Receipt) (string, string) {
	if receipt.SourceExecutionID != "" {
		return receipt.SourceExecutionID, receipt.SourceReceiptSHA256
	}
	return receipt.ExecutionID, receipt.SHA256
}

func (run *Run) writeReceipt(reference CommandRef, receipt Receipt) (Receipt, string, error) {
	directory, err := secureRunSubdirectory(run.directory, receiptsDirectory, true)
	if err != nil {
		return Receipt{}, "", err
	}
	file, display, err := artifactFilePath(directory, reference.SHA256+".json")
	if err != nil {
		return Receipt{}, "", err
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
	if err := writeArtifactAtomic(file, data); err != nil {
		return Receipt{}, "", err
	}
	return receipt, display, nil
}

func (run *Run) receiptFile(reference CommandRef, create bool) (artifactFile, string, error) {
	directory, err := secureRunSubdirectory(run.directory, receiptsDirectory, create)
	if err != nil {
		return artifactFile{}, "", err
	}
	return artifactFilePath(directory, reference.SHA256+".json")
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
