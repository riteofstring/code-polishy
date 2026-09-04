package gaterun

import (
	"bytes"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"reflect"
	"strings"

	"github.com/riteofstring/code-polishy/internal/testreceipt"
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

func ValidateExecutionEvidence(evidence ExecutionEvidence) error {
	if !validGate(evidence.Gate) || !validSHA256(evidence.IdentitySHA256) || !validExecutionID(evidence.ExecutionID) || !validSHA256(evidence.ReportSHA256) {
		return fmt.Errorf("%w: gate execution evidence is invalid", ErrInvalidInput)
	}
	return nil
}

func ValidatePassedExecution(repositoryRoot string, evidence ExecutionEvidence) error {
	_, err := LoadPassedExecution(repositoryRoot, evidence)
	return err
}

func LoadPassedExecution(repositoryRoot string, evidence ExecutionEvidence) (Report, error) {
	if err := ValidateExecutionEvidence(evidence); err != nil {
		return Report{}, err
	}
	root, runDirectory, directory, err := locateExecutionEvidence(repositoryRoot, evidence)
	if err != nil {
		return Report{}, err
	}
	report, err := readExecutionEvidenceReport(directory, evidence.ExecutionID)
	if err != nil {
		return Report{}, err
	}
	if !matchesPassedExecution(report, evidence) {
		return Report{}, fmt.Errorf("%w: gate execution does not match its passed evidence", ErrStaleArtifact)
	}
	if err := validateStoredReportArtifacts(root, runDirectory, directory, report); err != nil {
		return Report{}, err
	}
	return cloneReport(report), nil
}

func locateExecutionEvidence(repositoryRoot string, evidence ExecutionEvidence) (artifactRoot, artifactDirectory, artifactDirectory, error) {
	root, runDirectory, err := managedRunDirectory(repositoryRoot, evidence.Gate, evidence.IdentitySHA256, false)
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, artifactDirectory{}, err
	}
	pointer, err := loadLatestReportPointer(runDirectory)
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, artifactDirectory{}, err
	}
	if pointer.ExecutionID != evidence.ExecutionID || pointer.ReportSHA256 != evidence.ReportSHA256 {
		return artifactRoot{}, artifactDirectory{}, artifactDirectory{}, fmt.Errorf("%w: gate execution is not the latest report", ErrStaleArtifact)
	}
	directory, err := existingExecutionDirectory(runDirectory, evidence.ExecutionID)
	if err != nil {
		return artifactRoot{}, artifactDirectory{}, artifactDirectory{}, err
	}
	return root, runDirectory, directory, nil
}

func matchesPassedExecution(report Report, evidence ExecutionEvidence) bool {
	return report.Identity.Gate == evidence.Gate && report.IdentitySHA256 == evidence.IdentitySHA256 &&
		report.SHA256 == evidence.ReportSHA256 && report.Status == RunPassed
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
	report, err := readStoredReport(directory, expected, pointer.ExecutionID)
	if err != nil {
		return loadedReport{}, err
	}
	if pointer.ReportSHA256 != report.SHA256 {
		return loadedReport{}, fmt.Errorf("%w: gate run report digest does not match its execution pointer", ErrStaleArtifact)
	}
	if err := validateStoredReportArtifacts(root, runDirectory, directory, report); err != nil {
		return loadedReport{}, err
	}
	return loadedReport{root: root, directory: directory, report: cloneReport(report)}, nil
}

func readStoredReport(directory artifactDirectory, expected Identity, executionID string) (Report, error) {
	report, err := readExecutionEvidenceReport(directory, executionID)
	if err != nil {
		return Report{}, err
	}
	if err := validateReport(report, expected); err != nil {
		return Report{}, err
	}
	return report, nil
}

