package rustfacts

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func cargoResolutionRoots(repo repository.Repository, selected []string) ([]string, map[string]repository.CargoManifest, []policy.Finding) {
	owners := map[string]repository.CargoManifest{}
	consumers := map[string][]string{}
	for _, owner := range repo.GovernedCargoPatchManifests().Manifests {
		if len(owner.Patches["patch.crates-io"]) == 0 {
			continue
		}
		owners[owner.Path] = owner
		for _, dependency := range owner.Dependencies {
			if path := repository.CargoPatchManifest(owner, dependency); path != "" {
				consumers[path] = append(consumers[path], owner.Path)
			}
		}
	}
	roots := map[string]bool{}
	findings := []policy.Finding{}
	for _, manifest := range selected {
		if err := addCargoResolutionRoot(manifest, consumers, owners, roots, map[string]bool{}); err != nil {
			findings = append(findings, resolutionFinding(manifest, err.Error()))
		}
	}
	patches := map[string]repository.CargoManifest{}
	for root := range roots {
		if owner, present := owners[root]; present {
			patches[root] = owner
		}
	}
	return slices.Sorted(maps.Keys(roots)), patches, findings
}

func addCargoResolutionRoot(manifest string, consumers map[string][]string, owners map[string]repository.CargoManifest, roots, visiting map[string]bool) error {
	if visiting[manifest] {
		return fmt.Errorf("cargo replacement ownership is cyclic at %s", manifest)
	}
	if len(consumers[manifest]) == 0 {
		roots[manifest] = true
		return nil
	}
	if _, owner := owners[manifest]; owner {
		return fmt.Errorf("cargo patch owner %s is itself a consumed replacement; only workspace-root patches apply", manifest)
	}
	visiting[manifest] = true
	defer delete(visiting, manifest)
	for _, consumer := range consumers[manifest] {
		if err := addCargoResolutionRoot(consumer, consumers, owners, roots, visiting); err != nil {
			return err
		}
	}
	return nil
}

func cargoResolvedPatchFindings(owner repository.CargoManifest, resolution Resolution) []policy.Finding {
	if owner.Path != filepath.ToSlash(filepath.Join(resolution.Workspace.Root, "Cargo.toml")) {
		return []policy.Finding{resolutionFinding(owner.Path, "governed Cargo patches must be declared in the actual workspace-root manifest")}
	}
	findings := []policy.Finding{}
	for _, dependency := range owner.Dependencies {
		if dependency.Table != "patch.crates-io" {
			continue
		}
		if !slices.ContainsFunc(resolution.Packages, func(pkg Package) bool {
			return cargoPatchMatchesPackage(owner, dependency, pkg) && !slices.Contains(resolution.Workspace.Members, pkg.ID)
		}) {
			findings = append(findings, policy.Finding{
				Check: "supplyChain.cargoSource", Path: owner.Path, Subject: dependency.Table + ":" + dependency.Alias,
				Message: "Cargo replacement must resolve to the declared source as a dependency in the consumer's locked all-feature graph, outside its workspace members",
			})
		}
	}
	return findings
}

func cargoPatchMatchesPackage(owner repository.CargoManifest, dependency repository.CargoDependencyDeclaration, pkg Package) bool {
	if pkg.Name != dependency.Package {
		return false
	}
	if path := repository.CargoPatchManifest(owner, dependency); path != "" {
		return pkg.Source == nil && pkg.Manifest == path
	}
	if dependency.Git == "" || pkg.Source == nil {
		return false
	}
	declared, declarationErr := repository.ParseCargoGitDeclaration(dependency)
	resolved, resolutionErr := repository.ParseCargoGitLockSource(*pkg.Source)
	return declarationErr == nil && resolutionErr == nil && declared.Commit != "" && declared.RepositoryIdentity() == resolved.RepositoryIdentity() && declared.Commit == resolved.Commit
}
