package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

type PythonProject struct {
	Manifest            string
	Root                string
	Venv                string
	SourceRoots         []string
	BuildBackend        PythonBuildBackend
	BackendPaths        []PythonProjectPath
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

func PythonInventoryFinding(problem PythonInventoryProblem) policy.Finding {
	subject := problem.Subject
	if subject == "" {
		subject = string(problem.Kind)
	}
	return policy.Finding{
		Check: "policy.pythonProject", Path: problem.Path, Line: problem.Line, Subject: subject, Message: problem.Message,
	}
}

type pythonProjectInventoryCache struct {
	files         []string
	inventory     PythonProjectInventory
	manifestReads map[string]pythonProjectReadResult
	building      bool
	hits          atomic.Int64
	misses        atomic.Int64
	builds        atomic.Int64
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
	repo.pythonProjectCache.builds.Add(1)
	inventory := repo.pythonProjectInventory(canonical)
	repo.pythonProjectCache.inventory = clonePythonProjectInventory(inventory)
	repo.pythonProjectCache.building = false
	return repo, nil
}

func (repo Repository) PythonProjectInventory(files []string) PythonProjectInventory {
	canonical, err := repo.canonicalPythonProjectInventoryFiles(files)
	if err == nil && repo.pythonProjectCache != nil && slices.Equal(canonical, repo.pythonProjectCache.files) {
		repo.pythonProjectCache.hits.Add(1)
		return clonePythonProjectInventory(repo.pythonProjectCache.inventory)
	}
	if repo.pythonProjectCache != nil {
		repo.pythonProjectCache.misses.Add(1)
	}
	repo.pythonProjectCache = nil
	return repo.pythonProjectInventory(files)
}

func (repo Repository) PythonProjectInventoryCacheStats() (int64, int64, int64) {
	if repo.pythonProjectCache == nil {
		return 0, 0, 0
	}
	return repo.pythonProjectCache.hits.Load(), repo.pythonProjectCache.misses.Load(), repo.pythonProjectCache.builds.Load()
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
	clone.SourceRoots = slices.Clone(project.SourceRoots)
	clone.BackendPaths = slices.Clone(project.BackendPaths)
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
	paths := sortedPythonManifestPaths(manifests)
	results := repo.readPythonProjects(paths)
	for index, manifest := range paths {
		result := results[index]
		project, problem := repo.pythonInventoryProject(manifest, result.project, result.err)
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
		if problem := pythonProjectModuleLayoutProblem(*project); problem != nil {
			inventory.Problems = append(inventory.Problems, *problem)
		}
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

func (repo Repository) pythonInventoryProject(manifest string, project PythonProject, err error) (PythonProject, *PythonInventoryProblem) {
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
	if problem := repo.pythonProjectSourceRoots(manifest, &project); problem != nil {
		return PythonProject{}, problem
	}
	return project, nil
}

func (repo Repository) readPythonProjects(manifests []string) []pythonProjectReadResult {
	results := make([]pythonProjectReadResult, len(manifests))
	inputs := make([]pythonfacts.Input, 0, len(manifests))
	indexes := make([]int, 0, len(manifests))
	for index, manifest := range manifests {
		data, err := repo.Read(manifest)
		if err != nil {
			results[index].err = err
			continue
		}
		inputs = append(inputs, pythonfacts.Input{Path: manifest, Source: string(data)})
		indexes = append(indexes, index)
	}
	parsed, failures, err := parsePythonManifestsWith(repo.PythonTool(), inputs)
	for inputIndex, resultIndex := range indexes {
		if err != nil {
			results[resultIndex].err = fmt.Errorf("parse %s: %w", manifests[resultIndex], err)
			continue
		}
		if failures[inputIndex] != nil {
			results[resultIndex].err = failures[inputIndex]
			continue
		}
		results[resultIndex].project, results[resultIndex].err = pythonProjectFromParsed(manifests[resultIndex], parsed[inputIndex])
	}
	if cache := repo.pythonProjectCache; cache != nil && cache.building {
		for index, manifest := range manifests {
			result := results[index]
			cache.manifestReads[manifest] = pythonProjectReadResult{project: clonePythonProject(result.project), err: result.err}
		}
	}
	return results
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
	}
	return nil
}

func (repo Repository) pythonProjectSourceRoots(manifest string, project *PythonProject) *PythonInventoryProblem {
	roots := []string{project.Root}
	for _, declared := range project.BackendPaths {
		directory := pythonProjectContainedPath(project.Root, declared.Path)
		found, problem := repo.pythonProjectSourceDirectory(manifest, directory, true)
		if problem != nil {
			return problem
		}
		if found {
			roots = append(roots, directory)
		}
	}
	for _, root := range slices.Clone(roots) {
		directory := pythonProjectContainedPath(root, "src")
		found, problem := repo.pythonProjectSourceDirectory(manifest, directory, false)
		if problem != nil {
			return problem
		}
		if found {
			roots = append(roots, directory)
		}
	}
	project.SourceRoots = uniqueSorted(roots)
	return nil
}

func pythonProjectContainedPath(root, relative string) string {
	if root == "." {
		return relative
	}
	if relative == "." {
		return root
	}
	return root + "/" + relative
}

func (repo Repository) pythonProjectSourceDirectory(manifest, directory string, required bool) (bool, *PythonInventoryProblem) {
	abs := filepath.Join(repo.Root, filepath.FromSlash(directory))
	info, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) && !required {
		return false, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		problem := pythonInventoryProblem(PythonUnsupportedLayoutProblem, manifest, directory, "build-system.backend-path directory does not exist")
		return false, &problem
	}
	if err != nil {
		problem := pythonInventoryProblem(PythonUnreadableInputProblem, manifest, directory, err.Error())
		return false, &problem
	}
	if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		problem := pythonInventoryProblem(PythonUnsupportedLayoutProblem, manifest, directory, "Python source root is not a directory")
		return false, &problem
	}
	resolved, resolveErr := repo.Resolve(directory)
	if resolveErr != nil {
		problem := pythonInventoryProblem(PythonEscapingPathProblem, manifest, directory, resolveErr.Error())
		return false, &problem
	}
	resolvedInfo, statErr := os.Stat(resolved)
	if statErr != nil {
		problem := pythonInventoryProblem(PythonUnreadableInputProblem, manifest, directory, statErr.Error())
		return false, &problem
	}
	if !resolvedInfo.IsDir() {
		problem := pythonInventoryProblem(PythonUnsupportedLayoutProblem, manifest, directory, "Python source root is not a directory")
		return false, &problem
	}
	return true, nil
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
	base := project.Root
	for _, root := range project.SourceRoots {
		if pythonPathContains(root, source) && len(root) > len(base) {
			base = root
		}
	}
	return base
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
	project, err := parsePythonProjectWith(repo.PythonTool(), normalized, data)
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
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return PythonProject{}, err
	}
	return parsePythonProjectWith(python, manifest, data)
}

