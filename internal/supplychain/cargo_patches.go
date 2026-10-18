package supplychain

import (
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func cargoStaticFindings(repo repository.Repository, files []string) []policy.Finding {
	inventory := repo.InspectCargoManifests(files)
	if len(inventory.Manifests) == 0 && len(inventory.Problems) == 0 {
		return nil
	}
	patches, replacements, findings := cargoPatchGovernance(repo, &inventory)
	for _, problem := range inventory.Problems {
		findings = append(findings, policy.Finding{Check: "supplyChain.cargoManifest", Path: problem.Path, Line: problem.Line, Subject: "manifest", Message: problem.Message})
	}
	for _, manifest := range inventory.Manifests {
		for _, dependency := range manifest.Dependencies {
			findings = append(findings, cargoDeclarationFindings(repo, manifest, dependency, replacements[manifest.Path], patches[manifest.Path])...)
		}
		if replacements[manifest.Path] {
			findings = append(findings, policy.Finding{
				Check: "supplyChain.cargoPatchSourceCoverage", Path: manifest.Path, Subject: "local replacement", Severity: policy.FindingWarning,
				Message:     "Local Cargo replacement is governed as repository source; public registry evidence does not authenticate its modified contents or establish their advisory, license or release-age coverage",
				Remediation: policy.FindingRemediation{Summary: "Review the replacement's provenance and complete source delta under its exact override owner; retain the consumer's locked dependency checks."},
			})
		}
	}
	return findings
}

func cargoPatchGovernance(repo repository.Repository, selected *repository.CargoManifestInventory) (map[string]bool, map[string]bool, []policy.Finding) {
	patches, replacements := map[string]bool{}, map[string]bool{}
	findings := []policy.Finding{}
	for _, owner := range cargoPatchOwners(repo, selected.Manifests) {
		if !cargoPatchOwnerSelected(owner, selected.Manifests) {
			continue
		}
		if !slices.ContainsFunc(selected.Manifests, func(m repository.CargoManifest) bool { return m.Path == owner.Path }) {
			selected.Manifests = append(selected.Manifests, owner)
		}
		if !nonemptyJSONObject(owner.Patches["patch.crates-io"]) {
			continue
		}
		problems := dependencyOverrideBlockFindings(repo, owner.Path, "cargo", "patch.crates-io", owner.Patches["patch.crates-io"])
		findings = append(findings, problems...)
		if len(problems) != 0 {
			continue
		}
		patches[owner.Path] = true
		for _, dependency := range owner.Dependencies {
			path := repository.CargoPatchManifest(owner, dependency)
			if path != "" && cargoDeclarationSource(repo, owner, dependency) == nil {
				replacements[path] = true
			}
		}
	}
	return patches, replacements, findings
}

func cargoPatchOwners(repo repository.Repository, selected []repository.CargoManifest) []repository.CargoManifest {
	owners := repo.GovernedCargoPatchManifests().Manifests
	for _, manifest := range selected {
		if nonemptyJSONObject(manifest.Patches["patch.crates-io"]) && !slices.ContainsFunc(owners, func(owner repository.CargoManifest) bool { return owner.Path == manifest.Path }) {
			owners = append(owners, manifest)
		}
	}
	return owners
}

func cargoPatchOwnerSelected(owner repository.CargoManifest, selected []repository.CargoManifest) bool {
	for _, manifest := range selected {
		if manifest.Path == owner.Path {
			return true
		}
		for _, dependency := range owner.Dependencies {
			if repository.CargoPatchManifest(owner, dependency) == manifest.Path {
				return true
			}
		}
	}
	return false
}
