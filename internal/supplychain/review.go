package supplychain

import (
	"bufio"
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
		return nil, fmt.Errorf("inventory dependencies at merge base: %w", err)
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
	inventory := dependencyInventoryMap{}
	fileSet := map[string]bool{}
	for _, path := range source.files {
		fileSet[path] = true
	}
	for _, path := range source.files {
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
		for _, record := range records {
			if direct[record.Ecosystem+"\x00"+record.Name+"\x00"+record.Version] {
				continue
			}
			addDependencyRecords(inventory, []dependencyRecord{record})
		}
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
			if exactVersion.MatchString(version) {
				records = append(records, dependencyRecord{
					Ecosystem: manager, Scope: path, Directness: "direct", Usage: group.usage,
					Name: name, Version: version,
				})
			}
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
	records := []dependencyRecord{}
	for _, dependency := range pythonReviewDependencies(string(data)) {
		if !pythonExact.MatchString(dependency.Value) {
			continue
		}
		beforeMarker := strings.SplitN(dependency.Value, ";", 2)[0]
		parts := strings.SplitN(beforeMarker, "==", 2)
		name := strings.TrimSpace(strings.SplitN(parts[0], "[", 2)[0])
		version := strings.TrimSpace(parts[1])
		records = append(records, dependencyRecord{
			Ecosystem: "pypi", Scope: path, Directness: "direct", Usage: dependency.Usage, Name: name, Version: version,
		})
	}
	return records, nil
}

type pythonReviewDependency struct {
	Value string
	Usage string
}

func pythonReviewDependencies(text string) []pythonReviewDependency {
	dependencies := []pythonReviewDependency{}
	scanner := bufio.NewScanner(strings.NewReader(text))
	section := ""
	inDependencies := false
	usage := ""
	for scanner.Scan() {
		line := strings.TrimSpace(stripTOMLComment(scanner.Text()))
		if !inDependencies && strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.Trim(line, "[]"))
			continue
		}
		if !inDependencies {
			if !pythonDependencyArray(section, line) {
				continue
			}
			inDependencies = true
			usage = pythonDependencyUsage(section)
			line = strings.SplitN(line, "[", 2)[1]
		}
		for _, match := range quotedString.FindAllStringSubmatch(line, -1) {
			dependencies = append(dependencies, pythonReviewDependency{Value: strings.TrimSpace(match[1]), Usage: usage})
		}
		if strings.Contains(line, "]") {
			inDependencies = false
		}
	}
	return dependencies
}

func pythonDependencyUsage(section string) string {
	switch section {
	case "project":
		return "runtime"
	case "project.optional-dependencies":
		return "optional"
	default:
		return "development"
	}
}

func directGoRecords(source inventorySource, path string) ([]dependencyRecord, error) {
	data, err := source.read(path)
	if err != nil {
		return nil, err
	}
	records := []dependencyRecord{}
	block := false
	for _, raw := range strings.Split(string(data), "\n") {
		indirect := strings.Contains(raw, "// indirect")
		line := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
		if line == "require (" {
			block = true
			continue
		}
		if block && line == ")" {
			block = false
			continue
		}
		if strings.HasPrefix(line, "require ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		} else if !block {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && goVersion.MatchString(fields[1]) {
			directness := "direct"
			usage := "runtime"
			if indirect {
				directness = "transitive"
				usage = "resolved"
			}
			records = append(records, dependencyRecord{
				Ecosystem: "go", Scope: path, Directness: directness, Usage: usage, Name: fields[0], Version: fields[1],
			})
		}
	}
	return records, nil
}

func resolvedLockRecords(ctx context.Context, repo repository.Repository, source inventorySource, path string) ([]dependencyRecord, error) {
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
		packages, err = parseUVLock(data, path)
	}
	if err != nil {
		return nil, err
	}
	records := make([]dependencyRecord, 0, len(packages))
	for _, item := range packages {
		records = append(records, dependencyRecord{
			Ecosystem: item.Ecosystem, Scope: path, Directness: "transitive", Usage: "resolved",
			Name: item.Name, Version: item.Version,
		})
	}
	return records, nil
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
		strings.Contains(" go npm pnpm pypi ", " "+change.Ecosystem+" ")
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