func parsePythonProjectWith(python, manifest string, data []byte) (PythonProject, error) {
	parsed, err := parsePythonManifestWith(python, manifest, data)
	if err != nil {
		return PythonProject{}, err
	}
	return pythonProjectFromParsed(manifest, parsed)
}

func pythonProjectFromParsed(manifest string, parsed pythonParsedManifest) (PythonProject, error) {
	project, err := pythonProjectInventoryFromParsed(manifest, parsed)
	if err != nil {
		return PythonProject{}, err
	}
	for _, requirement := range project.Requirements {
		if requirement.Kind == PythonGitRequirement {
			if err := requirement.Git.ValidateExactPin(); err != nil {
				return PythonProject{}, pythonManifestError(manifest, requirement.Location.Line, "dependency %q: %v", requirement.Name, err)
			}
		}
	}
	return project, nil
}

func ParsePythonProjectInventory(manifest string, data []byte) (PythonProject, error) {
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return PythonProject{}, err
	}
	parsed, err := parsePythonManifestWith(python, manifest, data)
	if err != nil {
		return PythonProject{}, err
	}
	return pythonProjectInventoryFromParsed(manifest, parsed)
}

func pythonProjectInventoryFromParsed(manifest string, parsed pythonParsedManifest) (PythonProject, error) {
	var err error
	project := pythonProjectDefinition(manifest, parsed.document.tables, parsed.document.assignments)
	project.BuildBackend, project.BackendPaths, err = pythonProjectBuildMetadata(manifest, parsed.document.assignments)
	if err != nil {
		return PythonProject{}, err
	}
	project.Ruff, project.RuffProblems = pythonProjectRuffMetadata(manifest, project.IsPythonProject(), parsed.document.assignments, parsed.specifiers)
	dynamicReferences, err := pythonProjectDynamicReferences(manifest, parsed.document.assignments)
	if err != nil {
		return PythonProject{}, err
	}
	project.DynamicReferences = dynamicReferences
	requirements, err := pythonProjectRequirements(manifest, parsed.document.assignments, parsed.requirements)
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

func pythonProjectRequirements(manifest string, assignments []pythonTOMLAssignment, facts map[string]pythonfacts.Requirement) ([]PythonRequirement, error) {
	requirements := []PythonRequirement{}
	for _, assignment := range assignments {
		usage, group, dependency := pythonDependencyAssignment(assignment)
		if !dependency {
			continue
		}
		values, err := pythonAssignmentRequirements(manifest, assignment, usage, group, facts)
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, values...)
	}
	return requirements, nil
}

func pythonAssignmentRequirements(manifest string, assignment pythonTOMLAssignment, usage, group string, facts map[string]pythonfacts.Requirement) ([]PythonRequirement, error) {
	if assignment.value.kind != pythonTOMLArray {
		return nil, pythonManifestError(manifest, assignment.line, "%s must be an array of requirement strings", assignment.key)
	}
	requirements := make([]PythonRequirement, 0, len(assignment.value.array))
	for element, value := range assignment.value.array {
		requirement, err := pythonAssignmentRequirement(manifest, assignment, usage, group, element, value, facts)
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, requirement)
	}
	return requirements, nil
}

func pythonAssignmentRequirement(manifest string, assignment pythonTOMLAssignment, usage, group string, element int, value pythonTOMLValue, facts map[string]pythonfacts.Requirement) (PythonRequirement, error) {
	if value.kind != pythonTOMLString {
		return PythonRequirement{}, pythonManifestError(manifest, value.line, "%s must contain only requirement strings", assignment.key)
	}
	fact, found := facts[value.text]
	if !found {
		return PythonRequirement{}, pythonManifestError(manifest, value.line, "python-facts omitted requirement %q", value.text)
	}
	requirement, err := pythonRequirementInventoryFact(fact)
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
	return left.markerVariable == "" || right.markerVariable == "" ||
		left.markerVariable != right.markerVariable || left.markerValue == right.markerValue
}

func pythonManifestError(manifest string, line int, format string, arguments ...any) error {
	message := fmt.Sprintf(format, arguments...)
	if line <= 0 {
		return fmt.Errorf("%s: %s", manifest, message)
	}
	return fmt.Errorf("%s line %d: %s", manifest, line, message)
}
