package gaterun

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func LoadReport(repositoryRoot string, expected Identity) (Report, error) {
	if err := validateIdentity(expected); err != nil {
		return Report{}, err
	}
	root, directory, err := existingRunDirectory(repositoryRoot, expected)
	if err != nil {
		return Report{}, err
	}
	path, _, err := artifactFilePath(root, directory, reportFilename)
	if err != nil {
		return Report{}, err
	}
	data, err := readArtifact(path, maximumReportBytes, "gate run report")
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := decodeStrict(data, &report, "gate run report"); err != nil {
		return Report{}, err
	}
	if err := validateReport(report, expected); err != nil {
		return Report{}, err
	}
	digest, err := reportDigest(report)
	if err != nil {
		return Report{}, err
	}
	if report.SHA256 != digest {
		return Report{}, fmt.Errorf("%w: gate run report digest does not match", ErrStaleArtifact)
	}
	if err := validateStoredReportArtifacts(root, directory, report); err != nil {
		return Report{}, err
	}
	return cloneReport(report), nil
}

func LoadReusableReceipt(repositoryRoot string, identity Identity, index int) (ReusableReceipt, error) {
	reference, outcome, err := reusableReceiptOutcome(repositoryRoot, identity, index)
	if err != nil {
		return ReusableReceipt{}, err
	}
	return loadReusableReceipt(repositoryRoot, identity, reference, outcome)
}

func reusableReceiptOutcome(repositoryRoot string, identity Identity, index int) (CommandRef, CommandOutcome, error) {
	if identity.Gate != MergeGate {
		return CommandRef{}, CommandOutcome{}, fmt.Errorf("%w: only merge gate reports can provide reusable receipts", ErrIneligible)
	}
	report, err := LoadReport(repositoryRoot, identity)
	if err != nil {
		return CommandRef{}, CommandOutcome{}, err
	}
	if report.Status != RunFailed {
		return CommandRef{}, CommandOutcome{}, fmt.Errorf("%w: only failed merge gate reports can resume", ErrIneligible)
	}
	reference, err := identity.Command(index)
	if err != nil {
		return CommandRef{}, CommandOutcome{}, err
	}
	if reference.Spec.Category != OrdinaryTest {
		return CommandRef{}, CommandOutcome{}, fmt.Errorf("%w: command %q is not an ordinary test", ErrIneligible, reference.Spec.Name)
	}
	outcome, found := outcomeAt(report.Commands, index)
	if !eligibleReusableOutcome(found, outcome) {
		return CommandRef{}, CommandOutcome{}, fmt.Errorf("%w: command %q has no passed reusable receipt", ErrMissingArtifact, reference.Spec.Name)
	}
	return reference, outcome, nil
}

func eligibleReusableOutcome(found bool, outcome CommandOutcome) bool {
	return found && outcome.Status == Passed && !outcome.Reused && outcome.ReceiptPath != ""
}

