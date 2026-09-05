package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

type PythonPluginDependency struct {
	Distribution string `json:"distribution"`
	Version      string `json:"version"`
	Kind         string `json:"kind"`
	Source       string `json:"source"`
	Error        string `json:"error"`
}

type PythonPluginDependencyFacts struct {
	Project        string                   `json:"project"`
	Lock           string                   `json:"lock"`
	ManifestSHA256 string                   `json:"manifestSha256"`
	LockSHA256     string                   `json:"lockSha256"`
	Dependencies   []PythonPluginDependency `json:"dependencies"`
}

func (repo Repository) PythonPluginDependencies(project PythonProject, distributions []string) (PythonPluginDependencyFacts, error) {
	if path.Base(project.Manifest) != "pyproject.toml" || path.Dir(project.Manifest) != project.Root {
		return PythonPluginDependencyFacts{}, fmt.Errorf("external plug-in project does not match its manifest")
	}
	manifestData, err := repo.pythonPluginDependencyInput(project.Manifest)
	if err != nil {
		return PythonPluginDependencyFacts{}, err
	}
	manifest, err := ParsePythonProjectInventory(project.Manifest, manifestData)
	if err != nil {
		return PythonPluginDependencyFacts{}, err
	}
	lockPath := path.Join(project.Root, "uv.lock")
	lockData, err := repo.pythonPluginDependencyInput(lockPath)
	if err != nil {
		return PythonPluginDependencyFacts{}, err
	}
	lock, err := ParsePythonUVLock(lockPath, lockData)
	if err != nil {
		return PythonPluginDependencyFacts{}, err
	}
	if lock.Version != 1 {
		return PythonPluginDependencyFacts{}, fmt.Errorf("external plug-in requires a supported version 1 uv.lock")
	}
	distributions = slices.Clone(distributions)
	slices.Sort(distributions)
	distributions = slices.Compact(distributions)
	versions, err := pythonPluginVersionFacts(manifest.Requirements, lock.Packages, distributions)
	if err != nil {
		return PythonPluginDependencyFacts{}, err
	}
	manifestSHA, lockSHA := sha256.Sum256(manifestData), sha256.Sum256(lockData)
	result := PythonPluginDependencyFacts{Project: project.Manifest, Lock: lockPath, ManifestSHA256: hex.EncodeToString(manifestSHA[:]), LockSHA256: hex.EncodeToString(lockSHA[:]), Dependencies: []PythonPluginDependency{}}
	for _, distribution := range distributions {
		dependency, err := pythonPluginDependency(manifest.Requirements, lock.Packages, distribution, versions)
		if err != nil {
			dependency = PythonPluginDependency{Distribution: distribution, Error: err.Error()}
		}
		result.Dependencies = append(result.Dependencies, dependency)
	}
	return result, nil
}

func (repo Repository) pythonPluginDependencyInput(input string) ([]byte, error) {
	if normalized, err := repo.NormalizePath(input); err != nil || normalized != input || repo.IsExcluded(input) {
		return nil, fmt.Errorf("plug-in dependency input must be a contained governed manifest or lock: %s", input)
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := repo.containedRegularFileInfo(root, input)
	if err != nil {
		return nil, err
	}
	return readContainedFile(root, input, info, 4<<20)
}

func pythonPluginVersionFacts(requirements []PythonRequirement, packages []PythonLockPackage, distributions []string) (map[string]string, error) {
	selected := map[string]bool{}
	for _, distribution := range distributions {
		selected[distribution] = true
	}
	versions := []string{}
	for _, requirement := range requirements {
		if version, exact := requirement.ExactRegistryVersion(); exact && selected[requirement.Name] {
			versions = append(versions, version)
		}
	}
	for _, candidate := range packages {
		if selected[candidate.Name] {
			versions = append(versions, candidate.Version)
		}
	}
	slices.Sort(versions)
	versions = slices.Compact(versions)
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		return nil, err
	}
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Versions: versions})
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, value := range response.Versions {
		if value.Error == "" {
			result[value.Input] = value.Normalized
		}
	}
	return result, nil
}

