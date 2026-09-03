package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

type PythonProject struct {
	Manifest            string
	Root                string
	Venv                string
	SourceRoot          string
	Ruff                PythonRuffSettings
	RuffProblems        []PythonRuffProblem
	Files               []string
	PackageCandidates   []PythonPackageCandidate
	Requirements        []PythonRequirement
	DynamicReferences   []PythonDynamicReference
	HasProjectTable     bool
	HasBuildSystemTable bool
}

type PythonPackageCandidate struct {
	Name      string
	Path      string
	Namespace bool
}

type PythonInventoryProblemKind string

const (
	PythonNoProjectOwnerProblem    PythonInventoryProblemKind = "no-project-owner"
	PythonAmbiguousProjectProblem  PythonInventoryProblemKind = "ambiguous-project"
	PythonEscapingPathProblem      PythonInventoryProblemKind = "escaping-path"
	PythonUnreadableInputProblem   PythonInventoryProblemKind = "unreadable-input"
	PythonUnsupportedLayoutProblem PythonInventoryProblemKind = "unsupported-layout"
	PythonRuffConfigurationProblem PythonInventoryProblemKind = "ruff-configuration"
)

type PythonInventoryProblem struct {
	Kind    PythonInventoryProblemKind
	Path    string
	Line    int
	Subject string
	Message string
}

type PythonProjectAssignment struct {
	Path     string
	Manifest string
}

type PythonProjectInventory struct {
	Projects    []PythonProject
	Assignments []PythonProjectAssignment
	Problems    []PythonInventoryProblem
}

type pythonProjectInventoryCache struct {
	files         []string
	inventory     PythonProjectInventory
	manifestReads map[string]pythonProjectReadResult
	building      bool
}

type pythonProjectReadResult struct {
	project PythonProject
	err     error
}

func (project PythonProject) IsPythonProject() bool {
	return project.HasProjectTable || project.HasBuildSystemTable
}

func (repo Repository) WithPythonProjectInventory(files []string) (Repository, error) {
	canonical, err := repo.canonicalPythonProjectInventoryFiles(files)
	if err != nil {
		return Repository{}, err
	}
	repo.pythonProjectCache = newPythonProjectInventoryCache(canonical)
	inventory := repo.pythonProjectInventory(canonical)
	repo.pythonProjectCache.inventory = clonePythonProjectInventory(inventory)
	repo.pythonProjectCache.building = false
	return repo, nil
}

func (repo Repository) PythonProjectInventory(files []string) PythonProjectInventory {
	canonical, err := repo.canonicalPythonProjectInventoryFiles(files)
	if err == nil && repo.pythonProjectCache != nil && slices.Equal(canonical, repo.pythonProjectCache.files) {
		return clonePythonProjectInventory(repo.pythonProjectCache.inventory)
	}
	repo.pythonProjectCache = nil
	return repo.pythonProjectInventory(files)
}

func (repo Repository) canonicalPythonProjectInventoryFiles(files []string) ([]string, error) {
	canonical := make([]string, 0, len(files))
	for _, candidate := range files {
		normalized, err := repo.NormalizePath(candidate)
		if err != nil {
			return nil, fmt.Errorf("python project inventory path %q: %w", candidate, err)
		}
		canonical = append(canonical, normalized)
	}
	return uniqueSorted(canonical), nil
}

func newPythonProjectInventoryCache(files []string) *pythonProjectInventoryCache {
	return &pythonProjectInventoryCache{files: slices.Clone(files), manifestReads: map[string]pythonProjectReadResult{}, building: true}
}

func clonePythonProjectInventory(inventory PythonProjectInventory) PythonProjectInventory {
	clone := PythonProjectInventory{
		Projects:    slices.Clone(inventory.Projects),
		Assignments: slices.Clone(inventory.Assignments),
		Problems:    slices.Clone(inventory.Problems),
	}
	for index := range clone.Projects {
		clone.Projects[index] = clonePythonProject(clone.Projects[index])
	}
	return clone
}

