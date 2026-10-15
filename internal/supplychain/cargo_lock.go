package supplychain

import (
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func checkCargoLock(repo repository.Repository, path string) []policy.Finding {
	lock, err := repo.ReadCargoLock(path)
	if err != nil {
		return []policy.Finding{{Check: "supplyChain.cargoLock", Path: path, Subject: "lockfile", Message: err.Error()}}
	}
	_, findings := resolvedCargoPackages(lock)
	return findings
}

func resolvedCargoPackages(lock repository.CargoLock) ([]resolvedPackage, []policy.Finding) {
	packages := []resolvedPackage{}
	findings := []policy.Finding{}
	for _, pkg := range lock.Packages {
		if pkg.Source == "" {
			continue
		}
		item := resolvedPackage{Ecosystem: "crates.io", Name: pkg.Name, Version: pkg.Version, Scope: lock.Path}
		switch {
		case pkg.Source == repository.CargoCratesIOSource:
			item.Source = resolvedSource{Kind: "registry", Registry: "https://crates.io"}
		case strings.HasPrefix(pkg.Source, "git+"):
			source, err := repository.ParseCargoGitLockSource(pkg.Source)
			if err != nil {
				findings = append(findings, cargoLockSourceFinding(lock.Path, pkg, err.Error()))
				continue
			}
			item.Ecosystem, item.Source = "git", resolvedSource{Kind: "git", Git: source}
		default:
			findings = append(findings, cargoLockSourceFinding(lock.Path, pkg, "Cargo lock source requires authenticated registry/source governance; it is not a public crates.io coordinate"))
			continue
		}
		packages = append(packages, item)
	}
	return packages, findings
}

func cargoLockSourceFinding(path string, pkg repository.CargoLockedPackage, message string) policy.Finding {
	return policy.Finding{Check: "supplyChain.cargoSource", Path: path, Subject: pkg.Name + "@" + pkg.Version, Message: message}
}
