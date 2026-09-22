package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type computedImportResolution struct {
	facts    []importFact
	failures map[string]string
}

func resolvePythonComputedImports(policy json.RawMessage, scope analysisScope, files []string, data map[string][]byte, dynamic []dynamicImport) (computedImportResolution, error) {
	scopeData, err := decodePythonScopeData(scope.Data)
	if err != nil {
		return computedImportResolution{}, err
	}
	index, err := newPythonModuleIndex(scope, scopeData)
	if err != nil {
		return computedImportResolution{}, err
	}
	declarations, err := computedImportsForScope(policy, scope.Handle)
	if err != nil {
		return computedImportResolution{}, err
	}
	byCall, failures, err := indexComputedImportDeclarations(scope, scopeData, index, files, data, declarations)
	if err != nil {
		return computedImportResolution{}, err
	}
	return resolveComputedImportCalls(scopeData, index, data, dynamic, byCall, failures), nil
}

func indexComputedImportDeclarations(scope analysisScope, scopeData pythonScopeData, index pythonModuleIndex, files []string, data map[string][]byte, declarations []scopedComputedImport) (map[string]scopedComputedImport, map[string]string, error) {
	selected := map[string]bool{}
	for _, file := range files {
		selected[file] = true
	}
	byCall := map[string]scopedComputedImport{}
	failures := map[string]string{}
	for _, declaration := range declarations {
		value := declaration.declaration
		if !slices.Contains(scope.Members, value.Importer) {
			return nil, nil, fmt.Errorf("computed Python import %s is outside its bound scope", value.Importer)
		}
		if !selected[value.Importer] {
			continue
		}
		if reason := computedImportDeclarationProblem(scopeData, index, data, declaration); reason != "" {
			setComputedImportFailure(failures, value.Importer, reason)
			continue
		}
		identity := computedImportCallIdentity(value.Importer, value.Line, value.Column, value.Callee)
		if _, found := byCall[identity]; found {
			return nil, nil, errors.New("computed Python import declarations repeat a callsite")
		}
		byCall[identity] = declaration
	}
	return byCall, failures, nil
}

func resolveComputedImportCalls(scope pythonScopeData, index pythonModuleIndex, data map[string][]byte, dynamic []dynamicImport, byCall map[string]scopedComputedImport, failures map[string]string) computedImportResolution {
	result := computedImportResolution{failures: failures}
	matched := map[string]bool{}
	dynamic = slices.Clone(dynamic)
	sortComputedImports(dynamic)
	for _, call := range dynamic {
		identity := computedImportCallIdentity(call.Path, call.Line, call.Column, call.Callee)
		declaration, found := byCall[identity]
		if !found {
			setComputedImportFailure(result.failures, call.Path, fmt.Sprintf("computed Python import at line %d requires an explicit pack declaration with exact callsite evidence", call.Line))
			continue
		}
		matched[identity] = true
		facts, reason := computedImportFacts(scope, index, data, declaration.declaration)
		if reason != "" {
			setComputedImportFailure(result.failures, call.Path, reason)
			continue
		}
		result.facts = append(result.facts, facts...)
	}
	markUnmatchedComputedImports(byCall, matched, result.failures)
	result.facts = slices.DeleteFunc(result.facts, func(fact importFact) bool {
		return result.failures[fact.Path] != ""
	})
	return result
}

func markUnmatchedComputedImports(byCall map[string]scopedComputedImport, matched map[string]bool, failures map[string]string) {
	for identity, declaration := range byCall {
		if !matched[identity] {
			setComputedImportFailure(failures, declaration.declaration.Importer, "declared computed Python import has no matching authored callsite")
		}
	}
}

func computedImportCallIdentity(path string, line, column int, callee string) string {
	if callee == "__import__" {
		callee = "builtins.__import__"
	}
	return fmt.Sprintf("%s\x00%09d\x00%09d\x00%s", path, line, column, callee)
}

func setComputedImportFailure(failures map[string]string, path, reason string) {
	if failures[path] == "" {
		failures[path] = reason
	}
}

func computedImportDeclarationProblem(scope pythonScopeData, index pythonModuleIndex, data map[string][]byte, input scopedComputedImport) string {
	value := input.declaration
	if reason := computedImportProjectProblem(scope, index, value); reason != "" {
		return reason
	}
	if reason := computedImportCallsiteProblem(value); reason != "" {
		return reason
	}
	if reason := computedImportSourceProblem(data, value); reason != "" {
		return reason
	}
	if reason := computedImportTargetContractProblem(value); reason != "" {
		return reason
	}
	if reason := computedImportInputsProblem(value, input.inputs); reason != "" {
		return reason
	}
	return computedImportTargetInventoryProblem(value)
}

