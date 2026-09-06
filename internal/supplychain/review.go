package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"golang.org/x/mod/modfile"
)

type DependencyChange struct {
	Ecosystem  string
	Scope      string
	Directness string
	Usage      string
	Change     string
	Package    string
	From       string
	To         string
}

func DependencyChanges(ctx context.Context, repo repository.Repository, baseRevision string) ([]DependencyChange, error) {
	baseFiles, err := repo.FilesAt(baseRevision)
	if err != nil {
		return nil, err
	}
	currentFiles, err := repo.AllFiles()
	if err != nil {
		return nil, err
	}
	base, err := dependencyInventory(ctx, repo, inventorySource{
		files: baseFiles,
		read:  func(path string) ([]byte, error) { return repo.ReadAt(baseRevision, path) },
	})
	if err != nil {
		return nil, fmt.Errorf("dependency comparison coverage at merge base: %w", err)
	}
	current, err := dependencyInventory(ctx, repo, inventorySource{files: currentFiles, read: repo.Read})
	if err != nil {
		return nil, fmt.Errorf("inventory candidate dependencies: %w", err)
	}
	return compareDependencyInventories(base, current), nil
}

type inventorySource struct {
	files []string
	read  func(string) ([]byte, error)
}

type dependencyRecord struct {
	Ecosystem  string
	Scope      string
	Directness string
	Usage      string
	Name       string
	Version    string
}

type dependencyInventoryMap map[string]map[string]dependencyRecord

func dependencyInventory(ctx context.Context, repo repository.Repository, source inventorySource) (dependencyInventoryMap, error) {
	source = boundedInventorySource(source)
	inventory := dependencyInventoryMap{}
	fileSet := map[string]bool{}
	for _, path := range source.files {
		fileSet[path] = true
	}
	for _, path := range source.files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var records []dependencyRecord
		var err error
		switch filepath.Base(path) {
		case "package.json":
			records, err = directNodeRecords(source, fileSet, path)
		case "pyproject.toml":
			records, err = directPythonRecords(source, path)
		case "go.mod":
			records, err = directGoRecords(source, path)
		}
		if err != nil {
			return nil, err
		}
		addDependencyRecords(inventory, records)
	}
	direct := directDependencyVersions(inventory)
	for _, path := range source.files {
		records, err := resolvedLockRecords(ctx, repo, source, path)
		if err != nil {
			return nil, err
		}
		addResolvedInventoryRecords(inventory, direct, records)
	}
	return inventory, nil
}

func directNodeRecords(source inventorySource, files map[string]bool, path string) ([]dependencyRecord, error) {
	data, err := source.read(path)
	if err != nil {
		return nil, err
	}
	var manifest packageJSON
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	manager := inheritedManagerFromSource(source, files, path, manifest.PackageManager)
	if manager == "" {
		manager = "node"
	}
	records := []dependencyRecord{}
	groups := []struct {
		usage        string
		dependencies map[string]string
	}{
		{usage: "runtime", dependencies: manifest.Dependencies},
		{usage: "optional", dependencies: manifest.OptionalDependencies},
		{usage: "development", dependencies: manifest.DevDependencies},
	}
	for _, group := range groups {
		for name, version := range group.dependencies {
			if err := validateNodeInventoryDeclaration(path, version); err != nil {
				return nil, fmt.Errorf("parse %s: dependency %q: %w", path, name, err)
			}
			records = append(records, dependencyRecord{
				Ecosystem: manager, Scope: path, Directness: "direct", Usage: group.usage,
				Name: name, Version: version,
			})
		}
	}
	if name, version, found := strings.Cut(manifest.PackageManager, "@"); found && exactVersion.MatchString(version) {
		records = append(records, dependencyRecord{
			Ecosystem: name, Scope: path, Directness: "direct", Usage: "toolchain", Name: name, Version: version,
		})
	}
	return records, nil
}

func inheritedManagerFromSource(source inventorySource, files map[string]bool, path, declaration string) string {
	if manager, _, found := strings.Cut(declaration, "@"); found {
		return manager
	}
	directory := filepath.ToSlash(filepath.Dir(path))
	for directory != "." {
		directory = filepath.ToSlash(filepath.Dir(directory))
		candidate := "package.json"
		if directory != "." {
			candidate = directory + "/package.json"
		}
		if !files[candidate] {
			continue
		}
		data, err := source.read(candidate)
		if err != nil {
			continue
		}
		var owner packageJSON
		if json.Unmarshal(data, &owner) == nil {
			if manager, _, found := strings.Cut(owner.PackageManager, "@"); found {
				return manager
			}
		}
	}
	return ""
}

