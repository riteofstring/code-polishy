package gaterun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type loadedReport struct {
	root      artifactRoot
	directory artifactDirectory
	report    Report
}

func LoadReport(repositoryRoot string, expected Identity) (Report, error) {
	loaded, err := loadStoredReport(repositoryRoot, expected)
	if err != nil {
		return Report{}, err
	}
	return cloneReport(loaded.report), nil
}

func LoadReusableReceipt(repositoryRoot string, identity Identity, index int) (ReusableReceipt, error) {
	loaded, reference, outcome, err := reusableReceiptOutcome(repositoryRoot, identity, index)
	if err != nil {
		return ReusableReceipt{}, err
	}
	return loadReusableReceipt(loaded, reference, outcome)
}

func loadStoredReport(repositoryRoot string, expected Identity) (loadedReport, error) {
	if err := validateIdentity(expected); err != nil {
		return loadedReport{}, err
	}
	root, runDirectory, err := existingRunDirectory(repositoryRoot, expected)
	if err != nil {
		return loadedReport{}, err
	}
	pointer, err := loadLatestReportPointer(runDirectory)
	if err != nil {
		return loadedReport{}, err
	}
	return loadPointedReport(root, runDirectory, pointer, expected)
}

func loadPointedReport(root artifactRoot, runDirectory artifactDirectory, pointer reportPointer, expected Identity) (loadedReport, error) {
	directory, err := existingExecutionDirectory(runDirectory, pointer.ExecutionID)
	if err != nil {
		return loadedReport{}, err
	}
	file, _, err := artifactFilePath(directory, reportFilename)
	if err != nil {
		return loadedReport{}, err
	}
	data, err := readArtifact(file, maximumReportBytes, "gate run report")
	if err != nil {
		return loadedReport{}, err
	}
	var report Report
	if err := decodeStrict(data, &report, "gate run report"); err != nil {
		return loadedReport{}, err
	}
	if err := validateReport(report, expected); err != nil {
		return loadedReport{}, err
	}
	digest, err := reportDigest(report)
	if err != nil {
		return loadedReport{}, err
	}
	if report.SHA256 != digest || pointer.ReportSHA256 != report.SHA256 || report.ExecutionID != pointer.ExecutionID {
		return loadedReport{}, fmt.Errorf("%w: gate run report digest does not match its execution pointer", ErrStaleArtifact)
	}
	if err := validateStoredReportArtifacts(root, directory, report); err != nil {
		return loadedReport{}, err
	}
	return loadedReport{root: root, directory: directory, report: cloneReport(report)}, nil
}

func loadLatestReportPointer(directory artifactDirectory) (reportPointer, error) {
	file, _, err := artifactFilePath(directory, latestFilename)
	if err != nil {
		return reportPointer{}, err
	}
	data, err := readArtifact(file, maximumReceiptBytes, "gate run report pointer")
	if err != nil {
		return reportPointer{}, err
	}
	var pointer reportPointer
	if err := decodeStrict(data, &pointer, "gate run report pointer"); err != nil {
		return reportPointer{}, err
	}
	if pointer.Version != Version || !validExecutionID(pointer.ExecutionID) || !validSHA256(pointer.ReportSHA256) {
		return reportPointer{}, fmt.Errorf("%w: gate run report pointer is malformed", ErrInvalidArtifact)
	}
	return pointer, nil
}

func existingExecutionDirectory(runDirectory artifactDirectory, executionID string) (artifactDirectory, error) {
	if !validExecutionID(executionID) {
		return artifactDirectory{}, fmt.Errorf("%w: gate execution identity is invalid", ErrInvalidArtifact)
	}
	executions, err := secureRunSubdirectory(runDirectory, executionsDirectory, false)
	if err != nil {
		return artifactDirectory{}, err
	}
	return secureRunSubdirectory(executions, executionID, false)
}