func computedImportProjectProblem(scope pythonScopeData, index pythonModuleIndex, value computedImportDeclaration) string {
	if value.Project != scope.Manifest {
		return "computed Python import belongs to a different project"
	}
	identity, found := index.byPath[value.Importer]
	if !found || identity.module != value.Module {
		return "declared Python module identity is stale"
	}
	return ""
}

func computedImportCallsiteProblem(value computedImportDeclaration) string {
	if !validComputedImportCallsite(value) {
		return "computed Python import has invalid bounded callsite evidence"
	}
	if !validComputedImportCallable(value) {
		return "computed Python import has invalid callable scope evidence"
	}
	if !slices.Contains([]string{"importlib.import_module", "builtins.__import__", "pkgutil.resolve_name"}, value.Callee) {
		return "computed Python import uses an unsupported loader"
	}
	return ""
}

func validComputedImportCallsite(value computedImportDeclaration) bool {
	return value.Line >= 1 && value.Column >= 1 && value.Shape != "" && len(value.Shape) <= 16384 && value.Argument != "" && len(value.Argument) <= 4096 && !strings.ContainsAny(value.Shape+value.Argument, "\x00\r\n")
}

func validComputedImportCallable(value computedImportDeclaration) bool {
	if value.ModuleScope == (value.Callable != "") {
		return false
	}
	return value.Callable == "" || validPythonQualifiedName(value.Callable)
}

func computedImportSourceProblem(data map[string][]byte, value computedImportDeclaration) string {
	source, found := data[value.Importer]
	if !found {
		return "declared Python source is unavailable"
	}
	digest := sha256.Sum256(source)
	if !validDigest(value.SourceSHA256) || hex.EncodeToString(digest[:]) != value.SourceSHA256 {
		return "declared Python source digest is stale"
	}
	return ""
}

func computedImportTargetContractProblem(value computedImportDeclaration) string {
	hasNamespace := value.Namespace != ""
	hasEntryPoints := value.EntryPointGroup != ""
	if hasNamespace == hasEntryPoints {
		return "computed Python import must select one namespace or entry-point group"
	}
	if hasNamespace && !validComputedImportNamespace(value.Namespace) {
		return "computed Python import namespace must be a non-top-level module"
	}
	if hasEntryPoints && !validComputedEntryPointContract(value) {
		return "computed Python entry-point import has duplicate target evidence"
	}
	if hasNamespace && len(value.Targets) == 0 && len(value.Configuration) == 0 {
		return "computed Python import target set is empty"
	}
	if value.Callee == "pkgutil.resolve_name" && !validComputedObjectContract(value) {
		return "computed object import requires one registry and module-object-call/v1"
	}
	return ""
}

func validComputedImportNamespace(value string) bool {
	return validPythonQualifiedName(value) && strings.Contains(value, ".")
}

func validComputedEntryPointContract(value computedImportDeclaration) bool {
	return len(value.EntryPointGroup) <= 256 && len(value.Targets) == 0 && len(value.Configuration) == 0
}

func validComputedObjectContract(value computedImportDeclaration) bool {
	return value.Shape == "module-object-call/v1" && len(value.Targets) == 0 && value.EntryPointGroup == "" && len(value.Configuration) == 1
}

func computedImportInputsProblem(value computedImportDeclaration, inputs []string) string {
	expectedInputs := []string{value.Project, value.Importer}
	for _, configuration := range value.Configuration {
		if !validComputedImportConfiguration(configuration) {
			return "computed Python import has invalid configuration evidence"
		}
		if value.Callee != "pkgutil.resolve_name" && configuration.JSONPointer == "" {
			return "computed Python module import requires a non-root JSON pointer"
		}
		expectedInputs = append(expectedInputs, configuration.Path)
	}
	if !slices.Equal(uniqueSorted(expectedInputs), inputs) {
		return "computed Python import declaration inputs do not match its evidence"
	}
	return ""
}

func validComputedImportConfiguration(value computedImportInput) bool {
	return exactPath(value.Path) == nil && strings.HasSuffix(value.Path, ".json") && validDigest(value.SHA256) && len(value.JSONPointer) <= 4096 && !strings.ContainsAny(value.JSONPointer, "\x00\r\n")
}

func computedImportTargetInventoryProblem(value computedImportDeclaration) string {
	for _, target := range value.Targets {
		if !validPythonQualifiedName(target) || !pythonComputedTargetContained(target, value.Namespace) {
			return fmt.Sprintf("computed Python import target %q escapes namespace %q", target, value.Namespace)
		}
	}
	return ""
}

func validPythonQualifiedName(value string) bool {
	return value != "" && validPythonModuleParts(strings.Split(value, "."))
}

