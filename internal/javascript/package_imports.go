package javascript

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
)

func (bundle Bundle) PackageImports(ctx context.Context, root, manifest string) (ImportResult, error) {
	payload, err := fileRequest("package-imports", root, []string{manifest})
	if err != nil {
		return ImportResult{}, err
	}
	result, err := bundle.exchange(ctx, payload, importsTimeout)
	if err != nil {
		return ImportResult{}, err
	}
	return decodePackageImports(result, manifest)
}

func decodePackageImports(data json.RawMessage, manifest string) (ImportResult, error) {
	var wire importResultWire
	if err := decodeExactly(data, &wire); err != nil {
		return ImportResult{}, err
	}
	if wire.Analyzed == nil || wire.Unsupported == nil {
		return ImportResult{}, fmt.Errorf("published artifact imports omitted coverage")
	}
	paths := slices.Clone(*wire.Analyzed)
	for _, item := range *wire.Unsupported {
		paths = append(paths, item.Path)
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	if err := validatePackageImportCoverage(manifest, paths, len(*wire.Unsupported)); err != nil {
		return ImportResult{}, err
	}
	result, err := decodeImportResult(data, paths)
	if err != nil {
		return ImportResult{}, err
	}
	for _, fact := range result.Imports {
		if fact.Resolved != "" && !slices.Contains(paths, fact.Resolved) {
			return ImportResult{}, fmt.Errorf("published artifact import %q has no target coverage", fact.Specifier)
		}
	}
	return result, nil
}

func validatePackageImportCoverage(manifest string, paths []string, unsupported int) error {
	if !slices.Contains(paths, manifest) || len(paths) > maximumOperationPaths || unsupported == 0 && len(paths) < 2 {
		return fmt.Errorf("published artifact imports omitted the manifest or its artifacts, or exceeded the file limit")
	}
	directory := filepath.ToSlash(filepath.Dir(manifest))
	for _, path := range paths {
		if !containedPath(path) || !containsPath(directory, path) {
			return fmt.Errorf("published artifact imports escaped package %q", directory)
		}
	}
	return nil
}