func reusableReceiptOutcome(repositoryRoot string, identity Identity, index int) (loadedReport, CommandRef, CommandOutcome, error) {
	if identity.Gate != MergeGate {
		return loadedReport{}, CommandRef{}, CommandOutcome{}, fmt.Errorf("%w: only merge gate reports can provide reusable receipts", ErrIneligible)
	}
	loaded, err := loadStoredReport(repositoryRoot, identity)
	if err != nil {
		return loadedReport{}, CommandRef{}, CommandOutcome{}, err
	}
	if loaded.report.Status != RunFailed {
		return loadedReport{}, CommandRef{}, CommandOutcome{}, fmt.Errorf("%w: only failed merge gate reports can resume", ErrIneligible)
	}
	reference, err := identity.Command(index)
	if err != nil {
		return loadedReport{}, CommandRef{}, CommandOutcome{}, err
	}
	if reference.Spec.Category != OrdinaryTest {
		return loadedReport{}, CommandRef{}, CommandOutcome{}, fmt.Errorf("%w: command %q is not an ordinary test", ErrIneligible, reference.Spec.Name)
	}
	outcome, found := outcomeAt(loaded.report.Commands, index)
	if !eligibleReusableOutcome(found, outcome) {
		return loadedReport{}, CommandRef{}, CommandOutcome{}, fmt.Errorf("%w: command %q has no passed reusable receipt", ErrMissingArtifact, reference.Spec.Name)
	}
	return loaded, reference, outcome, nil
}

func eligibleReusableOutcome(found bool, outcome CommandOutcome) bool {
	return found && outcome.Status == Passed && !outcome.Reused && outcome.ReceiptPath != ""
}

func loadReusableReceipt(loaded loadedReport, reference CommandRef, outcome CommandOutcome) (ReusableReceipt, error) {
	run := &Run{
		repositoryRoot: loaded.root.path, directory: loaded.directory, identity: loaded.report.Identity,
		runSHA256: loaded.report.IdentitySHA256, executionID: loaded.report.ExecutionID,
	}
	file, display, err := run.receiptFile(reference, false)
	if err != nil {
		return ReusableReceipt{}, err
	}
	if outcome.ReceiptPath != display {
		return ReusableReceipt{}, fmt.Errorf("%w: receipt path does not match the command identity", ErrStaleArtifact)
	}
	receipt, err := readReceipt(file)
	if err != nil {
		return ReusableReceipt{}, err
	}
	if receipt.SHA256 != outcome.ReceiptSHA256 {
		return ReusableReceipt{}, fmt.Errorf("%w: receipt digest does not match its report", ErrStaleArtifact)
	}
	if err := validateReceipt(reference, run.identity.Gate, run.runSHA256, run.executionID, receipt); err != nil {
		return ReusableReceipt{}, err
	}
	return ReusableReceipt{Receipt: receipt, Path: display}, nil
}

func validateReport(report Report, expected Identity) error {
	if err := validateReportHeader(report); err != nil {
		return err
	}
	if err := validateReportIdentity(report, expected); err != nil {
		return err
	}
	return validateReportOutcomes(report, expected)
}

func validateReportHeader(report Report) error {
	if !validReportHeader(report) {
		return fmt.Errorf("%w: gate run report header is invalid", ErrInvalidArtifact)
	}
	if report.Commands == nil || report.Findings == nil || report.Notes == nil || report.TestEvidence == nil || report.TestDiagnostics == nil {
		return fmt.Errorf("%w: gate run report collections are missing", ErrInvalidArtifact)
	}
	return nil
}

func validReportHeader(report Report) bool {
	return report.Version == Version && validExecutionID(report.ExecutionID) && validRunStatus(report.Status) && !report.CompletedAt.Before(report.StartedAt)
}

func validateReportIdentity(report Report, expected Identity) error {
	if err := validateIdentity(report.Identity); err != nil {
		return fmt.Errorf("%w: gate run report identity is invalid", ErrInvalidArtifact)
	}
	expectedSHA256, err := expected.Digest()
	if err != nil {
		return err
	}
	if report.IdentitySHA256 != expectedSHA256 || !sameIdentity(report.Identity, expected) {
		return fmt.Errorf("%w: gate run report does not match the current identity", ErrStaleArtifact)
	}
	if report.SHA256 != "" && !validSHA256(report.SHA256) {
		return fmt.Errorf("%w: gate run report digest is invalid", ErrInvalidArtifact)
	}
	return nil
}

