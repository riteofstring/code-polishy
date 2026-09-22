package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

type pythonModuleIdentity struct {
	module      string
	packageName string
}

type pythonModuleIndex struct {
	byModule  map[string][]string
	byPath    map[string]pythonModuleIdentity
	localTop  map[string]bool
	ambiguous map[string]bool
}

type pythonImportResolution struct {
	facts    []importFact
	findings []responseFinding
	failures map[string]string
}

func (state *analysisState) architecture(ctx context.Context, ruff ruffExecutor, facts factExecutor, workspace string, groups []analysisGroup) error {
	imports := []importFact{}
	for _, group := range groups {
		if len(group.files) == 0 {
			continue
		}
		resolved, err := state.architectureGroup(ctx, ruff, facts, workspace, group)
		if err != nil {
			return err
		}
		imports = append(imports, resolved...)
	}
	if len(imports) > 20000 || len(state.result.Findings) > 4096 {
		return errors.New("python import facts or architecture diagnostics exceed the protocol collection limit")
	}
	sortImportFacts(imports)
	state.result.Facts = &sourceFacts{Imports: &imports}
	state.result.Evidence = []string{"Ruff 0.16.0 resolved authenticated Python project imports and CPython 3.12 supplied authored import sites"}
	return nil
}

func (state *analysisState) architectureGroup(ctx context.Context, ruff ruffExecutor, facts factExecutor, workspace string, group analysisGroup) ([]importFact, error) {
	result, err := facts.imports(ctx, workspace, group.files)
	if err != nil {
		return nil, err
	}
	sourceFailures := architectureSourceFailures(result)
	valid := slices.DeleteFunc(slices.Clone(group.files), func(file string) bool {
		return sourceFailures[file] != ""
	})
	resolution := pythonImportResolution{failures: map[string]string{}}
	if len(valid) > 0 {
		complete, graphErr := ruff.graph(ctx, workspace, group.scope, valid, true)
		if graphErr != nil {
			return nil, graphErr
		}
		runtime, graphErr := ruff.graph(ctx, workspace, group.scope, valid, false)
		if graphErr != nil {
			return nil, graphErr
		}
		resolution, err = resolvePythonImports(group.scope, valid, result.Imports, complete, runtime)
		if err != nil {
			return nil, err
		}
	}
	state.result.Findings = append(state.result.Findings, resolution.findings...)
	state.accountArchitectureFiles(group.files, sourceFailures, resolution.failures)
	return resolution.facts, nil
}

func architectureSourceFailures(result factResult) map[string]string {
	failures := map[string]string{}
	for file, reason := range result.Failures {
		failures[file] = reason
	}
	for _, dynamic := range result.DynamicImports {
		if failures[dynamic.Path] == "" {
			failures[dynamic.Path] = fmt.Sprintf("computed Python import at line %d requires an explicit pack declaration", dynamic.Line)
		}
	}
	return failures
}

func (state *analysisState) accountArchitectureFiles(files []string, sourceFailures, resolutionFailures map[string]string) {
	for _, file := range files {
		reason := sourceFailures[file]
		if reason == "" {
			reason = resolutionFailures[file]
		}
		if reason != "" {
			state.result.Coverage.Unsupported = append(state.result.Coverage.Unsupported, unsupported{Path: file, Reason: reason})
			continue
		}
		state.result.Coverage.Analyzed = append(state.result.Coverage.Analyzed, file)
	}
}

func resolvePythonImports(scope analysisScope, files []string, imports []authoredImport, complete, runtime ruffGraph) (pythonImportResolution, error) {
	data, err := decodePythonScopeData(scope.Data)
	if err != nil {
		return pythonImportResolution{}, err
	}
	index, err := newPythonModuleIndex(scope, data)
	if err != nil {
		return pythonImportResolution{}, err
	}
	result := pythonImportResolution{failures: map[string]string{}}
	result.findings = ambiguousPythonModuleFindings(index)
	byPath := map[string][]authoredImport{}
	for _, item := range imports {
		byPath[item.Path] = append(byPath[item.Path], item)
	}
	for _, file := range files {
		facts, reason := resolvePythonFileImports(file, byPath[file], index, complete[file], runtime[file])
		if reason != "" {
			result.failures[file] = reason
			continue
		}
		result.facts = append(result.facts, facts...)
	}
	return result, nil
}

