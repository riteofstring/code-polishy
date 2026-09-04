package testartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func writeManifest(directory string, value manifest) error {
	value.SHA256 = ""
	digest, _, err := manifestDigest(value)
	if err != nil {
		return err
	}
	value.SHA256 = digest
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".owner-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
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
	if err := replaceManifest(temporaryPath, filepath.Join(directory, manifestFilename)); err != nil {
		return fmt.Errorf("publish test artifact ownership manifest: %w", err)
	}
	return nil
}

func readManifest(directory string) (manifest, error) {
	path := filepath.Join(directory, manifestFilename)
	data, err := readManifestData(path)
	if err != nil {
		return manifest{}, err
	}
	value, err := decodeManifest(data)
	if err != nil {
		return manifest{}, err
	}
	if err := validateManifest(value); err != nil {
		return manifest{}, err
	}
	recorded := value.SHA256
	value.SHA256 = ""
	digest, _, err := manifestDigest(value)
	if err != nil || recorded != digest {
		return manifest{}, errors.New("test artifact ownership manifest digest is invalid")
	}
	value.SHA256 = recorded
	return value, nil
}

func readManifestData(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 16<<10 {
		return nil, errors.New("test artifact ownership manifest is unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func decodeManifest(data []byte) (manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	value := manifest{}
	if err := decoder.Decode(&value); err != nil {
		return manifest{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return manifest{}, errors.New("test artifact ownership manifest has trailing data")
	}
	return value, nil
}

func validateManifest(value manifest) error {
	if value.Version != manifestVersion || !executionIDPattern.MatchString(value.ExecutionID) ||
		value.Status != "active" && value.Status != "completed" || value.CreatedAt.IsZero() {
		return errors.New("test artifact ownership manifest is invalid")
	}
	if value.Status == "active" && !value.CompletedAt.IsZero() || value.Status == "completed" && value.CompletedAt.IsZero() {
		return errors.New("test artifact ownership manifest state is invalid")
	}
	return nil
}

func manifestDigest(value manifest) (string, []byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), data, nil
}
