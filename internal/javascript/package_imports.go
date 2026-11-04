package javascript

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

func (bundle Bundle) packageImports(ctx context.Context, root string) (ImportResult, error) {
	payload, err := fileRequest("package-imports", root, []string{packedManifest})
	if err != nil {
		return ImportResult{}, err
	}
	result, err := bundle.exchange(ctx, payload, importsTimeout)
	if err != nil {
		return ImportResult{}, err
	}
	return decodePackageImports(result)
}

func decodePackageImports(data json.RawMessage) (ImportResult, error) {
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
	if err := validatePackageImportCoverage(paths, len(*wire.Unsupported)); err != nil {
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

func validatePackageImportCoverage(paths []string, unsupported int) error {
	if !slices.Contains(paths, packedManifest) || len(paths) > maximumOperationPaths || unsupported == 0 && len(paths) < 2 {
		return fmt.Errorf("published artifact imports omitted the manifest or its artifacts, or exceeded the file limit")
	}
	for _, path := range paths {
		if !containedPath(path) {
			return fmt.Errorf("published artifact imports escaped the packed package")
		}
	}
	return nil
}