func readExecutionEvidenceReport(directory artifactDirectory, executionID string) (Report, error) {
	file, _, err := artifactFilePath(directory, reportFilename)
	if err != nil {
		return Report{}, err
	}
	data, err := readArtifact(file, maximumReportBytes, "gate run report")
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := decodeStrict(data, &report, "gate run report"); err != nil {
		return Report{}, err
	}
	if err := validateReport(report, report.Identity); err != nil {
		return Report{}, err
	}
	digest, err := reportDigest(report)
	if err != nil {
		return Report{}, err
	}
	if report.SHA256 != digest || report.ExecutionID != executionID {
		return Report{}, fmt.Errorf("%w: gate run report digest does not match its execution", ErrStaleArtifact)
	}
	return report, nil
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
	return found && outcome.Status == Passed && outcome.ReceiptPath != ""
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
	if receiptProvenanceMismatch(outcome, receipt) {
		return ReusableReceipt{}, fmt.Errorf("%w: receipt provenance does not match its command outcome", ErrStaleArtifact)
	}
	return ReusableReceipt{Receipt: receipt, Path: display}, nil
}

func receiptProvenanceMismatch(outcome CommandOutcome, receipt Receipt) bool {
	return outcome.Reused != receiptHasSource(receipt)
}

func validateReport(report Report, expected Identity) error {
	if err := validateReportHeader(report); err != nil {
		return err
	}
	if err := validateReportIdentity(report, expected); err != nil {
		return err
	}
	if err := validateReportBehaviorReview(expected.BehaviorReview, report.BehaviorReview); err != nil {
		return err
	}
	return validateReportOutcomes(report, expected)
}

func validateReportHeader(report Report) error {
	if !validReportHeader(report) {
		return fmt.Errorf("%w: gate run report header is invalid", ErrInvalidArtifact)
	}
	if report.Commands == nil || report.Findings == nil || report.Notes == nil || report.TestEvidence == nil || report.TestDiagnostics == nil || report.SuiteSatisfactions == nil {
		return fmt.Errorf("%w: gate run report collections are missing", ErrInvalidArtifact)
	}
	return validateFindingLocations(report.Findings)
}