func directPythonRecords(source inventorySource, path string) ([]dependencyRecord, error) {
	data, err := source.read(path)
	if err != nil {
		return nil, err
	}
	project, err := repository.ParsePythonProjectInventory(path, data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: Python dependency inventory is malformed, ambiguous, or unsafe", path)
	}
	records := []dependencyRecord{}
	for _, dependency := range project.Requirements {
		record, err := pythonInventoryRecord(path, dependency)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func directGoRecords(source inventorySource, path string) ([]dependencyRecord, error) {
	data, err := source.read(path)
	if err != nil {
		return nil, err
	}
	document, err := modfile.Parse(path, data, func(_ string, version string) (string, error) {
		return version, validateDependencyDeclaration(version)
	})
	if err != nil {
		return nil, fmt.Errorf("parse %s: Go dependency inventory is malformed", path)
	}
	records := []dependencyRecord{}
	for _, requirement := range document.Require {
		directness, usage := "direct", "runtime"
		if requirement.Indirect {
			directness, usage = "transitive", "resolved"
		}
		records = append(records, dependencyRecord{
			Ecosystem: "go", Scope: path, Directness: directness, Usage: usage,
			Name: requirement.Mod.Path, Version: requirement.Mod.Version,
		})
	}
	return records, nil
}

func resolvedLockRecords(ctx context.Context, repo repository.Repository, source inventorySource, path string) ([]dependencyRecord, error) {
	packages, err := readResolvedLockInventory(ctx, repo, source, path)
	if err != nil {
		return nil, err
	}
	records := make([]dependencyRecord, 0, len(packages))
	for _, item := range packages {
		if err := validateResolvedInventorySource(item); err != nil {
			return nil, err
		}
		ecosystem, version := resolvedDependencyReviewIdentity(item)
		records = append(records, dependencyRecord{
			Ecosystem: ecosystem, Scope: path, Directness: "transitive", Usage: "resolved",
			Name: item.Name, Version: version,
		})
	}
	return records, nil
}

func readResolvedLockInventory(ctx context.Context, repo repository.Repository, source inventorySource, path string) ([]resolvedPackage, error) {
	name := filepath.Base(path)
	if name != "package-lock.json" && name != "npm-shrinkwrap.json" && name != pnpmLockName && name != "uv.lock" {
		return nil, nil
	}
	data, err := source.read(path)
	if err != nil {
		return nil, err
	}
	var packages []resolvedPackage
	switch name {
	case "package-lock.json", "npm-shrinkwrap.json":
		packages, err = parsePackageLock(data, path, "npm")
	case pnpmLockName:
		packages, err = revisionPNPMPackages(ctx, repo, data, path)
	case "uv.lock":
		packages, err = parseUVLockWith(data, path, parseUVInventorySourceFields)
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: resolved dependency inventory is malformed, ambiguous, or unsupported: %w", path, err)
	}
	return packages, nil
}

func resolvedDependencyReviewIdentity(item resolvedPackage) (string, string) {
	switch item.Source.Kind {
	case "git":
		return "git", item.Source.Git.InventoryIdentity()
	case "local":
		return "file", "file:" + item.Source.LocalPath
	default:
		return item.Ecosystem, item.Version
	}
}

func addDependencyRecords(inventory dependencyInventoryMap, records []dependencyRecord) {
	for _, record := range records {
		key := dependencyRecordKey(record)
		if inventory[key] == nil {
			inventory[key] = map[string]dependencyRecord{}
		}
		inventory[key][record.Version] = record
	}
}

func directDependencyVersions(inventory dependencyInventoryMap) map[string]bool {
	result := map[string]bool{}
	for _, versions := range inventory {
		for _, record := range versions {
			if record.Directness == "direct" {
				result[record.Ecosystem+"\x00"+record.Name+"\x00"+record.Version] = true
			}
		}
	}
	return result
}

func compareDependencyInventories(before, after dependencyInventoryMap) []DependencyChange {
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	changes := []DependencyChange{}
	for _, key := range ordered {
		changes = append(changes, compareDependencyVersions(before[key], after[key])...)
	}
	return changes
}

func compareDependencyVersions(before, after map[string]dependencyRecord) []DependencyChange {
	beforeVersions := recordVersions(before)
	afterVersions := recordVersions(after)
	if slicesEqual(beforeVersions, afterVersions) {
		return nil
	}
	if len(beforeVersions) == 1 && len(afterVersions) == 1 {
		return []DependencyChange{dependencyChange(firstRecord(before, after), "updated", beforeVersions[0], afterVersions[0])}
	}
	changes := []DependencyChange{}
	for _, version := range beforeVersions {
		if _, exists := after[version]; !exists {
			changes = append(changes, dependencyChange(before[version], "removed", version, "-"))
		}
	}
	for _, version := range afterVersions {
		if _, exists := before[version]; !exists {
			changes = append(changes, dependencyChange(after[version], "added", "-", version))
		}
	}
	return changes
}

func dependencyChange(record dependencyRecord, change, from, to string) DependencyChange {
	return DependencyChange{
		Ecosystem: record.Ecosystem, Scope: record.Scope, Directness: record.Directness,
		Usage: record.Usage, Change: change, Package: record.Name, From: from, To: to,
	}
}

func firstRecord(before, after map[string]dependencyRecord) dependencyRecord {
	for _, records := range []map[string]dependencyRecord{after, before} {
		for _, record := range records {
			return record
		}
	}
	return dependencyRecord{}
}

func recordVersions(records map[string]dependencyRecord) []string {
	versions := make([]string, 0, len(records))
	for version := range records {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	return versions
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func dependencyRecordKey(record dependencyRecord) string {
	return record.Ecosystem + "\x00" + record.Directness + "\x00" + record.Scope + "\x00" + record.Usage + "\x00" + record.Name
}

func NewDependencyAgeAdvisories(ctx context.Context, repo repository.Repository, changes []DependencyChange) []policy.Advisory {
	packages := []resolvedPackage{}
	advisories := []policy.Advisory{}
	for _, change := range changes {
		if !newDependencyAgeCandidate(change) {
			continue
		}
		if change.Ecosystem == "go" {
			if advisory, found := newGoDependencyAgeAdvisory(ctx, repo, change); found {
				advisories = append(advisories, advisory)
			}
			continue
		}
		packages = append(packages, resolvedPackage{
			Ecosystem: change.Ecosystem, Name: change.Package, Version: change.To, Scope: change.Scope,
		})
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -repo.Config.SupplyChain.PreferredNewDependencyAgeDays)
	for _, observation := range observeReleases(ctx, repo, packages) {
		if advisory := observedDependencyAgeAdvisory(observation, cutoff, repo.Config.SupplyChain.PreferredNewDependencyAgeDays); advisory != nil {
			advisories = append(advisories, *advisory)
		}
	}
	return advisories
}

func newDependencyAgeCandidate(change DependencyChange) bool {
	return change.Directness == "direct" &&
		strings.Contains(" added updated ", " "+change.Change+" ") &&
		strings.Contains(" runtime optional ", " "+change.Usage+" ") &&
		change.To != "-" &&
		dependencyAgeVersion(change) &&
		strings.Contains(" go npm pnpm pypi ", " "+change.Ecosystem+" ")
}

func dependencyAgeVersion(change DependencyChange) bool {
	if change.Ecosystem == "go" {
		return goVersion.MatchString(change.To)
	}
	if change.Ecosystem == "pypi" {
		return change.To != "" && !strings.ContainsAny(change.To, "<>=~*,")
	}
	return exactVersion.MatchString(change.To)
}

func observedDependencyAgeAdvisory(observation releaseObservation, cutoff time.Time, preferredDays int) *policy.Advisory {
	if observation.Err != nil || !observation.Released.After(cutoff) {
		return nil
	}
	item := observation.Package
	message := fmt.Sprintf("new direct runtime dependency was released %s; prefer at least %d days of observation when practical", observation.Released.Format(time.RFC3339), preferredDays)
	return &policy.Advisory{
		Check: "supplyChain.newDependencyAge", Path: item.Scope, Subject: item.Name + "@" + item.Version, Message: message,
	}
}

func newGoDependencyAgeAdvisory(ctx context.Context, repo repository.Repository, change DependencyChange) (policy.Advisory, bool) {
	lookupContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(lookupContext, repo.GoTool("go"), "list", "-m", "-json", change.Package+"@"+change.To)
	command.Dir = filepath.Join(repo.Root, filepath.FromSlash(filepath.Dir(change.Scope)))
	command.Env = runner.Environment(repo.Config.SupplyChain.Environment)
	output, err := command.Output()
	if err != nil {
		return policy.Advisory{}, false
	}
	var metadata struct {
		Time time.Time
	}
	if json.Unmarshal(output, &metadata) != nil || metadata.Time.IsZero() {
		return policy.Advisory{}, false
	}
	if !metadata.Time.After(time.Now().UTC().AddDate(0, 0, -repo.Config.SupplyChain.PreferredNewDependencyAgeDays)) {
		return policy.Advisory{}, false
	}
	message := fmt.Sprintf("new direct runtime dependency was released %s; prefer at least %d days of observation when practical", metadata.Time.Format(time.RFC3339), repo.Config.SupplyChain.PreferredNewDependencyAgeDays)
	return policy.Advisory{
		Check: "supplyChain.newDependencyAge", Path: change.Scope,
		Subject: change.Package + "@" + change.To, Message: message,
	}, true
}
