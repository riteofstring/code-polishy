package javascript

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type importResultWire struct {
	Analyzed    *[]string         `json:"analyzed"`
	Imports     *[]importFactWire `json:"imports"`
	Unsupported *[]Unsupported    `json:"unsupported"`
}

type importFactWire struct {
	Path      *string `json:"path"`
	Line      *int    `json:"line"`
	Column    *int    `json:"column"`
	Specifier *string `json:"specifier"`
	Resolved  *string `json:"resolved"`
	Package   *string `json:"package"`
	Kind      *string `json:"kind"`
}

func decodeImportResult(data []byte, requested []string) (ImportResult, error) {
	var wire importResultWire
	if err := decodeExactly(data, &wire); err != nil {
		return ImportResult{}, err
	}
	if wire.Analyzed == nil || wire.Imports == nil || wire.Unsupported == nil {
		return ImportResult{}, fmt.Errorf("the import result is missing required fields")
	}
	requestedSet, err := importRequestedPaths(requested)
	if err != nil {
		return ImportResult{}, err
	}
	analyzed, analyzedSet, err := validateAnalyzedImports(*wire.Analyzed, requestedSet)
	if err != nil {
		return ImportResult{}, err
	}
	unsupportedSet, err := validateImportUnsupported(*wire.Unsupported, requestedSet)
	if err != nil {
		return ImportResult{}, err
	}
	if err := validateImportCoverage(requestedSet, analyzedSet, unsupportedSet); err != nil {
		return ImportResult{}, err
	}
	facts, err := validateImportFacts(*wire.Imports, analyzedSet)
	if err != nil {
		return ImportResult{}, err
	}
	unsupported := append([]Unsupported{}, (*wire.Unsupported)...)
	sort.Slice(unsupported, func(left, right int) bool {
		return unsupported[left].Path+"\x00"+unsupported[left].Reason < unsupported[right].Path+"\x00"+unsupported[right].Reason
	})
	return ImportResult{Analyzed: analyzed, Imports: facts, Unsupported: unsupported}, nil
}

func importRequestedPaths(requested []string) (map[string]bool, error) {
	paths := make(map[string]bool, len(requested))
	for _, path := range requested {
		if paths[path] {
			return nil, fmt.Errorf("the import request repeats path %q", path)
		}
		paths[path] = true
	}
	return paths, nil
}

func validateImportCoverage(requested, analyzed, unsupported map[string]bool) error {
	for path := range requested {
		if !analyzed[path] && !unsupported[path] {
			return fmt.Errorf("the import result omitted requested path %q", path)
		}
	}
	return nil
}

func validateAnalyzedImports(paths []string, requested map[string]bool) ([]string, map[string]bool, error) {
	result := append([]string{}, paths...)
	seen := map[string]bool{}
	for _, path := range result {
		if !requested[path] || seen[path] {
			return nil, nil, fmt.Errorf("the import result reports invalid analyzed path %q", path)
		}
		seen[path] = true
	}
	sort.Strings(result)
	return result, seen, nil
}

func validateImportUnsupported(unsupported []Unsupported, requested map[string]bool) (map[string]bool, error) {
	seen := map[string]bool{}
	for _, item := range unsupported {
		if !requested[item.Path] || !validParseReason(item.Reason) {
			return nil, fmt.Errorf("the import result reports invalid unsupported path %q", item.Path)
		}
		seen[item.Path] = true
	}
	return seen, nil
}

func validateImportFacts(wire []importFactWire, analyzed map[string]bool) ([]ImportFact, error) {
	facts := make([]ImportFact, 0, len(wire))
	seen := map[string]bool{}
	for index, item := range wire {
		fact, err := validateImportFact(item)
		if err != nil {
			return nil, fmt.Errorf("the import result fact %d is invalid: %w", index, err)
		}
		if !analyzed[fact.Path] {
			return nil, fmt.Errorf("the import result fact %d names unanalyzed path %q", index, fact.Path)
		}
		identity := importFactIdentity(fact)
		if seen[identity] {
			return nil, fmt.Errorf("the import result repeats fact %d", index)
		}
		seen[identity] = true
		facts = append(facts, fact)
	}
	sort.Slice(facts, func(left, right int) bool { return importFactIdentity(facts[left]) < importFactIdentity(facts[right]) })
	return facts, nil
}

func validateImportFact(wire importFactWire) (ImportFact, error) {
	if err := validateImportFactSource(wire); err != nil {
		return ImportFact{}, err
	}
	if err := validateImportFactTarget(wire); err != nil {
		return ImportFact{}, err
	}
	return ImportFact{
		Path: *wire.Path, Line: *wire.Line, Column: *wire.Column, Specifier: *wire.Specifier,
		Resolved: *wire.Resolved, Package: *wire.Package, Kind: *wire.Kind,
	}, nil
}

func validateImportFactSource(wire importFactWire) error {
	if wire.Path == nil || wire.Line == nil || wire.Column == nil {
		return fmt.Errorf("required fields are missing")
	}
	if !containedPath(*wire.Path) || *wire.Line <= 0 || *wire.Column <= 0 {
		return fmt.Errorf("source path or location is invalid")
	}
	return nil
}

func validateImportFactTarget(wire importFactWire) error {
	if wire.Specifier == nil || wire.Resolved == nil || wire.Package == nil || wire.Kind == nil {
		return fmt.Errorf("required fields are missing")
	}
	if !validImportSpecifier(*wire.Specifier) {
		return fmt.Errorf("specifier is invalid")
	}
	if *wire.Resolved != "" && !containedPath(*wire.Resolved) {
		return fmt.Errorf("resolved target is not contained")
	}
	if len(*wire.Package) > 214 || strings.ContainsAny(*wire.Package, "\x00\r\n") {
		return fmt.Errorf("package identity is invalid")
	}
	if !validImportKind(*wire.Kind) {
		return fmt.Errorf("kind %q is unsupported", *wire.Kind)
	}
	return nil
}

func validImportSpecifier(specifier string) bool {
	return specifier != "" && len(specifier) <= 4096 && !strings.ContainsAny(specifier, "\x00\r\n")
}

func validImportKind(kind string) bool {
	return kind == "runtime" || kind == "type-only" || kind == "re-export" || kind == "proven-dynamic"
}

func importFactIdentity(fact ImportFact) string {
	return strings.Join([]string{
		fact.Path, strconv.Itoa(fact.Line), strconv.Itoa(fact.Column), fact.Specifier,
		fact.Resolved, fact.Package, fact.Kind,
	}, "\x00")
}
