package repository

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

const pythonRuffLineLengthLimit = 88

type PythonRuffSettings struct {
	RequiresPython string
	TargetVersion  string
}

type PythonRuffProblem struct {
	Kind    PythonRuffProblemKind
	Path    string
	Line    int
	Message string
}

type PythonRuffProblemKind string

const (
	PythonRuffTargetProblemKind     PythonRuffProblemKind = "target-version"
	PythonRuffLineLengthProblemKind PythonRuffProblemKind = "line-length"
	PythonRuffConfigProblemKind     PythonRuffProblemKind = "configuration"
)

type pythonTOMLLeaf struct {
	path  []string
	value pythonTOMLValue
}

func (settings PythonRuffSettings) CommandOptions(projectRoot string, sourceRoots []string) ([]string, error) {
	if settings.TargetVersion == "" {
		return nil, fmt.Errorf("project.requires-python does not resolve to a supported Ruff target")
	}
	if len(sourceRoots) == 0 {
		return nil, fmt.Errorf("python project has no validated Ruff source root")
	}
	roots := make([]string, 0, len(sourceRoots))
	seen := map[string]bool{}
	for _, root := range sourceRoots {
		relative, err := filepath.Rel(filepath.FromSlash(projectRoot), filepath.FromSlash(root))
		if err != nil {
			return nil, fmt.Errorf("python project has an invalid Ruff source root %q", root)
		}
		relative = filepath.ToSlash(relative)
		if relative == "" {
			relative = "."
		}
		if relative == ".." || strings.HasPrefix(relative, "../") || filepath.IsAbs(filepath.FromSlash(relative)) || seen[relative] {
			return nil, fmt.Errorf("python project has an invalid Ruff source root %q", root)
		}
		seen[relative] = true
		roots = append(roots, strconv.Quote(relative))
	}
	lineLength := strconv.Itoa(pythonRuffLineLengthLimit)
	return []string{
		"--target-version", settings.TargetVersion,
		"--config", "line-length = " + lineLength,
		"--config", "lint.pycodestyle.max-line-length = " + lineLength,
		"--config", "src = [" + strings.Join(roots, ", ") + "]",
	}, nil
}

func pythonProjectRuffMetadata(manifest string, requireTarget bool, assignments []pythonTOMLAssignment, specifiers map[string]pythonfacts.Specifier) (PythonRuffSettings, []PythonRuffProblem) {
	settings := PythonRuffSettings{}
	problems := []PythonRuffProblem{}
	versionFound := false
	for _, leaf := range pythonTOMLLeaves(assignments) {
		path := strings.Join(leaf.path, ".")
		if path == "project.requires-python" {
			problem := applyPythonRuffTarget(manifest, leaf.value, specifiers, &settings, &versionFound)
			if problem != nil {
				problems = append(problems, *problem)
			}
		}
		if path == "tool.ruff.line-length" || path == "tool.ruff.lint.pycodestyle.max-line-length" {
			if problem := pythonRuffLineLengthProblem(manifest, path, leaf.value); problem != nil {
				problems = append(problems, *problem)
			}
		}
	}
	if requireTarget && !versionFound {
		problems = append(problems, pythonRuffProblem(PythonRuffTargetProblemKind, manifest, 0, "project.requires-python is required for policy-owned Ruff analysis"))
	}
	return settings, problems
}

func applyPythonRuffTarget(manifest string, value pythonTOMLValue, specifiers map[string]pythonfacts.Specifier, settings *PythonRuffSettings, found *bool) *PythonRuffProblem {
	if *found {
		problem := pythonRuffProblem(PythonRuffTargetProblemKind, manifest, value.line, "project.requires-python is declared more than once")
		return &problem
	}
	*found = true
	if value.kind != pythonTOMLString {
		problem := pythonRuffProblem(PythonRuffTargetProblemKind, manifest, value.line, "project.requires-python must be a string")
		return &problem
	}
	settings.RequiresPython = value.text
	fact, present := specifiers[value.text]
	if present && fact.Error == "" && fact.Target != "" {
		settings.TargetVersion = fact.Target
		return nil
	}
	message := "python-facts omitted the version specifier"
	if present && fact.Error != "" {
		message = fact.Error
	}
	problem := pythonRuffProblem(PythonRuffTargetProblemKind, manifest, value.line, "project.requires-python cannot select Ruff: "+message)
	return &problem
}

func pythonCompleteRuffSettings(project *PythonProject) {
	if len(project.Files) > 0 && project.Ruff.TargetVersion == "" && !pythonRuffTargetProblem(project.RuffProblems) {
		project.RuffProblems = append(project.RuffProblems, pythonRuffProblem(PythonRuffTargetProblemKind, project.Manifest, 0, "project.requires-python is required for policy-owned Ruff analysis"))
	}
}

func pythonRuffTargetProblem(problems []PythonRuffProblem) bool {
	for _, problem := range problems {
		if problem.Kind == PythonRuffTargetProblemKind {
			return true
		}
	}
	return false
}

