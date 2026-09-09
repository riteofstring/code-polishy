package pack

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func retainUnavailableClaims(root string, selection policy.PackSelection, resolution *Resolution) {
	receipt, err := readReceipt(root)
	if err != nil || receipt.Name != selection.Name || receipt.Version != selection.Version || receipt.Digest != selection.Digest {
		return
	}
	entries, err := receiptEntries(receipt)
	if err != nil {
		return
	}
	entry, found := entries[ManifestFilename]
	if !found {
		return
	}
	data, err := authenticatedManifestData(root, entry)
	if err != nil {
		return
	}
	manifest, err := ParseManifest(data, ManifestFilename)
	if err == nil {
		compileManifest(root, selection, manifest, resolution)
	}
}

func authenticatedManifestData(root string, entry ReceiptEntry) ([]byte, error) {
	info, err := os.Lstat(filepath.Join(root, ManifestFilename))
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumPackFileBytes {
		return nil, errors.New("unavailable manifest is not a bounded regular file")
	}
	data, err := os.ReadFile(filepath.Join(root, ManifestFilename))
	if err != nil || inputDigest(data) != entry.SHA256 {
		return nil, errors.New("unavailable manifest does not match selected receipt")
	}
	return data, nil
}