func validateReportOutcomes(report Report, expected Identity) error {
	for index, outcome := range report.Commands {
		if err := validateOrderedOutcome(report.Commands, index, outcome, expected); err != nil {
			return err
		}
	}
	if report.Status == RunPassed && !completedSuccessfully(expected.Commands, report.Commands) {
		return fmt.Errorf("%w: passed gate run outcomes are incomplete", ErrInvalidArtifact)
	}
	return validateReportTestEvidence(report)
}

func validateOrderedOutcome(outcomes []CommandOutcome, index int, outcome CommandOutcome, expected Identity) error {
	if index > 0 && outcomes[index-1].CommandIndex >= outcome.CommandIndex {
		return fmt.Errorf("%w: gate run outcomes are not ordered", ErrInvalidArtifact)
	}
	return validateOutcome(expected, outcome)
}

func validateOutcome(identity Identity, outcome CommandOutcome) error {
	reference, err := identity.Command(outcome.CommandIndex)
	if err != nil {
		return fmt.Errorf("%w: command outcome index is invalid", ErrInvalidArtifact)
	}
	if outcome.CommandSHA256 != reference.SHA256 || outcome.Name != reference.Spec.Name || outcome.Category != reference.Spec.Category || !validCommandStatus(outcome.Status) {
		return fmt.Errorf("%w: command outcome identity is invalid", ErrInvalidArtifact)
	}
	if outcome.Reused {
		return validateReusedOutcome(identity, reference, outcome)
	}
	planned, found, err := validatedPlannedAttempt(outcome.Attempts)
	if err != nil {
		return err
	}
	if !found || outcome.Status != planned.Status {
		return fmt.Errorf("%w: command outcome attempts are invalid", ErrInvalidArtifact)
	}
	return validateOutcomeReceipt(reference, outcome)
}

func validatedPlannedAttempt(attempts []Attempt) (Attempt, bool, error) {
	var planned Attempt
	found := false
	for index, attempt := range attempts {
		if err := validateAttempt(attempt, index+1); err != nil {
			return Attempt{}, false, err
		}
		if attempt.Diagnostic {
			if !found || planned.Status != Failed {
				return Attempt{}, false, fmt.Errorf("%w: diagnostic command attempt is not preceded by a failed planned attempt", ErrInvalidArtifact)
			}
			continue
		}
		planned = attempt
		found = true
	}
	return planned, found, nil
}

func validateReusedOutcome(identity Identity, reference CommandRef, outcome CommandOutcome) error {
	if identity.Gate != MergeGate || reference.Spec.Category != OrdinaryTest || outcome.Status != Passed || len(outcome.Attempts) != 0 ||
		outcome.ReceiptPath == "" || !validSHA256(outcome.ReceiptSHA256) {
		return fmt.Errorf("%w: reused command outcome is invalid", ErrInvalidArtifact)
	}
	return nil
}

func validateOutcomeReceipt(reference CommandRef, outcome CommandOutcome) error {
	requiresReceipt := reference.Spec.Category == OrdinaryTest && outcome.Status == Passed
	if requiresReceipt && (outcome.ReceiptPath == "" || !validSHA256(outcome.ReceiptSHA256)) {
		return fmt.Errorf("%w: passed ordinary test has no valid receipt", ErrInvalidArtifact)
	}
	if !requiresReceipt && (outcome.ReceiptPath != "" || outcome.ReceiptSHA256 != "") {
		return fmt.Errorf("%w: command outcome has an ineligible receipt", ErrInvalidArtifact)
	}
	return nil
}

func validateAttempt(attempt Attempt, expectedNumber int) error {
	if !validAttemptHeader(attempt, expectedNumber) {
		return fmt.Errorf("%w: command attempt is invalid", ErrInvalidArtifact)
	}
	return validateAttemptStatus(attempt)
}

func validAttemptHeader(attempt Attempt, expectedNumber int) bool {
	return attempt.Number == expectedNumber && validCommandStatus(attempt.Status) && attempt.DurationMilliseconds >= 0 &&
		attempt.ResourceWaitMilliseconds >= 0 && validArtifactDisplayPath(attempt.LogPath) && validSHA256(attempt.LogSHA256)
}

func validateAttemptStatus(attempt Attempt) error {
	if attempt.Status == Passed && (attempt.ExitStatus != 0 || attempt.FailureCategory != "") {
		return fmt.Errorf("%w: passed command attempt is invalid", ErrInvalidArtifact)
	}
	if attempt.Status == Failed && !validFailureCategory(attempt.FailureCategory) {
		return fmt.Errorf("%w: failed command attempt is invalid", ErrInvalidArtifact)
	}
	return nil
}