func clonePythonProject(project PythonProject) PythonProject {
	clone := project
	clone.Files = slices.Clone(project.Files)
	clone.Ruff.SourceRoots = slices.Clone(project.Ruff.SourceRoots)
	clone.RuffProblems = slices.Clone(project.RuffProblems)
	clone.PackageCandidates = slices.Clone(project.PackageCandidates)
	clone.Requirements = slices.Clone(project.Requirements)
	clone.DynamicReferences = slices.Clone(project.DynamicReferences)
	for index := range clone.Requirements {
		clone.Requirements[index] = clonePythonRequirement(clone.Requirements[index])
	}
	return clone
}

func clonePythonRequirement(requirement PythonRequirement) PythonRequirement {
	clone := requirement
	clone.Extras = slices.Clone(requirement.Extras)
	clone.Specifiers = slices.Clone(requirement.Specifiers)
	return clone
}

type pythonInventoryInputs struct {
	manifests   map[string]bool
	ruffConfigs []string
	owners      map[string]string
	seenSources map[string]bool
	sources     []string
	problems    []PythonInventoryProblem
}

func (repo Repository) pythonProjectInventory(files []string) PythonProjectInventory {
	inputs := repo.pythonInventoryInputs(files)
	inventory := PythonProjectInventory{Problems: inputs.problems}
	projects := repo.pythonInventoryProjects(inputs.manifests, &inventory)
	repo.assignPythonInventoryFiles(&inventory, projects, inputs.owners, inputs.sources)
	repo.completePythonProjectInventory(&inventory, projects, inputs.manifests)
	repo.appendPythonRuffConfigurationProblems(&inventory, inputs.ruffConfigs)
	pythonInventorySort(&inventory)
	inventory.Problems = compactPythonInventoryProblems(inventory.Problems)
	return inventory
}

func (repo Repository) pythonInventoryInputs(files []string) pythonInventoryInputs {
	inputs := pythonInventoryInputs{
		manifests:   map[string]bool{},
		owners:      map[string]string{},
		seenSources: map[string]bool{},
	}
	for _, candidate := range files {
		repo.collectPythonInventoryCandidate(&inputs, candidate)
	}
	return inputs
}

func (repo Repository) collectPythonInventoryCandidate(inputs *pythonInventoryInputs, candidate string) {
	normalized, err := repo.NormalizePath(candidate)
	if err != nil {
		inputs.problems = append(inputs.problems, pythonInventoryProblem(PythonEscapingPathProblem, candidate, candidate, err.Error()))
		return
	}
	if filepath.Base(normalized) == "pyproject.toml" {
		inputs.manifests[normalized] = true
	}
	if pythonRuffConfigurationPath(normalized) {
		inputs.ruffConfigs = append(inputs.ruffConfigs, normalized)
	}
	if pythonSourcePath(normalized) {
		repo.collectPythonInventorySource(inputs, normalized)
	}
}

func (repo Repository) collectPythonInventorySource(inputs *pythonInventoryInputs, source string) {
	if inputs.seenSources[source] {
		return
	}
	inputs.seenSources[source] = true
	if problem := repo.pythonSourceProblem(source); problem != nil {
		inputs.problems = append(inputs.problems, *problem)
		return
	}
	manifest, problem := repo.nearestPythonManifest(source)
	if problem != nil {
		inputs.problems = append(inputs.problems, *problem)
		return
	}
	if manifest == "" {
		inputs.problems = append(inputs.problems, pythonInventoryProblem(PythonNoProjectOwnerProblem, source, source, "no contained pyproject.toml owns this Python source file"))
		return
	}
	inputs.owners[source] = manifest
	inputs.manifests[manifest] = true
	inputs.sources = append(inputs.sources, source)
}

func (repo Repository) pythonInventoryProjects(manifests map[string]bool, inventory *PythonProjectInventory) map[string]*PythonProject {
	projects := map[string]*PythonProject{}
	for _, manifest := range sortedPythonManifestPaths(manifests) {
		project, problem := repo.pythonInventoryProject(manifest)
		if problem != nil {
			inventory.Problems = append(inventory.Problems, *problem)
			continue
		}
		projects[manifest] = &project
	}
	return projects
}

func (repo Repository) assignPythonInventoryFiles(inventory *PythonProjectInventory, projects map[string]*PythonProject, owners map[string]string, sources []string) {
	sort.Strings(sources)
	for _, source := range sources {
		manifest := owners[source]
		project, found := projects[manifest]
		if !found {
			continue
		}
		project.Files = append(project.Files, source)
		inventory.Assignments = append(inventory.Assignments, PythonProjectAssignment{Path: source, Manifest: manifest})
	}
}

