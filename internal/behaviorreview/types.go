package behaviorreview

import (
	"context"
	"errors"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

var (
	ErrInvalidInput     = errors.New("invalid behavior review input")
	ErrInvalidReview    = errors.New("invalid behavior review result")
	ErrInvalidEvidence  = errors.New("invalid regression proof evidence")
	ErrMissingReceipt   = errors.New("missing behavior review receipt")
	ErrStaleReceipt     = errors.New("stale behavior review receipt")
	ErrStaleReview      = errors.New("stale behavior review result")
	ErrCandidateChanged = errors.New("candidate changed during behavior review")
	ErrOperational      = errors.New("behavior review operational failure")
	ErrCleanup          = errors.New("behavior review cleanup failure")
)

type OperationalError struct {
	Operation string
	Err       error
}

func (err *OperationalError) Error() string {
	return fmt.Sprintf("%s: %v", err.Operation, err.Err)
}

func (err *OperationalError) Unwrap() error {
	return errors.Join(ErrOperational, err.Err)
}

type CleanupError struct {
	Operation string
	Err       error
}

func (err *CleanupError) Error() string {
	return fmt.Sprintf("%s: %v", err.Operation, err.Err)
}

func (err *CleanupError) Unwrap() error {
	return errors.Join(ErrCleanup, err.Err)
}

type PrepareOptions struct {
	Base       string
	IntentPath string
}

type PrepareResult struct {
	ReviewID     string
	Base         string
	Candidate    string
	IntentSHA256 string
	PacketPath   string
}

type FinalizeOptions struct {
	Base string
}

type FinalizeResult struct {
	ReviewID    string
	Base        string
	Candidate   string
	ReceiptPath string
}

type ProveOptions struct {
	Base                  string
	Suite                 string
	Evidence              []string
	ID                    string
	ExpectedRedExitStatus int
}

type ProveResult struct {
	ID        string
	Base      string
	Candidate string
	Suite     string
	ProofPath string
}

type ValidateGateReceiptOptions struct {
	Base string
}

type ProofReference struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type GateReceipt struct {
	Version       int              `json:"version"`
	ReviewID      string           `json:"review_id"`
	Base          string           `json:"base"`
	Candidate     string           `json:"candidate"`
	IntentSHA256  string           `json:"intent_sha256"`
	PacketSHA256  string           `json:"packet_sha256"`
	PrepareSHA256 string           `json:"prepare_sha256"`
	ReviewSHA256  string           `json:"review_sha256"`
	Proofs        []ProofReference `json:"proofs"`
}

const (
	CheckpointScopeChanged       = "changed"
	CheckpointScopeDocumentation = "documentation"
	CheckpointReceiptPath        = ".code-polishy-reports/checkpoint-gate/receipt.json"
)

type RecordCheckpointOptions struct {
	Base             string
	Candidate        string
	Scope            string
	BehaviorReviewID string
}

type CheckpointReceipt struct {
	Version          int    `json:"version"`
	Base             string `json:"base"`
	Candidate        string `json:"candidate"`
	Scope            string `json:"scope"`
	BehaviorReviewID string `json:"behavior_review_id,omitempty"`
}

type RecordCheckpointResult struct {
	Receipt     CheckpointReceipt
	ReceiptPath string
}

func Prepare(ctx context.Context, repo repository.Repository, options PrepareOptions) (PrepareResult, error) {
	return prepare(ctx, repo, options)
}

func Finalize(ctx context.Context, repo repository.Repository, options FinalizeOptions) (FinalizeResult, error) {
	return finalize(ctx, repo, options)
}

func Prove(ctx context.Context, repo repository.Repository, commandRunner runner.OutputRunner, options ProveOptions) (ProveResult, error) {
	return prove(ctx, repo, commandRunner, options)
}

func ValidateGateReceipt(ctx context.Context, repo repository.Repository, options ValidateGateReceiptOptions) (GateReceipt, error) {
	return validateGateReceipt(ctx, repo, options)
}

func ReplayGateReceipt(ctx context.Context, repo repository.Repository, commandRunner runner.OutputRunner, options ValidateGateReceiptOptions) (GateReceipt, error) {
	return replayGateReceipt(ctx, repo, commandRunner, options)
}

func RecordCheckpoint(ctx context.Context, repo repository.Repository, options RecordCheckpointOptions) (RecordCheckpointResult, error) {
	return recordCheckpoint(ctx, repo, options)
}
