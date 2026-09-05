package behaviorreview

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const (
	intentJournalVersion    = 3
	maximumIntentEntries    = 128
	maximumTaskRequirements = 128
)

type intentCapture struct {
	ID                   string `json:"id"`
	CapturedAtCommit     string `json:"captured_at_commit"`
	CandidateStateSHA256 string `json:"candidate_state_sha256"`
	Intent               string `json:"intent"`
	IntentSHA256         string `json:"intent_sha256"`
	PreviousSHA256       string `json:"previous_entry_sha256"`
	EntrySHA256          string `json:"entry_sha256"`
}

type intentJournal struct {
	Version      int               `json:"version"`
	Entries      []intentCapture   `json:"entries"`
	Requirements []TaskRequirement `json:"requirements"`
}

type intentJournalAppend struct {
	entry       intentCapture
	requirement TaskRequirement
	data        []byte
}

type intentCaptureMaterial struct {
	ID                   string `json:"id"`
	CapturedAtCommit     string `json:"captured_at_commit"`
	CandidateStateSHA256 string `json:"candidate_state_sha256"`
	Intent               string `json:"intent"`
	IntentSHA256         string `json:"intent_sha256"`
	PreviousSHA256       string `json:"previous_entry_sha256"`
}

func captureIntent(ctx context.Context, repo repository.Repository, options CaptureIntentOptions) (CaptureIntentResult, error) {
	ctx = reviewContext(ctx)
	features, err := configuredFeatureNames(repo.Config, options.Features)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	snapshot, intent, err := captureIntentInputs(ctx, repo, options)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	defer root.Close()
	return appendCurrentIntentCapture(ctx, repo, root, snapshot, intent, features)
}

func appendCurrentIntentCapture(ctx context.Context, repo repository.Repository, root *artifactHandle, snapshot repository.CandidateStateSnapshot, intent []byte, features []string) (CaptureIntentResult, error) {
	release, err := lockIntentJournal(root)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	defer release()
	journal, err := readIntentJournal(repo, root, true)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	appendResult, err := prepareIntentJournalAppend(repo, journal, snapshot, intent, features)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	return publishIntentCapture(ctx, repo, root, snapshot, appendResult, features)
}

func publishIntentCapture(ctx context.Context, repo repository.Repository, root *artifactHandle, snapshot repository.CandidateStateSnapshot, appendResult intentJournalAppend, features []string) (CaptureIntentResult, error) {
	if err := ensureIntentCaptureCandidate(repo, snapshot); err != nil {
		return CaptureIntentResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return CaptureIntentResult{}, operational("publish intent capture", err)
	}
	if err := root.writeArtifactAtomic(intentJournalFilename, appendResult.data); err != nil {
		return CaptureIntentResult{}, err
	}
	return intentCaptureResult(appendResult, features), nil
}

func captureIntentInputs(ctx context.Context, repo repository.Repository, options CaptureIntentOptions) (repository.CandidateStateSnapshot, []byte, error) {
	ctx = reviewContext(ctx)
	if err := ctx.Err(); err != nil {
		return repository.CandidateStateSnapshot{}, nil, operational("capture behavior review intent", err)
	}
	if strings.TrimSpace(options.IntentPath) == "" {
		return repository.CandidateStateSnapshot{}, nil, fmt.Errorf("%w: intent path is required", ErrInvalidInput)
	}
	snapshot, err := repo.CandidateState()
	if err != nil {
		return repository.CandidateStateSnapshot{}, nil, err
	}
	intent, err := readExternalRegularUTF8(options.IntentPath, maximumIntentBytes, true, "intent file")
	if err != nil {
		return repository.CandidateStateSnapshot{}, nil, err
	}
	return snapshot, intent, nil
}

func intentCaptureResult(appendResult intentJournalAppend, features []string) CaptureIntentResult {
	return CaptureIntentResult{
		ID: appendResult.entry.ID, Commit: appendResult.entry.CapturedAtCommit, CandidateSHA256: appendResult.entry.CandidateStateSHA256, IntentSHA256: appendResult.entry.IntentSHA256,
		JournalSHA256: appendResult.entry.EntrySHA256, JournalPath: artifactDisplayPath(intentJournalFilename),
		RequirementID: appendResult.requirement.ID, RequirementSHA256: appendResult.requirement.EntrySHA256, Features: append([]string{}, features...),
	}
}

