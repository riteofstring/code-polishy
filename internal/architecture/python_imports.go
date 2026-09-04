package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type pythonImportReference struct {
	Module  string
	Names   []string
	Line    int
	Literal bool
}

type pythonComputedImportFact struct {
	Callee          string
	Callable        string
	Line            int
	Column          int
	EndLine         int
	EndColumn       int
	Argument        string
	Shape           string
	Targets         []string
	Configuration   []pythonComputedConfigurationFact
	EntryPointGroup string
	EvidenceError   string
}

type pythonComputedConfigurationFact struct {
	Path        string
	JSONPointer string
}

type pythonSourceFact struct {
	SHA256          string
	Imports         []pythonImportReference
	ComputedImports []pythonComputedImportFact
}

type pythonModuleIndex struct {
	files     map[string][]string
	packages  map[string]bool
	packageOf map[string]string
	ambiguous map[string]bool
}

func newPythonModuleIndex(project repository.PythonProject) pythonModuleIndex {
	index := pythonModuleIndex{
		files: map[string][]string{}, packages: map[string]bool{}, packageOf: map[string]string{}, ambiguous: map[string]bool{},
	}
	for _, candidate := range project.PackageCandidates {
		index.packages[candidate.Name] = true
	}
	for _, source := range project.Files {
		module, packageName := repository.PythonModuleName(project, source)
		index.packageOf[source] = packageName
		if module == "" {
			continue
		}
		index.files[module] = append(index.files[module], source)
		parts := strings.Split(module, ".")
		for length := 1; length <= len(parts); length++ {
			index.packages[strings.Join(parts[:length], ".")] = true
		}
	}
	for module := range index.files {
		sort.Strings(index.files[module])
		if pythonConflictingModulePaths(index.files[module]) {
			index.ambiguous[module] = true
		}
	}
	return index
}

func pythonConflictingModulePaths(paths []string) bool {
	locations := map[string]bool{}
	for _, path := range paths {
		location := strings.TrimSuffix(strings.TrimSuffix(path, ".pyi"), ".py")
		locations[location] = true
	}
	return len(locations) > 1
}

func pythonSourceFacts(repo repository.Repository, sources []string) (map[string]pythonSourceFact, error) {
	inputs := make([]pythonfacts.Input, 0, len(sources))
	data := make(map[string][]byte, len(sources))
	for _, source := range sources {
		contents, err := repo.Read(source)
		if err != nil {
			return nil, fmt.Errorf("read %s for python-facts: %w", source, err)
		}
		data[source] = contents
		inputs = append(inputs, pythonfacts.Input{Path: source, Source: string(contents)})
	}
	python := repo.PythonTool()
	if !filepath.IsAbs(python) {
		var err error
		python, err = pythonfacts.DefaultInterpreter()
		if err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(python); err != nil {
		python, err = pythonfacts.DefaultInterpreter()
		if err != nil {
			return nil, err
		}
	}
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Sources: inputs})
	if err != nil {
		return nil, err
	}
	result := make(map[string]pythonSourceFact, len(sources))
	for index, source := range sources {
		fact := response.Sources[index]
		if fact.Error != "" {
			return nil, fmt.Errorf("parse %s: %s", source, fact.Error)
		}
		converted, err := pythonSourceFactFromAdapter(fact, data[source])
		if err != nil {
			return nil, fmt.Errorf("parse %s facts: %w", source, err)
		}
		result[source] = converted
	}
	return result, nil
}

func pythonSourceFactFromAdapter(fact pythonfacts.Source, data []byte) (pythonSourceFact, error) {
	digest := sha256.Sum256(data)
	if fact.SHA256 != hex.EncodeToString(digest[:]) {
		return pythonSourceFact{}, fmt.Errorf("source digest does not match")
	}
	result := pythonSourceFact{SHA256: fact.SHA256}
	for _, reference := range fact.Imports {
		converted, err := pythonImportFactFromAdapter(reference)
		if err != nil {
			return pythonSourceFact{}, err
		}
		result.Imports = append(result.Imports, converted)
	}
	for _, computed := range fact.ComputedImports {
		converted, err := pythonComputedFactFromAdapter(computed)
		if err != nil {
			return pythonSourceFact{}, err
		}
		result.ComputedImports = append(result.ComputedImports, converted)
	}
	return result, nil
}

func pythonImportFactFromAdapter(reference pythonfacts.Import) (pythonImportReference, error) {
	if reference.Module == "" || reference.Line <= 0 || !reference.Literal {
		return pythonImportReference{}, fmt.Errorf("import fact is invalid")
	}
	names := []string(nil)
	if len(reference.Names) > 0 {
		names = append(names, reference.Names...)
	}
	return pythonImportReference{Module: reference.Module, Names: names, Line: reference.Line, Literal: true}, nil
}

func pythonComputedFactFromAdapter(computed pythonfacts.ComputedImport) (pythonComputedImportFact, error) {
	if !validPythonComputedFact(computed) {
		return pythonComputedImportFact{}, fmt.Errorf("computed import fact is invalid")
	}
	converted := pythonComputedImportFact{
		Callee: computed.Callee, Callable: computed.Callable, Line: computed.Line, Column: computed.Column,
		EndLine: computed.EndLine, EndColumn: computed.EndColumn, Argument: computed.Argument, Shape: computed.Shape,
		Targets: append([]string{}, computed.Targets...), EntryPointGroup: computed.EntryPointGroup, EvidenceError: computed.EvidenceError,
	}
	for _, input := range computed.Configuration {
		if input.Path == "" || input.JSONPointer == "" {
			return pythonComputedImportFact{}, fmt.Errorf("computed import configuration fact is invalid")
		}
		converted.Configuration = append(converted.Configuration, pythonComputedConfigurationFact{Path: input.Path, JSONPointer: input.JSONPointer})
	}
	return converted, nil
}

