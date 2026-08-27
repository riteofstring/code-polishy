package supplychain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

// How long the sealed bundle has to read one pnpm project. It parses one
// lockfile and one manifest per workspace package and resolves nothing.
const pnpmFactsBudget = 10 * time.Minute

// The lockfile that makes a directory a pnpm project. It is the exact record of
// what the target installs, so it also decides which directory owns a package.
const pnpmLockName = "pnpm-lock.yaml"

// The protocols a target must already allow for a local directory to be an
// admissible resolved source. Only a registry resolution names a published
// release the other supply-chain lanes can age, scan, and license; a directory
// is the local dependency surface allowedDependencyProtocols governs, and
// nothing else is admissible.
var pnpmDirectoryProtocols = []string{"file:", "link:"}

// pnpmFacts asks the sealed, policy-owned JavaScript bundle what one pnpm
// project installs.
//
// A pnpm lockfile is YAML, and this engine is standard-library-only, so reading
// it here would mean approximating with line scanning a format the bundle reads
// exactly with the parser pnpm itself uses. The bundle reports facts; every
// decision below this call is Go's.
func pnpmFacts(ctx context.Context, repo repository.Repository, directory string) (javascript.PackageResult, error) {
	factsContext, cancel := context.WithTimeout(ctx, pnpmFactsBudget)
	defer cancel()
	return javascript.Bundle{PolicyRoot: repo.PolicyRoot}.Packages(factsContext, repo.Root, directory)
}

// pnpmRevisionFacts reads the resolved graph one lockfile recorded at a
// revision. The bundle reads a directory and a revision is not one, so the
// lockfile is written into a policy-owned scratch directory and read there.
// Both sides of a dependency comparison go through this, so a reported change
// is a real change rather than the difference between two readers.
func pnpmRevisionFacts(ctx context.Context, repo repository.Repository, data []byte) (javascript.PackageResult, error) {
	scratch, err := os.MkdirTemp("", "code-polishy-pnpm-lock-")
	if err != nil {
		return javascript.PackageResult{}, fmt.Errorf("create a scratch directory for a pnpm lockfile: %w", err)
	}
	defer os.RemoveAll(scratch)
	if err := os.WriteFile(filepath.Join(scratch, pnpmLockName), data, 0o600); err != nil {
		return javascript.PackageResult{}, fmt.Errorf("stage a pnpm lockfile for reading: %w", err)
	}
	factsContext, cancel := context.WithTimeout(ctx, pnpmFactsBudget)
	defer cancel()
	return javascript.Bundle{PolicyRoot: repo.PolicyRoot}.Packages(factsContext, scratch, ".")
}

// revisionPNPMPackages reports the releases one lockfile resolved at a
// revision. A dependency comparison needs the graph alone: the manifests that
// would make lock consistency decidable are not part of a revision's lockfile,
// and consistency is decided against the working tree instead.
func revisionPNPMPackages(ctx context.Context, repo repository.Repository, data []byte, scope string) ([]resolvedPackage, error) {
	result, err := pnpmRevisionFacts(ctx, repo, data)
	if err != nil {
		return nil, err
	}
	if err := unreadableLock(result, scope); err != nil {
		return nil, err
	}
	return resolvedPNPMPackages(result, scope), nil
}

// unreadableLock reports why the bundle could not read one lockfile at all. An
// empty version is exactly the answer it gives when nothing in the result can
// be trusted; a manifest it could not read leaves the resolved graph intact and
// is reported as missing lock coverage instead.
func unreadableLock(result javascript.PackageResult, scope string) error {
	if result.LockfileVersion != "" {
		return nil
	}
	reason := "the sealed JavaScript bundle reported no lockfile version"
	if len(result.Unsupported) > 0 {
		reason = result.Unsupported[0].Reason
	}
	return fmt.Errorf("read %s: %s", scope, reason)
}

// resolvedPNPMPackages reports every release one lockfile resolved, as the
// package identities the release-age, vulnerability, and dependency-review
// lanes govern. A source that is not the registry is refused by name in
// pnpmSourceFindings rather than aged or scanned as though it were a published
// release, and a workspace link resolves to no release at all.
func resolvedPNPMPackages(result javascript.PackageResult, scope string) []resolvedPackage {
	packages := []resolvedPackage{}
	for _, item := range result.Packages {
		if item.Source != "registry" || !exactVersion.MatchString(item.Version) {
			continue
		}
		packages = append(packages, resolvedPackage{
			Ecosystem: "pnpm", Name: item.Name, Version: item.Version, Scope: scope,
			Locations: []string{item.Name + "@" + item.Version},
		})
	}
	return uniqueResolvedPackages(packages)
}

// pnpmProjectFindings decides what the sealed bundle reported about every pnpm
// project the selection touches.
func pnpmProjectFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, directory := range pnpmProjectRoots(repo, files) {
		lock := lockPath(directory)
		result, err := pnpmFacts(ctx, repo, directory)
		if err != nil {
			findings = append(findings, policy.Finding{
				Check: "policy.tool", Path: lock, Subject: "javascript-bundle", Message: err.Error(),
			})
			continue
		}
		findings = append(findings, pnpmLockFindings(repo.Config, result, lock)...)
		findings = append(findings, pnpmLicenseFindings(ctx, repo, result, directory, lock)...)
	}
	return findings
}