func newPythonModuleIndex(scope analysisScope, data pythonScopeData) (pythonModuleIndex, error) {
	index := pythonModuleIndex{
		byModule: map[string][]string{}, byPath: map[string]pythonModuleIdentity{},
		localTop: map[string]bool{}, ambiguous: map[string]bool{},
	}
	for _, file := range scope.Members {
		identity, err := pythonModuleName(data.SourceRoots, file)
		if err != nil {
			return pythonModuleIndex{}, err
		}
		index.byPath[file] = identity
		if identity.module == "" {
			continue
		}
		index.byModule[identity.module] = append(index.byModule[identity.module], file)
		index.localTop[strings.Split(identity.module, ".")[0]] = true
	}
	for module, paths := range index.byModule {
		sort.Strings(paths)
		locations := map[string]bool{}
		for _, file := range paths {
			locations[strings.TrimSuffix(strings.TrimSuffix(file, ".pyi"), ".py")] = true
		}
		index.ambiguous[module] = len(locations) > 1
	}
	return index, nil
}

func pythonModuleName(sourceRoots []string, file string) (pythonModuleIdentity, error) {
	base := pythonSourceBase(sourceRoots, file)
	if base == "" {
		return pythonModuleIdentity{}, fmt.Errorf("python source %s has no authenticated source root", file)
	}
	relative := file
	if base != "." {
		relative = strings.TrimPrefix(file, base+"/")
	}
	extension := path.Ext(relative)
	if extension != ".py" && extension != ".pyi" {
		return pythonModuleIdentity{}, fmt.Errorf("python source %s has an unsupported extension", file)
	}
	parts := strings.Split(strings.TrimSuffix(relative, extension), "/")
	if !validPythonModuleParts(parts) {
		return pythonModuleIdentity{}, fmt.Errorf("python source %s has an invalid module path", file)
	}
	packageParts := parts[:len(parts)-1]
	if parts[len(parts)-1] == "__init__" {
		parts = packageParts
	}
	return pythonModuleIdentity{module: strings.Join(parts, "."), packageName: strings.Join(packageParts, ".")}, nil
}

func pythonSourceBase(sourceRoots []string, file string) string {
	base := ""
	for _, root := range sourceRoots {
		if pythonPathContains(root, file) && (base == "" || len(root) > len(base)) {
			base = root
		}
	}
	return base
}

func validPythonModuleParts(parts []string) bool {
	return len(parts) > 0 && !slices.ContainsFunc(parts, func(part string) bool {
		return !validPythonModulePart(part)
	})
}

func validPythonModulePart(value string) bool {
	if value == "" || !pythonIdentifierStart(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		if !pythonIdentifierPart(value[index]) {
			return false
		}
	}
	return true
}

func pythonIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func pythonIdentifierPart(value byte) bool {
	return pythonIdentifierStart(value) || value >= '0' && value <= '9'
}

func pythonPathContains(root, file string) bool {
	return root == "." || file == root || strings.HasPrefix(file, root+"/")
}

func ambiguousPythonModuleFindings(index pythonModuleIndex) []responseFinding {
	findings := []responseFinding{}
	modules := make([]string, 0, len(index.ambiguous))
	for module, ambiguous := range index.ambiguous {
		if ambiguous {
			modules = append(modules, module)
		}
	}
	sort.Strings(modules)
	for _, module := range modules {
		paths := index.byModule[module]
		findings = append(findings, responseFinding{
			Capability: "architecture", Path: paths[0], Subject: module, Rule: "ambiguous-module",
			Message: fmt.Sprintf("Python module %s has multiple governed source locations: %s", module, strings.Join(paths, ", ")),
		})
	}
	return findings
}

func resolvePythonFileImports(file string, imports []authoredImport, index pythonModuleIndex, complete, runtime map[string]bool) ([]importFact, string) {
	if complete == nil || runtime == nil {
		return nil, "Ruff omitted this Python source from an import graph"
	}
	if !runtimeGraphContained(complete, runtime) {
		return nil, "Ruff returned a runtime import absent from its complete import graph"
	}
	matched := map[string]bool{}
	facts := []importFact{}
	for _, item := range imports {
		resolved, reason := resolvePythonAuthoredImport(file, item, index, complete, runtime)
		if reason != "" {
			return nil, reason
		}
		for _, fact := range resolved {
			if fact.Resolved != "" {
				matched[fact.Resolved] = true
			}
		}
		facts = append(facts, resolved...)
	}
	if target := unmatchedPythonImport(complete, matched); target != "" {
		return nil, fmt.Sprintf("Ruff resolved %s without an authored static import site", target)
	}
	return facts, ""
}

func runtimeGraphContained(complete, runtime map[string]bool) bool {
	return !slices.ContainsFunc(sortedMapKeys(runtime), func(target string) bool {
		return !complete[target]
	})
}

