package behaviorreview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const (
	artifactDirectory       = ".code-polishy-reports/behavior-review"
	checkpointDirectory     = ".code-polishy-reports/checkpoint-gate"
	packetFilename          = "packet.json"
	defaultResultFilename   = "result.json"
	finalReviewFilename     = "review.json"
	receiptFilename         = "receipt.json"
	proofDirectory          = "proofs"
	maximumIntentBytes      = 64 << 10
	maximumTemplateBytes    = 64 << 10
	maximumResultBytes      = 256 << 10
	maximumDesignBytes      = 256 << 10
	maximumArtifactReadByte = 8 << 20
)

func behaviorReviewRoot(repo repository.Repository) (string, error) {
	return managedArtifactRoot(repo, artifactDirectory, "behavior review")
}

func checkpointRoot(repo repository.Repository) (string, error) {
	return managedArtifactRoot(repo, checkpointDirectory, "checkpoint gate")
}

func managedArtifactRoot(repo repository.Repository, directory, label string) (string, error) {
	root, candidate, err := managedArtifactCandidate(repo, directory)
	if err != nil {
		return "", err
	}
	if err := validateOutputAncestor(root, candidate, label); err != nil {
		return "", err
	}
	if err := os.MkdirAll(candidate, 0o700); err != nil {
		return "", operational("create "+label+" artifact root", err)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || !pathInside(root, resolved) {
		return "", fmt.Errorf("%w: %s artifact root resolves outside the repository", ErrInvalidInput, label)
	}
	return resolved, nil
}

func existingBehaviorReviewRoot(repo repository.Repository) (string, error) {
	root, candidate, err := behaviorReviewCandidate(repo)
	if err != nil {
		return "", err
	}
	if err := validateOutputAncestor(root, candidate, "behavior review"); err != nil {
		return "", err
	}
	info, err := os.Lstat(candidate)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: behavior review artifacts are unavailable", ErrMissingReceipt)
	}
	if err != nil {
		return "", operational("inspect behavior review artifact root", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: behavior review artifact root is not a directory", ErrInvalidInput)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || !pathInside(root, resolved) {
		return "", fmt.Errorf("%w: behavior review artifact root resolves outside the repository", ErrInvalidInput)
	}
	return resolved, nil
}

func behaviorReviewCandidate(repo repository.Repository) (string, string, error) {
	return managedArtifactCandidate(repo, artifactDirectory)
}

func managedArtifactCandidate(repo repository.Repository, directory string) (string, string, error) {
	root, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return "", "", operational("resolve repository root", err)
	}
	candidate := filepath.Join(root, filepath.FromSlash(directory))
	if !pathInside(root, candidate) {
		return "", "", fmt.Errorf("%w: behavior review artifact root escapes the repository", ErrInvalidInput)
	}
	return root, candidate, nil
}

func validateOutputAncestor(root, candidate, label string) error {
	ancestor := candidate
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			if !info.IsDir() && ancestor == candidate {
				return fmt.Errorf("%w: %s artifact root is not a directory", ErrInvalidInput, label)
			}
			resolved, resolveErr := filepath.EvalSymlinks(ancestor)
			if resolveErr != nil || !pathInside(root, resolved) {
				return fmt.Errorf("%w: %s artifact root has an escaping symlink ancestor", ErrInvalidInput, label)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return operational("inspect behavior review artifact root", err)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return fmt.Errorf("%w: %s artifact root has no contained ancestor", ErrInvalidInput, label)
		}
		ancestor = parent
	}
}

func recordCheckpoint(ctx context.Context, repo repository.Repository, options RecordCheckpointOptions) (RecordCheckpointResult, error) {
	if err := validateCheckpointOptions(ctx, repo, options); err != nil {
		return RecordCheckpointResult{}, err
	}
	receipt := CheckpointReceipt{
		Version: artifactVersion, Base: options.Base, Candidate: options.Candidate,
		Scope: options.Scope, BehaviorReviewID: options.BehaviorReviewID,
	}
	data, err := marshalArtifact(receipt)
	if err != nil {
		return RecordCheckpointResult{}, err
	}
	root, err := checkpointRoot(repo)
	if err != nil {
		return RecordCheckpointResult{}, err
	}
	if err := writeArtifactAtomic(artifactPath(root, receiptFilename), data); err != nil {
		return RecordCheckpointResult{}, err
	}
	return RecordCheckpointResult{Receipt: receipt, ReceiptPath: CheckpointReceiptPath}, nil
}

func validateCheckpointOptions(ctx context.Context, repo repository.Repository, options RecordCheckpointOptions) error {
	if err := ctx.Err(); err != nil {
		return operational("record checkpoint gate receipt", err)
	}
	if err := validateCheckpointFields(options); err != nil {
		return err
	}
	return validateCheckpointCandidate(repo, options)
}