func computedImportFacts(scope pythonScopeData, index pythonModuleIndex, data map[string][]byte, declaration computedImportDeclaration) ([]importFact, string) {
	targets, reason := computedImportTargets(scope, data, declaration)
	if reason != "" {
		return nil, reason
	}
	facts := make([]importFact, 0, len(targets))
	for _, target := range targets {
		paths := index.byModule[target]
		if len(paths) != 1 || index.ambiguous[target] {
			return nil, fmt.Sprintf("line %d computed Python import target %q does not resolve to exactly one governed project module", declaration.Line, target)
		}
		facts = append(facts, importFact{
			Path: declaration.Importer, Line: declaration.Line, Column: declaration.Column,
			Specifier: target, Resolved: paths[0], Kind: "proven-dynamic",
		})
	}
	return facts, ""
}

func computedImportTargets(scope pythonScopeData, data map[string][]byte, declaration computedImportDeclaration) ([]string, string) {
	if declaration.EntryPointGroup != "" {
		return computedEntryPointTargets(scope, declaration.EntryPointGroup)
	}
	if declaration.Callee == "pkgutil.resolve_name" {
		return computedObjectConfigurationTargets(data, declaration.Configuration[0], declaration.Namespace)
	}
	return computedModuleTargets(data, declaration)
}

