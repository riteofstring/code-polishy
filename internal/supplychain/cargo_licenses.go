package supplychain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const cargoArchiveCache = ".tools/cargo-home/registry/cache"

type cargoLicenseObservation struct {
	License string
	Err     error
}

type cargoArchiveObservation struct {
	cargoLicenseObservation
	Checksum string
}

type cargoLicenseInventory struct {
	Archives  map[string]cargoArchiveObservation
	Remaining int
}

func cargoLicenseFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	allowed := licenseAllowList(repo.Config.SupplyChain.AllowedLicenses)
	if len(allowed) == 0 {
		return nil
	}
	factsContext, cancel := context.WithTimeout(ctx, licenseFactsBudget)
	defer cancel()
	observed := map[[3]string]cargoLicenseObservation{}
	inventory := cargoLicenseInventory{Archives: map[string]cargoArchiveObservation{}, Remaining: 1 << 30}
	findings := []policy.Finding{}
	for _, path := range files {
		if filepath.Base(path) != "Cargo.lock" {
			continue
		}
		lock, err := repo.ReadCargoLock(path)
		if err != nil {
			continue
		}
		for _, pkg := range lock.Packages {
			if pkg.Source != repository.CargoCratesIOSource {
				continue
			}
			key := [3]string{pkg.Name, pkg.Version, strings.ToLower(pkg.Checksum)}
			observation, found := observed[key]
			if !found {
				license, err := cachedCargoLicense(factsContext, repo, pkg, &inventory)
				observation = cargoLicenseObservation{License: license, Err: err}
				observed[key] = observation
			}
			if finding := cargoLicenseFinding(path, pkg, observation, allowed); finding != nil {
				findings = append(findings, *finding)
			}
		}
	}
	return uniqueFindings(findings)
}

func cargoLicenseFinding(scope string, pkg repository.CargoLockedPackage, observation cargoLicenseObservation, allowed map[string]bool) *policy.Finding {
	finding := policy.Finding{Path: scope, Subject: pkg.Name + "@" + pkg.Version}
	if observation.Err != nil {
		finding.Check, finding.Message = "supplyChain.licenseCoverage", observation.Err.Error()
		return &finding
	}
	if err := licenseAdmitted(observation.License, allowed); err != nil {
		finding.Check, finding.Message = "supplyChain.dependencyLicense", err.Error()
		return &finding
	}
	return nil
}

func cachedCargoLicense(ctx context.Context, repo repository.Repository, pkg repository.CargoLockedPackage, inventory *cargoLicenseInventory) (string, error) {
	if !cargoArchiveName(pkg.Name) {
		return "", fmt.Errorf("public crate name is not a safe registry identity")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	directories, err := cargoArchiveDirectories(repo)
	if err != nil {
		return "", err
	}
	var problem error
	for _, directory := range directories {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		path := directory + "/" + pkg.Name + "-" + pkg.Version + ".crate"
		observed := inventory.archive(ctx, repo, path, pkg)
		if errors.Is(observed.Err, os.ErrNotExist) {
			continue
		}
		if observed.Checksum == "" {
			problem = observed.Err
			continue
		}
		if !strings.EqualFold(observed.Checksum, pkg.Checksum) {
			continue
		}
		return observed.License, observed.Err
	}
	return "", errors.Join(fmt.Errorf("no cached crate archive matches the Cargo.lock checksum; prepare the governed Cargo home with pinned cargo fetch --locked before installed-license verification"), problem)
}

func (inventory *cargoLicenseInventory) archive(ctx context.Context, repo repository.Repository, path string, pkg repository.CargoLockedPackage) cargoArchiveObservation {
	if observed, found := inventory.Archives[path]; found {
		return observed
	}
	data, err := readCargoArchive(repo, path, &inventory.Remaining)
	observed := cargoArchiveObservation{cargoLicenseObservation: cargoLicenseObservation{Err: err}}
	if err == nil {
		checksum := sha256.Sum256(data)
		observed.Checksum = hex.EncodeToString(checksum[:])
		observed.License, observed.Err = cargoArchiveLicense(ctx, data, pkg)
	}
	inventory.Archives[path] = observed
	return observed
}

func readCargoArchive(repo repository.Repository, path string, remaining *int) ([]byte, error) {
	if *remaining <= 0 {
		return nil, fmt.Errorf("cargo license inventory exceeds its archive byte budget")
	}
	data, err := repo.ReadContainedFile(path, min(128<<20, *remaining))
	*remaining -= len(data)
	return data, err
}

func cargoArchiveDirectories(repo repository.Repository) ([]string, error) {
	entries, err := repo.ReadContainedDirectory(cargoArchiveCache, 256)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	directories := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directories = append(directories, cargoArchiveCache+"/"+entry.Name())
	}
	slices.Sort(directories)
	return directories, nil
}

func cargoArchiveName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, character := range name {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_", character) {
			return false
		}
	}
	return true
}