func validateCheckpointFields(options RecordCheckpointOptions) error {
	if !validRevision(options.Base) || !validRevision(options.Candidate) {
		return fmt.Errorf("%w: checkpoint revisions are malformed", ErrInvalidInput)
	}
	switch options.Scope {
	case CheckpointScopeChanged:
		if !validIdentifier(options.BehaviorReviewID) {
			return fmt.Errorf("%w: checkpoint behavior review is invalid", ErrInvalidInput)
		}
	case CheckpointScopeDocumentation:
		if options.BehaviorReviewID != "" {
			return fmt.Errorf("%w: checkpoint behavior review is invalid", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: checkpoint scope is invalid", ErrInvalidInput)
	}
	return nil
}

func validateCheckpointCandidate(repo repository.Repository, options RecordCheckpointOptions) error {
	candidate, err := repo.CleanHead()
	if err != nil {
		return err
	}
	if candidate != options.Candidate {
		return fmt.Errorf("%w: checkpoint candidate does not match clean HEAD", ErrCandidateChanged)
	}
	ancestor, err := repo.IsAncestor(options.Base, options.Candidate)
	if err != nil {
		return operational("validate checkpoint ancestry", err)
	}
	if !ancestor {
		return fmt.Errorf("%w: checkpoint base is not an ancestor of the candidate", ErrInvalidInput)
	}
	return nil
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func artifactPath(root, name string) string {
	return filepath.Join(root, filepath.FromSlash(name))
}

func artifactDisplayPath(name string) string {
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(artifactDirectory), filepath.FromSlash(name)))
}

func proofArtifactName(id, suffix string) string {
	return filepath.ToSlash(filepath.Join(proofDirectory, id+suffix))
}

func writeArtifactAtomic(path string, data []byte) error {
	if err := rejectUnsafeArtifactTarget(path); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".behavior-review-*")
	if err != nil {
		return operational("create behavior review artifact", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return operational("set behavior review artifact permissions", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return operational("write behavior review artifact", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return operational("sync behavior review artifact", err)
	}
	if err := temporary.Close(); err != nil {
		return operational("close behavior review artifact", err)
	}
	if err := replaceAtomicFile(temporaryPath, path); err != nil {
		return operational("replace behavior review artifact", err)
	}
	return nil
}

func rejectUnsafeArtifactTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return operational("inspect behavior review artifact", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: behavior review artifact target is not a regular file", ErrInvalidInput)
	}
	return nil
}

func readArtifact(path string, limit int) ([]byte, error) {
	return readRegularFile(path, limit, false, "behavior review artifact")
}

func readRegularUTF8(path string, limit int, requireContent bool, label string) ([]byte, error) {
	data, err := readRegularFile(path, limit, requireContent, label)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: %s must be valid UTF-8", ErrInvalidInput, label)
	}
	return data, nil
}

func readRegularFile(path string, limit int, requireContent bool, label string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s is missing", ErrInvalidInput, label)
	}
	if err != nil {
		return nil, operational("inspect "+label, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s must be a regular file", ErrInvalidInput, label)
	}
	if info.Size() > int64(limit) {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidInput, label, limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, operational("open "+label, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, operational("read "+label, err)
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidInput, label, limit)
	}
	if requireContent && strings.TrimSpace(string(data)) == "" {
		return nil, fmt.Errorf("%w: %s must not be empty", ErrInvalidInput, label)
	}
	return data, nil
}

func marshalArtifact(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, operational("encode behavior review artifact", err)
	}
	return append(data, '\n'), nil
}

func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: parse JSON: %v", ErrInvalidReview, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("%w: parse JSON: %v", ErrInvalidReview, err)
		}
		return fmt.Errorf("%w: JSON contains more than one value", ErrInvalidReview)
	}
	return nil
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func operational(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &OperationalError{Operation: operation, Err: err}
}

func cleanup(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &CleanupError{Operation: operation, Err: err}
}

func readCurrentDesignDocuments(repo repository.Repository, paths []string) ([]packetDesignDocument, error) {
	documents := []packetDesignDocument{}
	for _, path := range repo.DesignDocumentsForFiles(paths) {
		resolved, err := repo.Resolve(path)
		if err != nil {
			return nil, fmt.Errorf("%w: resolve mapped design document %q: %v", ErrInvalidInput, path, err)
		}
		data, err := readRegularUTF8(resolved, maximumDesignBytes, true, "mapped design document "+path)
		if err != nil {
			return nil, err
		}
		documents = append(documents, packetDesignDocument{Path: path, Content: string(data), SHA256: sha256Hex(data)})
	}
	return documents, nil
}
