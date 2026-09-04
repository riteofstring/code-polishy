package testartifact

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func validateDeclared(directory, suite string, declarations []policy.TestArtifact) ([]Record, error) {
	records := []Record{}
	for _, declaration := range declarations {
		record, present, err := validateOne(directory, suite, declaration)
		if err != nil {
			return records, err
		}
		if present {
			records = append(records, record)
		}
	}
	return records, nil
}

func validateOne(directory, suite string, declaration policy.TestArtifact) (Record, bool, error) {
	path, err := containedArtifactPath(directory, declaration.Path)
	if err != nil {
		return Record{}, false, err
	}
	info, present, err := regularArtifactInfo(path, declaration)
	if err != nil || !present {
		return Record{}, false, err
	}
	if err := validateArtifactParents(directory, declaration.Path); err != nil {
		return Record{}, false, err
	}
	if info.Size() > MaximumArtifactBytes {
		return Record{}, false, fmt.Errorf("test artifact %q exceeds %d bytes", declaration.Path, MaximumArtifactBytes)
	}
	data, digest, err := readArtifact(path, declaration.Path)
	if err != nil {
		return Record{}, false, err
	}
	if err := validateXML(data, declaration.Type); err != nil {
		return Record{}, false, fmt.Errorf("test artifact %q: %w", declaration.Path, err)
	}
	return Record{
		Suite: suite, Path: filepath.Base(directory) + "/" + declaration.Path, Type: declaration.Type,
		Required: declaration.Required, Size: int64(len(data)), SHA256: digest,
	}, true, nil
}

func regularArtifactInfo(path string, declaration policy.TestArtifact) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && !declaration.Required {
		return nil, false, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("required %s artifact %q is missing", declaration.Type, declaration.Path)
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("test artifact %q is linked, special, or unavailable", declaration.Path)
	}
	return info, true, nil
}

func readArtifact(path, declaredPath string) ([]byte, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	digest := sha256.New()
	limited := &io.LimitedReader{R: file, N: MaximumArtifactBytes + 1}
	data, err := io.ReadAll(io.TeeReader(limited, digest))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > MaximumArtifactBytes {
		return nil, "", fmt.Errorf("test artifact %q exceeds %d bytes", declaredPath, MaximumArtifactBytes)
	}
	return data, hex.EncodeToString(digest.Sum(nil)), nil
}

func containedArtifactPath(directory, relative string) (string, error) {
	if filepath.IsAbs(relative) || strings.Contains(relative, "\\") {
		return "", errors.New("test artifact path is not contained")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("test artifact path is not contained")
	}
	return filepath.Join(directory, clean), nil
}

func validateArtifactParents(directory, relative string) error {
	current := directory
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("test artifact parent %q is linked or unavailable", part)
		}
	}
	return nil
}

func validateXML(data []byte, artifactType string) error {
	lower := strings.ToLower(string(data))
	if strings.Contains(lower, "<!doctype") || strings.Contains(lower, "<!entity") {
		return errors.New("document types and entities are forbidden in XML")
	}
	root, err := documentRoot(data)
	if err != nil {
		return err
	}
	return validateDocumentRoot(root, artifactType)
}

func documentRoot(data []byte) (string, error) {
	decoder := xml.NewDecoder(bufio.NewReader(strings.NewReader(string(data))))
	decoder.Strict = true
	root := ""
	depth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("malformed XML: %w", err)
		}
		switch typed := token.(type) {
		case xml.Directive:
			return "", errors.New("document directives are forbidden")
		case xml.StartElement:
			if depth == 0 {
				root = typed.Name.Local
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	if depth != 0 || root == "" {
		return "", errors.New("document has no complete XML root")
	}
	return root, nil
}

func validateDocumentRoot(root, artifactType string) error {
	if artifactType == "junit" && root != "testsuite" && root != "testsuites" {
		return fmt.Errorf("invalid JUnit root %q; want testsuite or testsuites", root)
	}
	if artifactType == "cobertura" && root != "coverage" {
		return fmt.Errorf("invalid Cobertura root %q; want coverage", root)
	}
	return nil
}