func pnpmLockFindings(config policy.Config, result javascript.PackageResult, lock string) []policy.Finding {
	findings := []policy.Finding{}
	for _, entry := range result.Unsupported {
		findings = append(findings, policy.Finding{
			Check: "supplyChain.lockCoverage", Path: entry.Path, Subject: "pnpm",
			Message: "the policy-owned pnpm reader could not read this file: " + entry.Reason,
		})
	}
	for _, importer := range result.Importers {
		findings = append(findings, pnpmImporterFindings(importer)...)
	}
	return append(findings, pnpmSourceFindings(config, result, lock)...)
}

// pnpmImporterFindings compares what one workspace package declares with what
// the lock resolved for it. Both directions are drift: a declaration the lock
// never resolved means the installed tree is not what the manifest asks for,
// and a resolution no manifest declared means the target installs something it
// never asked for.
func pnpmImporterFindings(importer javascript.Importer) []policy.Finding {
	findings := []policy.Finding{}
	for _, dependency := range importer.Dependencies {
		message := ""
		switch {
		case dependency.Specifier == "":
			message = fmt.Sprintf("%s dependency %q is declared as %q and the lockfile resolves it to nothing", dependency.Scope, dependency.Name, dependency.Declared)
		case dependency.Declared == "":
			message = fmt.Sprintf("the lockfile resolves %s dependency %q, which this manifest does not declare", dependency.Scope, dependency.Name)
		case dependency.Declared != dependency.Specifier:
			message = fmt.Sprintf("%s dependency %q is declared as %q and the lockfile records %q", dependency.Scope, dependency.Name, dependency.Declared, dependency.Specifier)
		default:
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "supplyChain.lockConsistency", Path: importer.Manifest, Subject: dependency.Name, Message: message,
		})
	}
	return findings
}

// pnpmSourceFindings refuses every resolved package the target did not admit a
// source for. A registry resolution is the only published release the other
// supply-chain lanes can age, scan, and license; a directory is the local
// dependency surface allowedDependencyProtocols governs, and a Git revision, a
// downloaded tarball, or a resolution this format does not describe is refused
// outright, whether or not it records integrity over its own bytes.
func pnpmSourceFindings(config policy.Config, result javascript.PackageResult, lock string) []policy.Finding {
	findings := []policy.Finding{}
	reported := map[string]bool{}
	for _, item := range result.Packages {
		subject := item.Name + "@" + item.Version
		if reported[subject] || pnpmSourceAdmitted(config, item.Source) {
			continue
		}
		reported[subject] = true
		findings = append(findings, policy.Finding{
			Check: "supplyChain.dependencySource", Path: lock, Subject: subject,
			Message: pnpmSourceMessage(item.Source),
		})
	}
	return findings
}

func pnpmSourceAdmitted(config policy.Config, source string) bool {
	if source == "registry" {
		return true
	}
	if source != "directory" {
		return false
	}
	for _, protocol := range pnpmDirectoryProtocols {
		if slices.Contains(config.SupplyChain.AllowedDependencyProtocols, protocol) {
			return true
		}
	}
	return false
}

func pnpmSourceMessage(source string) string {
	if source == "directory" {
		return "resolved package is a local directory, which supplyChain.allowedDependencyProtocols does not admit"
	}
	return fmt.Sprintf("resolved package comes from a %s source, which is not a published registry release", source)
}

// pnpmProjectRoots names every pnpm project the selection touches, once each. A
// project is a directory owning a lockfile, and a workspace package is governed
// by the project above it, so selecting one member reads the project that
// actually resolves it.
func pnpmProjectRoots(repo repository.Repository, files []string) []string {
	roots := map[string]bool{}
	for _, path := range files {
		if !slices.Contains([]string{"package.json", pnpmLockName, "pnpm-workspace.yaml"}, filepath.Base(path)) {
			continue
		}
		if root, found := pnpmProjectRoot(repo, filepathDirectory(path)); found {
			roots[root] = true
		}
	}
	ordered := make([]string, 0, len(roots))
	for root := range roots {
		ordered = append(ordered, root)
	}
	sort.Strings(ordered)
	return ordered
}

// pnpmProjectRoot walks up from one directory to the nearest one holding a
// lockfile.
func pnpmProjectRoot(repo repository.Repository, directory string) (string, bool) {
	for {
		if regularFile(filepath.Join(repo.Root, filepath.FromSlash(lockPath(directory)))) {
			return directory, true
		}
		if directory == "." {
			return "", false
		}
		directory = filepathDirectory(directory)
	}
}

func lockPath(directory string) string {
	if directory == "." {
		return pnpmLockName
	}
	return directory + "/" + pnpmLockName
}

// pnpmGoverned reports whether one dependency manifest belongs to a pnpm
// project: its package manager is pnpm and a lockfile above it records what it
// installs. Code Polishy owns lock consistency for those, so the target needs no
// lock-sync provider of its own.
func pnpmGoverned(repo repository.Repository, manifest string) bool {
	if nodeManagerName(repo, manifest) != "pnpm" {
		return false
	}
	_, found := pnpmProjectRoot(repo, filepathDirectory(manifest))
	return found
}
