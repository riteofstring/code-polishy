package architecture

import (
	"fmt"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func (packages *nodePackages) verifyBundledDependencies(owner *nodePackage) {
	manifest := nodeManifestPath(owner.root)
	for _, declaration := range packages.repo.Config.Scope.BundledDependencies {
		if declaration.Manifest != manifest {
			continue
		}
		admitted := map[string]bool{}
		for _, name := range declaration.Packages {
			if !owner.development[name] || owner.runtime[name] {
				packages.coverage = append(packages.coverage, policy.Finding{
					Check: "architecture.packageDependency", Path: manifest, Subject: name,
					Message: fmt.Sprintf("bundled dependency %q must be declared only in this manifest's devDependencies", name),
				})
			} else {
				admitted[name] = true
			}
		}
		if len(admitted) != len(declaration.Packages) {
			return
		}
		owner.bundled = admitted
		archive := filepath.Join(packages.repo.Root, filepath.FromSlash(declaration.Artifact))
		result, err := (javascript.Bundle{PolicyRoot: packages.repo.PolicyRoot}).PackedImports(packages.ctx, archive, owner.name)
		if err != nil {
			packages.coverage = append(packages.coverage, packedCoverageFinding(declaration.Artifact, err.Error()))
			return
		}
		packages.coverage = append(packages.coverage, bundledArtifactFindings(declaration.Artifact, result, admitted)...)
		return
	}
}

func packedCoverageFinding(artifact, reason string) policy.Finding {
	finding := importCoverageFinding(artifact, "cannot verify bundled dependencies against the packed package: "+reason)
	finding.Remediation.Summary = "Build and pack the package with its package manager at the declared artifact path, then rerun architecture. Fix unreadable packed files, unresolved local imports, or unsupported entry mappings."
	return finding
}

func bundledArtifactFindings(artifact string, result javascript.ImportResult, admitted map[string]bool) []policy.Finding {
	findings := []policy.Finding{}
	for _, item := range result.Unsupported {
		findings = append(findings, packedCoverageFinding(artifact, item.Path+": "+item.Reason))
	}
	for _, fact := range result.Imports {
		if admitted[fact.Package] {
			findings = append(findings, policy.Finding{
				Check: "architecture.packageDependency", Path: artifact, Subject: fact.Package,
				Message: fmt.Sprintf("packed file %s:%d:%d imports bundled development dependency %q; consumers still need this package", fact.Path, fact.Line, fact.Column, fact.Package),
			})
		}
	}
	return findings
}
