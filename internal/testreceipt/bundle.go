package testreceipt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	bundleVersion      = 1
	maximumBundleBytes = 64 << 20
	ciBundleFilename   = "ci-bundle.json"
)

type exportBundle struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Receipts  []Receipt `json:"receipts"`
}

type importedBundle struct {
	Version      int       `json:"version"`
	SourceSHA256 string    `json:"source_sha256"`
	ImportedAt   time.Time `json:"imported_at"`
	Receipts     []Receipt `json:"receipts"`
	SHA256       string    `json:"sha256"`
}

func ExportBundle(repositoryRoot, destination string) (string, int, error) {
	return exportBundleAt(repositoryRoot, destination, time.Now().UTC())
}

func exportBundleAt(repositoryRoot, destination string, now time.Time) (string, int, error) {
	receipts, err := localReceipts(repositoryRoot, now)
	if err != nil {
		return "", 0, err
	}
	if len(receipts) == 0 {
		return "", 0, ErrMissing
	}
	bundle := exportBundle{Version: bundleVersion, CreatedAt: now.UTC(), Receipts: receipts}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return "", 0, err
	}
	data = append(data, '\n')
	if err := writeExternalFile(destination, data); err != nil {
		return "", 0, err
	}
	return digest(data), len(receipts), nil
}

func ImportBundle(repositoryRoot, source, expectedSHA256 string) (int, error) {
	return importBundleAt(repositoryRoot, source, expectedSHA256, time.Now().UTC())
}

func importBundleAt(repositoryRoot, source, expectedSHA256 string, now time.Time) (int, error) {
	if !validSHA256(expectedSHA256) {
		return 0, fmt.Errorf("%w: bundle digest is malformed", ErrInvalid)
	}
	data, err := readExternalFile(source)
	if err != nil {
		return 0, err
	}
	if digest(data) != expectedSHA256 {
		return 0, fmt.Errorf("%w: bundle digest does not match", ErrInvalid)
	}
	bundle, err := decodeExportBundle(data, now)
	if err != nil {
		return 0, err
	}
	imported, err := importedFromExport(bundle, expectedSHA256, now)
	if err != nil {
		return 0, err
	}
	return storeImportedBundle(repositoryRoot, imported)
}

func importedFromExport(bundle exportBundle, sourceSHA256 string, now time.Time) (importedBundle, error) {
	imported := importedBundle{
		Version: bundleVersion, SourceSHA256: sourceSHA256, ImportedAt: now.UTC(),
		Receipts: make([]Receipt, 0, len(bundle.Receipts)),
	}
	for _, receipt := range bundle.Receipts {
		receipt.Source = Source{Kind: "ci", BundleSHA256: sourceSHA256}
		receipt.SHA256 = ""
		digest, err := receiptDigest(receipt)
		if err != nil {
			return importedBundle{}, err
		}
		receipt.SHA256 = digest
		if err := validateReceiptWithDigest(receipt); err != nil {
			return importedBundle{}, err
		}
		imported.Receipts = append(imported.Receipts, receipt)
	}
	var err error
	imported.SHA256, err = importedBundleDigest(imported)
	if err != nil {
		return importedBundle{}, err
	}
	return imported, nil
}

func storeImportedBundle(repositoryRoot string, imported importedBundle) (int, error) {
	encoded, err := json.MarshalIndent(imported, "", "  ")
	if err != nil {
		return 0, err
	}
	encoded = append(encoded, '\n')
	directory, err := receiptDirectory(repositoryRoot, true)
	if err != nil {
		return 0, err
	}
	if err := writeReceiptFile(filepath.Join(directory, ciBundleFilename), encoded); err != nil {
		return 0, err
	}
	return len(imported.Receipts), nil
}

func localReceipts(repositoryRoot string, now time.Time) ([]Receipt, error) {
	directory, err := receiptDirectory(repositoryRoot, false)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	receipts := []Receipt{}
	for _, entry := range entries {
		receipt, include, err := eligibleLocalReceipt(directory, entry, now)
		if err != nil {
			return nil, err
		}
		if include {
			receipts = append(receipts, receipt)
		}
	}
	sort.Slice(receipts, func(left, right int) bool { return receipts[left].IdentitySHA256 < receipts[right].IdentitySHA256 })
	return receipts, nil
}

func eligibleLocalReceipt(directory string, entry os.DirEntry, now time.Time) (Receipt, bool, error) {
	name := entry.Name()
	if entry.IsDir() || len(name) != 69 || !strings.HasSuffix(name, ".json") || !validSHA256(strings.TrimSuffix(name, ".json")) {
		return Receipt{}, false, nil
	}
	data, err := readReceiptFile(filepath.Join(directory, name))
	if err != nil {
		return Receipt{}, false, err
	}
	receipt, err := decodeReceipt(data)
	if err != nil {
		return Receipt{}, false, err
	}
	if receipt.Source.Kind != "local" || now.Before(receipt.CreatedAt) || now.Sub(receipt.CreatedAt) > Retention {
		return Receipt{}, false, nil
	}
	return receipt, true, nil
}

