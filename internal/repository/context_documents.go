package repository

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const MaximumContextDocumentBytes = 1 << 20

const MaximumContextDocumentSetBytes = 8 << 20

type ContextDocument struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Content string `json:"content"`
}

func (repo Repository) ReadContextDocument(path string) (ContextDocument, error) {
	if !policy.IsMarkdownPath(path) {
		return ContextDocument{}, fmt.Errorf("context document must be Markdown")
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return ContextDocument{}, err
	}
	defer root.Close()
	info, err := repo.containedRegularFileInfo(root, path)
	if err != nil {
		return ContextDocument{}, err
	}
	if info.Size() > MaximumContextDocumentBytes {
		return ContextDocument{}, fmt.Errorf("context document exceeds %d bytes", MaximumContextDocumentBytes)
	}
	data, err := readContainedFile(root, path, info, MaximumContextDocumentBytes)
	if err != nil {
		return ContextDocument{}, err
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 || len(bytes.TrimSpace(data)) == 0 {
		return ContextDocument{}, fmt.Errorf("context document must contain nonempty UTF-8 text without NUL bytes")
	}
	digest := sha256.Sum256(data)
	return ContextDocument{Path: path, SHA256: hex.EncodeToString(digest[:]), Content: string(data)}, nil
}
