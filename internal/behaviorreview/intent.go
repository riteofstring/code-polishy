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
	intentJournalVersion = 1
	maximumIntentEntries = 128
)

type intentCapture struct {
	ID               string `json:"id"`
	CapturedAtCommit string `json:"captured_at_commit"`
	Intent           string `json:"intent"`
	IntentSHA256     string `json:"intent_sha256"`
	PreviousSHA256   string `json:"previous_entry_sha256"`
	EntrySHA256      string `json:"entry_sha256"`
}

type intentJournal struct {
	Version int             `json:"version"`
	Entries []intentCapture `json:"entries"`
}

type intentCaptureMaterial struct {
	ID               string `json:"id"`
	CapturedAtCommit string `json:"captured_at_commit"`
	Intent           string `json:"intent"`
	IntentSHA256     string `json:"intent_sha256"`
	PreviousSHA256   string `json:"previous_entry_sha256"`
}

func captureIntent(ctx context.Context, repo repository.Repository, options CaptureIntentOptions) (CaptureIntentResult, error) {
	commit, intent, err := captureIntentInputs(ctx, repo, options)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	defer root.Close()
	return appendIntentCapture(repo, root, commit, intent)
}

func captureIntentInputs(ctx context.Context, repo repository.Repository, options CaptureIntentOptions) (string, []byte, error) {
	ctx = reviewContext(ctx)
	if err := ctx.Err(); err != nil {
		return "", nil, operational("capture behavior review intent", err)
	}
	if strings.TrimSpace(options.IntentPath) == "" {
		return "", nil, fmt.Errorf("%w: intent path is required", ErrInvalidInput)
	}
	commit, err := repo.CleanHead()
	if err != nil {
		return "", nil, err
	}
	intent, err := readExternalRegularUTF8(options.IntentPath, maximumIntentBytes, true, "intent file")
	if err != nil {
		return "", nil, err
	}
	return commit, intent, nil
}

func appendIntentCapture(repo repository.Repository, root *artifactHandle, commit string, intent []byte) (CaptureIntentResult, error) {
	journal, err := readIntentJournal(repo, root, true)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	entry, err := newIntentCapture(commit, intent, journal)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	journal.Entries = append(journal.Entries, entry)
	if err := validateIntentJournal(repo, journal); err != nil {
		return CaptureIntentResult{}, err
	}
	data, err := marshalArtifact(journal)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	if len(data) > maximumIntentJournal {
		return CaptureIntentResult{}, fmt.Errorf("%w: intent journal exceeds %d bytes", ErrInvalidInput, maximumIntentJournal)
	}
	current, err := repo.CleanHead()
	if err != nil {
		return CaptureIntentResult{}, err
	}
	if current != commit {
		return CaptureIntentResult{}, fmt.Errorf("%w: candidate changed while intent was captured", ErrCandidateChanged)
	}
	if err := root.writeArtifactAtomic(intentJournalFilename, data); err != nil {
		return CaptureIntentResult{}, err
	}
	return CaptureIntentResult{
		ID: entry.ID, Commit: entry.CapturedAtCommit, IntentSHA256: entry.IntentSHA256,
		JournalSHA256: entry.EntrySHA256, JournalPath: artifactDisplayPath(intentJournalFilename),
	}, nil
}

func newIntentCapture(commit string, intent []byte, journal intentJournal) (intentCapture, error) {
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
		ID: id, CapturedAtCommit: commit, Intent: string(intent), IntentSHA256: sha256Hex(intent), PreviousSHA256: previous,
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
		ID: entry.ID, CapturedAtCommit: entry.CapturedAtCommit, Intent: entry.Intent,
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
		return intentJournal{Version: intentJournalVersion, Entries: []intentCapture{}}, nil
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
	return nil
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