func resolvePythonAuthoredImport(file string, item authoredImport, index pythonModuleIndex, complete, runtime map[string]bool) ([]importFact, string) {
	modules, relative, valid := pythonImportCandidates(file, item, index)
	if !valid {
		return []importFact{unresolvedPythonImport(item)}, ""
	}
	targets := pythonCandidateTargets(modules, index, complete)
	if len(targets) == 0 {
		return externalOrUnresolvedPythonImport(item, modules, relative, index)
	}
	facts := make([]importFact, 0, len(targets))
	for _, target := range targets {
		kind, valid := resolvedPythonImportKind(item.Kind, runtime[target])
		if !valid {
			return nil, "CPython and Ruff disagree about a type-only import"
		}
		facts = append(facts, importFact{Path: item.Path, Line: item.Line, Column: item.Column, Specifier: item.Module, Resolved: target, Kind: kind})
	}
	return facts, ""
}

func externalOrUnresolvedPythonImport(item authoredImport, modules []string, relative bool, index pythonModuleIndex) ([]importFact, string) {
	if relative || pythonLocalImport(modules, index) {
		return []importFact{unresolvedPythonImport(item)}, ""
	}
	packageName := pythonExternalPackage(item.Module)
	if packageName == "" || len(packageName) > 214 {
		return nil, "Python import has no bounded external package identity"
	}
	return []importFact{{Path: item.Path, Line: item.Line, Column: item.Column, Specifier: item.Module, Package: packageName, Kind: item.Kind}}, ""
}

func resolvedPythonImportKind(authored string, runtime bool) (string, bool) {
	if !runtime {
		return "type-only", true
	}
	return authored, authored != "type-only"
}

func unmatchedPythonImport(complete, matched map[string]bool) string {
	for _, target := range sortedMapKeys(complete) {
		if !matched[target] {
			return target
		}
	}
	return ""
}

func sortedMapKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func pythonImportCandidates(file string, item authoredImport, index pythonModuleIndex) ([]string, bool, bool) {
	module, relative, valid := absolutePythonImport(file, item.Module, index)
	if !valid {
		return nil, relative, false
	}
	candidates := pythonModulePrefixes(module)
	if len(item.Names) > 0 {
		for _, name := range item.Names {
			if name == "*" {
				continue
			}
			candidate := name
			if module != "" {
				candidate = module + "." + name
			}
			candidates = append(candidates, pythonModulePrefixes(candidate)...)
		}
	}
	return uniqueStrings(candidates), relative, true
}

func absolutePythonImport(file, module string, index pythonModuleIndex) (string, bool, bool) {
	level := len(module) - len(strings.TrimLeft(module, "."))
	if level == 0 {
		return module, false, true
	}
	identity, found := index.byPath[file]
	if !found {
		return "", true, false
	}
	parts := []string{}
	if identity.packageName != "" {
		parts = strings.Split(identity.packageName, ".")
	}
	up := level - 1
	if up > len(parts) {
		return "", true, false
	}
	parts = parts[:len(parts)-up]
	suffix := strings.TrimLeft(module, ".")
	if suffix != "" {
		parts = append(parts, strings.Split(suffix, ".")...)
	}
	return strings.Join(parts, "."), true, true
}

func pythonModulePrefixes(module string) []string {
	if module == "" {
		return nil
	}
	parts := strings.Split(module, ".")
	result := make([]string, 0, len(parts))
	for index := range parts {
		result = append(result, strings.Join(parts[:index+1], "."))
	}
	return result
}

func pythonCandidateTargets(modules []string, index pythonModuleIndex, complete map[string]bool) []string {
	targets := []string{}
	for _, module := range modules {
		for _, target := range index.byModule[module] {
			if complete[target] {
				targets = append(targets, target)
			}
		}
	}
	return uniqueStrings(targets)
}

func pythonLocalImport(modules []string, index pythonModuleIndex) bool {
	for _, module := range modules {
		if module == "" {
			continue
		}
		if index.localTop[strings.Split(module, ".")[0]] || len(index.byModule[module]) > 0 {
			return true
		}
	}
	return false
}

func unresolvedPythonImport(item authoredImport) importFact {
	return importFact{Path: item.Path, Line: item.Line, Column: item.Column, Specifier: item.Module, Kind: item.Kind}
}

func pythonExternalPackage(module string) string {
	module = strings.TrimLeft(module, ".")
	if module == "" {
		return ""
	}
	return strings.Split(module, ".")[0]
}

func uniqueStrings(values []string) []string {
	sort.Strings(values)
	return slices.Compact(values)
}

func sortImportFacts(facts []importFact) {
	sort.SliceStable(facts, func(left, right int) bool {
		first := facts[left]
		second := facts[right]
		return fmt.Sprintf("%s\x00%09d\x00%09d\x00%s\x00%s", first.Path, first.Line, first.Column, first.Specifier, first.Resolved) < fmt.Sprintf("%s\x00%09d\x00%09d\x00%s\x00%s", second.Path, second.Line, second.Column, second.Specifier, second.Resolved)
	})
}
