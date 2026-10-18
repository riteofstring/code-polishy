package repository

import (
	"path/filepath"
	"strings"
)

func (repo Repository) GovernedCargoPatchManifests() CargoManifestInventory {
	paths := []string{}
	for _, owner := range repo.Config.SupplyChain.DependencyOverridePolicies {
		if owner.Ecosystem == "cargo" && owner.Field == "patch.crates-io" {
			paths = append(paths, owner.Path)
		}
	}
	return repo.InspectCargoManifests(paths)
}

func CargoPatchManifest(owner CargoManifest, dependency CargoDependencyDeclaration) string {
	if dependency.Table != "patch.crates-io" || dependency.Path == "" || filepath.IsAbs(dependency.Path) || strings.ContainsAny(dependency.Path, "\\\x00:") {
		return ""
	}
	path := filepath.ToSlash(filepath.Join(owner.Root, dependency.Path, "Cargo.toml"))
	if !filepath.IsLocal(path) {
		return ""
	}
	return path
}