func loadReusableReceipt(repositoryRoot string, identity Identity, reference CommandRef, outcome CommandOutcome) (ReusableReceipt, error) {
	root, directory, err := existingRunDirectory(repositoryRoot, identity)
	if err != nil {
		return ReusableReceipt{}, err
	}
	run := &Run{repositoryRoot: root, directory: directory, identity: identity}
	path, display, err := run.receiptPath(reference, false)
	if err != nil {
		return ReusableReceipt{}, err
	}
	if outcome.ReceiptPath != display {
		return ReusableReceipt{}, fmt.Errorf("%w: receipt path does not match the command identity", ErrStaleArtifact)
	}
	receipt, err := readReceipt(path)
	if err != nil {
		return ReusableReceipt{}, err
	}
	runSHA256, err := identity.Digest()
	if err != nil {
		return ReusableReceipt{}, err
	}
	if receipt.SHA256 != outcome.ReceiptSHA256 {
		return ReusableReceipt{}, fmt.Errorf("%w: receipt digest does not match its report", ErrStaleArtifact)
	}
	if err := validateReceipt(reference, identity.Gate, runSHA256, receipt); err != nil {
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
	if report.Commands == nil || report.Findings == nil || report.Notes == nil {
		return fmt.Errorf("%w: gate run report collections are missing", ErrInvalidArtifact)
	}
	return nil
}

func validReportHeader(report Report) bool {
	return report.Version == Version && validRunStatus(report.Status) && !report.CompletedAt.Before(report.StartedAt)
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
	return nil
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
	if len(outcome.Attempts) == 0 || outcome.Status != outcome.Attempts[len(outcome.Attempts)-1].Status {
		return fmt.Errorf("%w: command outcome attempts are invalid", ErrInvalidArtifact)
	}
	for index, attempt := range outcome.Attempts {
		if err := validateAttempt(attempt, index+1); err != nil {
			return err
		}
	}
	return validateOutcomeReceipt(reference, outcome)
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
		attempt.ResourceWaitMilliseconds >= 0 && attempt.LogPath != "" && validSHA256(attempt.LogSHA256)
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

func validateStoredReportArtifacts(root, directory string, report Report) error {
	run := &Run{repositoryRoot: root, directory: directory, identity: report.Identity, runSHA256: report.IdentitySHA256}
	for _, outcome := range report.Commands {
		reference, err := report.Identity.Command(outcome.CommandIndex)
		if err != nil {
			return err
		}
		if outcome.Reused {
			if err := validateStoredReceipt(run, reference, outcome, ""); err != nil {
				return err
			}
			continue
		}
		for _, attempt := range outcome.Attempts {
			if err := validateStoredAttempt(run, reference, attempt); err != nil {
				return err
			}
		}
		if reference.Spec.Category == OrdinaryTest && outcome.Status == Passed {
			last := outcome.Attempts[len(outcome.Attempts)-1]
			if err := validateStoredReceipt(run, reference, outcome, last.LogSHA256); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateStoredAttempt(run *Run, reference CommandRef, attempt Attempt) error {
	path, display, err := run.logPath(reference.SHA256, attempt.Number, false)
	if err != nil {
		return err
	}
	if attempt.LogPath != display {
		return fmt.Errorf("%w: command log path does not match its identity", ErrStaleArtifact)
	}
	document, data, err := readCommandLog(path)
	if err != nil {
		return err
	}
	if ContentSHA256(data) != attempt.LogSHA256 || document.StdoutTruncated != attempt.StdoutTruncated || document.StderrTruncated != attempt.StderrTruncated {
		return fmt.Errorf("%w: command log does not match its report", ErrStaleArtifact)
	}
	return nil
}

func validateStoredReceipt(run *Run, reference CommandRef, outcome CommandOutcome, expectedLogSHA256 string) error {
	path, display, err := run.receiptPath(reference, false)
	if err != nil {
		return err
	}
	if outcome.ReceiptPath != display {
		return fmt.Errorf("%w: receipt path does not match its identity", ErrStaleArtifact)
	}
	receipt, err := readReceipt(path)
	if err != nil {
		return err
	}
	if receipt.SHA256 != outcome.ReceiptSHA256 {
		return fmt.Errorf("%w: receipt does not match its report", ErrStaleArtifact)
	}
	if err := validateReceipt(reference, run.identity.Gate, run.runSHA256, receipt); err != nil {
		return err
	}
	if expectedLogSHA256 != "" && receipt.LogSHA256 != expectedLogSHA256 {
		return fmt.Errorf("%w: receipt does not match its passed command log", ErrStaleArtifact)
	}
	return nil
}

func readReceipt(path string) (Receipt, error) {
	data, err := readArtifact(path, maximumReceiptBytes, "gate run receipt")
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

func validateReceipt(reference CommandRef, gate GateKind, runSHA256 string, receipt Receipt) error {
	if receipt.Version != Version || receipt.Gate != gate || receipt.RunSHA256 != runSHA256 || receipt.CommandSHA256 != reference.SHA256 ||
		receipt.Category != OrdinaryTest || receipt.Status != Passed || !validSHA256(receipt.LogSHA256) || !validSHA256(receipt.SHA256) {
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
	return identity
}

func cloneCommandOutcome(outcome CommandOutcome) CommandOutcome {
	outcome.Attempts = append([]Attempt{}, outcome.Attempts...)
	return outcome
}

func cloneFindings(findings []Finding) []Finding {
	return append([]Finding{}, findings...)
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
	return report
}
