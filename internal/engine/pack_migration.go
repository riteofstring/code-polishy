package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
)

func PlanPackMigration(ctx context.Context, options PackMigrationPlanOptions) (PackMigrationPlanResult, error) {
	repoRoot, configPath, configRelative, err := resolvePackMigrationPaths(options.RepositoryRoot, options.ConfigPath)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	if len(options.References) == 0 {
		return PackMigrationPlanResult{}, errors.New("pack migration requires at least one exact pack selection")
	}
	dataRoot, err := resolvePackMigrationDataRoot(options.DataRoot)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	beforeData, mode, err := readPackMigrationConfig(configPath)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	configured, err := validatePackMigrationConfig(beforeData, configPath)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	lockData, err := readPackMigrationLock(repoRoot)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	engineVersion, err := packMigrationEngineVersion(options.PolicyRoot)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	catalog, err := pack.LoadCatalog(options.CatalogPath, options.CatalogSHA256)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	selections, previews, err := packMigrationSelections(catalog, options.References, engineVersion)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	afterData, err := renderPackMigrationConfig(beforeData, selections)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	candidateConfig, err := validatePackMigrationConfig(afterData, configPath)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	if packMigrationBytesDigest(beforeData) == packMigrationBytesDigest(afterData) {
		return PackMigrationPlanResult{}, errors.New("pack migration selection already matches repository policy")
	}
	installed, present, err := installPackMigrationSelections(ctx, catalog, selections, options.PolicyRoot, engineVersion, dataRoot)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	currentEngine, err := openConfigured(repoRoot, options.PolicyRoot, configured, dataRoot, nil)
	if err != nil {
		return PackMigrationPlanResult{}, fmt.Errorf("open current pack policy: %w", err)
	}
	candidateEngine, err := openConfigured(repoRoot, options.PolicyRoot, candidateConfig, dataRoot, nil)
	if err != nil {
		return PackMigrationPlanResult{}, fmt.Errorf("open candidate pack policy: %w", err)
	}
	assessment, err := assessPackMigration(ctx, currentEngine, candidateEngine, configured)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	beforePacks := canonicalPackSelections(configured.Packs)
	plan := PackMigrationPlan{
		Schema: PackMigrationSchema, Protocol: PackMigrationProtocol, EngineVersion: engineVersion,
		ConfigPath: configRelative, LockSHA256: packMigrationBytesDigest(lockData),
		CatalogPath: catalog.Path, CatalogSHA256: catalog.SHA256,
		BeforeConfigSHA256: packMigrationBytesDigest(beforeData), AfterConfigSHA256: packMigrationBytesDigest(afterData),
		BeforePacks: beforePacks, AfterPacks: selections, Packs: previews,
		Inventory: assessment.inventory, Coverage: assessment.coverage, CoverageSHA256: assessment.coverageSHA256,
		CurrentDiagnosticsSHA256:   assessment.currentDiagnosticsSHA256,
		CandidateDiagnosticsSHA256: assessment.candidateDiagnosticsSHA256,
		AddedDiagnostics:           assessment.addedDiagnostics, Gaps: assessment.gaps,
	}
	plan.Ready = len(plan.Gaps) == 0 && !packMigrationHasNewErrors(plan.AddedDiagnostics)
	plan.MigrationID = packMigrationIdentity(plan)
	plan.BackupPath = filepath.ToSlash(filepath.Join(PackMigrationDirectory, plan.MigrationID, "before.json"))
	if err := revalidatePackMigrationSource(configPath, beforeData, repoRoot, lockData, mode); err != nil {
		return PackMigrationPlanResult{}, err
	}
	planPath, err := writePackMigrationPlan(repoRoot, plan, beforeData)
	if err != nil {
		return PackMigrationPlanResult{}, err
	}
	return PackMigrationPlanResult{Plan: plan, PlanPath: planPath, Installed: installed, AlreadyPresent: present}, nil
}

