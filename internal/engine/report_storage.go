package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const maximumManagedReportBytes = 64 << 20

func OpenReportCustodian(root string) (*Engine, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("report repository root is not a directory")
	}
	return &Engine{Repository: repository.Repository{Root: root}}, nil
}

func (engine *Engine) FinalizeReport(command string, report Report) (Report, error) {
	if !validReportCommand(command) {
		return Report{}, fmt.Errorf("invalid report command %q", command)
	}
	report.Command = command
	report.ReportPath = ""
	report = engine.normalizeReport(report)
	identityData, err := json.Marshal(report)
	if err != nil {
		return Report{}, fmt.Errorf("encode report identity: %w", err)
	}
	digest := sha256.Sum256(identityData)
	relative := filepath.ToSlash(filepath.Join(".code-polishy-reports", command, hex.EncodeToString(digest[:]), "report.json"))
	report.ReportPath = relative
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return Report{}, fmt.Errorf("encode managed report: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maximumManagedReportBytes {
		return Report{}, fmt.Errorf("managed report exceeds %d bytes", maximumManagedReportBytes)
	}
	if err := validateCanonicalReport(data); err != nil {
		return Report{}, err
	}
	directory := filepath.Join(engine.Repository.Root, filepath.FromSlash(filepath.Dir(relative)))
	if err := ensureManagedReportDirectory(engine.Repository.Root, directory); err != nil {
		return Report{}, err
	}
	if err := writeAtomic(filepath.Join(engine.Repository.Root, filepath.FromSlash(relative)), data, 0o600); err != nil {
		return Report{}, fmt.Errorf("write managed report: %w", err)
	}
	return report, nil
}

func validReportCommand(command string) bool {
	if command == "" {
		return false
	}
	for index, value := range command {
		if value >= 'a' && value <= 'z' || index > 0 && value >= '0' && value <= '9' || index > 0 && value == '-' {
			continue
		}
		return false
	}
	return true
}

func ensureManagedReportDirectory(root, directory string) error {
	root, err := resolvedReportRoot(root)
	if err != nil {
		return err
	}
	reports := filepath.Join(root, ".code-polishy-reports")
	if err := validateManagedReportRoot(reports); err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create managed report directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("managed report directory escapes the repository")
	}
	return nil
}

func resolvedReportRoot(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

func validateManagedReportRoot(reports string) error {
	info, err := os.Lstat(reports)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed report root is not a real directory")
	}
	return nil
}
