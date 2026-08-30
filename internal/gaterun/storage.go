package gaterun

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	reportsDirectory      = ".code-polishy-reports"
	reportFilename        = "report.json"
	logsDirectory         = "logs"
	receiptsDirectory     = "receipts"
	maximumReportBytes    = 8 << 20
	maximumReceiptBytes   = 64 << 10
	maximumLogHeaderBytes = 32 << 10
)

func newRunDirectory(repositoryRoot string, identity Identity) (string, string, error) {
	runSHA256, err := identity.Digest()
	if err != nil {
		return "", "", err
	}
	return managedRunDirectory(repositoryRoot, identity.Gate, runSHA256, true)
}

func existingRunDirectory(repositoryRoot string, identity Identity) (string, string, error) {
	runSHA256, err := identity.Digest()
	if err != nil {
		return "", "", err
	}
	return managedRunDirectory(repositoryRoot, identity.Gate, runSHA256, false)
}

func managedRunDirectory(repositoryRoot string, gate GateKind, runSHA256 string, create bool) (string, string, error) {
	if !validGate(gate) || !validSHA256(runSHA256) {
		return "", "", fmt.Errorf("%w: gate run path is invalid", ErrInvalidInput)
	}
	root, err := resolveRepositoryRoot(repositoryRoot)
	if err != nil {
		return "", "", err
	}
	directory, err := secureChildDirectory(root, []string{reportsDirectory, string(gate), runSHA256}, create)
	if err != nil {
		return "", "", err
	}
	if create {
		if err := os.Chmod(filepath.Join(root, reportsDirectory, string(gate)), 0o700); err != nil {
			return "", "", operational("restrict gate artifact directory", err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return "", "", operational("restrict gate run directory", err)
		}
	}
	return root, directory, nil
}

func resolveRepositoryRoot(repositoryRoot string) (string, error) {
	if strings.TrimSpace(repositoryRoot) == "" {
		return "", fmt.Errorf("%w: repository root is required", ErrInvalidInput)
	}
	abs, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", operational("resolve gate repository root", err)
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", operational("resolve gate repository root", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", operational("inspect gate repository root", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: repository root is not a directory", ErrInvalidInput)
	}
	return root, nil
}

func secureChildDirectory(root string, components []string, create bool) (string, error) {
	current := root
	for _, component := range components {
		if !validArtifactComponent(component) {
			return "", fmt.Errorf("%w: gate artifact path component is invalid", ErrInvalidInput)
		}
		current = filepath.Join(current, component)
		if !pathInside(root, current) {
			return "", fmt.Errorf("%w: gate artifact path escapes the repository", ErrInvalidArtifact)
		}
		if err := ensureDirectory(current, create); err != nil {
			return "", err
		}
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil || !pathInside(root, resolved) {
			return "", fmt.Errorf("%w: gate artifact directory resolves outside the repository", ErrInvalidArtifact)
		}
	}
	return current, nil
}

func ensureDirectory(path string, create bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return fmt.Errorf("%w: gate artifact directory is unavailable", ErrMissingArtifact)
		}
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return operational("create gate artifact directory", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return operational("inspect gate artifact directory", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: gate artifact directory is not a contained directory", ErrInvalidArtifact)
	}
	return nil
}

func secureRunSubdirectory(root, directory, name string, create bool) (string, error) {
	if !pathInside(root, directory) || !validArtifactComponent(name) {
		return "", fmt.Errorf("%w: gate artifact directory is invalid", ErrInvalidArtifact)
	}
	if err := ensureDirectory(directory, false); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || !pathInside(root, resolved) {
		return "", fmt.Errorf("%w: gate run directory resolves outside the repository", ErrInvalidArtifact)
	}
	child := filepath.Join(directory, name)
	if !pathInside(directory, child) {
		return "", fmt.Errorf("%w: gate artifact path escapes its run directory", ErrInvalidArtifact)
	}
	if err := ensureDirectory(child, create); err != nil {
		return "", err
	}
	if create {
		if err := os.Chmod(child, 0o700); err != nil {
			return "", operational("restrict gate artifact subdirectory", err)
		}
	}
	return child, nil
}

func validArtifactComponent(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00")
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func artifactFilePath(root, directory, relative string) (string, string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") {
		return "", "", fmt.Errorf("%w: gate artifact file path is invalid", ErrInvalidInput)
	}
	path := filepath.Join(directory, filepath.FromSlash(relative))
	if !pathInside(directory, path) || !pathInside(root, path) {
		return "", "", fmt.Errorf("%w: gate artifact file path escapes its run directory", ErrInvalidArtifact)
	}
	display, err := filepath.Rel(root, path)
	if err != nil {
		return "", "", operational("render gate artifact path", err)
	}
	return path, filepath.ToSlash(display), nil
}

func writeArtifactAtomic(path string, data []byte) error {
	if err := rejectUnsafeFileTarget(path); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".gaterun-*")
	if err != nil {
		return operational("create gate artifact", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return operational("restrict gate artifact", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return operational("write gate artifact", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return operational("sync gate artifact", err)
	}
	if err := temporary.Close(); err != nil {
		return operational("close gate artifact", err)
	}
	if err := replaceAtomicFile(temporaryPath, path); err != nil {
		return operational("replace gate artifact", err)
	}
	return nil
}

func rejectUnsafeFileTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return operational("inspect gate artifact", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: gate artifact target is not a restrictive regular file", ErrInvalidArtifact)
	}
	return nil
}

func readArtifact(path string, maximum int, label string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s is missing", ErrMissingArtifact, label)
	}
	if err != nil {
		return nil, operational("inspect "+label, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s is not a restrictive regular file", ErrInvalidArtifact, label)
	}
	if info.Size() > int64(maximum) {
		return nil, fmt.Errorf("%w: %s exceeds its size limit", ErrInvalidArtifact, label)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, operational("open "+label, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, operational("read "+label, err)
	}
	if len(data) > maximum {
		return nil, fmt.Errorf("%w: %s grew beyond its size limit", ErrInvalidArtifact, label)
	}
	return data, nil
}

func marshalArtifact(value any, operation string) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, operational(operation, err)
	}
	return append(data, '\n'), nil
}

func decodeStrict(data []byte, value any, label string) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return fmt.Errorf("%w: %s JSON is invalid: %v", ErrInvalidArtifact, label, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: %s JSON is invalid: %v", ErrInvalidArtifact, label, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("%w: %s JSON is invalid: %v", ErrInvalidArtifact, label, err)
		}
		return fmt.Errorf("%w: %s JSON has more than one value", ErrInvalidArtifact, label)
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("JSON has more than one value")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		return consumeJSONObject(decoder)
	case '[':
		return consumeJSONArray(decoder)
	default:
		return errors.New("JSON has an unexpected delimiter")
	}
}

func consumeJSONObject(decoder *json.Decoder) error {
	seen := map[string]bool{}
	for decoder.More() {
		if err := consumeJSONObjectEntry(decoder, seen); err != nil {
			return err
		}
	}
	return consumeJSONEnd(decoder, '}')
}

func consumeJSONObjectEntry(decoder *json.Decoder, seen map[string]bool) error {
	key, err := decoder.Token()
	if err != nil {
		return err
	}
	name, ok := key.(string)
	if !ok || seen[name] {
		return errors.New("JSON object has duplicate or invalid key")
	}
	seen[name] = true
	return consumeJSONValue(decoder)
}

func consumeJSONArray(decoder *json.Decoder) error {
	for decoder.More() {
		if err := consumeJSONValue(decoder); err != nil {
			return err
		}
	}
	return consumeJSONEnd(decoder, ']')
}

func consumeJSONEnd(decoder *json.Decoder, expected rune) error {
	end, err := decoder.Token()
	if err != nil || end != json.Delim(expected) {
		return errors.New("JSON container is not closed")
	}
	return nil
}
