package behaviorreview

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
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

var (
	errUnsafeArtifactPath  = errors.New("unsafe behavior review artifact path")
	errMissingArtifactRoot = errors.New("behavior review artifact root is missing")
)

type artifactHandle struct {
	path  string
	file  *os.File
	roots []*os.Root
}

func (directory *artifactHandle) Close() error {
	if directory == nil {
		return nil
	}
	if directory.file != nil {
		return directory.file.Close()
	}
	if len(directory.roots) > 0 {
		var result error
		for index := len(directory.roots) - 1; index >= 0; index-- {
			result = errors.Join(result, directory.roots[index].Close())
		}
		return result
	}
	return nil
}

func behaviorReviewRoot(repo repository.Repository) (*artifactHandle, error) {
	return managedArtifactRoot(repo, artifactDirectory, "behavior review")
}

func checkpointRoot(repo repository.Repository) (*artifactHandle, error) {
	return managedArtifactRoot(repo, checkpointDirectory, "checkpoint gate")
}

func managedArtifactRoot(repo repository.Repository, directory, label string) (*artifactHandle, error) {
	root, err := openArtifactRoot(repo.Root, artifactComponents(directory), true)
	if err != nil {
		return nil, artifactRootError(label, err, nil)
	}
	return root, nil
}

func existingBehaviorReviewRoot(repo repository.Repository) (*artifactHandle, error) {
	root, err := openArtifactRoot(repo.Root, artifactComponents(artifactDirectory), false)
	if err != nil {
		return nil, artifactRootError("behavior review", err, ErrMissingReceipt)
	}
	return root, nil
}

func existingCheckpointRoot(repo repository.Repository) (*artifactHandle, error) {
	root, err := openArtifactRoot(repo.Root, artifactComponents(checkpointDirectory), false)
	if err != nil {
		return nil, artifactRootError("checkpoint gate", err, ErrMissingCheckpoint)
	}
	return root, nil
}

func artifactRootError(label string, err, missing error) error {
	if missing != nil && errors.Is(err, errMissingArtifactRoot) {
		return fmt.Errorf("%w: %s artifacts are unavailable", missing, label)
	}
	if errors.Is(err, errUnsafeArtifactPath) {
		return fmt.Errorf("%w: %s artifact root is not a contained directory", ErrInvalidInput, label)
	}
	return operational("open "+label+" artifact root", err)
}

func artifactComponents(name string) []string {
	components, err := artifactNameComponents(name)
	if err != nil {
		panic("invalid behavior review artifact path")
	}
	return components
}

func artifactNameComponents(name string) ([]string, error) {
	if filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return nil, errUnsafeArtifactPath
	}
	components := strings.Split(filepath.ToSlash(name), "/")
	if len(components) == 0 {
		return nil, errUnsafeArtifactPath
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return nil, errUnsafeArtifactPath
		}
	}
	return components, nil
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
	defer root.Close()
	if err := root.writeArtifactAtomic(receiptFilename, data); err != nil {
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

func artifactPath(root, name string) string {
	return filepath.Join(root, filepath.FromSlash(name))
}

func artifactDisplayPath(name string) string {
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(artifactDirectory), filepath.FromSlash(name)))
}

func proofArtifactName(id, suffix string) string {
	return filepath.ToSlash(filepath.Join(proofDirectory, id+suffix))
}

func (directory *artifactHandle) writeArtifactAtomic(name string, data []byte) error {
	return writeArtifactAtomicAt(directory, name, data)
}

func (directory *artifactHandle) ensureDirectory(name, label string) error {
	return ensureArtifactDirectoryAt(directory, name, label)
}

func (directory *artifactHandle) reserveDirectory(parent, prefix string) (string, error) {
	return reserveArtifactDirectoryAt(directory, parent, prefix)
}

func (directory *artifactHandle) readArtifact(name string, limit int) ([]byte, error) {
	return directory.readRegularFile(name, limit, false, "behavior review artifact")
}

func (directory *artifactHandle) readRegularUTF8(name string, limit int, requireContent bool, label string) ([]byte, error) {
	data, err := directory.readRegularFile(name, limit, requireContent, label)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: %s must be valid UTF-8", ErrInvalidInput, label)
	}
	return data, nil
}

func (directory *artifactHandle) readRegularFile(name string, limit int, requireContent bool, label string) ([]byte, error) {
	file, err := openArtifactFile(directory, name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %w: %s is missing", ErrInvalidInput, os.ErrNotExist, label)
	}
	if errors.Is(err, errUnsafeArtifactPath) {
		return nil, fmt.Errorf("%w: %s must be a regular file", ErrInvalidInput, label)
	}
	if err != nil {
		return nil, operational("open "+label, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, operational("inspect "+label, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s must be a regular file", ErrInvalidInput, label)
	}
	if info.Size() > int64(limit) {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidInput, label, limit)
	}
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

func readExternalRegularUTF8(path string, limit int, requireContent bool, label string) ([]byte, error) {
	data, err := readExternalRegularFile(path, limit, requireContent, label)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: %s must be valid UTF-8", ErrInvalidInput, label)
	}
	return data, nil
}

func readExternalRegularFile(path string, limit int, requireContent bool, label string) ([]byte, error) {
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

func temporaryArtifactName() (string, error) {
	data := make([]byte, 16)
	if _, err := cryptorand.Read(data); err != nil {
		return "", err
	}
	return ".behavior-review-" + hex.EncodeToString(data), nil
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
		data, err := readExternalRegularUTF8(resolved, maximumDesignBytes, true, "mapped design document "+path)
		if err != nil {
			return nil, err
		}
		documents = append(documents, packetDesignDocument{Path: path, Content: string(data), SHA256: sha256Hex(data)})
	}
	return documents, nil
}
