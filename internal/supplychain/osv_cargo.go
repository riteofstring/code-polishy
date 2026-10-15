package supplychain

import (
	"encoding/json"
	"fmt"
	"path/filepath"

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
		scan.Command.Argv = append(scan.Command.Argv, "--all-packages", "--no-call-analysis", "rust")
		scans = append(scans, scan)
	}
	return scans, nil
}

func validateOSVCargoReport(scan osvScan, report osvReport) error {
	if scan.InputPath == "" || filepath.Base(scan.Scope) != "Cargo.lock" {
		return nil
	}
	var projected osvReport
	if err := json.Unmarshal(scan.Projection, &projected); err != nil {
		return fmt.Errorf("planned Cargo public package projection is malformed")
	}
	expected := map[string]bool{}
	for _, result := range projected.Results {
		for _, pkg := range result.Packages {
			expected[pkg.Package.Ecosystem+"\x00"+pkg.Package.Name+"\x00"+pkg.Package.Version] = true
		}
	}
	for _, result := range report.Results {
		for _, pkg := range result.Packages {
			key := pkg.Package.Ecosystem + "\x00" + pkg.Package.Name + "\x00" + pkg.Package.Version
			if !expected[key] {
				return fmt.Errorf("cargo scanner report repeats a package or refers outside its public package projection")
			}
			delete(expected, key)
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("cargo scanner report omitted packages from its complete locked registry inventory")
	}
	return nil
}
