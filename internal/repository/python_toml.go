package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

type pythonTOMLValueKind uint8

const (
	pythonTOMLOther pythonTOMLValueKind = iota
	pythonTOMLString
	pythonTOMLArray
	pythonTOMLInlineTable
)

type pythonTOMLValue struct {
	kind   pythonTOMLValueKind
	text   string
	array  []pythonTOMLValue
	inline []pythonTOMLInlineAssignment
	line   int
}

type pythonTOMLInlineAssignment struct {
	path  []string
	value pythonTOMLValue
}

type pythonTOMLAssignment struct {
	table      string
	tablePath  []string
	arrayTable bool
	key        string
	keyPath    []string
	value      pythonTOMLValue
	line       int
}

type pythonTOMLTable struct {
	name  string
	array bool
}

type pythonTOMLDocument struct {
	assignments []pythonTOMLAssignment
	tables      []pythonTOMLTable
}

type pythonParsedManifest struct {
	document     pythonTOMLDocument
	requirements map[string]pythonfacts.Requirement
	specifiers   map[string]pythonfacts.Specifier
}

func parsePythonTOMLWith(python, path string, data []byte) (pythonTOMLDocument, error) {
	parsed, err := parsePythonManifestWith(python, path, data)
	return parsed.document, err
}

func parsePythonManifestWith(python, path string, data []byte) (pythonParsedManifest, error) {
	parsed, failures, err := parsePythonManifestsWith(python, []pythonfacts.Input{{Path: path, Source: string(data)}})
	if err != nil {
		return pythonParsedManifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if failures[0] != nil {
		return pythonParsedManifest{}, failures[0]
	}
	return parsed[0], nil
}

func parsePythonManifestsWith(python string, inputs []pythonfacts.Input) ([]pythonParsedManifest, []error, error) {
	parsed := make([]pythonParsedManifest, len(inputs))
	valid, indexes, failures := validPythonManifestInputs(inputs)
	if len(inputs) == 0 {
		return parsed, failures, nil
	}
	if len(valid) == 0 {
		return parsed, failures, nil
	}
	python, err := resolvedPythonInterpreter(python)
	if err != nil {
		return nil, nil, err
	}
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Manifests: valid})
	if err != nil {
		return nil, nil, err
	}
	convertPythonManifestFacts(response.Manifests, indexes, parsed, failures)
	return parsed, failures, nil
}

func validPythonManifestInputs(inputs []pythonfacts.Input) ([]pythonfacts.Input, []int, []error) {
	valid := make([]pythonfacts.Input, 0, len(inputs))
	indexes := make([]int, 0, len(inputs))
	failures := make([]error, len(inputs))
	for index, input := range inputs {
		if !utf8.ValidString(input.Source) {
			failures[index] = fmt.Errorf("parse %s: manifest is not valid UTF-8", input.Path)
			continue
		}
		valid = append(valid, input)
		indexes = append(indexes, index)
	}
	return valid, indexes, failures
}

func resolvedPythonInterpreter(python string) (string, error) {
	_, err := os.Stat(python)
	if filepath.IsAbs(python) && err == nil {
		return python, nil
	}
	return pythonfacts.DefaultInterpreter()
}

func convertPythonManifestFacts(facts []pythonfacts.Manifest, indexes []int, parsed []pythonParsedManifest, failures []error) {
	for responseIndex, fact := range facts {
		index := indexes[responseIndex]
		converted, conversionErr := pythonManifestFact(fact)
		if conversionErr != nil {
			failures[index] = conversionErr
			continue
		}
		parsed[index] = converted
	}
}

func pythonManifestFact(fact pythonfacts.Manifest) (pythonParsedManifest, error) {
	if fact.Error != "" {
		return pythonParsedManifest{}, fmt.Errorf("parse %s: %s", fact.Path, fact.Error)
	}
	document, err := pythonManifestDocument(fact)
	if err != nil {
		return pythonParsedManifest{}, err
	}
	requirements, err := indexedPythonRequirements(fact)
	if err != nil {
		return pythonParsedManifest{}, err
	}
	specifiers, err := indexedPythonSpecifiers(fact)
	if err != nil {
		return pythonParsedManifest{}, err
	}
	return pythonParsedManifest{document: document, requirements: requirements, specifiers: specifiers}, nil
}

