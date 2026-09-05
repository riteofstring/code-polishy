package supplychain

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func validateDependencyDeclaration(value string) error {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 || strings.ContainsAny(value, "$"+"{}") {
		return fmt.Errorf("dependency declaration is empty, oversized, or dynamic")
	}
	if !strings.Contains(value, "://") {
		return nil
	}
	return validateInventoryURL(value)
}

func validateInventoryURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" {
		return fmt.Errorf("dependency source URL is malformed")
	}
	if parsed.User != nil && !((parsed.Scheme == "ssh" || parsed.Scheme == "git+ssh") && parsed.User.String() == "git") {
		return fmt.Errorf("dependency source URL contains user information")
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("dependency source URL query is unsupported for comparison")
	}
	return nil
}

func validateNodeInventoryDeclaration(manifest, value string) error {
	if err := validateDependencyDeclaration(value); err != nil {
		return err
	}
	for _, protocol := range []string{"file:", "link:"} {
		if relative, local := strings.CutPrefix(value, protocol); local {
			return validateInventoryLocalPath(manifest, relative)
		}
	}
	return nil
}

func addResolvedInventoryRecords(inventory dependencyInventoryMap, direct map[string]bool, records []dependencyRecord) {
	for _, record := range records {
		if record.Ecosystem != "git" && direct[record.Ecosystem+"\x00"+record.Name+"\x00"+record.Version] {
			continue
		}
		addDependencyRecords(inventory, []dependencyRecord{record})
	}
}

func pythonInventoryRecord(manifest string, dependency repository.PythonRequirement) (dependencyRecord, error) {
	record := dependencyRecord{Scope: manifest, Directness: "direct", Usage: dependency.Usage, Name: dependency.Name}
	switch dependency.Kind {
	case repository.PythonRegistryRequirement:
		record.Ecosystem, record.Version = "pypi", pythonInventoryVersion(dependency)
	case repository.PythonGitRequirement:
		record.Ecosystem, record.Version = "git", dependency.Git.InventoryIdentity()
	case repository.PythonFileRequirement:
		if err := validateInventoryLocalPath(manifest, dependency.FilePath); err != nil {
			return dependencyRecord{}, err
		}
		record.Ecosystem, record.Version = "file", "file:"+dependency.FilePath
	case repository.PythonURLRequirement:
		if err := validateDependencyDeclaration(dependency.URL); err != nil {
			return dependencyRecord{}, fmt.Errorf("parse %s: dependency %q: %w", manifest, dependency.Name, err)
		}
		record.Ecosystem, record.Version = "url", dependency.URL
	default:
		return dependencyRecord{}, fmt.Errorf("parse %s: dependency %q has an unsupported source kind", manifest, dependency.Name)
	}
	return record, nil
}

func pythonInventoryVersion(dependency repository.PythonRequirement) string {
	if version, exact := dependency.ExactRegistryVersion(); exact {
		return version
	}
	if dependency.Version == "" {
		return "*"
	}
	return dependency.Version
}

func validateInventoryLocalPath(manifest, relative string) error {
	if relative == "" || path.IsAbs(relative) || strings.Contains(relative, "\\") || strings.Contains(relative, ":") {
		return fmt.Errorf("parse %s: local dependency must be repository-relative", manifest)
	}
	target := path.Join(path.Dir(manifest), relative)
	if target == ".." || strings.HasPrefix(target, "../") {
		return fmt.Errorf("parse %s: local dependency escapes the repository", manifest)
	}
	return nil
}

func validateResolvedInventorySource(item resolvedPackage) error {
	switch item.Source.Kind {
	case "unsupported":
		return fmt.Errorf("parse %s: package %q has unsupported source coverage", item.Scope, item.Name)
	case "local":
		return validateInventoryLocalPath(item.Scope, item.Source.LocalPath)
	case "registry":
		if err := validateDependencyDeclaration(item.Source.Registry); err != nil {
			return fmt.Errorf("parse %s: package %q: %w", item.Scope, item.Name, err)
		}
	}
	return nil
}