func (repo Repository) completePythonProjectInventory(inventory *PythonProjectInventory, projects map[string]*PythonProject, manifests map[string]bool) {
	for _, manifest := range sortedPythonManifestPaths(manifests) {
		project, found := projects[manifest]
		if !found {
			continue
		}
		sort.Strings(project.Files)
		project.PackageCandidates = pythonPackageCandidates(*project)
		pythonCompleteRuffSettings(project)
		for _, problem := range project.RuffProblems {
			inventory.Problems = append(inventory.Problems, pythonRuffInventoryProblem(project.Manifest, problem))
		}
		inventory.Projects = append(inventory.Projects, *project)
	}
	pythonInventoryAmbiguities(inventory)
}

func pythonInventorySort(inventory *PythonProjectInventory) {
	sort.Slice(inventory.Projects, func(left, right int) bool {
		return inventory.Projects[left].Manifest < inventory.Projects[right].Manifest
	})
	sort.Slice(inventory.Assignments, func(left, right int) bool {
		return inventory.Assignments[left].Path < inventory.Assignments[right].Path
	})
	sort.Slice(inventory.Problems, func(left, right int) bool {
		leftProblem := inventory.Problems[left]
		rightProblem := inventory.Problems[right]
		if leftProblem.Path != rightProblem.Path {
			return leftProblem.Path < rightProblem.Path
		}
		if leftProblem.Line != rightProblem.Line {
			return leftProblem.Line < rightProblem.Line
		}
		return string(leftProblem.Kind)+"\x00"+leftProblem.Subject+"\x00"+leftProblem.Message <
			string(rightProblem.Kind)+"\x00"+rightProblem.Subject+"\x00"+rightProblem.Message
	})
}

func pythonSourcePath(value string) bool {
	extension := strings.ToLower(filepath.Ext(value))
	return extension == ".py" || extension == ".pyi"
}

func (repo Repository) pythonSourceProblem(source string) *PythonInventoryProblem {
	resolved, err := repo.Resolve(source)
	if err != nil {
		kind := PythonUnreadableInputProblem
		if strings.Contains(err.Error(), "outside the repository") || strings.Contains(err.Error(), "stay inside") {
			kind = PythonEscapingPathProblem
		}
		problem := pythonInventoryProblem(kind, source, source, err.Error())
		return &problem
	}
	info, err := os.Stat(resolved)
	if err != nil {
		problem := pythonInventoryProblem(PythonUnreadableInputProblem, source, source, err.Error())
		return &problem
	}
	if !info.Mode().IsRegular() {
		problem := pythonInventoryProblem(PythonUnsupportedLayoutProblem, source, source, "governed Python source is not a regular file")
		return &problem
	}
	return nil
}

func (repo Repository) nearestPythonManifest(source string) (string, *PythonInventoryProblem) {
	directory := filepath.ToSlash(filepath.Dir(source))
	for {
		manifest := "pyproject.toml"
		if directory != "." {
			manifest = directory + "/pyproject.toml"
		}
		absolute := filepath.Join(repo.Root, filepath.FromSlash(manifest))
		info, err := os.Lstat(absolute)
		if err == nil {
			if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				problem := pythonInventoryProblem(PythonUnsupportedLayoutProblem, source, manifest, "nearest pyproject.toml is not a regular file")
				return "", &problem
			}
			resolved, resolveErr := repo.Resolve(manifest)
			if resolveErr != nil {
				problem := pythonInventoryProblem(PythonEscapingPathProblem, source, manifest, resolveErr.Error())
				return "", &problem
			}
			resolvedInfo, statErr := os.Stat(resolved)
			if statErr != nil {
				problem := pythonInventoryProblem(PythonUnreadableInputProblem, source, manifest, statErr.Error())
				return "", &problem
			}
			if !resolvedInfo.Mode().IsRegular() {
				problem := pythonInventoryProblem(PythonUnsupportedLayoutProblem, source, manifest, "nearest pyproject.toml is not a regular file")
				return "", &problem
			}
			return manifest, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			problem := pythonInventoryProblem(PythonUnreadableInputProblem, source, manifest, err.Error())
			return "", &problem
		}
		if directory == "." {
			return "", nil
		}
		directory = filepath.ToSlash(filepath.Dir(directory))
	}
}