func validPythonComputedFact(computed pythonfacts.ComputedImport) bool {
	return validPythonComputedCallee(computed.Callee) && validPythonComputedPosition(computed) && validPythonComputedBounds(computed)
}

func validPythonComputedCallee(callee string) bool {
	return callee == "importlib.import_module" || callee == "builtins.__import__"
}

func validPythonComputedPosition(computed pythonfacts.ComputedImport) bool {
	return computed.Callable != "" && computed.Line > 0 && computed.Column > 0 && computed.EndLine >= computed.Line
}

func validPythonComputedBounds(computed pythonfacts.ComputedImport) bool {
	return computed.Shape != "" && len(computed.Shape) <= 16384 && len(computed.Argument) <= 4096 &&
		len(computed.Targets) <= 4096 && len(computed.Configuration) <= 4096
}

func pythonUnprovenImport(index pythonModuleIndex, source string, reference pythonImportReference, dependencies map[string]bool) string {
	if !reference.Literal {
		return fmt.Sprintf("line %d dynamic Python import cannot be proven statically", reference.Line)
	}
	module, valid := pythonResolvedImportModule(index, source, reference.Module)
	if !valid {
		return fmt.Sprintf("line %d relative Python import escapes its project", reference.Line)
	}
	if message := pythonAmbiguousImportMessage(index, module, reference.Line); message != "" {
		return message
	}
	if !pythonLocalModule(index, module) {
		return ""
	}
	return pythonProvenImportMessage(index, module, reference, dependencies)
}

func pythonAmbiguousImportMessage(index pythonModuleIndex, module string, line int) string {
	if !pythonAmbiguousImportModule(index, module) {
		return ""
	}
	return fmt.Sprintf("line %d local-looking Python import %q resolves through conflicting source roots", line, module)
}

func pythonProvenImportMessage(index pythonModuleIndex, module string, reference pythonImportReference, dependencies map[string]bool) string {
	if len(reference.Names) == 0 {
		return pythonModuleDependencyMessage(index, module, reference.Line, dependencies)
	}
	for _, name := range reference.Names {
		if message := pythonNamedImportMessage(index, module, name, reference.Line, dependencies); message != "" {
			return message
		}
	}
	return ""
}

func pythonNamedImportMessage(index pythonModuleIndex, module, name string, line int, dependencies map[string]bool) string {
	if name == "*" {
		return pythonModuleDependencyMessage(index, module, line, dependencies)
	}
	qualified := module + "." + name
	if message := pythonAmbiguousImportMessage(index, qualified, line); message != "" {
		return message
	}
	candidates := append([]string{}, index.files[qualified]...)
	candidates = append(candidates, index.files[module]...)
	if len(candidates) == 0 {
		return fmt.Sprintf("line %d local-looking Python import %q cannot be resolved", line, qualified)
	}
	if !pythonGraphContains(dependencies, candidates) {
		return fmt.Sprintf("line %d Ruff did not resolve local-looking Python import %q", line, qualified)
	}
	return ""
}

func pythonAmbiguousImportModule(index pythonModuleIndex, module string) bool {
	parts := strings.Split(module, ".")
	for length := 1; length <= len(parts); length++ {
		if index.ambiguous[strings.Join(parts[:length], ".")] {
			return true
		}
	}
	return false
}

func pythonResolvedImportModule(index pythonModuleIndex, source, module string) (string, bool) {
	if !strings.HasPrefix(module, ".") {
		return module, true
	}
	dots := len(module) - len(strings.TrimLeft(module, "."))
	packageName := index.packageOf[source]
	parts := []string{}
	if packageName != "" {
		parts = strings.Split(packageName, ".")
	}
	if dots > len(parts)+1 {
		return "", false
	}
	parts = parts[:len(parts)-dots+1]
	tail := strings.TrimLeft(module, ".")
	if tail != "" {
		parts = append(parts, strings.Split(tail, ".")...)
	}
	return strings.Join(parts, "."), true
}

func pythonLocalModule(index pythonModuleIndex, module string) bool {
	if module == "" {
		return true
	}
	if index.packages[module] || len(index.files[module]) > 0 {
		return true
	}
	first, _, _ := strings.Cut(module, ".")
	return index.packages[first]
}

func pythonModuleDependencyMessage(index pythonModuleIndex, module string, line int, dependencies map[string]bool) string {
	candidates := index.files[module]
	if len(candidates) == 0 {
		if index.packages[module] {
			return ""
		}
		return fmt.Sprintf("line %d local-looking Python import %q cannot be resolved", line, module)
	}
	if pythonGraphContains(dependencies, candidates) {
		return ""
	}
	return fmt.Sprintf("line %d Ruff did not resolve local-looking Python import %q", line, module)
}

func pythonGraphContains(dependencies map[string]bool, candidates []string) bool {
	for _, candidate := range candidates {
		if dependencies[candidate] {
			return true
		}
	}
	return false
}
