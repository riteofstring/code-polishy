package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func JSONReport(report Report) ([]byte, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := validateCanonicalReport(data); err != nil {
		return nil, err
	}
	return data, nil
}

func (engine *Engine) WriteReportOutput(path string, data []byte) (string, error) {
	normalized, destination, err := engine.reportOutputDestination(path)
	if err != nil {
		return "", err
	}
	if err := validateReportOutputFile(destination); err != nil {
		return "", err
	}
	if err := validateReportOutputParent(engine.Repository.Root, destination); err != nil {
		return "", err
	}
	if err := writeAtomic(destination, data, 0o600); err != nil {
		return "", fmt.Errorf("write report output: %w", err)
	}
	return normalized, nil
}

func (engine *Engine) reportOutputDestination(path string) (string, string, error) {
	normalized, err := engine.Repository.NormalizePath(path)
	if err != nil {
		return "", "", fmt.Errorf("report output path: %w", err)
	}
	return normalized, filepath.Join(engine.Repository.Root, filepath.FromSlash(normalized)), nil
}

func validateReportOutputFile(destination string) error {
	if info, statErr := os.Lstat(destination); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("report output must be a regular non-symbolic-link file")
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return nil
}

func validateReportOutputParent(repositoryRoot, destination string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return fmt.Errorf("report output parent: %w", err)
	}
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("report output parent escapes the repository")
	}
	return nil
}
