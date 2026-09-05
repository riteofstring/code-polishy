package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type resolvedPackage struct {
	Ecosystem string
	Name      string
	Version   string
	Scope     string
	Locations []string
	Source    resolvedSource
}

type resolvedSource struct {
	Kind      string
	Registry  string
	LocalPath string
	Reason    string
	Git       repository.PythonGitSource
}

func resolvedNodePackages(ctx context.Context, repo repository.Repository, manifest nodeManifest) ([]resolvedPackage, error) {
	path, found := nodeLockPath(repo, manifest.Root, manifest.Manager)
	if !found {
		return nil, fmt.Errorf("exact %s lockfile is unavailable", manifest.Manager)
	}
	if manifest.Manager == "pnpm" {
		result, err := pnpmFacts(ctx, repo, manifest.Root)
		if err != nil {
			return nil, err
		}
		if err := unreadableLock(result, path); err != nil {
			return nil, err
		}
		return resolvedPNPMPackages(result, path), nil
	}
	data, err := repo.Read(path)
	if err != nil {
		return nil, err
	}
	if manifest.Manager != "npm" {
		return nil, fmt.Errorf("resolved release-age inspection does not support %s", manifest.Manager)
	}
	return parsePackageLock(data, path, manifest.Manager)
}

func nodeLockPath(repo repository.Repository, root, manager string) (string, bool) {
	names := map[string][]string{
		"npm":  {"npm-shrinkwrap.json", "package-lock.json"},
		"pnpm": {pnpmLockName},
	}
	for _, name := range names[manager] {
		path := name
		if root != "." {
			path = root + "/" + name
		}
		if regularFile(filepath.Join(repo.Root, filepath.FromSlash(path))) {
			return path, true
		}
	}
	return "", false
}

type packageLockDocument struct {
	LockfileVersion int                         `json:"lockfileVersion"`
	Packages        map[string]packageLockEntry `json:"packages"`
	Dependencies    map[string]packageLockEntry `json:"dependencies"`
}

type packageLockEntry struct {
	Name         string                      `json:"name"`
	Version      string                      `json:"version"`
	Resolved     string                      `json:"resolved"`
	Link         bool                        `json:"link"`
	Dependencies map[string]packageLockEntry `json:"dependencies"`
}

func parsePackageLock(data []byte, scope, ecosystem string) ([]resolvedPackage, error) {
	var document packageLockDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse %s: %w", scope, err)
	}
	packages := []resolvedPackage{}
	if document.Packages != nil {
		for path, entry := range document.Packages {
			name := packageLockName(path, entry.Name)
			if name == "" || entry.Link || !exactVersion.MatchString(entry.Version) {
				continue
			}
			packages = append(packages, resolvedPackage{
				Ecosystem: ecosystem, Name: name, Version: entry.Version, Scope: scope,
				Locations: []string{filepath.ToSlash(path)},
			})
		}
	} else {
		walkPackageLockDependencies(document.Dependencies, ecosystem, scope, &packages)
	}
	return uniqueResolvedPackages(packages), nil
}

func packageLockName(path, declared string) string {
	_ = declared
	marker := "node_modules/"
	index := strings.LastIndex(filepath.ToSlash(path), marker)
	if index >= 0 {
		return strings.TrimPrefix(filepath.ToSlash(path)[index+len(marker):], "/")
	}
	return ""
}

func walkPackageLockDependencies(entries map[string]packageLockEntry, ecosystem, scope string, result *[]resolvedPackage) {
	for name, entry := range entries {
		if !entry.Link && exactVersion.MatchString(entry.Version) {
			*result = append(*result, resolvedPackage{Ecosystem: ecosystem, Name: name, Version: entry.Version, Scope: scope})
		}
		walkPackageLockDependencies(entry.Dependencies, ecosystem, scope, result)
	}
}

func parseUVLock(data []byte, scope string) ([]resolvedPackage, error) {
	return parseUVLockWith(data, scope, parseUVSourceFields)
}

func parseUVLockWith(data []byte, scope string, parseSource func([]repository.PythonLockSourceField) (resolvedSource, error)) ([]resolvedPackage, error) {
	lock, err := repository.ParsePythonUVLock(scope, data)
	if err != nil {
		return nil, err
	}
	packages := make([]resolvedPackage, 0, len(lock.Packages))
	for _, item := range lock.Packages {
		source, err := parseSource(item.Source)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", scope, err)
		}
		if source.Kind == "registry" && item.Version == "" {
			return nil, fmt.Errorf("parse %s: registry package omitted a version", scope)
		}
		ecosystem := "pypi"
		switch source.Kind {
		case "git":
			ecosystem = "git"
		case "local":
			ecosystem = "local"
		case "unsupported":
			ecosystem = "python"
		}
		packages = append(packages, resolvedPackage{
			Ecosystem: ecosystem,
			Name:      item.Name,
			Version:   item.Version,
			Scope:     scope,
			Source:    source,
		})
	}
	return uniqueResolvedPackages(packages), nil
}

