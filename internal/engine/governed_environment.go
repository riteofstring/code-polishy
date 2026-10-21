package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const GovernedEnvironmentVersion = 1

type GovernedEnvironmentReceipt struct {
	Version     int                       `json:"version"`
	PathEntries []string                  `json:"path_entries"`
	Tools       []repository.GovernedTool `json:"tools"`
	Digest      string                    `json:"digest"`
}

func FreezeGovernedEnvironment(repo repository.Repository) (GovernedEnvironmentReceipt, error) {
	environment := repo.CommandEnvironment()
	receipt := GovernedEnvironmentReceipt{
		Version:     GovernedEnvironmentVersion,
		PathEntries: append([]string{}, environment.PathEntries...),
		Tools:       append([]repository.GovernedTool{}, environment.Tools...),
	}
	if err := validateGovernedEnvironment(receipt); err != nil {
		return GovernedEnvironmentReceipt{}, err
	}
	var err error
	receipt.Digest, err = governedEnvironmentDigest(receipt)
	return receipt, err
}

func MarshalGovernedEnvironment(receipt GovernedEnvironmentReceipt) ([]byte, error) {
	if err := validateGovernedEnvironment(receipt); err != nil {
		return nil, err
	}
	digest, err := governedEnvironmentDigest(receipt)
	if err != nil {
		return nil, err
	}
	if receipt.Digest != digest {
		return nil, errors.New("governed environment digest does not match its content")
	}
	return json.MarshalIndent(receipt, "", "  ")
}

func validateGovernedEnvironment(receipt GovernedEnvironmentReceipt) error {
	if receipt.Version != GovernedEnvironmentVersion || len(receipt.PathEntries) == 0 || len(receipt.Tools) == 0 {
		return errors.New("governed environment has an invalid version or empty inventory")
	}
	if err := validateGovernedPaths(receipt.PathEntries); err != nil {
		return err
	}
	return validateGovernedTools(receipt.Tools)
}

func validateGovernedPaths(paths []string) error {
	seenPaths := map[string]bool{}
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") || seenPaths[path] {
			return fmt.Errorf("governed environment contains an invalid path entry %q", path)
		}
		seenPaths[path] = true
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("governed command directory is unavailable: %s", path)
		}
	}
	return nil
}

func validateGovernedTools(tools []repository.GovernedTool) error {
	seenTools := map[string]bool{}
	for _, tool := range tools {
		if tool.Name == "" || tool.Version == "" || !filepath.IsAbs(tool.Path) || strings.ContainsAny(tool.Name+tool.Path+tool.Version, "\r\n\x00") || seenTools[tool.Name] {
			return fmt.Errorf("governed environment contains an invalid tool record for %q", tool.Name)
		}
		seenTools[tool.Name] = true
		info, err := os.Stat(tool.Path)
		if err != nil || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			return fmt.Errorf("required locked tool %q is unavailable at %s", tool.Name, tool.Path)
		}
	}
	return nil
}

func governedEnvironmentDigest(receipt GovernedEnvironmentReceipt) (string, error) {
	receipt.Digest = ""
	data, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