func pythonManifestDocument(fact pythonfacts.Manifest) (pythonTOMLDocument, error) {
	document := pythonTOMLDocument{assignments: make([]pythonTOMLAssignment, 0, len(fact.Assignments)), tables: make([]pythonTOMLTable, 0, len(fact.Tables))}
	for _, table := range fact.Tables {
		if len(table.Path) == 0 || table.Line <= 0 {
			return pythonTOMLDocument{}, fmt.Errorf("parse %s: python-facts returned an invalid table", fact.Path)
		}
		document.tables = append(document.tables, pythonTOMLTable{name: strings.Join(table.Path, "."), array: table.Array})
	}
	for _, assignment := range fact.Assignments {
		converted, err := pythonManifestAssignment(fact.Path, assignment)
		if err != nil {
			return pythonTOMLDocument{}, err
		}
		document.assignments = append(document.assignments, converted)
	}
	return document, nil
}

func pythonManifestAssignment(path string, assignment pythonfacts.Assignment) (pythonTOMLAssignment, error) {
	value, err := pythonTOMLFactValue(assignment.Value, 0)
	if err == nil && (len(assignment.KeyPath) == 0 || assignment.Line <= 0) {
		err = errors.New("invalid assignment")
	}
	if err != nil {
		return pythonTOMLAssignment{}, fmt.Errorf("parse %s: python-facts returned an %v", path, err)
	}
	return pythonTOMLAssignment{
		table: strings.Join(assignment.TablePath, "."), tablePath: assignment.TablePath,
		arrayTable: assignment.ArrayTable, key: strings.Join(assignment.KeyPath, "."), keyPath: assignment.KeyPath,
		value: value, line: assignment.Line,
	}, nil
}

func indexedPythonRequirements(fact pythonfacts.Manifest) (map[string]pythonfacts.Requirement, error) {
	result := make(map[string]pythonfacts.Requirement, len(fact.Requirements))
	for _, requirement := range fact.Requirements {
		if _, found := result[requirement.Input]; found {
			return nil, fmt.Errorf("parse %s: python-facts returned duplicate requirement facts", fact.Path)
		}
		result[requirement.Input] = requirement
	}
	return result, nil
}

func indexedPythonSpecifiers(fact pythonfacts.Manifest) (map[string]pythonfacts.Specifier, error) {
	result := make(map[string]pythonfacts.Specifier, len(fact.Specifiers))
	for _, specifier := range fact.Specifiers {
		if _, found := result[specifier.Input]; found {
			return nil, fmt.Errorf("parse %s: python-facts returned duplicate specifier facts", fact.Path)
		}
		result[specifier.Input] = specifier
	}
	return result, nil
}

func pythonTOMLFactValue(fact pythonfacts.Value, depth int) (pythonTOMLValue, error) {
	if depth > 64 || fact.Line <= 0 {
		return pythonTOMLValue{}, fmt.Errorf("invalid TOML value")
	}
	value := pythonTOMLValue{text: fact.Text, line: fact.Line}
	var err error
	switch fact.Kind {
	case "other":
		value.kind = pythonTOMLOther
	case "string":
		value.kind = pythonTOMLString
	case "array":
		value.kind = pythonTOMLArray
		value.array, err = pythonTOMLFactArray(fact.Array, depth)
	case "inline":
		value.kind = pythonTOMLInlineTable
		value.inline, err = pythonTOMLFactInline(fact.Inline, depth)
	default:
		return pythonTOMLValue{}, fmt.Errorf("unknown TOML value kind %q", fact.Kind)
	}
	if err != nil {
		return pythonTOMLValue{}, err
	}
	return value, nil
}

func pythonTOMLFactArray(facts []pythonfacts.Value, depth int) ([]pythonTOMLValue, error) {
	values := make([]pythonTOMLValue, 0, len(facts))
	for _, fact := range facts {
		converted, err := pythonTOMLFactValue(fact, depth+1)
		if err != nil {
			return nil, err
		}
		values = append(values, converted)
	}
	return values, nil
}

func pythonTOMLFactInline(facts []pythonfacts.InlineAssignment, depth int) ([]pythonTOMLInlineAssignment, error) {
	values := make([]pythonTOMLInlineAssignment, 0, len(facts))
	for _, fact := range facts {
		if len(fact.Path) == 0 {
			return nil, errors.New("invalid inline TOML key")
		}
		converted, err := pythonTOMLFactValue(fact.Value, depth+1)
		if err != nil {
			return nil, err
		}
		values = append(values, pythonTOMLInlineAssignment{path: fact.Path, value: converted})
	}
	return values, nil
}