func pythonRuffProblem(kind PythonRuffProblemKind, path string, line int, message string) PythonRuffProblem {
	return PythonRuffProblem{Kind: kind, Path: path, Line: line, Message: message}
}

func pythonRuffInventoryProblem(manifest string, problem PythonRuffProblem) PythonInventoryProblem {
	return PythonInventoryProblem{
		Kind: PythonRuffConfigurationProblem, Path: problem.Path, Line: problem.Line, Subject: manifest,
		Message: problem.Message,
	}
}

func pythonRuffConfigurationPath(path string) bool {
	base := filepath.Base(path)
	return base == "ruff.toml" || base == ".ruff.toml"
}

func (repo Repository) appendPythonRuffConfigurationProblems(inventory *PythonProjectInventory, paths []string) {
	sort.Strings(paths)
	for _, path := range slices.Compact(paths) {
		manifest := pythonRuffConfigurationOwner(path, inventory.Projects)
		if manifest == "" {
			continue
		}
		data, err := repo.Read(path)
		if err != nil {
			inventory.Problems = append(inventory.Problems, pythonRuffInventoryProblem(manifest, pythonRuffProblem(PythonRuffConfigProblemKind, path, 0, "Ruff configuration is unreadable: "+err.Error())))
			continue
		}
		document, err := parsePythonTOMLWith(repo.PythonTool(), path, data)
		if err != nil {
			inventory.Problems = append(inventory.Problems, pythonRuffInventoryProblem(manifest, pythonRuffProblem(PythonRuffConfigProblemKind, path, 0, "Ruff configuration is malformed: "+err.Error())))
			continue
		}
		for _, leaf := range pythonTOMLLeaves(document.assignments) {
			configPath := strings.Join(leaf.path, ".")
			if configPath != "line-length" && configPath != "lint.pycodestyle.max-line-length" {
				continue
			}
			if problem := pythonRuffLineLengthProblem(path, configPath, leaf.value); problem != nil {
				inventory.Problems = append(inventory.Problems, pythonRuffInventoryProblem(manifest, *problem))
			}
		}
	}
}

func pythonRuffConfigurationOwner(path string, projects []PythonProject) string {
	owner := ""
	ownerRoot := ""
	directory := filepath.ToSlash(filepath.Dir(path))
	for _, project := range projects {
		if project.Root != "." && path != project.Root && !strings.HasPrefix(path, project.Root+"/") {
			continue
		}
		if directory != project.Root && !pythonRuffConfigurationCoversSource(directory, project.Files) {
			continue
		}
		if owner == "" || len(project.Root) > len(ownerRoot) {
			owner = project.Manifest
			ownerRoot = project.Root
		}
	}
	return owner
}

func pythonRuffConfigurationCoversSource(directory string, sources []string) bool {
	for _, source := range sources {
		if directory == "." || strings.HasPrefix(source, directory+"/") {
			return true
		}
	}
	return false
}

func pythonRuffLineLengthProblem(path, setting string, value pythonTOMLValue) *PythonRuffProblem {
	lineLength, err := pythonRuffLineLength(value)
	if err != nil {
		problem := pythonRuffProblem(PythonRuffLineLengthProblemKind, path, value.line, setting+" must be the integer "+strconv.Itoa(pythonRuffLineLengthLimit))
		return &problem
	}
	if lineLength == pythonRuffLineLengthLimit {
		return nil
	}
	problem := pythonRuffProblem(PythonRuffLineLengthProblemKind, path, value.line, fmt.Sprintf("%s conflicts with the policy-owned line length %d", setting, pythonRuffLineLengthLimit))
	return &problem
}

func pythonRuffLineLength(value pythonTOMLValue) (int, error) {
	if value.kind != pythonTOMLOther {
		return 0, fmt.Errorf("line length is not an integer")
	}
	parsed, err := strconv.ParseInt(value.text, 10, 32)
	if err != nil {
		return 0, err
	}
	return int(parsed), nil
}

func pythonTOMLLeaves(assignments []pythonTOMLAssignment) []pythonTOMLLeaf {
	leaves := []pythonTOMLLeaf{}
	for _, assignment := range assignments {
		if assignment.arrayTable {
			continue
		}
		pythonTOMLValueLeaves(&leaves, pythonTOMLAssignmentPath(assignment), assignment.value)
	}
	return leaves
}

func pythonTOMLValueLeaves(leaves *[]pythonTOMLLeaf, path []string, value pythonTOMLValue) {
	if value.kind != pythonTOMLInlineTable {
		*leaves = append(*leaves, pythonTOMLLeaf{path: slices.Clone(path), value: value})
		return
	}
	for _, field := range value.inline {
		child := append(slices.Clone(path), field.path...)
		pythonTOMLValueLeaves(leaves, child, field.value)
	}
}

func pythonRuffTargetVersion(value string) (string, error) {
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return "", err
	}
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Specifiers: []string{value}})
	if err != nil {
		return "", err
	}
	fact := response.Specifiers[0]
	if fact.Error != "" || fact.Target == "" {
		return "", fmt.Errorf("%s", fact.Error)
	}
	return fact.Target, nil
}
