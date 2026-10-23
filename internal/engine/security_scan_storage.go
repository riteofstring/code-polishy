package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/durablefile"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/strictjson"
)

const maximumSecurityEvidenceBytes = 32 << 20

func readSecurityEvidence(repo repository.Repository, path string, target any) (bool, error) {
	data, err := repo.ReadContainedFile(path, maximumSecurityEvidenceBytes)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := strictjson.Decode(data, target); err != nil {
		return false, fmt.Errorf("invalid security evidence %s: %w", path, err)
	}
	return true, nil
}

func writeSecurityEvidence(repo repository.Repository, path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maximumSecurityEvidenceBytes {
		return fmt.Errorf("security evidence exceeds its %d byte bound", maximumSecurityEvidenceBytes)
	}
	full := filepath.Join(repo.Root, filepath.FromSlash(path))
	if err := ensureManagedReportDirectory(repo.Root, filepath.Dir(full)); err != nil {
		return err
	}
	if err := validateReportOutputFile(full); err != nil {
		return err
	}
	return durablefile.Write(full, append(data, '\n'), 0o600)
}