func prepareIntentJournalAppend(repo repository.Repository, journal intentJournal, snapshot repository.CandidateStateSnapshot, intent []byte, features []string) (intentJournalAppend, error) {
	entry, err := newIntentCapture(snapshot, intent, journal)
	if err != nil {
		return intentJournalAppend{}, err
	}
	journal.Entries = append(journal.Entries, entry)
	requirement, err := appendIntentCaptureRequirement(&journal, snapshot.Head, entry, features)
	if err != nil {
		return intentJournalAppend{}, err
	}
	data, err := marshalValidatedIntentJournal(repo, journal)
	if err != nil {
		return intentJournalAppend{}, err
	}
	return intentJournalAppend{entry: entry, requirement: requirement, data: data}, nil
}

func appendIntentCaptureRequirement(journal *intentJournal, commit string, entry intentCapture, features []string) (TaskRequirement, error) {
	if len(features) == 0 {
		return TaskRequirement{}, nil
	}
	requirement, err := newTaskRequirement(commit, []intentCapture{entry}, features, journal.Requirements)
	if err != nil {
		return TaskRequirement{}, err
	}
	journal.Requirements = append(journal.Requirements, requirement)
	return requirement, nil
}

func marshalValidatedIntentJournal(repo repository.Repository, journal intentJournal) ([]byte, error) {
	if err := validateIntentJournal(repo, journal); err != nil {
		return nil, err
	}
	data, err := marshalArtifact(journal)
	if err != nil {
		return nil, err
	}
	if len(data) > maximumIntentJournal {
		return nil, fmt.Errorf("%w: intent journal exceeds %d bytes", ErrInvalidInput, maximumIntentJournal)
	}
	return data, nil
}

func ensureIntentCaptureCandidate(repo repository.Repository, snapshot repository.CandidateStateSnapshot) error {
	current, err := repo.CandidateState()
	if err != nil {
		return err
	}
	if current != snapshot {
		return fmt.Errorf("%w: candidate changed while intent was captured", ErrCandidateChanged)
	}
	return nil
}

func newIntentCapture(snapshot repository.CandidateStateSnapshot, intent []byte, journal intentJournal) (intentCapture, error) {
	if len(journal.Entries) == 0 && snapshot.Dirty {
		return intentCapture{}, fmt.Errorf("%w: capture the original request before candidate changes", repository.ErrDirtyCandidate)
	}
	if len(journal.Entries) >= maximumIntentEntries {
		return intentCapture{}, fmt.Errorf("%w: intent journal exceeds %d entries", ErrInvalidInput, maximumIntentEntries)
	}
	previous := ""
	if len(journal.Entries) > 0 {
		latest := journal.Entries[len(journal.Entries)-1]
		previous = latest.EntrySHA256
	}
	id, err := newCaptureID()
	if err != nil {
		return intentCapture{}, operational("generate intent capture identifier", err)
	}
	entry := intentCapture{
		ID: id, CapturedAtCommit: snapshot.Head, CandidateStateSHA256: snapshot.SHA256,
		Intent: string(intent), IntentSHA256: sha256Hex(intent), PreviousSHA256: previous,
	}
	entry.EntrySHA256, err = intentEntryDigest(entry)
	if err != nil {
		return intentCapture{}, err
	}
	return entry, nil
}

func newCaptureID() (string, error) {
	data := make([]byte, 16)
	if _, err := cryptorand.Read(data); err != nil {
		return "", err
	}
	return fmt.Sprintf("intent-%x", data), nil
}

func intentEntryDigest(entry intentCapture) (string, error) {
	material := intentCaptureMaterial{
		ID: entry.ID, CapturedAtCommit: entry.CapturedAtCommit, CandidateStateSHA256: entry.CandidateStateSHA256, Intent: entry.Intent,
		IntentSHA256: entry.IntentSHA256, PreviousSHA256: entry.PreviousSHA256,
	}
	data, err := json.Marshal(material)
	if err != nil {
		return "", operational("encode intent journal entry", err)
	}
	return sha256Hex(data), nil
}

func readIntentJournal(repo repository.Repository, root *artifactHandle, allowMissing bool) (intentJournal, error) {
	data, err := root.readArtifact(intentJournalFilename, maximumIntentJournal)
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		return intentJournal{Version: intentJournalVersion, Entries: []intentCapture{}, Requirements: []TaskRequirement{}}, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return intentJournal{}, fmt.Errorf("%w: capture the original request at the review base before implementation", ErrInvalidInput)
	}
	if err != nil {
		return intentJournal{}, fmt.Errorf("%w: intent journal is unavailable: %v", ErrInvalidInput, err)
	}
	var journal intentJournal
	if err := decodeStrict(data, &journal); err != nil {
		return intentJournal{}, fmt.Errorf("%w: intent journal JSON is invalid: %v", ErrInvalidInput, err)
	}
	if err := validateIntentJournal(repo, journal); err != nil {
		return intentJournal{}, err
	}
	return journal, nil
}

