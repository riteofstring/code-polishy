package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/schema"
)

const maximumPackMigrationPlanBytes = 64 << 20
const maximumPackMigrationConfigBytes = 8 << 20

func writePackMigrationPlan(repoRoot string, plan PackMigrationPlan, backup []byte) (string, error) {
	planData, err := renderPackMigrationPlan(plan)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(repoRoot, filepath.FromSlash(PackMigrationDirectory), plan.MigrationID)
	if err := ensureManagedReportDirectory(repoRoot, directory); err != nil {
		return "", err
	}
	backupPath := filepath.Join(repoRoot, filepath.FromSlash(plan.BackupPath))
	planPath := filepath.Join(directory, "plan.json")
	if err := writePackMigrationArtifact(backupPath, backup); err != nil {
		return "", fmt.Errorf("write pack migration backup: %w", err)
	}
	if err := writePackMigrationArtifact(planPath, planData); err != nil {
		return "", fmt.Errorf("write pack migration plan: %w", err)
	}
	stored, err := readPackMigrationPlan(repoRoot, filepath.ToSlash(filepath.Join(PackMigrationDirectory, plan.MigrationID, "plan.json")))
	if err != nil || stored.MigrationID != plan.MigrationID {
		if err == nil {
			err = errors.New("pack migration plan changed during publication")
		}
		return "", err
	}
	return filepath.ToSlash(filepath.Join(PackMigrationDirectory, plan.MigrationID, "plan.json")), nil
}

func writePackMigrationArtifact(destination string, data []byte) error {
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("managed migration artifact must be a regular file")
		}
		existing, err := os.ReadFile(destination)
		if err != nil {
			return err
		}
		if bytes.Equal(existing, data) {
			return nil
		}
		return errors.New("managed migration artifact already exists with different bytes")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomic(destination, data, 0o600)
}

func readPackMigrationPlan(repoRoot, planPath string) (PackMigrationPlan, error) {
	prefix := PackMigrationDirectory + "/"
	if !strings.HasPrefix(planPath, prefix) || strings.Contains(planPath, "\\") {
		return PackMigrationPlan{}, errors.New("pack migration plan must be a managed repository-relative path")
	}
	relative := strings.TrimPrefix(planPath, prefix)
	directory, filename := path.Split(relative)
	identity := strings.TrimSuffix(directory, "/")
	if filename != "plan.json" || identity == "" || strings.Contains(identity, "/") {
		return PackMigrationPlan{}, errors.New("pack migration plan path is invalid")
	}
	data, err := readPackMigrationArtifact(repoRoot, planPath, maximumPackMigrationPlanBytes)
	if err != nil {
		return PackMigrationPlan{}, err
	}
	plan, err := parsePackMigrationPlan(data)
	if err != nil {
		return PackMigrationPlan{}, err
	}
	if plan.MigrationID != identity || plan.BackupPath != prefix+identity+"/before.json" {
		return PackMigrationPlan{}, errors.New("pack migration plan path does not match its identity")
	}
	return plan, nil
}

func readPackMigrationBackup(repoRoot string, plan PackMigrationPlan) ([]byte, error) {
	data, err := readPackMigrationArtifact(repoRoot, plan.BackupPath, maximumPackMigrationConfigBytes)
	if err != nil {
		return nil, err
	}
	if packMigrationBytesDigest(data) != plan.BeforeConfigSHA256 {
		return nil, errors.New("pack migration backup does not match the planned configuration")
	}
	return data, nil
}

