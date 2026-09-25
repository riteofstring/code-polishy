package supplychain

import (
	"fmt"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func directCargoRecords(source inventorySource, path string) ([]dependencyRecord, error) {
	data, err := source.read(path)
	if err != nil {
		return nil, err
	}
	manifest, err := repository.ParseCargoManifest(path, data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: Cargo dependency inventory is malformed, ambiguous, or unsafe", path)
	}
	records := []dependencyRecord{}
	for _, dependency := range manifest.Dependencies {
		record, err := cargoReviewDeclaration(manifest, dependency)
		if err != nil {
			return nil, fmt.Errorf("parse %s: Cargo dependency inventory is malformed, ambiguous, or unsafe", path)
		}
		records = append(records, record)
	}
	return records, nil
}

func cargoReviewDeclaration(manifest repository.CargoManifest, dependency repository.CargoDependencyDeclaration) (dependencyRecord, error) {
	if err := cargoDeclarationFields(dependency); err != nil {
		return dependencyRecord{}, err
	}
	if dependency.Path != "" && dependency.Git != "" {
		return dependencyRecord{}, fmt.Errorf("cargo dependency has conflicting source kinds")
	}
	record := dependencyRecord{Scope: manifest.Path, Directness: "direct", Usage: cargoReviewUsage(dependency), Name: dependency.Package}
	if dependency.Table == "workspace.dependencies" {
		record.Directness, record.Usage = "workspace", "definition"
	}
	if dependency.Table == "replace" || strings.HasPrefix(dependency.Table, "patch.") {
		if registry, found := strings.CutPrefix(dependency.Table, "patch."); found {
			if err := validateDependencyDeclaration(registry); err != nil {
				return dependencyRecord{}, err
			}
		}
		record.Directness, record.Usage = "override", dependency.Table
	}
	ecosystem, version, err := cargoReviewSource(manifest, dependency)
	if err != nil {
		return dependencyRecord{}, err
	}
	record.Ecosystem, record.Version = ecosystem, version
	return record, nil
}

func cargoReviewUsage(dependency repository.CargoDependencyDeclaration) string {
	if strings.HasSuffix(dependency.Table, "dev-dependencies") {
		return "development"
	}
	if strings.HasSuffix(dependency.Table, "build-dependencies") {
		return "build"
	}
	if dependency.Optional {
		return "optional"
	}
	return "runtime"
}

func cargoReviewSource(manifest repository.CargoManifest, dependency repository.CargoDependencyDeclaration) (string, string, error) {
	switch {
	case dependency.Workspace:
		version := "workspace"
		if manifest.Package != nil && manifest.Package.Workspace != "" {
			if err := validateInventoryLocalPath(manifest.Path, manifest.Package.Workspace); err != nil {
				return "", "", err
			}
			version += ":" + manifest.Package.Workspace
		}
		return "cargo-workspace", version, nil
	case dependency.Git != "":
		source, err := repository.ParseCargoGitDeclaration(dependency)
		return "git", source.InventoryIdentity(), err
	case dependency.Path != "":
		return "file", "file:" + dependency.Path, validateInventoryLocalPath(manifest.Path, dependency.Path)
	default:
		return cargoReviewRegistry(dependency)
	}
}

func cargoReviewRegistry(dependency repository.CargoDependencyDeclaration) (string, string, error) {
	version := strings.TrimSpace(dependency.Version)
	if err := validateDependencyDeclaration(version); err != nil {
		return "", "", err
	}
	if exactCargoRequirement(version) {
		version = strings.TrimSpace(strings.TrimPrefix(version, "="))
	} else if exactVersion.MatchString(version) {
		version = "^" + version
	}
	if dependency.Registry != "" && dependency.Registry != "crates-io" {
		if err := validateDependencyDeclaration(dependency.Registry); err != nil {
			return "", "", err
		}
		return "cargo-registry", "registry:" + dependency.Registry + "@" + version, nil
	}
	return "crates.io", version, nil
}