func validCommandStatus(status CommandStatus) bool {
	return status == Passed || status == Failed
}

func validateReportTestEvidence(report Report) error {
	known := map[string]TestEvidence{}
	for _, evidence := range report.TestEvidence {
		if err := validateTestEvidence(evidence); err != nil {
			return err
		}
		key := testEvidenceKey(evidence)
		if _, duplicate := known[key]; duplicate {
			return fmt.Errorf("%w: test evidence is duplicated", ErrInvalidArtifact)
		}
		if evidence.LogPath != "" && !evidenceMatchesAttempt(report.Commands, evidence) {
			return fmt.Errorf("%w: test evidence log reference is invalid", ErrInvalidArtifact)
		}
		known[key] = evidence
	}
	return validateTestDiagnostics(report.TestDiagnostics, known)
}

func validateTestEvidence(evidence TestEvidence) error {
	if !validTestEvidenceHeader(evidence) || !validTestEvidenceCollections(evidence) || !validTestEvidenceStatus(evidence) {
		return fmt.Errorf("%w: test evidence is invalid", ErrInvalidArtifact)
	}
	return nil
}

func validTestEvidenceHeader(evidence TestEvidence) bool {
	return validToken(evidence.Name) && validToken(evidence.Kind) && validToken(evidence.Scope) && validToken(evidence.Cost) && validToken(evidence.Target) &&
		evidence.Attempt > 0 && (evidence.LogPath == "" || validArtifactDisplayPath(evidence.LogPath)) && !strings.ContainsRune(evidence.FailureMessage, '\x00')
}

func validTestEvidenceCollections(evidence TestEvidence) bool {
	collections := [][]string{
		evidence.SuiteModules, evidence.SuitePaths, evidence.ChangedModules, evidence.ImpactedModules,
		evidence.ChangedModuleOverlap, evidence.ImpactedModuleOverlap, evidence.ChangedPathOverlap,
	}
	for _, collection := range collections {
		if collection == nil || !validStrings(collection) {
			return false
		}
	}
	return true
}

func validTestEvidenceStatus(evidence TestEvidence) bool {
	if evidence.Status == Passed {
		return evidence.FailureCategory == "" && evidence.FailureMessage == ""
	}
	return evidence.Status == Failed && validFailureCategory(evidence.FailureCategory)
}

func testEvidenceKey(evidence TestEvidence) string {
	return evidence.Name + "\x00" + evidence.Target + "\x00" + fmt.Sprint(evidence.Attempt)
}

func evidenceMatchesAttempt(outcomes []CommandOutcome, evidence TestEvidence) bool {
	for _, outcome := range outcomes {
		if outcome.Name != evidence.Name {
			continue
		}
		for _, attempt := range outcome.Attempts {
			if attempt.Number == evidence.Attempt && attempt.LogPath == evidence.LogPath && attempt.Status == evidence.Status &&
				attempt.FailureCategory == evidence.FailureCategory && attempt.Diagnostic == evidence.Diagnostic {
				return true
			}
		}
	}
	return false
}

func validateTestDiagnostics(diagnostics []TestDiagnostic, known map[string]TestEvidence) error {
	referenced := map[string]bool{}
	for _, diagnostic := range diagnostics {
		if err := validateTestDiagnostic(diagnostic, known, referenced); err != nil {
			return err
		}
	}
	for key, evidence := range known {
		if evidence.Diagnostic && !referenced[key] {
			return fmt.Errorf("%w: diagnostic test evidence has no diagnostic state", ErrInvalidArtifact)
		}
	}
	return nil
}

func validateTestDiagnostic(diagnostic TestDiagnostic, known map[string]TestEvidence, referenced map[string]bool) error {
	if !validToken(diagnostic.Suite) || !validToken(diagnostic.State) || diagnostic.CandidateRetry == nil {
		return fmt.Errorf("%w: test diagnostic is invalid", ErrInvalidArtifact)
	}
	if err := validateDiagnosticEvidence(diagnostic.Suite, diagnostic.CandidateRetry, known, referenced); err != nil {
		return err
	}
	return validateDiagnosticEvidence(diagnostic.Suite, diagnostic.BaselineReplay, known, referenced)
}