func (repo Repository) pythonInventoryProject(manifest string) (PythonProject, *PythonInventoryProblem) {
	project, err := repo.ReadPythonProject(manifest)
	if err != nil {
		kind := PythonUnreadableInputProblem
		if strings.Contains(err.Error(), "outside the repository") || strings.Contains(err.Error(), "stay inside") {
			kind = PythonEscapingPathProblem
		}
		problem := pythonInventoryProblem(kind, manifest, manifest, err.Error())
		return PythonProject{}, &problem
	}
	if problem := repo.pythonProjectDirectoryProblem(manifest, project.Root, ".venv", true, &project); problem != nil {
		return PythonProject{}, problem
	}
	if problem := repo.pythonProjectDirectoryProblem(manifest, project.Root, "src", false, &project); problem != nil {
		return PythonProject{}, problem
	}
	return project, nil
}

func (repo Repository) pythonProjectDirectoryProblem(manifest, root, name string, venv bool, project *PythonProject) *PythonInventoryProblem {
	directory := name
	if root != "." {
		directory = root + "/" + name
	}
	abs := filepath.Join(repo.Root, filepath.FromSlash(directory))
	info, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		problem := pythonInventoryProblem(PythonUnreadableInputProblem, manifest, directory, err.Error())
		return &problem
	}
	if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		problem := pythonInventoryProblem(PythonUnsupportedLayoutProblem, manifest, directory, "project directory is not a directory")
		return &problem
	}
	resolved, resolveErr := repo.Resolve(directory)
	if resolveErr != nil {
		problem := pythonInventoryProblem(PythonEscapingPathProblem, manifest, directory, resolveErr.Error())
		return &problem
	}
	resolvedInfo, statErr := os.Stat(resolved)
	if statErr != nil {
		problem := pythonInventoryProblem(PythonUnreadableInputProblem, manifest, directory, statErr.Error())
		return &problem
	}
	if !resolvedInfo.IsDir() {
		problem := pythonInventoryProblem(PythonUnsupportedLayoutProblem, manifest, directory, "project directory is not a directory")
		return &problem
	}
	if venv {
		project.Venv = directory
	} else {
		project.SourceRoot = directory
		project.Ruff.SourceRoots = []string{".", name}
	}
	return nil
}

func pythonInventoryProblem(kind PythonInventoryProblemKind, path, subject, message string) PythonInventoryProblem {
	return PythonInventoryProblem{Kind: kind, Path: path, Subject: subject, Message: message}
}

func sortedPythonManifestPaths(manifests map[string]bool) []string {
	paths := make([]string, 0, len(manifests))
	for manifest := range manifests {
		paths = append(paths, manifest)
	}
	sort.Strings(paths)
	return paths
}

func pythonInventoryAmbiguities(inventory *PythonProjectInventory) {
	roots := map[string][]PythonProject{}
	for _, project := range inventory.Projects {
		roots[project.Root] = append(roots[project.Root], project)
	}
	for root, projects := range roots {
		if len(projects) < 2 {
			continue
		}
		manifests := make([]string, 0, len(projects))
		for _, project := range projects {
			manifests = append(manifests, project.Manifest)
		}
		sort.Strings(manifests)
		inventory.Problems = append(inventory.Problems, pythonInventoryProblem(PythonAmbiguousProjectProblem, root, strings.Join(manifests, ","), "multiple Python projects claim the same root"))
	}
}

func compactPythonInventoryProblems(problems []PythonInventoryProblem) []PythonInventoryProblem {
	result := make([]PythonInventoryProblem, 0, len(problems))
	for _, problem := range problems {
		if len(result) > 0 && result[len(result)-1] == problem {
			continue
		}
		result = append(result, problem)
	}
	return result
}

func pythonPackageCandidates(project PythonProject) []PythonPackageCandidate {
	candidates := map[string]PythonPackageCandidate{}
	initFiles := pythonPackageInitFiles(project.Files)
	for _, source := range project.Files {
		pythonPackageCandidatesForSource(candidates, project, source, initFiles)
	}
	return sortedPythonPackageCandidates(candidates)
}