func readPackMigrationArtifact(repoRoot, relative string, maximum int) ([]byte, error) {
	if filepath.IsAbs(relative) || filepath.Clean(relative) != filepath.FromSlash(relative) || !pathWithin(repoRoot, filepath.Join(repoRoot, filepath.FromSlash(relative))) {
		return nil, errors.New("managed migration artifact path is invalid")
	}
	name := filepath.Join(repoRoot, filepath.FromSlash(relative))
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > int64(maximum) {
		return nil, fmt.Errorf("managed migration artifact must be a regular file of 1 to %d bytes", maximum)
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil || len(data) > maximum {
		return nil, errors.New("managed migration artifact exceeds its byte bound")
	}
	return data, nil
}

func renderPackMigrationPlan(plan PackMigrationPlan) ([]byte, error) {
	if err := validatePackMigrationPlan(plan); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if len(data) > maximumPackMigrationPlanBytes {
		return nil, fmt.Errorf("pack migration plan exceeds %d bytes", maximumPackMigrationPlanBytes)
	}
	if err := schema.NewValidator(PackMigrationSchema).Validate(data); err != nil {
		return nil, fmt.Errorf("validate pack migration schema: %w", err)
	}
	return data, nil
}

func parsePackMigrationPlan(data []byte) (PackMigrationPlan, error) {
	if len(data) == 0 || len(data) > maximumPackMigrationPlanBytes {
		return PackMigrationPlan{}, errors.New("pack migration plan exceeds its byte bound")
	}
	if err := schema.NewValidator(PackMigrationSchema).Validate(data); err != nil {
		return PackMigrationPlan{}, fmt.Errorf("validate pack migration schema: %w", err)
	}
	if err := schema.ValidateUniqueJSON(data, 32); err != nil {
		return PackMigrationPlan{}, fmt.Errorf("decode pack migration plan: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	plan := PackMigrationPlan{}
	if err := decoder.Decode(&plan); err != nil {
		return PackMigrationPlan{}, fmt.Errorf("decode pack migration plan: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return PackMigrationPlan{}, errors.New("decode pack migration plan: document contains more than one JSON value")
	}
	canonical, err := renderPackMigrationPlan(plan)
	if err != nil || !bytes.Equal(canonical, data) {
		return PackMigrationPlan{}, errors.New("pack migration plan is not canonical")
	}
	return plan, nil
}

func validatePackMigrationPlan(plan PackMigrationPlan) error {
	if plan.Schema != PackMigrationSchema || plan.Protocol != PackMigrationProtocol {
		return errors.New("pack migration plan has an invalid protocol")
	}
	if plan.MigrationID != packMigrationIdentity(plan) {
		return errors.New("pack migration plan has an invalid identity")
	}
	if !filepath.IsAbs(plan.CatalogPath) || filepath.Clean(plan.CatalogPath) != plan.CatalogPath {
		return errors.New("pack migration catalog path must be absolute and clean")
	}
	if plan.BeforeConfigSHA256 == plan.AfterConfigSHA256 {
		return errors.New("pack migration plan does not change pack policy")
	}
	if !canonicalPackMigrationSelections(plan.BeforePacks) || !canonicalPackMigrationSelections(plan.AfterPacks) {
		return errors.New("pack migration selections are not canonical")
	}
	if len(plan.Packs) != len(plan.AfterPacks) {
		return errors.New("pack migration preview does not match the selected packs")
	}
	for index, preview := range plan.Packs {
		if preview.Selection != plan.AfterPacks[index] {
			return errors.New("pack migration preview identity does not match the selected packs")
		}
	}
	if !slices.IsSortedFunc(plan.Coverage, func(left, right PackMigrationCoverage) int {
		return strings.Compare(packMigrationCoverageSummaryKey(left), packMigrationCoverageSummaryKey(right))
	}) ||
		!slices.IsSortedFunc(plan.Gaps, func(left, right PackMigrationGap) int {
			return strings.Compare(packMigrationGapKey(left), packMigrationGapKey(right))
		}) ||
		!slices.IsSortedFunc(plan.AddedDiagnostics, func(left, right PackMigrationDiagnostic) int {
			return strings.Compare(left.Fingerprint, right.Fingerprint)
		}) {
		return errors.New("pack migration evidence is not canonical")
	}
	ready := len(plan.Gaps) == 0 && !packMigrationHasNewErrors(plan.AddedDiagnostics)
	if plan.Ready != ready {
		return errors.New("pack migration readiness does not match its evidence")
	}
	return nil
}

func canonicalPackMigrationSelections(selections []policy.PackSelection) bool {
	if selections == nil || !slices.IsSortedFunc(selections, func(left, right policy.PackSelection) int { return strings.Compare(left.Name, right.Name) }) {
		return false
	}
	for index, selection := range selections {
		if index > 0 && selections[index-1].Name == selection.Name {
			return false
		}
	}
	return true
}

func packMigrationIdentity(plan PackMigrationPlan) string {
	return packMigrationDigest([]string{
		plan.EngineVersion, plan.ConfigPath, plan.LockSHA256, plan.CatalogPath, plan.CatalogSHA256,
		plan.BeforeConfigSHA256, plan.AfterConfigSHA256, plan.CoverageSHA256,
		plan.CurrentDiagnosticsSHA256, plan.CandidateDiagnosticsSHA256,
	})
}
