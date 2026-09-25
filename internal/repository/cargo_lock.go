package repository

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/mod/semver"
)

const CargoCratesIOSource = "registry+https://github.com/rust-lang/crates.io-index"

type CargoLock struct {
	Path     string
	Version  int
	Packages []CargoLockedPackage
}

type CargoLockedPackage struct {
	Name         string   `toml:"name"`
	Version      string   `toml:"version"`
	Source       string   `toml:"source"`
	Checksum     string   `toml:"checksum"`
	Dependencies []string `toml:"dependencies"`
}

func (repo Repository) ReadCargoLock(path string) (CargoLock, error) {
	data, err := repo.ReadContainedFile(path, 8<<20)
	if err != nil {
		return CargoLock{}, err
	}
	return ParseCargoLock(path, data)
}

func ParseCargoLock(path string, data []byte) (CargoLock, error) {
	if len(data) > 8<<20 {
		return CargoLock{}, errors.New("cargo lockfile exceeds 8388608 bytes")
	}
	var document struct {
		Version  int                  `toml:"version"`
		Packages []CargoLockedPackage `toml:"package"`
	}
	if err := toml.Unmarshal(data, &document); err != nil {
		return CargoLock{}, fmt.Errorf("parse Cargo lockfile: %w", err)
	}
	if document.Version != 3 && document.Version != 4 {
		return CargoLock{}, fmt.Errorf("unsupported Cargo lockfile version %d; version 3 or 4 is required", document.Version)
	}
	if len(document.Packages) == 0 {
		return CargoLock{}, errors.New("cargo lockfile contains no package identities")
	}
	seen := map[[3]string]bool{}
	for _, pkg := range document.Packages {
		if err := validateCargoLockedPackage(pkg); err != nil {
			return CargoLock{}, fmt.Errorf("locked Cargo package %q: %w", pkg.Name, err)
		}
		identity := [3]string{pkg.Name, pkg.Version, pkg.Source}
		if seen[identity] {
			return CargoLock{}, fmt.Errorf("cargo lockfile repeats package %q version %s from the same source", pkg.Name, pkg.Version)
		}
		seen[identity] = true
	}
	return CargoLock{Path: path, Version: document.Version, Packages: document.Packages}, nil
}

func validateCargoLockedPackage(pkg CargoLockedPackage) error {
	core, _, _ := strings.Cut(pkg.Version, "+")
	core, _, _ = strings.Cut(core, "-")
	if strings.TrimSpace(pkg.Name) == "" || !semver.IsValid("v"+pkg.Version) || strings.Count(core, ".") != 2 {
		return errors.New("package must have a name and complete semantic version")
	}
	if strings.HasPrefix(pkg.Source, "registry+") || strings.HasPrefix(pkg.Source, "sparse+") {
		checksum, err := hex.DecodeString(pkg.Checksum)
		if err != nil || len(checksum) != 32 {
			return errors.New("registry package must carry its complete SHA-256 archive checksum")
		}
	} else if pkg.Checksum != "" {
		return errors.New("non-registry package cannot claim a registry archive checksum")
	}
	for _, dependency := range pkg.Dependencies {
		if strings.TrimSpace(dependency) == "" {
			return errors.New("locked dependency identity cannot be empty")
		}
	}
	return nil
}