func pythonPackageInitFiles(files []string) map[string]bool {
	initFiles := map[string]bool{}
	for _, source := range files {
		base := filepath.Base(source)
		if base == "__init__.py" || base == "__init__.pyi" {
			initFiles[filepath.ToSlash(filepath.Dir(source))] = true
		}
	}
	return initFiles
}

func pythonPackageCandidatesForSource(candidates map[string]PythonPackageCandidate, project PythonProject, source string, initFiles map[string]bool) {
	base := pythonPackageCandidateBase(project, source)
	relative, found := pythonPackageRelativeDirectory(base, source)
	if !found {
		return
	}
	pythonPackageCandidateSegments(candidates, base, relative, initFiles)
}

func pythonPackageCandidateBase(project PythonProject, source string) string {
	if project.SourceRoot != "" && pythonPathContains(project.SourceRoot, source) {
		return project.SourceRoot
	}
	return project.Root
}

func pythonPackageRelativeDirectory(base, source string) (string, bool) {
	directory := filepath.ToSlash(filepath.Dir(source))
	relative, err := filepath.Rel(filepath.FromSlash(base), filepath.FromSlash(directory))
	if err != nil {
		return "", false
	}
	relative = filepath.ToSlash(relative)
	if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", false
	}
	return relative, true
}

func pythonPackageCandidateSegments(candidates map[string]PythonPackageCandidate, base, relative string, initFiles map[string]bool) {
	segments := strings.Split(relative, "/")
	for index := range segments {
		candidatePath := strings.TrimPrefix(base+"/"+strings.Join(segments[:index+1], "/"), "./")
		candidates[candidatePath] = PythonPackageCandidate{
			Name: strings.Join(segments[:index+1], "."), Path: candidatePath, Namespace: !initFiles[candidatePath],
		}
	}
}

func sortedPythonPackageCandidates(candidates map[string]PythonPackageCandidate) []PythonPackageCandidate {
	paths := make([]string, 0, len(candidates))
	for candidate := range candidates {
		paths = append(paths, candidate)
	}
	sort.Strings(paths)
	result := make([]PythonPackageCandidate, 0, len(paths))
	for _, candidate := range paths {
		result = append(result, candidates[candidate])
	}
	return result
}

func pythonPathContains(root, candidate string) bool {
	return root == "." || candidate == root || strings.HasPrefix(candidate, root+"/")
}

func (repo Repository) ReadPythonProject(manifest string) (PythonProject, error) {
	normalized, err := repo.NormalizePath(manifest)
	if err != nil {
		return PythonProject{}, err
	}
	if filepath.Base(normalized) != "pyproject.toml" {
		return PythonProject{}, fmt.Errorf("python manifest must be pyproject.toml: %s", normalized)
	}
	if cache := repo.pythonProjectCache; cache != nil {
		if result, found := cache.manifestReads[normalized]; found {
			if result.err != nil {
				return PythonProject{}, result.err
			}
			return clonePythonProject(result.project), nil
		}
		if cache.building {
			project, readErr := repo.readPythonProject(normalized)
			cache.manifestReads[normalized] = pythonProjectReadResult{project: clonePythonProject(project), err: readErr}
			if readErr != nil {
				return PythonProject{}, readErr
			}
			return clonePythonProject(project), nil
		}
	}
	return repo.readPythonProject(normalized)
}

func (repo Repository) readPythonProject(normalized string) (PythonProject, error) {
	data, err := repo.Read(normalized)
	if err != nil {
		return PythonProject{}, err
	}
	project, err := ParsePythonProject(normalized, data)
	if err != nil {
		return PythonProject{}, err
	}
	venv := ".venv"
	if project.Root != "." {
		venv = project.Root + "/.venv"
	}
	if resolved, resolveErr := repo.Resolve(venv); resolveErr == nil {
		if info, statErr := os.Stat(resolved); statErr == nil && info.IsDir() {
			project.Venv = venv
		}
	}
	return project, nil
}