func decodeExportBundle(data []byte, now time.Time) (exportBundle, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	bundle := exportBundle{}
	if err := decoder.Decode(&bundle); err != nil {
		return exportBundle{}, fmt.Errorf("%w: bundle JSON is malformed", ErrInvalid)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return exportBundle{}, fmt.Errorf("%w: bundle JSON has trailing data", ErrInvalid)
	}
	if !validExportBundleHeader(bundle) {
		return exportBundle{}, fmt.Errorf("%w: bundle fields are malformed", ErrInvalid)
	}
	previous := ""
	for _, receipt := range bundle.Receipts {
		if !validExportReceipt(receipt, previous, now) {
			return exportBundle{}, fmt.Errorf("%w: bundle receipt is invalid or expired", ErrInvalid)
		}
		previous = receipt.IdentitySHA256
	}
	return bundle, nil
}

func validExportBundleHeader(bundle exportBundle) bool {
	return bundle.Version == bundleVersion && !bundle.CreatedAt.IsZero() && len(bundle.Receipts) > 0 && bundle.Receipts != nil
}

func validExportReceipt(receipt Receipt, previous string, now time.Time) bool {
	return validateReceiptWithDigest(receipt) == nil && receipt.Source.Kind == "local" &&
		receipt.IdentitySHA256 > previous && !now.Before(receipt.CreatedAt) && now.Sub(receipt.CreatedAt) <= Retention
}

func loadImportedReceipt(repositoryRoot, identitySHA256 string, now time.Time) (Reusable, error) {
	directory, err := receiptDirectory(repositoryRoot, false)
	if err != nil {
		return Reusable{}, err
	}
	path := filepath.Join(directory, ciBundleFilename)
	data, err := readBoundedRegularFile(path, maximumBundleBytes)
	if err != nil {
		return Reusable{}, err
	}
	bundle, err := decodeImportedBundle(data)
	if err != nil {
		return Reusable{}, err
	}
	for _, receipt := range bundle.Receipts {
		if receipt.IdentitySHA256 != identitySHA256 {
			continue
		}
		if now.Before(receipt.CreatedAt) || now.Sub(receipt.CreatedAt) > Retention {
			return Reusable{}, ErrExpired
		}
		return Reusable{Receipt: receipt, Path: receiptDisplayBundlePath()}, nil
	}
	return Reusable{}, ErrMissing
}

func decodeImportedBundle(data []byte) (importedBundle, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	bundle := importedBundle{}
	if err := decoder.Decode(&bundle); err != nil {
		return importedBundle{}, fmt.Errorf("%w: imported bundle JSON is malformed", ErrInvalid)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return importedBundle{}, fmt.Errorf("%w: imported bundle JSON has trailing data", ErrInvalid)
	}
	digestValue, err := importedBundleDigest(bundle)
	if err != nil || !validImportedBundleHeader(bundle, digestValue) {
		return importedBundle{}, fmt.Errorf("%w: imported bundle fields are malformed", ErrInvalid)
	}
	previous := ""
	for _, receipt := range bundle.Receipts {
		if !validImportedReceipt(receipt, bundle.SourceSHA256, previous) {
			return importedBundle{}, fmt.Errorf("%w: imported bundle receipt is invalid", ErrInvalid)
		}
		previous = receipt.IdentitySHA256
	}
	return bundle, nil
}

func validImportedBundleHeader(bundle importedBundle, digestValue string) bool {
	return bundle.Version == bundleVersion && validSHA256(bundle.SourceSHA256) && !bundle.ImportedAt.IsZero() &&
		len(bundle.Receipts) > 0 && bundle.SHA256 == digestValue
}

func validImportedReceipt(receipt Receipt, sourceSHA256, previous string) bool {
	return validateReceiptWithDigest(receipt) == nil && receipt.Source.Kind == "ci" &&
		receipt.Source.BundleSHA256 == sourceSHA256 && receipt.IdentitySHA256 > previous
}

func importedBundleDigest(bundle importedBundle) (string, error) {
	bundle.SHA256 = ""
	data, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func validateReceiptWithDigest(receipt Receipt) error {
	recorded := receipt.SHA256
	receipt.SHA256 = ""
	value, err := receiptDigest(receipt)
	if err != nil || recorded != value {
		return fmt.Errorf("%w: receipt digest does not match", ErrInvalid)
	}
	receipt.SHA256 = recorded
	return validateReceipt(receipt)
}

func readExternalFile(path string) ([]byte, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: bundle source must be an absolute path", ErrInvalid)
	}
	return readBoundedRegularFile(path, maximumBundleBytes)
}

func readBoundedRegularFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > limit {
		return nil, fmt.Errorf("%w: bundle file is unsafe", ErrInvalid)
	}
	return os.ReadFile(path)
}

func writeExternalFile(path string, data []byte) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%w: bundle destination must be an absolute path", ErrInvalid)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func receiptDisplayBundlePath() string {
	return rootDirectory + "/" + storeDirectory + "/" + ciBundleFilename
}
