package repository

import (
	"fmt"
	"sort"
	"strings"
)

func pythonProjectDynamicReferences(manifest string, assignments []pythonTOMLAssignment) ([]PythonDynamicReference, error) {
	references := []PythonDynamicReference{}
	for _, assignment := range assignments {
		found, err := pythonProjectDynamicReferencesForValue(manifest, pythonTOMLAssignmentPath(assignment), assignment.value, assignment.arrayTable)
		if err != nil {
			return nil, err
		}
		references = append(references, found...)
	}
	sort.Slice(references, func(left, right int) bool {
		return pythonDynamicReferenceIdentity(references[left]) < pythonDynamicReferenceIdentity(references[right])
	})
	for index := 1; index < len(references); index++ {
		previous, current := references[index-1], references[index]
		if previous.Table == current.Table && previous.Name == current.Name {
			return nil, pythonManifestError(manifest, current.Line, "%s.%s is declared more than once", current.Table, current.Name)
		}
	}
	return references, nil
}

func pythonTOMLAssignmentPath(assignment pythonTOMLAssignment) []string {
	table := assignment.tablePath
	if len(table) == 0 && assignment.table != "" {
		table = strings.Split(assignment.table, ".")
	}
	key := assignment.keyPath
	if len(key) == 0 && assignment.key != "" {
		key = strings.Split(assignment.key, ".")
	}
	path := make([]string, 0, len(table)+len(key))
	path = append(path, table...)
	return append(path, key...)
}

func pythonProjectDynamicReferencesForValue(manifest string, path []string, value pythonTOMLValue, arrayTable bool) ([]PythonDynamicReference, error) {
	if value.kind == pythonTOMLInlineTable {
		if _, _, found := pythonDynamicReferenceLocation(path); found {
			return nil, pythonManifestError(manifest, value.line, "%s must be an entry point string", strings.Join(path, "."))
		}
		references := []PythonDynamicReference{}
		for _, field := range value.inline {
			childPath := make([]string, 0, len(path)+len(field.path))
			childPath = append(childPath, path...)
			childPath = append(childPath, field.path...)
			found, err := pythonProjectDynamicReferencesForValue(manifest, childPath, field.value, arrayTable)
			if err != nil {
				return nil, err
			}
			references = append(references, found...)
		}
		return references, nil
	}
	table, name, found := pythonDynamicReferenceLocation(path)
	if !found {
		if pythonDynamicReferenceScope(path) {
			return nil, pythonManifestError(manifest, value.line, "%s must identify an entry point", strings.Join(path, "."))
		}
		return nil, nil
	}
	if arrayTable {
		return nil, pythonManifestError(manifest, value.line, "%s must not be an array table", table)
	}
	return pythonProjectDynamicReference(manifest, table, name, value)
}

func pythonDynamicReferenceLocation(path []string) (string, string, bool) {
	if len(path) < 3 || path[0] != "project" {
		return "", "", false
	}
	switch path[1] {
	case "scripts", "gui-scripts":
		if len(path) == 3 {
			return strings.Join(path[:2], "."), path[2], true
		}
	case "entry-points":
		if len(path) >= 4 {
			return strings.Join(path[:len(path)-1], "."), path[len(path)-1], true
		}
	}
	return "", "", false
}

func pythonDynamicReferenceScope(path []string) bool {
	return len(path) >= 2 && path[0] == "project" &&
		(path[1] == "scripts" || path[1] == "gui-scripts" || path[1] == "entry-points")
}

func pythonProjectDynamicReference(manifest, table, name string, value pythonTOMLValue) ([]PythonDynamicReference, error) {
	field := table + "." + name
	if value.kind != pythonTOMLString {
		return nil, pythonManifestError(manifest, value.line, "%s must be an entry point string", field)
	}
	module, symbol, err := parsePythonEntryPoint(value.text)
	if err != nil {
		return nil, pythonManifestError(manifest, value.line, "invalid entry point %q: %v", value.text, err)
	}
	return []PythonDynamicReference{{Module: module, Symbol: symbol, Table: table, Name: name, Line: value.line}}, nil
}

func parsePythonEntryPoint(value string) (string, string, error) {
	target, extras, err := pythonEntryPointTargetAndExtras(value)
	if err != nil {
		return "", "", err
	}
	if extras == "" && strings.Contains(value, "[") {
		return "", "", fmt.Errorf("has no extras")
	}
	if err := validatePythonEntryPointExtras(extras); err != nil {
		return "", "", err
	}
	module, symbol, found := strings.Cut(target, ":")
	if !found || strings.Contains(symbol, ":") || !validPythonEntryPointIdentifierChain(module) || !validPythonEntryPointIdentifierChain(symbol) {
		return "", "", fmt.Errorf("must be an exact module:symbol reference")
	}
	return module, symbol, nil
}

func pythonEntryPointTargetAndExtras(value string) (string, string, error) {
	opening := strings.IndexByte(value, '[')
	if opening < 0 {
		if strings.TrimSpace(value) != value {
			return "", "", fmt.Errorf("must not have surrounding whitespace")
		}
		return value, "", nil
	}
	if strings.Count(value, "[") != 1 || strings.Count(value, "]") != 1 || !strings.HasSuffix(value, "]") {
		return "", "", fmt.Errorf("has malformed extras")
	}
	target := strings.TrimRight(value[:opening], " \t")
	if target == "" || strings.ContainsAny(target, " \t\r\n") {
		return "", "", fmt.Errorf("must be an exact module:symbol reference")
	}
	return target, value[opening+1 : len(value)-1], nil
}

func validatePythonEntryPointExtras(value string) error {
	if value == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, candidate := range strings.Split(value, ",") {
		extra := strings.TrimSpace(candidate)
		normalized, err := NormalizePythonPackageName(extra)
		if err != nil {
			return fmt.Errorf("has invalid extra %q", extra)
		}
		if seen[normalized] {
			return fmt.Errorf("declares extra %q more than once", extra)
		}
		seen[normalized] = true
	}
	return nil
}

func validPythonEntryPointIdentifierChain(value string) bool {
	if value == "" {
		return false
	}
	for _, segment := range strings.Split(value, ".") {
		if !validPythonEntryPointIdentifier(segment) {
			return false
		}
	}
	return true
}

func validPythonEntryPointIdentifier(value string) bool {
	if value == "" || !pythonEntryPointIdentifierStart(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		if !pythonEntryPointIdentifierPart(value[index]) {
			return false
		}
	}
	return true
}

func pythonEntryPointIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func pythonEntryPointIdentifierPart(value byte) bool {
	return pythonEntryPointIdentifierStart(value) || value >= '0' && value <= '9'
}

func pythonDynamicReferenceIdentity(reference PythonDynamicReference) string {
	return reference.Table + "\x00" + reference.Name + "\x00" + reference.Module + "\x00" + reference.Symbol
}