func validateDiagnosticEvidence(suite string, evidence *TestEvidence, known map[string]TestEvidence, referenced map[string]bool) error {
	if evidence == nil {
		return nil
	}
	key := testEvidenceKey(*evidence)
	stored, found := known[key]
	if !found || evidence.Name != suite || !evidence.Diagnostic || !reflect.DeepEqual(stored, *evidence) {
		return fmt.Errorf("%w: diagnostic test evidence is invalid", ErrInvalidArtifact)
	}
	referenced[key] = true
	return nil
}

func validArtifactDisplayPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return false
	}
	components := strings.Split(path, "/")
	return len(components) > 1 && validArtifactComponents(components)
}

func validateStoredReportArtifacts(root artifactRoot, directory artifactDirectory, report Report) error {
	run := &Run{
		repositoryRoot: root.path, directory: directory, identity: report.Identity,
		runSHA256: report.IdentitySHA256, executionID: report.ExecutionID,
	}
	for _, outcome := range report.Commands {
		reference, err := report.Identity.Command(outcome.CommandIndex)
		if err != nil {
			return err
		}
		if err := validateStoredOutcomeArtifacts(run, reference, outcome); err != nil {
			return err
		}
	}
	return nil
}

func validateStoredOutcomeArtifacts(run *Run, reference CommandRef, outcome CommandOutcome) error {
	if outcome.Reused {
		return validateStoredReceipt(run, reference, outcome, "")
	}
	for _, attempt := range outcome.Attempts {
		if err := validateStoredAttempt(run, reference, attempt); err != nil {
			return err
		}
	}
	if reference.Spec.Category != OrdinaryTest || outcome.Status != Passed {
		return nil
	}
	last, found, err := validatedPlannedAttempt(outcome.Attempts)
	if err != nil || !found {
		return fmt.Errorf("%w: passed ordinary test has no planned attempt", ErrInvalidArtifact)
	}
	return validateStoredReceipt(run, reference, outcome, last.LogSHA256)
}

func validateStoredAttempt(run *Run, reference CommandRef, attempt Attempt) error {
	file, display, err := run.logPath(reference.SHA256, attempt.Number, false)
	if err != nil {
		return err
	}
	if attempt.LogPath != display {
		return fmt.Errorf("%w: command log path does not match its identity", ErrStaleArtifact)
	}
	document, data, err := readCommandLog(file)
	if err != nil {
		return err
	}
	if ContentSHA256(data) != attempt.LogSHA256 || document.StdoutTruncated != attempt.StdoutTruncated || document.StderrTruncated != attempt.StderrTruncated {
		return fmt.Errorf("%w: command log does not match its report", ErrStaleArtifact)
	}
	return nil
}

func validateStoredReceipt(run *Run, reference CommandRef, outcome CommandOutcome, expectedLogSHA256 string) error {
	file, display, err := run.receiptFile(reference, false)
	if err != nil {
		return err
	}
	if outcome.ReceiptPath != display {
		return fmt.Errorf("%w: receipt path does not match its identity", ErrStaleArtifact)
	}
	receipt, err := readReceipt(file)
	if err != nil {
		return err
	}
	if receipt.SHA256 != outcome.ReceiptSHA256 {
		return fmt.Errorf("%w: receipt does not match its report", ErrStaleArtifact)
	}
	if err := validateReceipt(reference, run.identity.Gate, run.runSHA256, run.executionID, receipt); err != nil {
		return err
	}
	if expectedLogSHA256 != "" && receipt.LogSHA256 != expectedLogSHA256 {
		return fmt.Errorf("%w: receipt does not match its passed command log", ErrStaleArtifact)
	}
	return nil
}

func readReceipt(file artifactFile) (Receipt, error) {
	data, err := readArtifact(file, maximumReceiptBytes, "gate run receipt")
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	if err := decodeStrict(data, &receipt, "gate run receipt"); err != nil {
		return Receipt{}, err
	}
	digest, err := receiptDigest(receipt)
	if err != nil {
		return Receipt{}, err
	}
	if receipt.SHA256 != digest {
		return Receipt{}, fmt.Errorf("%w: gate run receipt digest does not match", ErrStaleArtifact)
	}
	return receipt, nil
}

