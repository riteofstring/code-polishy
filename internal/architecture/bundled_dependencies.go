package architecture

import (
	"fmt"

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
		result, err := (javascript.Bundle{PolicyRoot: packages.repo.PolicyRoot}).PackageImports(packages.ctx, packages.repo.Root, manifest)
		if err != nil {
			packages.coverage = append(packages.coverage, importCoverageFinding(manifest, "cannot verify bundled dependencies against published artifacts: "+err.Error()))
			return
		}
		findings := bundledArtifactFindings(result, admitted)
		packages.coverage = append(packages.coverage, findings...)
		if len(findings) == 0 {
			owner.bundled = admitted
		}
		return
	}
}

func bundledArtifactFindings(result javascript.ImportResult, admitted map[string]bool) []policy.Finding {
	findings := []policy.Finding{}
	for _, item := range result.Unsupported {
		finding := importCoverageFinding(item.Path, "cannot verify bundled dependencies against published artifacts: "+item.Reason)
		finding.Remediation.Summary = "Build the package, then rerun architecture with its complete published JavaScript and public declarations present. Resolve unsupported artifact imports or entry mappings."
		findings = append(findings, finding)
	}
	for _, fact := range result.Imports {
		if admitted[fact.Package] {
			findings = append(findings, policy.Finding{
				Check: "architecture.packageDependency", Path: fact.Path, Line: fact.Line, Column: fact.Column, Subject: fact.Package,
				Message: fmt.Sprintf("published artifact imports bundled development dependency %q; consumers still need this package", fact.Package),
			})
		}
	}
	return findings
}