func validateIntentJournal(repo repository.Repository, journal intentJournal) error {
	if journal.Version != intentJournalVersion {
		return fmt.Errorf("%w: intent journal version must be %d", ErrInvalidInput, intentJournalVersion)
	}
	if len(journal.Entries) == 0 || len(journal.Entries) > maximumIntentEntries {
		return fmt.Errorf("%w: intent journal entries are invalid", ErrInvalidInput)
	}
	seen := map[string]bool{}
	previousDigest := ""
	previousCommit := ""
	for index, entry := range journal.Entries {
		if err := validateIntentCapture(entry, previousDigest, seen); err != nil {
			return fmt.Errorf("%w: intent journal entry %d is invalid: %v", ErrInvalidInput, index+1, err)
		}
		if previousCommit != "" {
			ancestor, err := repo.IsAncestor(previousCommit, entry.CapturedAtCommit)
			if err != nil {
				return operational("validate intent journal ancestry", err)
			}
			if !ancestor {
				return fmt.Errorf("%w: intent journal commits do not advance through one history", ErrInvalidInput)
			}
		}
		seen[entry.ID] = true
		previousDigest = entry.EntrySHA256
		previousCommit = entry.CapturedAtCommit
	}
	return validateTaskRequirements(repo, journal)
}

func validateIntentCapture(entry intentCapture, expectedPrevious string, seen map[string]bool) error {
	if !validIntentCaptureIdentity(entry, seen) {
		return errors.New("identifiers are malformed")
	}
	if !validIntentCaptureText(entry.Intent) {
		return errors.New("intent is invalid")
	}
	if !validSHA256(entry.IntentSHA256) || sha256Hex([]byte(entry.Intent)) != entry.IntentSHA256 {
		return errors.New("intent digest is invalid")
	}
	if !validSHA256(entry.CandidateStateSHA256) {
		return errors.New("candidate state digest is invalid")
	}
	if entry.PreviousSHA256 != expectedPrevious {
		return errors.New("previous entry digest is invalid")
	}
	digest, err := intentEntryDigest(entry)
	if err != nil {
		return err
	}
	if !validSHA256(entry.EntrySHA256) || entry.EntrySHA256 != digest {
		return errors.New("entry digest is invalid")
	}
	return nil
}

func validIntentCaptureIdentity(entry intentCapture, seen map[string]bool) bool {
	return allValid(validIdentifier(entry.ID), !seen[entry.ID], validRevision(entry.CapturedAtCommit))
}

func validIntentCaptureText(intent string) bool {
	return allValid(strings.TrimSpace(intent) != "", utf8.ValidString(intent), len([]byte(intent)) <= maximumIntentBytes)
}

func selectIntentCaptures(repo repository.Repository, journal intentJournal, base, candidate string) ([]intentCapture, string, error) {
	start := -1
	for index, entry := range journal.Entries {
		if entry.CapturedAtCommit == base {
			start = index
			break
		}
	}
	if start < 0 {
		return nil, "", fmt.Errorf("%w: capture the original request at the review base before implementation", ErrInvalidInput)
	}
	selected := append([]intentCapture{}, journal.Entries[start:]...)
	for _, entry := range selected {
		ancestor, err := repo.IsAncestor(entry.CapturedAtCommit, candidate)
		if err != nil {
			return nil, "", operational("select intent journal entries", err)
		}
		if !ancestor {
			return nil, "", fmt.Errorf("%w: intent journal contains a capture outside the reviewed candidate", ErrInvalidInput)
		}
	}
	if len(selected) == 0 || selected[len(selected)-1].CapturedAtCommit == candidate {
		return nil, "", fmt.Errorf("%w: intent must be captured before implementation", ErrInvalidInput)
	}
	digest, err := intentSelectionDigest(selected)
	if err != nil {
		return nil, "", err
	}
	return selected, digest, nil
}

func intentSelectionDigest(entries []intentCapture) (string, error) {
	data, err := json.Marshal(entries)
	if err != nil {
		return "", operational("encode selected behavior review intents", err)
	}
	return sha256Hex(data), nil
}

func sameIntentCaptures(left, right []intentCapture) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