func parseUVSourceFields(source []repository.PythonLockSourceField) (resolvedSource, error) {
	parsed, err := parseUVSourceFieldsWith(source, repository.ParsePythonGitSource)
	if err != nil {
		return resolvedSource{Kind: "unsupported", Reason: err.Error()}, nil
	}
	return parsed, nil
}

func parseUVInventorySourceFields(source []repository.PythonLockSourceField) (resolvedSource, error) {
	return parseUVSourceFieldsWith(source, repository.ParsePythonGitInventorySource)
}

func parseUVSourceFieldsWith(source []repository.PythonLockSourceField, readGit func(string, string, string) (repository.PythonGitSource, error)) (resolvedSource, error) {
	fields, err := uvSourceFieldMap(source)
	if err != nil {
		return resolvedSource{}, err
	}
	if registry, found := fields["registry"]; found && registry != "" {
		return resolvedSource{Kind: "registry", Registry: registry}, nil
	}
	if gitURL, found := fields["git"]; found {
		commit := fields["commit"]
		if commit == "" {
			commit = fields["rev"]
		}
		git, err := readGit(gitURL, commit, fields["subdirectory"])
		if err != nil {
			return resolvedSource{}, err
		}
		return resolvedSource{Kind: "git", Git: git}, nil
	}
	for _, key := range []string{"editable", "directory", "virtual"} {
		if path, found := fields[key]; found {
			return resolvedSource{Kind: "local", LocalPath: path}, nil
		}
	}
	return resolvedSource{Kind: "unsupported", Reason: "uv lock package source is unsupported"}, nil
}

func uvSourceFieldMap(source []repository.PythonLockSourceField) (map[string]string, error) {
	fields := make(map[string]string, len(source))
	for _, field := range source {
		if _, found := fields[field.Name]; found {
			return nil, fmt.Errorf("uv lock package source repeats a field")
		}
		fields[field.Name] = field.Value
	}
	if err := validateUVSourceFields(fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func validateUVSourceFields(fields map[string]string) error {
	kinds := 0
	for _, kind := range []string{"registry", "git", "editable", "directory", "virtual", "url", "path"} {
		if _, found := fields[kind]; found {
			kinds++
		}
	}
	if kinds != 1 {
		return fmt.Errorf("uv lock package must have exactly one source kind")
	}
	for name := range fields {
		switch name {
		case "registry", "git", "editable", "directory", "virtual", "url", "path":
		case "commit", "rev", "subdirectory":
			if fields["git"] == "" {
				return fmt.Errorf("uv lock package has Git fields without a Git source")
			}
		default:
			return fmt.Errorf("uv lock package has unsupported source fields")
		}
	}
	if fields["commit"] != "" && fields["rev"] != "" && fields["commit"] != fields["rev"] {
		return fmt.Errorf("uv lock package declares contradictory commit fields")
	}
	return nil
}

func uniqueResolvedPackages(packages []resolvedPackage) []resolvedPackage {
	sort.Slice(packages, func(left, right int) bool {
		return resolvedPackageKey(packages[left]) < resolvedPackageKey(packages[right])
	})
	result := make([]resolvedPackage, 0, len(packages))
	last := ""
	for _, item := range packages {
		key := resolvedPackageKey(item)
		sort.Strings(item.Locations)
		item.Locations = compactStrings(item.Locations)
		if len(result) > 0 && key == last {
			locations := append(result[len(result)-1].Locations, item.Locations...)
			sort.Strings(locations)
			result[len(result)-1].Locations = compactStrings(locations)
			continue
		}
		result = append(result, item)
		last = key
	}
	return result
}

func resolvedPackageKey(item resolvedPackage) string {
	return item.Ecosystem + "\x00" + item.Scope + "\x00" + item.Name + "\x00" + item.Version + "\x00" + resolvedSourceKey(item.Source)
}

func resolvedSourceKey(source resolvedSource) string {
	switch source.Kind {
	case "git":
		return source.Kind + "\x00" + source.Git.InventoryIdentity()
	case "registry":
		return source.Kind + "\x00" + source.Registry
	case "local":
		return source.Kind + "\x00" + source.LocalPath
	default:
		return source.Kind + "\x00" + source.Reason
	}
}