func validateReceipt(reference CommandRef, gate GateKind, runSHA256, executionID string, receipt Receipt) error {
	if receipt.Version != Version || receipt.Gate != gate || receipt.RunSHA256 != runSHA256 || receipt.ExecutionID != executionID ||
		receipt.CommandSHA256 != reference.SHA256 || receipt.Category != OrdinaryTest || receipt.Status != Passed ||
		!validSHA256(receipt.LogSHA256) || !validSHA256(receipt.SHA256) {
		return fmt.Errorf("%w: gate run receipt does not match its eligible command", ErrStaleArtifact)
	}
	if reference.Spec.Category != OrdinaryTest {
		return fmt.Errorf("%w: receipt belongs to a non-test command", ErrIneligible)
	}
	return nil
}

func outcomeAt(outcomes []CommandOutcome, index int) (CommandOutcome, bool) {
	for _, outcome := range outcomes {
		if outcome.CommandIndex == index {
			return outcome, true
		}
	}
	return CommandOutcome{}, false
}

func sameIdentity(left, right Identity) bool {
	leftJSON, leftErr := jsonIdentity(left)
	rightJSON, rightErr := jsonIdentity(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func jsonIdentity(identity Identity) ([]byte, error) {
	data, err := json.Marshal(identity)
	if err != nil {
		return nil, operational("encode gate run identity", err)
	}
	return data, nil
}

func cloneIdentity(identity Identity) Identity {
	identity.Commands = cloneCommands(identity.Commands)
	identity.Environment = append([]EnvironmentFingerprint{}, identity.Environment...)
	identity.AmbientEnvironment = append([]EnvironmentFingerprint{}, identity.AmbientEnvironment...)
	return identity
}

func cloneCommandOutcome(outcome CommandOutcome) CommandOutcome {
	outcome.Attempts = append([]Attempt{}, outcome.Attempts...)
	return outcome
}

func cloneFindings(findings []Finding) []Finding {
	return append([]Finding{}, findings...)
}

func cloneTestEvidence(evidence []TestEvidence) []TestEvidence {
	result := make([]TestEvidence, len(evidence))
	for index, entry := range evidence {
		result[index] = cloneOneTestEvidence(entry)
	}
	return result
}

func cloneOneTestEvidence(evidence TestEvidence) TestEvidence {
	evidence.SuiteModules = cloneStrings(evidence.SuiteModules)
	evidence.SuitePaths = cloneStrings(evidence.SuitePaths)
	evidence.ChangedModules = cloneStrings(evidence.ChangedModules)
	evidence.ImpactedModules = cloneStrings(evidence.ImpactedModules)
	evidence.ChangedModuleOverlap = cloneStrings(evidence.ChangedModuleOverlap)
	evidence.ImpactedModuleOverlap = cloneStrings(evidence.ImpactedModuleOverlap)
	evidence.ChangedPathOverlap = cloneStrings(evidence.ChangedPathOverlap)
	return evidence
}

func cloneTestDiagnostics(diagnostics []TestDiagnostic) []TestDiagnostic {
	result := make([]TestDiagnostic, len(diagnostics))
	for index, diagnostic := range diagnostics {
		result[index] = diagnostic
		result[index].CandidateRetry = cloneTestEvidencePointer(diagnostic.CandidateRetry)
		result[index].BaselineReplay = cloneTestEvidencePointer(diagnostic.BaselineReplay)
	}
	return result
}

func cloneTestEvidencePointer(evidence *TestEvidence) *TestEvidence {
	if evidence == nil {
		return nil
	}
	cloned := cloneOneTestEvidence(*evidence)
	return &cloned
}

func cloneReport(report Report) Report {
	report.Identity = cloneIdentity(report.Identity)
	commands := report.Commands
	report.Commands = make([]CommandOutcome, len(commands))
	for index, outcome := range commands {
		report.Commands[index] = cloneCommandOutcome(outcome)
	}
	report.Findings = cloneFindings(report.Findings)
	report.Notes = cloneStrings(report.Notes)
	report.TestEvidence = cloneTestEvidence(report.TestEvidence)
	report.TestDiagnostics = cloneTestDiagnostics(report.TestDiagnostics)
	return report
}
