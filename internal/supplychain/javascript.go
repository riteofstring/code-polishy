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

const pnpmFactsBudget = 10 * time.Minute

const pnpmLockName = "pnpm-lock.yaml"

var pnpmDirectoryProtocols = []string{"file:", "link:"}

func pnpmFacts(ctx context.Context, repo repository.Repository, directory string) (javascript.PackageResult, error) {
	factsContext, cancel := context.WithTimeout(ctx, pnpmFactsBudget)
	defer cancel()
	return javascript.Bundle{PolicyRoot: repo.PolicyRoot}.Packages(factsContext, repo.Root, directory)
}

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

func pnpmGoverned(repo repository.Repository, manifest string) bool {
	if nodeManagerName(repo, manifest) != "pnpm" {
		return false
	}
	_, found := pnpmProjectRoot(repo, filepathDirectory(manifest))
	return found
}
