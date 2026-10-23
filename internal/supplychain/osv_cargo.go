package supplychain

import (
	"fmt"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func osvCargoInventory(data []byte, path string) ([]resolvedPackage, error) {
	lock, err := repository.ParseCargoLock(path, data)
	if err != nil {
		return nil, err
	}
	packages, findings := resolvedCargoPackages(lock)
	if len(findings) != 0 {
		return nil, fmt.Errorf("cargo inventory contains unsupported sources")
	}
	return packages, nil
}

func osvCargoScans(repo repository.Repository, inputs []osvLockInput) ([]osvScan, error) {
	scans := []osvScan{}
	for _, input := range inputs {
		if !osvCoversScope(repo.Config, input.Scope) {
			continue
		}
		projection, err := publicLockProjection(input, "crates.io", "https://crates.io")
		if err != nil {
			return nil, err
		}
		if len(projection) == 0 {
			continue
		}
		scan := osvProjectionScan(repo, "cargo", input.Scope, projection)
		scans = append(scans, scan)
	}
	return scans, nil
}
