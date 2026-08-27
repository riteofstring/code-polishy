package supplychain

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type resolvedPackage struct {
	Ecosystem string
	Name      string
	Version   string
	Scope     string
	Locations []string
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
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	packages := []resolvedPackage{}
	current := uvLockPackage{}
	inPackage := false
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "[[package]]" {
			if err := appendUVPackage(&packages, current, inPackage, scope); err != nil {
				return nil, fmt.Errorf("parse %s before line %d: %w", scope, lineNumber, err)
			}
			current = uvLockPackage{}
			inPackage = true
			continue
		}
		if inPackage {
			parseUVPackageField(line, &current)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse %s: %w", scope, err)
	}
	if err := appendUVPackage(&packages, current, inPackage, scope); err != nil {
		return nil, fmt.Errorf("parse %s: %w", scope, err)
	}
	return uniqueResolvedPackages(packages), nil
}

func appendUVPackage(packages *[]resolvedPackage, current uvLockPackage, inPackage bool, scope string) error {
	if !inPackage || !current.registry {
		return nil
	}
	if current.name == "" || !exactVersion.MatchString(current.version) {
		return errors.New("registry package omitted an exact name or version")
	}
	*packages = append(*packages, resolvedPackage{Ecosystem: "pypi", Name: current.name, Version: current.version, Scope: scope})
	return nil
}

func parseUVPackageField(line string, current *uvLockPackage) {
	name, value, found := strings.Cut(line, "=")
	if !found {
		return
	}
	switch strings.TrimSpace(name) {
	case "name":
		current.name = tomlString(value)
	case "version":
		current.version = tomlString(value)
	case "source":
		current.registry = strings.Contains(value, "registry")
	}
}

type uvLockPackage struct {
	name, version string
	registry      bool
}

func tomlString(value string) string {
	value = strings.TrimSpace(strings.SplitN(value, "#", 2)[0])
	if decoded, err := strconv.Unquote(value); err == nil {
		return decoded
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	return ""
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
	return item.Ecosystem + "\x00" + item.Scope + "\x00" + item.Name + "\x00" + item.Version
}
