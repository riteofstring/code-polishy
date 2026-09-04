package testreceipt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/riteofstring/code-polishy/internal/testartifact"
)

func Load(repositoryRoot string, identity Identity) (Reusable, error) {
	identitySHA256, err := identity.Digest()
	if err != nil {
		return Reusable{}, err
	}
	reusable, err := LoadDigest(repositoryRoot, identitySHA256)
	if err != nil {
		return Reusable{}, err
	}
	if !sameIdentity(reusable.Receipt.Identity, identity) {
		return Reusable{}, fmt.Errorf("%w: receipt identity does not match", ErrInvalid)
	}
	return reusable, nil
}

func loadAt(repositoryRoot string, identity Identity, now time.Time) (Reusable, error) {
	identitySHA256, err := identity.Digest()
	if err != nil {
		return Reusable{}, err
	}
	return loadDigestAt(repositoryRoot, identitySHA256, now)
}

func LoadDigest(repositoryRoot, identitySHA256 string) (Reusable, error) {
	return loadDigestAt(repositoryRoot, identitySHA256, time.Now().UTC())
}

func loadDigestAt(repositoryRoot, identitySHA256 string, now time.Time) (Reusable, error) {
	if !validSHA256(identitySHA256) {
		return Reusable{}, fmt.Errorf("%w: receipt identity digest is malformed", ErrInvalid)
	}
	directory, err := receiptDirectory(repositoryRoot, false)
	if err != nil {
		return Reusable{}, err
	}
	file := filepath.Join(directory, identitySHA256+".json")
	data, err := readReceiptFile(file)
	if errors.Is(err, ErrMissing) {
		return loadImportedReceipt(repositoryRoot, identitySHA256, now)
	}
	if err != nil {
		return Reusable{}, err
	}
	receipt, err := decodeReceipt(data)
	if err != nil {
		return Reusable{}, err
	}
	if receipt.IdentitySHA256 != identitySHA256 {
		return Reusable{}, fmt.Errorf("%w: receipt identity does not match", ErrInvalid)
	}
	if now.Before(receipt.CreatedAt) || now.Sub(receipt.CreatedAt) > Retention {
		return Reusable{}, ErrExpired
	}
	return Reusable{Receipt: receipt, Path: receiptDisplayPath(identitySHA256)}, nil
}

func RecordPassed(repositoryRoot string, identity Identity, duration time.Duration, artifacts []testartifact.Record) (Reusable, error) {
	return recordPassedAt(repositoryRoot, identity, duration, artifacts, Source{Kind: "local"}, time.Now().UTC())
}

func recordPassedAt(repositoryRoot string, identity Identity, duration time.Duration, artifacts []testartifact.Record, source Source, now time.Time) (Reusable, error) {
	identitySHA256, err := identity.Digest()
	if err != nil {
		return Reusable{}, err
	}
	if err := validateReceiptResult(duration, source, now); err != nil {
		return Reusable{}, err
	}
	receipt := Receipt{
		Version: Version, Identity: identity, IdentitySHA256: identitySHA256, Suite: identity.Suite.Name,
		DurationMillis: duration.Milliseconds(), Artifacts: append([]testartifact.Record{}, artifacts...),
		CreatedAt: now.UTC(), Source: source,
	}
	if err := validateReceipt(receipt); err != nil {
		return Reusable{}, err
	}
	receipt.SHA256, err = receiptDigest(receipt)
	if err != nil {
		return Reusable{}, err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return Reusable{}, err
	}
	data = append(data, '\n')
	directory, err := receiptDirectory(repositoryRoot, true)
	if err != nil {
		return Reusable{}, err
	}
	if err := writeReceiptFile(filepath.Join(directory, identitySHA256+".json"), data); err != nil {
		return Reusable{}, err
	}
	return Reusable{Receipt: receipt, Path: receiptDisplayPath(identitySHA256)}, nil
}

func validateReceiptResult(duration time.Duration, source Source, now time.Time) error {
	if duration < 0 || now.IsZero() || !validReceiptSource(source) {
		return fmt.Errorf("%w: receipt result is malformed", ErrInvalid)
	}
	return nil
}

func decodeReceipt(data []byte) (Receipt, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	receipt := Receipt{}
	if err := decoder.Decode(&receipt); err != nil {
		return Receipt{}, fmt.Errorf("%w: receipt JSON is malformed", ErrInvalid)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Receipt{}, fmt.Errorf("%w: receipt JSON has trailing data", ErrInvalid)
	}
	recorded := receipt.SHA256
	receipt.SHA256 = ""
	digest, err := receiptDigest(receipt)
	if err != nil || digest != recorded {
		return Receipt{}, fmt.Errorf("%w: receipt digest does not match", ErrInvalid)
	}
	receipt.SHA256 = recorded
	if err := validateReceipt(receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func validateReceipt(receipt Receipt) error {
	identitySHA256, err := receipt.Identity.Digest()
	if err != nil || !validReceiptHeader(receipt, identitySHA256) || !validReceiptSource(receipt.Source) {
		return fmt.Errorf("%w: receipt fields are malformed", ErrInvalid)
	}
	for _, artifact := range receipt.Artifacts {
		if artifact.Suite != receipt.Suite || artifact.Path == "" || !validSHA256(artifact.SHA256) || artifact.Size < 0 {
			return fmt.Errorf("%w: receipt artifact is malformed", ErrInvalid)
		}
	}
	return nil
}

func validReceiptHeader(receipt Receipt, identitySHA256 string) bool {
	return receipt.Version == Version && receipt.IdentitySHA256 == identitySHA256 &&
		receipt.Suite == receipt.Identity.Suite.Name && receipt.DurationMillis >= 0 &&
		receipt.Artifacts != nil && !receipt.CreatedAt.IsZero()
}

func validReceiptSource(source Source) bool {
	return source.Kind == "local" && source.BundleSHA256 == "" ||
		source.Kind == "ci" && validSHA256(source.BundleSHA256)
}

func receiptDigest(receipt Receipt) (string, error) {
	receipt.SHA256 = ""
	data, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func receiptDirectory(repositoryRoot string, create bool) (string, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if resolved != root {
		root = resolved
	}
	current := root
	for _, name := range []string{rootDirectory, storeDirectory} {
		current = filepath.Join(current, name)
		if err := ensureReceiptDirectory(current, create); err != nil {
			return "", err
		}
	}
	return current, nil
}

func ensureReceiptDirectory(path string, create bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create test receipt directory: %w", err)
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return ErrMissing
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: test receipt directory is unsafe", ErrInvalid)
	}
	return nil
}

func readReceiptFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maximumReceiptBytes {
		return nil, fmt.Errorf("%w: receipt file is unsafe", ErrInvalid)
	}
	return os.ReadFile(path)
}

func writeReceiptFile(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".receipt-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	_, writeErr := temporary.Write(data)
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return replaceReceipt(temporaryPath, path)
}

func receiptDisplayPath(identitySHA256 string) string {
	return rootDirectory + "/" + storeDirectory + "/" + identitySHA256 + ".json"
}

func sameReceipt(left, right Receipt) bool {
	return left.SHA256 != "" && left.SHA256 == right.SHA256
}
