package behaviorreview

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func readCheckpoint(ctx context.Context, repo repository.Repository) (CheckpointReceipt, error) {
	if err := ctx.Err(); err != nil {
		return CheckpointReceipt{}, operational("read checkpoint receipt", err)
	}
	root, err := existingCheckpointRoot(repo)
	if err != nil {
		return CheckpointReceipt{}, checkpointReadError("checkpoint artifacts are invalid", err)
	}
	defer root.Close()
	receipt, err := readCheckpointReceipt(root)
	if err != nil {
		return CheckpointReceipt{}, err
	}
	if err := validateCheckpointReceipt(receipt); err != nil {
		return CheckpointReceipt{}, staleCheckpoint("receipt structure is invalid", err)
	}
	candidate, err := repo.CleanHead()
	if err != nil {
		if errors.Is(err, repository.ErrGitOperation) {
			return CheckpointReceipt{}, operational("read checkpoint candidate", err)
		}
		return CheckpointReceipt{}, staleCheckpoint("receipt does not match a clean current candidate", err)
	}
	if receipt.Candidate != candidate {
		return CheckpointReceipt{}, staleCheckpoint("receipt does not match the current candidate", nil)
	}
	ancestor, err := repo.IsAncestor(receipt.Base, receipt.Candidate)
	if err != nil {
		return CheckpointReceipt{}, operational("validate checkpoint ancestry", err)
	}
	if !ancestor {
		return CheckpointReceipt{}, staleCheckpoint("receipt base is not an ancestor of its candidate", nil)
	}
	if err := validateCheckpointGateRun(repo, receipt); err != nil {
		return CheckpointReceipt{}, err
	}
	return receipt, nil
}

func validateCheckpointGateRun(repo repository.Repository, receipt CheckpointReceipt) error {
	report, err := gaterun.LoadPassedExecution(repo.Root, receipt.GateRun)
	if err != nil {
		if errors.Is(err, gaterun.ErrOperational) {
			return operational("validate checkpoint gate evidence", err)
		}
		return staleCheckpoint("receipt does not bind a passed checkpoint gate run", err)
	}
	if report.Identity.Gate != gaterun.CheckpointGate || report.Identity.ExactBase != receipt.Base || report.Identity.Candidate != receipt.Candidate {
		return staleCheckpoint("receipt does not match its checkpoint gate run", nil)
	}
	return nil
}

func checkpointReadError(message string, err error) error {
	if errors.Is(err, ErrMissingCheckpoint) || errors.Is(err, ErrOperational) {
		return err
	}
	return staleCheckpoint(message, err)
}

func readCheckpointReceipt(root *artifactHandle) (CheckpointReceipt, error) {
	data, err := root.readArtifact(receiptFilename, maximumArtifactReadByte)
	if errors.Is(err, os.ErrNotExist) {
		return CheckpointReceipt{}, fmt.Errorf("%w: checkpoint receipt is unavailable", ErrMissingCheckpoint)
	}
	if err != nil {
		if errors.Is(err, ErrOperational) {
			return CheckpointReceipt{}, err
		}
		return CheckpointReceipt{}, staleCheckpoint("receipt is unreadable", err)
	}
	var receipt CheckpointReceipt
	if err := decodeStrict(data, &receipt); err != nil {
		return CheckpointReceipt{}, staleCheckpoint("receipt JSON is invalid", err)
	}
	return receipt, nil
}

func validateCheckpointReceipt(receipt CheckpointReceipt) error {
	if receipt.Version != checkpointReceiptVersion {
		return fmt.Errorf("receipt version must be %d", checkpointReceiptVersion)
	}
	return validateCheckpointFields(RecordCheckpointOptions{
		Base: receipt.Base, Candidate: receipt.Candidate, Scope: receipt.Scope, BehaviorReviewID: receipt.BehaviorReviewID, GateRun: receipt.GateRun,
	})
}

func staleCheckpoint(message string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrStaleCheckpoint, message)
	}
	return fmt.Errorf("%w: %s: %v", ErrStaleCheckpoint, message, cause)
}