func ApplyPackMigration(ctx context.Context, options PackMigrationActionOptions) (PackMigrationActionResult, error) {
	repoRoot, configPath, configRelative, err := resolvePackMigrationPaths(options.RepositoryRoot, options.ConfigPath)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	plan, err := readPackMigrationPlan(repoRoot, filepath.ToSlash(options.PlanPath))
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	if plan.ConfigPath != configRelative {
		return PackMigrationActionResult{}, errors.New("pack migration plan names a different repository configuration")
	}
	unlock, err := acquirePackMigrationLock(repoRoot)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	defer unlock()
	dataRoot, err := resolvePackMigrationDataRoot(options.DataRoot)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	backup, err := readPackMigrationBackup(repoRoot, plan)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	currentData, mode, err := readPackMigrationConfig(configPath)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	currentDigest := packMigrationBytesDigest(currentData)
	if currentDigest == plan.AfterConfigSHA256 {
		if err := validatePackMigrationAuthority(plan, repoRoot, options.PolicyRoot, dataRoot); err != nil {
			return PackMigrationActionResult{}, err
		}
		return PackMigrationActionResult{Plan: plan}, nil
	}
	if currentDigest != plan.BeforeConfigSHA256 || !slices.Equal(currentData, backup) {
		return PackMigrationActionResult{}, errors.New("repository configuration changed after pack migration planning; create a new plan")
	}
	if !plan.Ready {
		return PackMigrationActionResult{}, errors.New("pack migration plan is blocked by coverage gaps or new error diagnostics")
	}
	if err := validatePackMigrationAuthority(plan, repoRoot, options.PolicyRoot, dataRoot); err != nil {
		return PackMigrationActionResult{}, err
	}
	afterData, assessment, err := revalidatePackMigrationAssessment(ctx, plan, repoRoot, options.PolicyRoot, configPath, currentData, dataRoot)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	if !packMigrationAssessmentMatches(plan, assessment) || packMigrationBytesDigest(afterData) != plan.AfterConfigSHA256 {
		return PackMigrationActionResult{}, errors.New("repository pack migration evidence changed after planning; create a new plan")
	}
	lockData, err := readPackMigrationLock(repoRoot)
	if err != nil || packMigrationBytesDigest(lockData) != plan.LockSHA256 {
		return PackMigrationActionResult{}, errors.New("repository engine lock changed after pack migration planning")
	}
	if err := revalidatePackMigrationSource(configPath, currentData, repoRoot, lockData, mode); err != nil {
		return PackMigrationActionResult{}, err
	}
	if err := writeAtomic(configPath, afterData, mode.Perm()); err != nil {
		return PackMigrationActionResult{}, fmt.Errorf("apply pack migration: %w", err)
	}
	if err := verifyPackMigrationCutover(configPath, plan.AfterConfigSHA256, repoRoot, plan.LockSHA256); err != nil {
		restoreErr := writeAtomic(configPath, backup, mode.Perm())
		return PackMigrationActionResult{}, errors.Join(err, restoreErr)
	}
	return PackMigrationActionResult{Plan: plan, Changed: true}, nil
}

func RollbackPackMigration(options PackMigrationActionOptions) (PackMigrationActionResult, error) {
	repoRoot, configPath, configRelative, err := resolvePackMigrationPaths(options.RepositoryRoot, options.ConfigPath)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	plan, err := readPackMigrationPlan(repoRoot, filepath.ToSlash(options.PlanPath))
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	if plan.ConfigPath != configRelative {
		return PackMigrationActionResult{}, errors.New("pack migration plan names a different repository configuration")
	}
	unlock, err := acquirePackMigrationLock(repoRoot)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	defer unlock()
	backup, err := readPackMigrationBackup(repoRoot, plan)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	currentData, mode, err := readPackMigrationConfig(configPath)
	if err != nil {
		return PackMigrationActionResult{}, err
	}
	currentDigest := packMigrationBytesDigest(currentData)
	if currentDigest == plan.BeforeConfigSHA256 {
		return PackMigrationActionResult{Plan: plan}, nil
	}
	if currentDigest != plan.AfterConfigSHA256 {
		return PackMigrationActionResult{}, errors.New("repository configuration no longer matches the applied migration; rollback would overwrite unrelated changes")
	}
	lockData, err := readPackMigrationLock(repoRoot)
	if err != nil || packMigrationBytesDigest(lockData) != plan.LockSHA256 {
		return PackMigrationActionResult{}, errors.New("repository engine lock changed after pack migration planning; restore it before rollback")
	}
	if err := writeAtomic(configPath, backup, mode.Perm()); err != nil {
		return PackMigrationActionResult{}, fmt.Errorf("rollback pack migration: %w", err)
	}
	if err := verifyPackMigrationCutover(configPath, plan.BeforeConfigSHA256, repoRoot, plan.LockSHA256); err != nil {
		return PackMigrationActionResult{}, err
	}
	return PackMigrationActionResult{Plan: plan, Changed: true}, nil
}

