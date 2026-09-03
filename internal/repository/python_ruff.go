package repository

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const pythonRuffLineLengthLimit = 88

type PythonRuffSettings struct {
	RequiresPython string
	TargetVersion  string
	SourceRoots    []string
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

type pythonRuffRelease struct {
	major     int
	minor     int
	patch     int
	precision int
	wildcard  bool
}

type pythonTOMLLeaf struct {
	path  []string
	value pythonTOMLValue
}

func (settings PythonRuffSettings) CommandOptions() ([]string, error) {
	if settings.TargetVersion == "" {
		return nil, fmt.Errorf("project.requires-python does not resolve to a supported Ruff target")
	}
	if len(settings.SourceRoots) == 0 {
		return nil, fmt.Errorf("python project has no validated Ruff source root")
	}
	roots := make([]string, 0, len(settings.SourceRoots))
	seen := map[string]bool{}
	for _, root := range settings.SourceRoots {
		if root == "" || filepath.IsAbs(filepath.FromSlash(root)) || root == ".." || strings.HasPrefix(root, "../") || seen[root] {
			return nil, fmt.Errorf("python project has an invalid Ruff source root %q", root)
		}
		seen[root] = true
		roots = append(roots, strconv.Quote(root))
	}
	lineLength := strconv.Itoa(pythonRuffLineLengthLimit)
	return []string{
		"--target-version", settings.TargetVersion,
		"--config", "line-length = " + lineLength,
		"--config", "lint.pycodestyle.max-line-length = " + lineLength,
		"--config", "src = [" + strings.Join(roots, ", ") + "]",
	}, nil
}

func pythonProjectRuffMetadata(manifest string, requireTarget bool, assignments []pythonTOMLAssignment) (PythonRuffSettings, []PythonRuffProblem) {
	settings := PythonRuffSettings{SourceRoots: []string{"."}}
	problems := []PythonRuffProblem{}
	versionFound := false
	for _, leaf := range pythonTOMLLeaves(assignments) {
		path := strings.Join(leaf.path, ".")
		switch path {
		case "project.requires-python":
			if versionFound {
				problems = append(problems, pythonRuffProblem(PythonRuffTargetProblemKind, manifest, leaf.value.line, "project.requires-python is declared more than once"))
				continue
			}
			versionFound = true
			if leaf.value.kind != pythonTOMLString {
				problems = append(problems, pythonRuffProblem(PythonRuffTargetProblemKind, manifest, leaf.value.line, "project.requires-python must be a string"))
				continue
			}
			settings.RequiresPython = leaf.value.text
			target, err := pythonRuffTargetVersion(leaf.value.text)
			if err != nil {
				problems = append(problems, pythonRuffProblem(PythonRuffTargetProblemKind, manifest, leaf.value.line, "project.requires-python cannot select Ruff: "+err.Error()))
				continue
			}
			settings.TargetVersion = target
		case "tool.ruff.line-length", "tool.ruff.lint.pycodestyle.max-line-length":
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

func pythonCompleteRuffSettings(project *PythonProject) {
	if len(project.Files) > 0 && project.Ruff.TargetVersion == "" && !pythonRuffTargetProblem(project.RuffProblems) {
		project.RuffProblems = append(project.RuffProblems, pythonRuffProblem(PythonRuffTargetProblemKind, project.Manifest, 0, "project.requires-python is required for policy-owned Ruff analysis"))
	}
	if len(project.Ruff.SourceRoots) == 0 {
		project.Ruff.SourceRoots = []string{"."}
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
		document, err := parsePythonTOML(data)
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
	if value.kind != pythonTOMLOther || !validPythonTOMLInteger(value.text) {
		return 0, fmt.Errorf("line length is not an integer")
	}
	normalized := strings.ReplaceAll(value.text, "_", "")
	parsed, err := strconv.ParseInt(normalized, 0, 32)
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
	specifiers, err := parsePythonRequiresSpecifiers(value)
	if err != nil {
		return "", err
	}
	lower, found, err := pythonRuffLowerBound(specifiers)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("specifier has no minimum supported Python version")
	}
	major, minor := lower.major, lower.minor
	for attempts := 0; attempts < 128; attempts++ {
		if pythonRuffMinorMatches(major, minor, specifiers) {
			target := pythonRuffTarget(major, minor)
			if target == "" {
				return "", fmt.Errorf("minimum supported Python version %d.%d is outside Ruff's py37 through py315 range", major, minor)
			}
			return target, nil
		}
		minor++
		if minor == 100 {
			major++
			minor = 0
		}
	}
	return "", fmt.Errorf("specifier does not contain a usable stable Python version")
}

func parsePythonRequiresSpecifiers(value string) ([]PythonVersionSpecifier, error) {
	parser := pythonRequirementParser{input: value}
	parser.skipSpace()
	if parser.done() {
		return nil, fmt.Errorf("specifier is empty")
	}
	specifiers, err := parser.specifiers(false)
	if err != nil {
		return nil, err
	}
	parser.skipSpace()
	if !parser.done() {
		return nil, fmt.Errorf("unexpected specifier content %q", parser.remaining())
	}
	return specifiers, nil
}

func pythonRuffLowerBound(specifiers []PythonVersionSpecifier) (pythonRuffRelease, bool, error) {
	lower := pythonRuffRelease{}
	found := false
	for _, specifier := range specifiers {
		release, err := pythonRuffParseRelease(specifier)
		if err != nil {
			return pythonRuffRelease{}, false, err
		}
		switch specifier.Operator {
		case ">=", ">", "~=", "==":
			if !found || pythonRuffCompareRelease(release, lower) > 0 {
				lower = release
				found = true
			}
		}
	}
	return lower, found, nil
}

func pythonRuffParseRelease(specifier PythonVersionSpecifier) (pythonRuffRelease, error) {
	value, wildcard, err := pythonRuffSpecifierVersion(specifier)
	if err != nil {
		return pythonRuffRelease{}, err
	}
	parts := strings.Split(value, ".")
	if len(parts) == 0 || len(parts) > 3 || specifier.Operator == "~=" && len(parts) < 2 {
		return pythonRuffRelease{}, fmt.Errorf("version %q cannot select a Python minor", specifier.Version)
	}
	numbers := []int{0, 0, 0}
	for index, part := range parts {
		if part == "" || !pythonDigits(part) {
			return pythonRuffRelease{}, fmt.Errorf("version %q is not a stable numeric Python release", specifier.Version)
		}
		number, err := strconv.Atoi(part)
		if err != nil {
			return pythonRuffRelease{}, fmt.Errorf("version %q is out of range", specifier.Version)
		}
		numbers[index] = number
	}
	return pythonRuffRelease{major: numbers[0], minor: numbers[1], patch: numbers[2], precision: len(parts), wildcard: wildcard}, nil
}

func pythonRuffSpecifierVersion(specifier PythonVersionSpecifier) (string, bool, error) {
	if specifier.Operator == "===" {
		return "", false, fmt.Errorf("arbitrary equality cannot select a Ruff target")
	}
	value := strings.TrimPrefix(strings.ToLower(specifier.Version), "v")
	wildcard := strings.HasSuffix(value, ".*")
	if !wildcard {
		return value, false, nil
	}
	if specifier.Operator != "==" && specifier.Operator != "!=" {
		return "", false, fmt.Errorf("wildcard version requires == or !=")
	}
	return strings.TrimSuffix(value, ".*"), true, nil
}

func pythonRuffCompareRelease(left, right pythonRuffRelease) int {
	for _, pair := range [][2]int{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

func pythonRuffMinorMatches(major, minor int, specifiers []PythonVersionSpecifier) bool {
	patches := []int{0, 1, 1_000_000_000}
	for _, specifier := range specifiers {
		release, err := pythonRuffParseRelease(specifier)
		if err != nil || release.major != major || release.minor != minor {
			continue
		}
		for _, patch := range []int{release.patch - 1, release.patch, release.patch + 1} {
			if patch >= 0 {
				patches = append(patches, patch)
			}
		}
	}
	sort.Ints(patches)
	for _, patch := range slices.Compact(patches) {
		candidate := pythonRuffRelease{major: major, minor: minor, patch: patch, precision: 3}
		if pythonRuffReleaseMatches(candidate, specifiers) {
			return true
		}
	}
	return false
}

func pythonRuffReleaseMatches(candidate pythonRuffRelease, specifiers []PythonVersionSpecifier) bool {
	for _, specifier := range specifiers {
		release, err := pythonRuffParseRelease(specifier)
		if err != nil || !pythonRuffSpecifierMatches(candidate, release, specifier.Operator) {
			return false
		}
	}
	return true
}

func pythonRuffSpecifierMatches(candidate, required pythonRuffRelease, operator string) bool {
	comparison := pythonRuffCompareRelease(candidate, required)
	switch operator {
	case "==":
		if required.wildcard {
			return pythonRuffReleasePrefixMatches(candidate, required)
		}
		return comparison == 0
	case "!=":
		if required.wildcard {
			return !pythonRuffReleasePrefixMatches(candidate, required)
		}
		return comparison != 0
	case ">=":
		return comparison >= 0
	case ">":
		return comparison > 0
	case "<=":
		return comparison <= 0
	case "<":
		return comparison < 0
	case "~=":
		return comparison >= 0 && pythonRuffCompatibleRelease(candidate, required)
	}
	return false
}

func pythonRuffReleasePrefixMatches(candidate, required pythonRuffRelease) bool {
	if candidate.major != required.major {
		return false
	}
	return required.precision == 1 || candidate.minor == required.minor
}

func pythonRuffCompatibleRelease(candidate, required pythonRuffRelease) bool {
	if candidate.major != required.major {
		return false
	}
	return required.precision == 2 || candidate.minor == required.minor
}

func pythonRuffTarget(major, minor int) string {
	if major != 3 || minor < 7 || minor > 15 {
		return ""
	}
	return "py3" + strconv.Itoa(minor)
}