func computedEntryPointTargets(scope pythonScopeData, group string) ([]string, string) {
	targets := []string{}
	for _, entry := range scope.EntryPoints {
		if entry.Group == group {
			targets = append(targets, entry.Module)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Sprintf("computed Python import entry-point group %q is absent or empty", group)
	}
	return uniqueSorted(targets), ""
}

func computedModuleTargets(data map[string][]byte, declaration computedImportDeclaration) ([]string, string) {
	targets := slices.Clone(declaration.Targets)
	for _, input := range declaration.Configuration {
		values, reason := computedConfigurationTargets(data, input)
		if reason != "" {
			return nil, reason
		}
		targets = append(targets, values...)
	}
	for _, target := range targets {
		if !validPythonQualifiedName(target) || !pythonComputedTargetContained(target, declaration.Namespace) {
			return nil, fmt.Sprintf("computed Python import target %q escapes namespace %q", target, declaration.Namespace)
		}
	}
	targets = uniqueSorted(targets)
	if len(targets) == 0 {
		return nil, "computed Python import target set is empty"
	}
	return targets, ""
}

func pythonComputedTargetContained(target, namespace string) bool {
	return target == namespace || strings.HasPrefix(target, namespace+".")
}

func computedConfigurationTargets(data map[string][]byte, input computedImportInput) ([]string, string) {
	value, reason := computedConfigurationValue(data, input)
	if reason != "" {
		return nil, reason
	}
	switch selected := value.(type) {
	case string:
		if selected == "" {
			return nil, "computed import configuration selects an empty module"
		}
		return []string{selected}, ""
	case []any:
		result := make([]string, 0, len(selected))
		for _, item := range selected {
			text, ok := item.(string)
			if !ok || text == "" {
				return nil, "computed import configuration must select only nonempty module names"
			}
			result = append(result, text)
		}
		return result, ""
	default:
		return nil, "computed import configuration must select a module name or an array of module names"
	}
}

func computedObjectConfigurationTargets(data map[string][]byte, input computedImportInput, namespace string) ([]string, string) {
	value, reason := computedConfigurationValue(data, input)
	if reason != "" {
		return nil, reason
	}
	values, reason := computedObjectValues(value)
	if reason != "" {
		return nil, reason
	}
	return computedObjectModules(values, namespace)
}

func computedObjectValues(value any) ([]string, string) {
	switch current := value.(type) {
	case string:
		return []string{current}, ""
	case []any:
		return computedObjectStringValues(current)
	case map[string]any:
		values := make([]any, 0, len(current))
		for _, item := range current {
			values = append(values, item)
		}
		return computedObjectStringValues(values)
	default:
		return nil, "computed object import configuration must select module:object strings"
	}
}

func computedObjectStringValues(values []any) ([]string, string) {
	result := make([]string, 0, len(values))
	for _, item := range values {
		text, ok := item.(string)
		if !ok {
			return nil, "computed object import configuration must contain only module:object strings"
		}
		result = append(result, text)
	}
	return result, ""
}

func computedObjectModules(values []string, namespace string) ([]string, string) {
	targets := []string{}
	for _, value := range values {
		module, valid := computedObjectModule(value, namespace)
		if !valid {
			return nil, "computed object import configuration contains an invalid or escaping module:object target"
		}
		targets = append(targets, module)
	}
	targets = uniqueSorted(targets)
	if len(targets) == 0 {
		return nil, "computed object import configuration selects no targets"
	}
	return targets, ""
}

func computedObjectModule(value, namespace string) (string, bool) {
	module, object, found := strings.Cut(value, ":")
	if !found || strings.Contains(object, ":") {
		return "", false
	}
	return module, validPythonQualifiedName(module) && validPythonQualifiedName(object) && pythonComputedTargetContained(module, namespace)
}

func computedConfigurationValue(data map[string][]byte, input computedImportInput) (any, string) {
	contents, found := data[input.Path]
	if !found {
		return nil, fmt.Sprintf("computed import configuration %s is unavailable", input.Path)
	}
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != input.SHA256 {
		return nil, fmt.Sprintf("computed import configuration %s changed and its declaration is stale", input.Path)
	}
	value, err := decodeComputedJSON(contents)
	if err != nil {
		return nil, fmt.Sprintf("computed import configuration %s is malformed: %v", input.Path, err)
	}
	value, err = computedJSONPointer(value, input.JSONPointer)
	if err != nil {
		return nil, fmt.Sprintf("computed import configuration %s pointer %s is invalid: %v", input.Path, input.JSONPointer, err)
	}
	return value, ""
}

func decodeComputedJSON(data []byte) (any, error) {
	if len(data) == 0 || len(data) > 2<<20 {
		return nil, errors.New("JSON input has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeComputedJSONValue(decoder, 0, new(int))
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON input has trailing data")
	}
	return value, nil
}

func decodeComputedJSONValue(decoder *json.Decoder, depth int, count *int) (any, error) {
	(*count)++
	if depth > 64 || *count > 100000 {
		return nil, errors.New("JSON input exceeds its structural limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return token, nil
	}
	switch delimiter {
	case '{':
		return decodeComputedJSONObject(decoder, depth, count)
	case '[':
		return decodeComputedJSONArray(decoder, depth, count)
	default:
		return nil, errors.New("JSON input has an invalid delimiter")
	}
}

func decodeComputedJSONObject(decoder *json.Decoder, depth int, count *int) (map[string]any, error) {
	result := map[string]any{}
	seen := map[string]bool{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok || seen[key] {
			return nil, errors.New("JSON object has an invalid or duplicate key")
		}
		seen[key] = true
		value, err := decodeComputedJSONValue(decoder, depth+1, count)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	_, err := decoder.Token()
	return result, err
}

func decodeComputedJSONArray(decoder *json.Decoder, depth int, count *int) ([]any, error) {
	result := []any{}
	for decoder.More() {
		value, err := decodeComputedJSONValue(decoder, depth+1, count)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	_, err := decoder.Token()
	return result, err
}

func computedJSONPointer(value any, pointer string) (any, error) {
	if pointer == "" {
		return value, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, errors.New("JSON pointer must start with a slash")
	}
	for _, raw := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		segment, err := decodeComputedJSONPointerSegment(raw)
		if err != nil {
			return nil, err
		}
		value, err = computedJSONPointerStep(value, segment)
		if err != nil {
			return nil, err
		}
	}
	return value, nil
}

func decodeComputedJSONPointerSegment(raw string) (string, error) {
	var segment strings.Builder
	for index := 0; index < len(raw); index++ {
		if raw[index] != '~' {
			segment.WriteByte(raw[index])
			continue
		}
		if index+1 >= len(raw) || raw[index+1] != '0' && raw[index+1] != '1' {
			return "", errors.New("JSON pointer has an invalid escape")
		}
		index++
		if raw[index] == '0' {
			segment.WriteByte('~')
		} else {
			segment.WriteByte('/')
		}
	}
	return segment.String(), nil
}

func computedJSONPointerStep(value any, segment string) (any, error) {
	switch current := value.(type) {
	case map[string]any:
		selected, found := current[segment]
		if !found {
			return nil, errors.New("JSON pointer does not exist")
		}
		return selected, nil
	case []any:
		if segment == "" || len(segment) > 1 && segment[0] == '0' {
			return nil, errors.New("JSON pointer array index is invalid")
		}
		index, err := strconv.Atoi(segment)
		if err != nil || index < 0 || index >= len(current) {
			return nil, errors.New("JSON pointer array index is outside the value")
		}
		return current[index], nil
	default:
		return nil, errors.New("JSON pointer traverses a scalar")
	}
}

func sortComputedImports(values []dynamicImport) {
	sort.Slice(values, func(left, right int) bool {
		return computedImportCallIdentity(values[left].Path, values[left].Line, values[left].Column, values[left].Callee) < computedImportCallIdentity(values[right].Path, values[right].Line, values[right].Column, values[right].Callee)
	})
}
