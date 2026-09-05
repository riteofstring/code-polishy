package behaviorreview

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type PreparedIntentCapture struct {
	repo          repository.Repository
	snapshot      repository.CandidateStateSnapshot
	journalSHA256 string
	append        intentJournalAppend
	result        CaptureIntentResult
	committed     bool
}

func PrepareIntentCapture(ctx context.Context, repo repository.Repository, options CaptureIntentOptions) (*PreparedIntentCapture, error) {
	features, err := configuredFeatureNames(repo.Config, options.Features)
	if err != nil {
		return nil, err
	}
	snapshot, intent, err := captureIntentInputs(ctx, repo, options)
	if err != nil {
		return nil, err
	}
	journal, err := readCaptureJournal(repo)
	if err != nil {
		return nil, err
	}
	journalSHA256, err := captureJournalSHA256(journal)
	if err != nil {
		return nil, err
	}
	appendResult, err := prepareIntentJournalAppend(repo, journal, snapshot, intent, features)
	if err != nil {
		return nil, err
	}
	return &PreparedIntentCapture{
		repo: repo, snapshot: snapshot, journalSHA256: journalSHA256,
		append: appendResult, result: intentCaptureResult(appendResult, features),
	}, nil
}

func (prepared *PreparedIntentCapture) Result() CaptureIntentResult {
	result := prepared.result
	result.Features = slices.Clone(result.Features)
	return result
}

func (prepared *PreparedIntentCapture) Commit(ctx context.Context) (CaptureIntentResult, error) {
	if prepared == nil || prepared.committed || len(prepared.append.data) == 0 {
		return CaptureIntentResult{}, fmt.Errorf("%w: intent capture is absent or already committed", ErrInvalidInput)
	}
	ctx = reviewContext(ctx)
	if err := ctx.Err(); err != nil {
		return CaptureIntentResult{}, operational("commit intent capture", err)
	}
	if err := ensureIntentCaptureCandidate(prepared.repo, prepared.snapshot); err != nil {
		return CaptureIntentResult{}, err
	}
	root, err := behaviorReviewRoot(prepared.repo)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	defer root.Close()
	release, err := lockIntentJournal(root)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	defer release()
	if err := prepared.validatePublication(root); err != nil {
		return CaptureIntentResult{}, err
	}
	result, err := publishIntentCapture(ctx, prepared.repo, root, prepared.snapshot, prepared.append, prepared.result.Features)
	if err != nil {
		return CaptureIntentResult{}, err
	}
	prepared.committed = true
	return result, nil
}

func (prepared *PreparedIntentCapture) validatePublication(root *artifactHandle) error {
	journal, err := readIntentJournal(prepared.repo, root, true)
	if err != nil {
		return err
	}
	digest, err := captureJournalSHA256(journal)
	if err != nil {
		return err
	}
	if digest != prepared.journalSHA256 {
		return fmt.Errorf("%w: intent journal changed after capture preparation", ErrCandidateChanged)
	}
	return nil
}

func readCaptureJournal(repo repository.Repository) (intentJournal, error) {
	root, err := existingBehaviorReviewRoot(repo)
	if errors.Is(err, ErrMissingReceipt) {
		return intentJournal{Version: intentJournalVersion, Entries: []intentCapture{}, Requirements: []TaskRequirement{}}, nil
	}
	if err != nil {
		return intentJournal{}, err
	}
	defer root.Close()
	return readIntentJournal(repo, root, true)
}

func captureJournalSHA256(journal intentJournal) (string, error) {
	data, err := marshalArtifact(journal)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}
