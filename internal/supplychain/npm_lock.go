package supplychain

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const npmPublicRegistry = "https://registry.npmjs.org"

func packageLockSource(installed, name string, entry modernPackageLockEntry) (resolvedSource, error) {
	if entry.Resolved == "" && name == installed {
		return resolvedSource{Kind: "registry", Registry: npmPublicRegistry}, nil
	}
	if err := validateDependencyDeclaration(entry.Resolved); err != nil {
		return resolvedSource{}, err
	}
	if relative, local := strings.CutPrefix(entry.Resolved, "file:"); local {
		return resolvedSource{Kind: "local", LocalPath: relative}, nil
	}
	artifact, err := url.Parse(entry.Resolved)
	if err != nil || artifact.Host == "" || artifact.Scheme == "" {
		return resolvedSource{}, fmt.Errorf("resolved npm source must be an absolute URL")
	}
	if strings.HasPrefix(artifact.Scheme, "git+") {
		return packageLockGitSource(artifact)
	}
	return resolvedSource{Kind: "registry", ArtifactURL: entry.Resolved}, nil
}

func packageLockGitSource(artifact *url.URL) (resolvedSource, error) {
	if artifact.Fragment == "" {
		return resolvedSource{}, fmt.Errorf("resolved npm Git source must identify a revision")
	}
	git := repository.PythonGitSource{
		Scheme: strings.TrimPrefix(artifact.Scheme, "git+"), Host: strings.ToLower(artifact.Host),
		Path: artifact.Path, DeclaredRef: artifact.Fragment,
	}
	if artifact.User != nil {
		git.User = artifact.User.Username()
	}
	if fullCommit.MatchString(artifact.Fragment) {
		git.Commit = strings.ToLower(artifact.Fragment)
		git.DeclaredRef = git.Commit
	}
	return resolvedSource{Kind: "git", Git: git}, nil
}

func validateNPMResolvedSource(repo repository.Repository, item resolvedPackage) error {
	switch item.Source.Kind {
	case "registry":
		if item.Source.ArtifactURL != "" {
			artifact, err := url.Parse(item.Source.ArtifactURL)
			if err != nil || (artifact.Scheme != "https" && artifact.Scheme != "http") || artifact.Fragment != "" {
				return fmt.Errorf("npm admission requires an HTTP or HTTPS artifact URL without a fragment")
			}
		}
	case "git":
		return item.Source.Git.ValidateExactPin()
	case "local":
		return validateRelativeFromManifest(repo, item.Scope, item.Source.LocalPath)
	}
	return nil
}

type npmReleaseArtifact struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Dist    struct {
		Tarball string `json:"tarball"`
	} `json:"dist"`
}

func validateNPMReleaseArtifact(item resolvedPackage, release npmReleaseArtifact) error {
	if release.Name != item.Name || release.Version != item.Version || !npmArtifactURLsEqual(item.Source.ArtifactURL, release.Dist.Tarball) {
		return fmt.Errorf("npm admission at %s (%s): configured registry metadata for %s@%s does not match the locked package identity and artifact URL", item.Scope, strings.Join(item.Locations, ", "), item.Name, item.Version)
	}
	return nil
}

func npmArtifactURLsEqual(observed, declared string) bool {
	left, leftErr := url.Parse(observed)
	right, rightErr := url.Parse(declared)
	if leftErr != nil || rightErr != nil || right.Host == "" {
		return false
	}
	left.Host, right.Host = strings.ToLower(left.Host), strings.ToLower(right.Host)
	return left.String() == right.String()
}