func pythonPluginDependency(requirements []PythonRequirement, packages []PythonLockPackage, distribution string, versions map[string]string) (PythonPluginDependency, error) {
	requirement, err := pythonPluginDirectRequirement(requirements, distribution, versions)
	if err != nil {
		return PythonPluginDependency{}, err
	}
	candidates := []PythonLockPackage{}
	for _, candidate := range packages {
		if candidate.Name == distribution {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) != 1 {
		return PythonPluginDependency{}, fmt.Errorf("external plug-in distribution must resolve to one authoritative lock package")
	}
	candidate := candidates[0]
	if versions[candidate.Version] == "" {
		return PythonPluginDependency{}, fmt.Errorf("external plug-in lock package has no valid PEP 440 version")
	}
	source, err := pythonPluginLockedSource(requirement, candidate, versions)
	if err != nil {
		return PythonPluginDependency{}, err
	}
	return PythonPluginDependency{Distribution: distribution, Version: candidate.Version, Kind: string(requirement.Kind), Source: source}, nil
}

func pythonPluginDirectRequirement(requirements []PythonRequirement, distribution string, versions map[string]string) (PythonRequirement, error) {
	var selected PythonRequirement
	identity := ""
	for _, requirement := range requirements {
		if requirement.Name != distribution || requirement.Usage != "runtime" && requirement.Usage != "optional" {
			continue
		}
		current, err := pythonPluginRequirementIdentity(requirement, versions)
		if err != nil {
			return PythonRequirement{}, err
		}
		if identity != "" && identity != current {
			return PythonRequirement{}, fmt.Errorf("external plug-in direct requirement has ambiguous marker or optional sources")
		}
		identity, selected = current, requirement
	}
	if identity == "" {
		return PythonRequirement{}, fmt.Errorf("external plug-in distribution is not a declared direct runtime or optional dependency")
	}
	return selected, nil
}

func pythonPluginRequirementIdentity(requirement PythonRequirement, versions map[string]string) (string, error) {
	switch requirement.Kind {
	case PythonRegistryRequirement:
		version, exact := requirement.ExactRegistryVersion()
		if !exact || versions[version] == "" {
			return "", fmt.Errorf("external plug-in registry dependency requires one exact PEP 440 pin")
		}
		return "registry:" + versions[version], nil
	case PythonGitRequirement:
		if err := requirement.Git.ValidateExactPin(); err != nil {
			return "", err
		}
		return requirement.Git.Identity(), nil
	default:
		return "", fmt.Errorf("external plug-in dependency must use an admitted registry or exact Git source")
	}
}

func pythonPluginLockedSource(requirement PythonRequirement, candidate PythonLockPackage, versions map[string]string) (string, error) {
	if requirement.Kind == PythonRegistryRequirement {
		version, _ := requirement.ExactRegistryVersion()
		if len(candidate.Source) != 1 || candidate.Source[0].Name != "registry" || candidate.Source[0].Value == "" || versions[version] != versions[candidate.Version] {
			return "", fmt.Errorf("external plug-in registry pin does not match its authoritative lock source and version")
		}
		return pythonPluginRegistrySource(candidate.Source[0].Value)
	}
	git, err := pythonPluginLockedGit(candidate.Source)
	if err != nil {
		return "", err
	}
	if !git.SameRepository(requirement.Git) || git.Commit != requirement.Git.Commit || git.Subdirectory != requirement.Git.Subdirectory {
		return "", fmt.Errorf("external plug-in Git lock does not match the direct repository, commit, and subdirectory")
	}
	return git.Identity(), nil
}

func pythonPluginLockedGit(source []PythonLockSourceField) (PythonGitSource, error) {
	fields := map[string]string{}
	for _, field := range source {
		if _, duplicate := fields[field.Name]; duplicate || !slices.Contains([]string{"git", "rev", "commit", "subdirectory"}, field.Name) {
			return PythonGitSource{}, fmt.Errorf("external plug-in Git lock has duplicate or unsupported source fields")
		}
		fields[field.Name] = field.Value
	}
	if fields["git"] == "" || fields["commit"] != "" && fields["rev"] != "" && !strings.EqualFold(fields["commit"], fields["rev"]) {
		return PythonGitSource{}, fmt.Errorf("external plug-in Git lock is missing or ambiguous")
	}
	commit := fields["commit"]
	if commit == "" {
		commit = fields["rev"]
	}
	return ParsePythonGitSource(fields["git"], commit, fields["subdirectory"])
}

func pythonPluginRegistrySource(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("external plug-in registry must be a stable URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("external plug-in registry must name an HTTP or HTTPS package index")
	}
	return value, nil
}