func validateFindingLocations(findings []Finding) error {
	for _, finding := range findings {
		if finding.Line < 0 || finding.Column < 0 || finding.Column > 0 && finding.Line == 0 {
			return fmt.Errorf("%w: gate run finding location is invalid", ErrInvalidArtifact)
		}
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
	if err := validateReportTestEvidence(report); err != nil {
		return err
	}
	return validateSuiteSatisfactions(report.SuiteSatisfactions, report.Commands)
}

func validateSuiteSatisfactions(satisfactions []SuiteSatisfaction, outcomes []CommandOutcome) error {
	seen := map[string]bool{}
	for _, satisfaction := range satisfactions {
		outcome, found := outcomeNamed(outcomes, satisfaction.ExecutedBy)
		if !validSuiteSatisfactionHeader(satisfaction, seen) || !validSuiteSatisfactionOutcome(satisfaction, outcome, found) {
			return fmt.Errorf("%w: suite satisfaction is invalid", ErrInvalidArtifact)
		}
		seen[satisfaction.Suite] = true
	}
	return nil
}

func validSuiteSatisfactionHeader(satisfaction SuiteSatisfaction, seen map[string]bool) bool {
	return validToken(satisfaction.Suite) && validToken(satisfaction.ExecutedBy) && satisfaction.Suite != satisfaction.ExecutedBy &&
		(satisfaction.Reason == "covered" || satisfaction.Reason == "duplicate-command") && !seen[satisfaction.Suite]
}

func validSuiteSatisfactionOutcome(satisfaction SuiteSatisfaction, outcome CommandOutcome, found bool) bool {
	return found && outcome.Category == OrdinaryTest && outcome.Status == Passed && satisfaction.ReceiptPath == outcome.ReceiptPath &&
		satisfaction.ReceiptSHA256 == outcome.ReceiptSHA256 && validArtifactDisplayPath(satisfaction.ReceiptPath) &&
		validSHA256(satisfaction.ReceiptSHA256)
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
		return validateReusedOutcome(reference, outcome)
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

func validateReusedOutcome(reference CommandRef, outcome CommandOutcome) error {
	if !validReusedOutcome(reference, outcome) {
		return fmt.Errorf("%w: reused command outcome is invalid", ErrInvalidArtifact)
	}
	external := outcome.ReuseSourcePath != "" || outcome.ReuseSourceSHA256 != "" || outcome.PriorDurationMilliseconds != 0
	if external && !validReuseSource(reference, outcome) {
		return fmt.Errorf("%w: reused command source is invalid", ErrInvalidArtifact)
	}
	return nil
}

func validReusedOutcome(reference CommandRef, outcome CommandOutcome) bool {
	return reference.Spec.Category == OrdinaryTest && outcome.Status == Passed && len(outcome.Attempts) == 0 &&
		outcome.ReceiptPath != "" && validSHA256(outcome.ReceiptSHA256)
}

func validReuseSource(reference CommandRef, outcome CommandOutcome) bool {
	return reference.Spec.SuiteIdentitySHA256 != "" && validArtifactDisplayPath(outcome.ReuseSourcePath) &&
		validSHA256(outcome.ReuseSourceSHA256) && outcome.PriorDurationMilliseconds >= 0
}

func validateOutcomeReceipt(reference CommandRef, outcome CommandOutcome) error {
	requiresReceipt := reference.Spec.Category == OrdinaryTest && outcome.Status == Passed
	if requiresReceipt && (outcome.ReceiptPath == "" || !validSHA256(outcome.ReceiptSHA256)) {
		return fmt.Errorf("%w: passed ordinary test has no valid receipt", ErrInvalidArtifact)
	}
	if !requiresReceipt && (outcome.ReceiptPath != "" || outcome.ReceiptSHA256 != "") {
		return fmt.Errorf("%w: command outcome has an ineligible receipt", ErrInvalidArtifact)
	}
	if outcome.ReuseSourcePath != "" || outcome.ReuseSourceSHA256 != "" || outcome.PriorDurationMilliseconds != 0 {
		return fmt.Errorf("%w: executed command outcome has reuse provenance", ErrInvalidArtifact)
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
	if evidence.Reused {
		if !validArtifactDisplayPath(evidence.ReceiptSourcePath) || !validSHA256(evidence.ReceiptSourceSHA256) || evidence.LogPath != "" || evidence.Diagnostic {
			return fmt.Errorf("%w: reused test evidence is invalid", ErrInvalidArtifact)
		}
	} else if evidence.ReceiptSourcePath != "" || evidence.ReceiptSourceSHA256 != "" {
		return fmt.Errorf("%w: executed test evidence has reuse provenance", ErrInvalidArtifact)
	}
	return validateTestArtifacts(evidence.Artifacts)
}

func validateTestArtifacts(artifacts []TestArtifact) error {
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		if !validToken(artifact.Suite) || !validArtifactRelativePath(artifact.Path) ||
			artifact.Type != "junit" && artifact.Type != "cobertura" || artifact.Size < 0 || artifact.Size > 32<<20 ||
			!validSHA256(artifact.SHA256) || seen[artifact.Path] {
			return fmt.Errorf("%w: test artifact evidence is invalid", ErrInvalidArtifact)
		}
		seen[artifact.Path] = true
	}
	return nil
}

func validArtifactRelativePath(value string) bool {
	return value != "" && value != "." && !strings.HasPrefix(value, "/") && !strings.Contains(value, "\\") &&
		pathpkg.Clean(value) == value
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

func validateStoredReportArtifacts(root artifactRoot, runDirectory, directory artifactDirectory, report Report) error {
	run := &Run{
		repositoryRoot: root.path, runDirectory: runDirectory, directory: directory, identity: report.Identity,
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
		return validateStoredReusedReceipt(run, reference, outcome)
	}
	return validateStoredExecutedOutcome(run, reference, outcome)
}

func validateStoredExecutedOutcome(run *Run, reference CommandRef, outcome CommandOutcome) error {
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
	receipt, err := validateStoredReceipt(run, reference, outcome, last.LogSHA256)
	if err != nil {
		return err
	}
	if receiptHasSource(receipt) {
		return fmt.Errorf("%w: executed command receipt has unexpected provenance", ErrStaleArtifact)
	}
	return nil
}

func validateStoredReusedReceipt(run *Run, reference CommandRef, outcome CommandOutcome) error {
	receipt, err := validateStoredReceipt(run, reference, outcome, "")
	if err != nil {
		return err
	}
	if !receiptHasSource(receipt) {
		return fmt.Errorf("%w: reused command receipt has no provenance", ErrStaleArtifact)
	}
	if receipt.ExternalSourcePath != "" {
		return validateExternalSuiteReceipt(run, reference, outcome, receipt)
	}
	return validateReusedReceiptProvenance(run, reference, receipt)
}

func validateExternalSuiteReceipt(run *Run, reference CommandRef, outcome CommandOutcome, receipt Receipt) error {
	if reference.Spec.SuiteIdentitySHA256 == "" || outcome.ReuseSourcePath != receipt.ExternalSourcePath ||
		outcome.ReuseSourceSHA256 != receipt.ExternalSourceSHA256 ||
		outcome.PriorDurationMilliseconds != receipt.ExternalSourceDurationMilliseconds {
		return fmt.Errorf("%w: external suite receipt provenance is stale", ErrStaleArtifact)
	}
	reusable, err := testreceipt.LoadDigest(run.repositoryRoot, reference.Spec.SuiteIdentitySHA256)
	if err != nil {
		return err
	}
	if reusable.Path != receipt.ExternalSourcePath || reusable.Receipt.SHA256 != receipt.ExternalSourceSHA256 ||
		reusable.Receipt.DurationMillis != receipt.ExternalSourceDurationMilliseconds {
		return fmt.Errorf("%w: external suite receipt changed", ErrStaleArtifact)
	}
	return nil
}

func validateReusedReceiptProvenance(run *Run, reference CommandRef, receipt Receipt) error {
	sourceDirectory, sourceReport, err := reusedReceiptSource(run, receipt)
	if err != nil {
		return err
	}
	sourceOutcome, found := outcomeAt(sourceReport.Commands, reference.Index)
	if !validReusedSourceOutcome(sourceReport, found, sourceOutcome, receipt) {
		return fmt.Errorf("%w: reused command receipt provenance is stale", ErrStaleArtifact)
	}
	sourceRun := sourceExecutionRun(run, sourceDirectory, sourceReport)
	if err := validateStoredExecutedOutcome(sourceRun, reference, sourceOutcome); err != nil {
		return err
	}
	return validateReusedSourceLog(sourceOutcome, receipt)
}

func reusedReceiptSource(run *Run, receipt Receipt) (artifactDirectory, Report, error) {
	if receipt.SourceExecutionID == run.executionID {
		return artifactDirectory{}, Report{}, fmt.Errorf("%w: reused command receipt points to its current execution", ErrStaleArtifact)
	}
	directory, err := existingExecutionDirectory(run.runDirectory, receipt.SourceExecutionID)
	if err != nil {
		return artifactDirectory{}, Report{}, err
	}
	report, err := readStoredReport(directory, run.identity, receipt.SourceExecutionID)
	if err != nil {
		return artifactDirectory{}, Report{}, err
	}
	return directory, report, nil
}

func validReusedSourceOutcome(report Report, found bool, outcome CommandOutcome, receipt Receipt) bool {
	return report.Status == RunFailed && found && !outcome.Reused && outcome.Status == Passed && outcome.ReceiptSHA256 == receipt.SourceReceiptSHA256
}

func sourceExecutionRun(run *Run, directory artifactDirectory, report Report) *Run {
	return &Run{
		repositoryRoot: run.repositoryRoot, runDirectory: run.runDirectory, directory: directory, identity: report.Identity,
		runSHA256: report.IdentitySHA256, executionID: report.ExecutionID,
	}
}

func validateReusedSourceLog(outcome CommandOutcome, receipt Receipt) error {
	attempt, found, err := validatedPlannedAttempt(outcome.Attempts)
	if err != nil || !found || attempt.LogSHA256 != receipt.LogSHA256 {
		return fmt.Errorf("%w: reused command receipt does not match its source log", ErrStaleArtifact)
	}
	return nil
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

func validateStoredReceipt(run *Run, reference CommandRef, outcome CommandOutcome, expectedLogSHA256 string) (Receipt, error) {
	file, display, err := run.receiptFile(reference, false)
	if err != nil {
		return Receipt{}, err
	}
	if outcome.ReceiptPath != display {
		return Receipt{}, fmt.Errorf("%w: receipt path does not match its identity", ErrStaleArtifact)
	}
	receipt, err := readReceipt(file)
	if err != nil {
		return Receipt{}, err
	}
	if receipt.SHA256 != outcome.ReceiptSHA256 {
		return Receipt{}, fmt.Errorf("%w: receipt does not match its report", ErrStaleArtifact)
	}
	if err := validateReceipt(reference, run.identity.Gate, run.runSHA256, run.executionID, receipt); err != nil {
		return Receipt{}, err
	}
	if expectedLogSHA256 != "" && receipt.LogSHA256 != expectedLogSHA256 {
		return Receipt{}, fmt.Errorf("%w: receipt does not match its passed command log", ErrStaleArtifact)
	}
	return receipt, nil
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
	if !receiptMatchesRun(receipt, gate, runSHA256, executionID) || !receiptMatchesCommand(receipt, reference) {
		return fmt.Errorf("%w: gate run receipt does not match its eligible command", ErrStaleArtifact)
	}
	if reference.Spec.Category != OrdinaryTest {
		return fmt.Errorf("%w: receipt belongs to a non-test command", ErrIneligible)
	}
	if !validReceiptSource(receipt) {
		return fmt.Errorf("%w: gate run receipt provenance is invalid", ErrStaleArtifact)
	}
	return nil
}

func receiptMatchesRun(receipt Receipt, gate GateKind, runSHA256, executionID string) bool {
	return receipt.Version == Version && receipt.Gate == gate && receipt.RunSHA256 == runSHA256 && receipt.ExecutionID == executionID
}

func receiptMatchesCommand(receipt Receipt, reference CommandRef) bool {
	validEvidence := validSHA256(receipt.LogSHA256)
	if receipt.ExternalSourcePath != "" {
		validEvidence = receipt.LogSHA256 == "" && validArtifactDisplayPath(receipt.ExternalSourcePath) &&
			validSHA256(receipt.ExternalSourceSHA256) && receipt.ExternalSourceDurationMilliseconds >= 0
	}
	return receipt.CommandSHA256 == reference.SHA256 && receipt.Category == OrdinaryTest && receipt.Status == Passed &&
		validEvidence && validSHA256(receipt.SHA256)
}

func receiptHasSource(receipt Receipt) bool {
	return receipt.SourceExecutionID != "" || receipt.SourceReceiptSHA256 != "" || receipt.ExternalSourcePath != "" || receipt.ExternalSourceSHA256 != ""
}

func validReceiptSource(receipt Receipt) bool {
	if !receiptHasSource(receipt) {
		return receipt.ExternalSourceDurationMilliseconds == 0
	}
	internal := receipt.SourceExecutionID != "" || receipt.SourceReceiptSHA256 != ""
	external := receipt.ExternalSourcePath != "" || receipt.ExternalSourceSHA256 != ""
	if internal == external {
		return false
	}
	if internal {
		return validExecutionID(receipt.SourceExecutionID) && validSHA256(receipt.SourceReceiptSHA256) && receipt.ExternalSourceDurationMilliseconds == 0
	}
	return validArtifactDisplayPath(receipt.ExternalSourcePath) && validSHA256(receipt.ExternalSourceSHA256) &&
		receipt.ExternalSourceDurationMilliseconds >= 0
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
	identity.BehaviorReview = cloneBehaviorReview(identity.BehaviorReview)
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
	evidence.Artifacts = append([]TestArtifact{}, evidence.Artifacts...)
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
	report.SuiteSatisfactions = append([]SuiteSatisfaction{}, report.SuiteSatisfactions...)
	report.BehaviorReview = cloneBehaviorReview(report.BehaviorReview)
	return report
}