func packMigrationSelections(catalog pack.LoadedCatalog, references []string, engineVersion string) ([]policy.PackSelection, []PackMigrationPack, error) {
	selections := []policy.PackSelection{}
	previews := []PackMigrationPack{}
	seen := map[string]bool{}
	for _, reference := range references {
		name, version, err := pack.ParsePackReference(reference)
		if err != nil {
			return nil, nil, err
		}
		if seen[name] {
			return nil, nil, fmt.Errorf("pack migration selects %s more than once", name)
		}
		seen[name] = true
		entry, err := catalog.Entry(name, version)
		if err != nil {
			return nil, nil, err
		}
		if entry.EngineVersion != engineVersion {
			return nil, nil, fmt.Errorf("pack %s@%s does not support Code Polishy %s", name, version, engineVersion)
		}
		if !slices.Contains(entry.Platforms, pack.CurrentPlatform()) {
			return nil, nil, fmt.Errorf("pack %s@%s does not support %s", name, version, pack.CurrentPlatform())
		}
		selection := policy.PackSelection{Name: name, Version: version, Digest: entry.Digest}
		selections = append(selections, selection)
		previews = append(previews, PackMigrationPack{
			Selection: selection, Capabilities: sortedPackMigrationStrings(entry.Capabilities),
			Tools: sortedPackMigrationStrings(entry.Tools), Dependencies: sortedPackMigrationStrings(entry.Dependencies),
			Licenses: sortedPackMigrationStrings(entry.Licenses), Builder: entry.Provenance.Builder,
			SourceRevision: entry.Provenance.SourceRevision,
		})
	}
	sort.Slice(selections, func(left, right int) bool { return selections[left].Name < selections[right].Name })
	sort.Slice(previews, func(left, right int) bool { return previews[left].Selection.Name < previews[right].Selection.Name })
	return selections, previews, nil
}

func installPackMigrationSelections(ctx context.Context, catalog pack.LoadedCatalog, selections []policy.PackSelection, policyRoot, engineVersion, dataRoot string) (int, int, error) {
	installed, present := 0, 0
	for _, selection := range selections {
		root := pack.InstalledRoot(dataRoot, selection.Name, selection.Version, selection.Digest)
		if receipt, err := pack.VerifyInstalled(root); err == nil && receipt.Name == selection.Name && receipt.Version == selection.Version && receipt.Digest == selection.Digest {
			present++
		} else {
			identity, installedRoot, err := pack.InstallOfficial(catalog, selection.Name, selection.Version, engineVersion, dataRoot)
			if err != nil {
				return installed, present, err
			}
			if identity.Name != selection.Name || identity.Version != selection.Version || identity.Digest != selection.Digest || installedRoot != root {
				return installed, present, errors.New("installed pack identity does not match migration selection")
			}
			installed++
		}
		if _, err := pack.VerifySource(ctx, root, policyRoot, engineVersion, pack.DefaultRunner()); err != nil {
			return installed, present, fmt.Errorf("verify migration pack %s@%s: %w", selection.Name, selection.Version, err)
		}
	}
	return installed, present, nil
}

func validatePackMigrationAuthority(plan PackMigrationPlan, repoRoot, policyRoot, dataRoot string) error {
	lockData, err := readPackMigrationLock(repoRoot)
	if err != nil || packMigrationBytesDigest(lockData) != plan.LockSHA256 {
		return errors.New("repository engine lock changed after pack migration planning")
	}
	version, err := packMigrationEngineVersion(policyRoot)
	if err != nil || version != plan.EngineVersion {
		return errors.New("pack migration engine changed after planning")
	}
	catalog, err := pack.LoadCatalog(plan.CatalogPath, plan.CatalogSHA256)
	if err != nil {
		return err
	}
	references := make([]string, 0, len(plan.AfterPacks))
	for _, selection := range plan.AfterPacks {
		references = append(references, selection.Name+"@"+selection.Version)
	}
	selections, previews, err := packMigrationSelections(catalog, references, version)
	if err != nil || !reflect.DeepEqual(selections, plan.AfterPacks) || !reflect.DeepEqual(previews, plan.Packs) {
		return errors.New("authenticated pack catalog changed after migration planning")
	}
	for _, selection := range selections {
		receipt, err := pack.VerifyInstalled(pack.InstalledRoot(dataRoot, selection.Name, selection.Version, selection.Digest))
		if err != nil || receipt.Name != selection.Name || receipt.Version != selection.Version || receipt.Digest != selection.Digest {
			return fmt.Errorf("selected migration pack %s@%s is unavailable or corrupt", selection.Name, selection.Version)
		}
	}
	return nil
}

func revalidatePackMigrationAssessment(ctx context.Context, plan PackMigrationPlan, repoRoot, policyRoot, configPath string, beforeData []byte, dataRoot string) ([]byte, packMigrationAssessment, error) {
	configured, err := validatePackMigrationConfig(beforeData, configPath)
	if err != nil {
		return nil, packMigrationAssessment{}, err
	}
	afterData, err := renderPackMigrationConfig(beforeData, plan.AfterPacks)
	if err != nil {
		return nil, packMigrationAssessment{}, err
	}
	candidate, err := validatePackMigrationConfig(afterData, configPath)
	if err != nil {
		return nil, packMigrationAssessment{}, err
	}
	currentEngine, err := openConfigured(repoRoot, policyRoot, configured, dataRoot, nil)
	if err != nil {
		return nil, packMigrationAssessment{}, err
	}
	candidateEngine, err := openConfigured(repoRoot, policyRoot, candidate, dataRoot, nil)
	if err != nil {
		return nil, packMigrationAssessment{}, err
	}
	assessment, err := assessPackMigration(ctx, currentEngine, candidateEngine, configured)
	return afterData, assessment, err
}

