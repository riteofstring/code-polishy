package repository

import "strings"

type PythonRequirementKind string

const (
	PythonRegistryRequirement PythonRequirementKind = "registry"
	PythonGitRequirement      PythonRequirementKind = "git"
	PythonFileRequirement     PythonRequirementKind = "file"
	PythonURLRequirement      PythonRequirementKind = "url"
)

type PythonRequirementLocation struct {
	Path    string
	Table   string
	Group   string
	Line    int
	Element int
}

type PythonVersionSpecifier struct {
	Operator string
	Version  string
}

type PythonDynamicReference struct {
	Module string
	Symbol string
	Table  string
	Name   string
	Line   int
}

type PythonGitSource struct {
	Scheme       string
	Host         string
	Path         string
	User         string
	DeclaredRef  string
	Commit       string
	Subdirectory string
}

func (source PythonGitSource) RepositoryIdentity() string {
	identity := "git+" + source.Scheme + "://"
	if source.User != "" {
		identity += source.User + "@"
	}
	return identity + source.Host + source.Path
}

func (source PythonGitSource) Identity() string {
	identity := source.RepositoryIdentity() + "@" + source.Commit
	if source.Subdirectory != "" {
		identity += "#subdirectory=" + source.Subdirectory
	}
	return identity
}

func (source PythonGitSource) SameRepository(other PythonGitSource) bool {
	return source.Scheme == other.Scheme && source.Host == other.Host && source.Path == other.Path
}

type PythonRequirement struct {
	Raw        string
	Name       string
	Extras     []string
	Marker     string
	Kind       PythonRequirementKind
	Version    string
	Specifiers []PythonVersionSpecifier
	URL        string
	FilePath   string
	Git        PythonGitSource
	Usage      string
	Location   PythonRequirementLocation
	markerKey  string
}

func (requirement PythonRequirement) ExactRegistryVersion() (string, bool) {
	if requirement.Kind != PythonRegistryRequirement || len(requirement.Specifiers) != 1 {
		return "", false
	}
	specifier := requirement.Specifiers[0]
	if specifier.Operator != "==" || strings.Contains(specifier.Version, "*") || !validPythonVersion(specifier.Version) {
		return "", false
	}
	return specifier.Version, true
}

func (requirement PythonRequirement) SourceIdentity() string {
	switch requirement.Kind {
	case PythonRegistryRequirement:
		return requirement.Version
	case PythonGitRequirement:
		return requirement.Git.Identity()
	case PythonFileRequirement:
		return "file:" + requirement.FilePath
	default:
		return requirement.URL
	}
}