func ParsePythonProject(manifest string, data []byte) (PythonProject, error) {
	if !utf8.Valid(data) {
		return PythonProject{}, fmt.Errorf("parse %s: manifest is not valid UTF-8", manifest)
	}
	document, err := parsePythonTOML(data)
	if err != nil {
		return PythonProject{}, fmt.Errorf("parse %s: %w", manifest, err)
	}
	project := pythonProjectDefinition(manifest, document.tables, document.assignments)
	project.Ruff, project.RuffProblems = pythonProjectRuffMetadata(manifest, project.IsPythonProject(), document.assignments)
	dynamicReferences, err := pythonProjectDynamicReferences(manifest, document.assignments)
	if err != nil {
		return PythonProject{}, err
	}
	project.DynamicReferences = dynamicReferences
	requirements, err := pythonProjectRequirements(manifest, document.assignments)
	if err != nil {
		return PythonProject{}, err
	}
	project.Requirements = requirements
	if err := pythonRequirementConflicts(project.Requirements); err != nil {
		return PythonProject{}, fmt.Errorf("parse %s: %w", manifest, err)
	}
	return project, nil
}

func pythonProjectDefinition(manifest string, tables []pythonTOMLTable, assignments []pythonTOMLAssignment) PythonProject {
	project := PythonProject{Manifest: manifest, Root: filepath.ToSlash(filepath.Dir(manifest))}
	for _, table := range tables {
		pythonProjectTable(&project, table)
	}
	for _, assignment := range assignments {
		pythonProjectAssignmentTable(&project, assignment)
	}
	return project
}

func pythonProjectTable(project *PythonProject, table pythonTOMLTable) {
	if table.array {
		return
	}
	switch table.name {
	case "project":
		project.HasProjectTable = true
	case "build-system":
		project.HasBuildSystemTable = true
	}
}

func pythonProjectAssignmentTable(project *PythonProject, assignment pythonTOMLAssignment) {
	root := pythonProjectAssignmentRoot(assignment)
	if root == "" {
		return
	}
	switch root {
	case "project":
		project.HasProjectTable = true
	case "build-system":
		project.HasBuildSystemTable = true
	}
}

func pythonProjectAssignmentRoot(assignment pythonTOMLAssignment) string {
	table := assignment.tablePath
	if len(table) == 0 && assignment.table != "" {
		table = strings.Split(assignment.table, ".")
	}
	if len(table) > 0 {
		return table[0]
	}
	key := assignment.keyPath
	if len(key) == 0 && assignment.key != "" {
		key = strings.Split(assignment.key, ".")
	}
	if len(key) > 1 || len(key) == 1 && assignment.value.kind == pythonTOMLInlineTable {
		return key[0]
	}
	return ""
}

func pythonProjectRequirements(manifest string, assignments []pythonTOMLAssignment) ([]PythonRequirement, error) {
	requirements := []PythonRequirement{}
	for _, assignment := range assignments {
		usage, group, dependency := pythonDependencyAssignment(assignment)
		if !dependency {
			continue
		}
		values, err := pythonAssignmentRequirements(manifest, assignment, usage, group)
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, values...)
	}
	return requirements, nil
}

func pythonAssignmentRequirements(manifest string, assignment pythonTOMLAssignment, usage, group string) ([]PythonRequirement, error) {
	if assignment.value.kind != pythonTOMLArray {
		return nil, pythonManifestError(manifest, assignment.line, "%s must be an array of requirement strings", assignment.key)
	}
	requirements := make([]PythonRequirement, 0, len(assignment.value.array))
	for element, value := range assignment.value.array {
		requirement, err := pythonAssignmentRequirement(manifest, assignment, usage, group, element, value)
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, requirement)
	}
	return requirements, nil
}

func pythonAssignmentRequirement(manifest string, assignment pythonTOMLAssignment, usage, group string, element int, value pythonTOMLValue) (PythonRequirement, error) {
	if value.kind != pythonTOMLString {
		return PythonRequirement{}, pythonManifestError(manifest, value.line, "%s must contain only requirement strings", assignment.key)
	}
	requirement, err := ParsePythonRequirement(value.text)
	if err != nil {
		return PythonRequirement{}, pythonManifestError(manifest, value.line, "invalid requirement %q: %v", value.text, err)
	}
	requirement.Usage = usage
	requirement.Location = PythonRequirementLocation{
		Path: manifest, Table: assignment.table, Group: group, Line: value.line, Element: element,
	}
	return requirement, nil
}