func packMigrationAssessmentMatches(plan PackMigrationPlan, assessment packMigrationAssessment) bool {
	return reflect.DeepEqual(plan.Inventory, assessment.inventory) && reflect.DeepEqual(plan.Coverage, assessment.coverage) &&
		plan.CoverageSHA256 == assessment.coverageSHA256 && plan.CurrentDiagnosticsSHA256 == assessment.currentDiagnosticsSHA256 &&
		plan.CandidateDiagnosticsSHA256 == assessment.candidateDiagnosticsSHA256 && reflect.DeepEqual(plan.AddedDiagnostics, assessment.addedDiagnostics) &&
		reflect.DeepEqual(plan.Gaps, assessment.gaps) && plan.Ready == (len(assessment.gaps) == 0 && !packMigrationHasNewErrors(assessment.addedDiagnostics))
}

func resolvePackMigrationPaths(repoRoot, configPath string) (string, string, string, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", "", "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", "", err
	}
	if configPath == "" {
		configPath = policy.ConfigFilename
	}
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(root, configPath)
	}
	configPath = filepath.Clean(configPath)
	info, err := os.Lstat(configPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", "", errors.New("pack migration configuration must be a regular repository file")
	}
	resolved, err := filepath.EvalSymlinks(configPath)
	if err != nil || !pathWithin(root, resolved) {
		return "", "", "", errors.New("pack migration configuration must remain inside the repository")
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." {
		return "", "", "", errors.New("pack migration configuration path is invalid")
	}
	return root, resolved, filepath.ToSlash(relative), nil
}

func resolvePackMigrationDataRoot(value string) (string, error) {
	if value == "" {
		return pack.UserDataRoot()
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func readPackMigrationConfig(path string) ([]byte, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > maximumPackMigrationConfigBytes {
		return nil, 0, fmt.Errorf("pack migration configuration must be a regular file of 1 to %d bytes", maximumPackMigrationConfigBytes)
	}
	data, err := os.ReadFile(path)
	return data, info.Mode(), err
}

func readPackMigrationLock(repoRoot string) ([]byte, error) {
	if _, present, err := release.ReadLock(repoRoot); err != nil || !present {
		if err == nil {
			err = errors.New("repository engine lock is missing")
		}
		return nil, err
	}
	path := filepath.Join(repoRoot, release.LockFilename)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > maximumPackMigrationConfigBytes {
		return nil, errors.New("repository engine lock must be a bounded regular file")
	}
	return os.ReadFile(path)
}

func packMigrationEngineVersion(policyRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(policyRoot, "VERSION"))
	if err != nil {
		return "", fmt.Errorf("read policy version: %w", err)
	}
	version := strings.TrimSpace(string(data))
	if !pack.ValidEngineVersion(version) {
		return "", errors.New("policy VERSION must contain one exact version")
	}
	return version, nil
}

func canonicalPackSelections(selections []policy.PackSelection) []policy.PackSelection {
	result := slices.Clone(selections)
	if result == nil {
		result = []policy.PackSelection{}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}

func packMigrationBytesDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func revalidatePackMigrationSource(configPath string, configData []byte, repoRoot string, lockData []byte, mode os.FileMode) error {
	current, currentMode, err := readPackMigrationConfig(configPath)
	if err != nil || !slices.Equal(current, configData) || currentMode.Perm() != mode.Perm() {
		return errors.New("repository configuration changed during pack migration preparation")
	}
	currentLock, err := readPackMigrationLock(repoRoot)
	if err != nil || !slices.Equal(currentLock, lockData) {
		return errors.New("repository engine lock changed during pack migration preparation")
	}
	return nil
}

func verifyPackMigrationCutover(configPath, configDigest, repoRoot, lockDigest string) error {
	data, _, err := readPackMigrationConfig(configPath)
	if err != nil || packMigrationBytesDigest(data) != configDigest {
		return errors.New("pack migration configuration cutover could not be verified")
	}
	lockData, err := readPackMigrationLock(repoRoot)
	if err != nil || packMigrationBytesDigest(lockData) != lockDigest {
		return errors.New("pack migration changed the repository engine lock")
	}
	return nil
}
