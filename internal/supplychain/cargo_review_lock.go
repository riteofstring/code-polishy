package supplychain

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func cargoReviewLock(data []byte, path string) ([]resolvedPackage, error) {
	lock, err := repository.ParseCargoLock(path, data)
	if err != nil {
		return nil, fmt.Errorf("cargo lock dependency inventory is malformed, ambiguous, or unsafe")
	}
	packages := []resolvedPackage{}
	for _, pkg := range lock.Packages {
		if pkg.Source == "" {
			continue
		}
		item := resolvedPackage{Name: pkg.Name, Version: pkg.Version, Scope: path}
		if err := cargoReviewLockedSource(pkg.Source, &item); err != nil {
			return nil, fmt.Errorf("cargo lock dependency inventory is malformed, ambiguous, or unsafe")
		}
		packages = append(packages, item)
	}
	return packages, nil
}

func cargoReviewLockedSource(value string, item *resolvedPackage) error {
	if strings.HasPrefix(value, "git+") {
		source, err := repository.ParseCargoGitLockSource(value)
		item.Ecosystem, item.Source = "git", resolvedSource{Kind: "git", Git: source}
		return err
	}
	kind, registry, found := strings.Cut(value, "+")
	if !found || !slices.Contains([]string{"registry", "sparse"}, kind) {
		return fmt.Errorf("unsupported Cargo lock source identity")
	}
	if err := validateInventoryURL(registry); err != nil {
		return err
	}
	parsed, err := url.Parse(registry)
	if err != nil || parsed.Scheme == "" || parsed.Fragment != "" {
		return fmt.Errorf("cargo registry identity must be an absolute URL without a fragment")
	}
	item.Ecosystem, item.Source = "cargo-registry", resolvedSource{Kind: "registry", Registry: registry}
	if value == repository.CargoCratesIOSource {
		item.Ecosystem = "crates.io"
		return nil
	}
	item.Version = value + "@" + item.Version
	return nil
}
