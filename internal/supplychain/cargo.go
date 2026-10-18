package supplychain

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"golang.org/x/mod/semver"
)

func checkCargoManifest(repo repository.Repository, path string) []policy.Finding {
	return cargoStaticFindings(repo, []string{path})
}

func cargoDeclarationFindings(repo repository.Repository, manifest repository.CargoManifest, dependency repository.CargoDependencyDeclaration, replacement, governedPatch bool) []policy.Finding {
	if err := cargoPatchDeclarationProblem(dependency, governedPatch); err != nil {
		return []policy.Finding{cargoDependencyFinding(manifest.Path, dependency, "supplyChain.cargoSource", err.Error())}
	}
	if err := cargoDeclarationFields(dependency); err != nil {
		return []policy.Finding{cargoDependencyFinding(manifest.Path, dependency, "supplyChain.cargoManifest", err.Error())}
	}
	if dependency.Workspace {
		return nil
	}
	findings := []policy.Finding{}
	if cargoRequiresExactRequirement(dependency, replacement) && !exactCargoRequirement(dependency.Version) {
		findings = append(findings, cargoDependencyFinding(manifest.Path, dependency, "supplyChain.exactVersion", "Cargo dependencies must use an exact =major.minor.patch requirement without build metadata; bare versions and ranges permit updates"))
	}
	if err := cargoDeclarationSource(repo, manifest, dependency); err != nil {
		findings = append(findings, cargoDependencyFinding(manifest.Path, dependency, "supplyChain.cargoSource", err.Error()))
	}
	return findings
}

func cargoPatchDeclarationProblem(dependency repository.CargoDependencyDeclaration, governed bool) error {
	if dependency.Table == "replace" || strings.HasPrefix(dependency.Table, "patch.") && (dependency.Table != "patch.crates-io" || !governed) {
		return fmt.Errorf("cargo patch/replace declarations require governed override and resolved-source evidence")
	}
	if dependency.Table == "patch.crates-io" && dependency.Path == "" && dependency.Git == "" {
		return fmt.Errorf("cargo registry patches must select a contained path or immutable Git source")
	}
	return nil
}

func cargoRequiresExactRequirement(dependency repository.CargoDependencyDeclaration, replacement bool) bool {
	registry := dependency.Path == "" && dependency.Git == ""
	if replacement && registry && dependency.Version != "" {
		return false
	}
	return dependency.Version != "" || registry
}

func cargoDeclarationFields(dependency repository.CargoDependencyDeclaration) error {
	allowed := []string{"package", "version", "path", "git", "rev", "branch", "tag", "registry", "workspace", "optional", "features", "default-features"}
	if dependency.Workspace {
		allowed = []string{"workspace", "optional", "features"}
	}
	for _, field := range dependency.Fields {
		if !slices.Contains(allowed, field) {
			return fmt.Errorf("dependency field %s requires supported Cargo source governance", field)
		}
	}
	if dependency.Table == "workspace.dependencies" && (dependency.Workspace || dependency.Optional) {
		return fmt.Errorf("workspace dependency definitions cannot be inherited or optional")
	}
	if dependency.Package == "" {
		return fmt.Errorf("dependency package identity cannot be empty")
	}
	return nil
}

func exactCargoRequirement(requirement string) bool {
	value, exact := strings.CutPrefix(strings.TrimSpace(requirement), "=")
	value = strings.TrimSpace(value)
	return exact && !strings.Contains(value, "+") && exactVersion.MatchString(value) && semver.IsValid("v"+value)
}

func cargoDeclarationSource(repo repository.Repository, manifest repository.CargoManifest, dependency repository.CargoDependencyDeclaration) error {
	if dependency.Path != "" && dependency.Git != "" {
		return fmt.Errorf("a Cargo dependency cannot combine path and Git sources")
	}
	if dependency.Registry != "" && dependency.Registry != "crates-io" {
		return fmt.Errorf("alternate Cargo registry %q requires authenticated provenance, age and security evidence", dependency.Registry)
	}
	if dependency.Git != "" {
		return cargoGitDependency(dependency)
	}
	if dependency.Revision != "" || dependency.Branch != "" || dependency.Tag != "" {
		return fmt.Errorf("cargo revision, branch and tag selectors require a Git source")
	}
	if dependency.Path != "" {
		return cargoLocalDependency(repo, manifest, dependency.Path)
	}
	return nil
}

func cargoGitDependency(dependency repository.CargoDependencyDeclaration) error {
	if !fullCommit.MatchString(dependency.Revision) || dependency.Branch != "" || dependency.Tag != "" {
		return fmt.Errorf("cargo Git dependencies must select a full immutable revision, without branch or tag selectors")
	}
	if strings.ContainsAny(dependency.Git, "?#") {
		return fmt.Errorf("cargo Git URLs must not embed revision selectors or query parameters")
	}
	_, err := repository.ParsePythonGitSource(dependency.Git, dependency.Revision, "")
	return err
}

func cargoLocalDependency(repo repository.Repository, manifest repository.CargoManifest, directory string) error {
	if filepath.IsAbs(directory) || strings.ContainsAny(directory, "\\\x00:") {
		return fmt.Errorf("cargo path dependencies must use contained repository-relative paths")
	}
	path := filepath.ToSlash(filepath.Join(manifest.Root, directory, "Cargo.toml"))
	if _, err := repo.ReadContainedFile(path, 8<<20); err != nil {
		return fmt.Errorf("cargo path dependency %q is not a contained readable manifest: %w", directory, err)
	}
	return nil
}

func cargoDependencyFinding(path string, dependency repository.CargoDependencyDeclaration, check, message string) policy.Finding {
	return policy.Finding{Check: check, Path: path, Subject: dependency.Table + ":" + dependency.Alias, Fields: map[string]string{"package": dependency.Package, "table": dependency.Table}, Message: message}
}