func pythonDependencyAssignment(assignment pythonTOMLAssignment) (string, string, bool) {
	if assignment.arrayTable {
		return "", "", false
	}
	switch assignment.table {
	case "project":
		if assignment.key == "dependencies" {
			return "runtime", "", true
		}
	case "project.optional-dependencies":
		return "optional", assignment.key, assignment.key != ""
	case "dependency-groups":
		return "development", assignment.key, assignment.key != ""
	case "build-system":
		if assignment.key == "requires" {
			return "build", "", true
		}
	}
	return "", "", false
}

func pythonRequirementConflicts(requirements []PythonRequirement) error {
	seen := map[string]PythonRequirement{}
	for _, requirement := range requirements {
		if existing, found := pythonConflictingRequirement(seen, requirement); found {
			return pythonManifestError(requirement.Location.Path, requirement.Location.Line, "dependency %q contradicts the declaration at line %d", requirement.Name, existing.Location.Line)
		}
		seen[pythonRequirementConflictKey(requirement)] = requirement
	}
	return nil
}

func pythonRequirementConflictKey(requirement PythonRequirement) string {
	key := requirement.Name + "\x00" + requirement.markerKey
	if requirement.Usage == "build" {
		return key + "\x00build"
	}
	return key
}

func pythonConflictingRequirement(existingRequirements map[string]PythonRequirement, requirement PythonRequirement) (PythonRequirement, bool) {
	for _, existing := range existingRequirements {
		if pythonRequirementsContradict(existing, requirement) {
			return existing, true
		}
	}
	return PythonRequirement{}, false
}

func pythonRequirementsContradict(left, right PythonRequirement) bool {
	if left.Name != right.Name || (left.Usage == "build") != (right.Usage == "build") {
		return false
	}
	if !pythonMarkersOverlap(left, right) {
		return false
	}
	return left.SourceIdentity() != right.SourceIdentity() || left.Kind != right.Kind
}

func pythonMarkersOverlap(left, right PythonRequirement) bool {
	if left.markerKey == right.markerKey || left.markerKey == "" || right.markerKey == "" {
		return true
	}
	leftVariable, leftValue, leftExact := pythonSimpleMarkerEquality(left.Marker)
	rightVariable, rightValue, rightExact := pythonSimpleMarkerEquality(right.Marker)
	return !leftExact || !rightExact || leftVariable != rightVariable || leftValue == rightValue
}

func pythonSimpleMarkerEquality(marker string) (string, string, bool) {
	tokens, err := pythonMarkerTokens(marker)
	if err != nil || len(tokens) != 3 || tokens[1].kind != pythonMarkerOperator || tokens[1].text != "==" {
		return "", "", false
	}
	variable, literal, found := pythonMarkerEqualityOperands(tokens[0], tokens[2])
	if !found || !pythonMarkerExactStringVariable(variable) {
		return "", "", false
	}
	return variable, literal, true
}

func pythonMarkerEqualityOperands(left, right pythonMarkerToken) (string, string, bool) {
	if left.kind == pythonMarkerIdentifier && right.kind == pythonMarkerString {
		literal, valid := pythonSimpleMarkerString(right.text)
		return left.text, literal, valid
	}
	if left.kind == pythonMarkerString && right.kind == pythonMarkerIdentifier {
		literal, valid := pythonSimpleMarkerString(left.text)
		return right.text, literal, valid
	}
	return "", "", false
}

func pythonSimpleMarkerString(value string) (string, bool) {
	if len(value) < 2 || value[0] != value[len(value)-1] || value[0] != '\'' && value[0] != '"' || strings.Contains(value, "\\") {
		return "", false
	}
	return value[1 : len(value)-1], true
}

func pythonMarkerExactStringVariable(value string) bool {
	return map[string]bool{
		"implementation_name": true, "os_name": true, "platform_machine": true,
		"platform_python_implementation": true, "platform_system": true, "sys_platform": true,
	}[value]
}

func pythonManifestError(manifest string, line int, format string, arguments ...any) error {
	message := fmt.Sprintf(format, arguments...)
	if line <= 0 {
		return fmt.Errorf("%s: %s", manifest, message)
	}
	return fmt.Errorf("%s line %d: %s", manifest, line, message)
}
