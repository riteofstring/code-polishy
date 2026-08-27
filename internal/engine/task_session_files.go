package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func writeNULArtifact(path string, values []string) (string, error) {
	var data bytes.Buffer
	for _, value := range values {
		if strings.ContainsRune(value, '\x00') {
			return "", errors.New("task-session authority values must contain no NUL bytes")
		}
		data.WriteString(value)
		data.WriteByte(0)
	}
	if err := writeAtomic(path, data.Bytes(), 0o600); err != nil {
		return "", err
	}
	digest := sha256.Sum256(data.Bytes())
	return hex.EncodeToString(digest[:]), nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".code-polishy-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceAtomicFile(temporaryPath, path)
}

func copyRegularFile(source, destination string, mode os.FileMode) error {
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func digestFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func replaceEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := environment[:0]
	for _, entry := range environment {
		candidate := entry
		if runtime.GOOS == "windows" {
			candidate = strings.ToUpper(candidate)
			prefix = strings.ToUpper(prefix)
		}
		if !strings.HasPrefix(candidate, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, key+"="+value)
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func samePath(first, second string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(first), filepath.Clean(second))
	}
	return filepath.Clean(first) == filepath.Clean(second)
}

func writerOrDiscard(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return writer
}
