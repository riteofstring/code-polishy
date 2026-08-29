package supplychain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const artifactMetadataMaximumBytes = 16 << 20

type artifactHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type releaseArtifactObservation struct {
	Artifact policy.ReleaseArtifact
	Version  string
	Released time.Time
	Err      error
}

func releaseArtifactAgeFindings(ctx context.Context, repo repository.Repository, client artifactHTTPClient, now time.Time) []policy.Finding {
	cutoff := now.AddDate(0, 0, -repo.Config.SupplyChain.MinimumReleaseAgeDays)
	findings := []policy.Finding{}
	for _, observation := range observeReleaseArtifacts(ctx, repo, client) {
		artifact := observation.Artifact
		subject := artifact.Name
		if observation.Version != "" {
			subject += "@" + observation.Version
		}
		if observation.Err != nil {
			findings = append(findings, policy.Finding{
				Check: "supplyChain.releaseAge", Path: artifact.VersionFile, Subject: subject,
				Message: "resolve standalone artifact release: " + observation.Err.Error(),
			})
			continue
		}
		if !observation.Released.After(cutoff) {
			continue
		}
		identity := policy.ReleaseAgeIdentity{
			Ecosystem: "artifact", Package: artifact.Name, Version: observation.Version, Scope: artifact.VersionFile,
			Released: observation.Released, Eligible: observation.Released.AddDate(0, 0, repo.Config.SupplyChain.MinimumReleaseAgeDays),
		}
		message := fmt.Sprintf(
			"artifact was released %s and has not reached the %d-day hard minimum (eligible %s)",
			observation.Released.Format(time.RFC3339), repo.Config.SupplyChain.MinimumReleaseAgeDays,
			identity.Eligible.Format(time.RFC3339),
		)
		findings = append(findings, policy.Finding{
			Check: "supplyChain.releaseAge", Path: artifact.VersionFile, Subject: policy.ReleaseAgeSubject(identity),
			Message: message, ReleaseAge: &identity,
		})
	}
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Path+"\x00"+findings[left].Subject < findings[right].Path+"\x00"+findings[right].Subject
	})
	return findings
}

func observeReleaseArtifacts(ctx context.Context, repo repository.Repository, client artifactHTTPClient) []releaseArtifactObservation {
	artifacts := repo.Config.SupplyChain.ReleaseArtifacts
	if len(artifacts) == 0 {
		return nil
	}
	workers := 8
	if len(artifacts) < workers {
		workers = len(artifacts)
	}
	tasks := make(chan policy.ReleaseArtifact)
	results := make(chan releaseArtifactObservation, len(artifacts))
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for artifact := range tasks {
				results <- observeReleaseArtifact(ctx, repo, client, artifact)
			}
		}()
	}
	go func() {
		for _, artifact := range artifacts {
			tasks <- artifact
		}
		close(tasks)
		wait.Wait()
		close(results)
	}()
	observations := make([]releaseArtifactObservation, 0, len(artifacts))
	for observation := range results {
		observations = append(observations, observation)
	}
	sort.Slice(observations, func(left, right int) bool {
		return observations[left].Artifact.VersionFile+"\x00"+observations[left].Artifact.Name <
			observations[right].Artifact.VersionFile+"\x00"+observations[right].Artifact.Name
	})
	return observations
}

func observeReleaseArtifact(ctx context.Context, repo repository.Repository, client artifactHTTPClient, artifact policy.ReleaseArtifact) releaseArtifactObservation {
	observation := releaseArtifactObservation{Artifact: artifact}
	version, err := readReleaseArtifactVersion(repo, artifact)
	observation.Version = version
	if err != nil {
		observation.Err = err
		return observation
	}
	observation.Released, observation.Err = lookupReleaseArtifact(ctx, client, repo, artifact, version)
	return observation
}

func readReleaseArtifactVersion(repo repository.Repository, artifact policy.ReleaseArtifact) (string, error) {
	data, err := repo.Read(artifact.VersionFile)
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > 256 {
		return "", fmt.Errorf("version pin must contain one bounded exact version")
	}
	contents := string(data)
	version := contents
	if strings.HasSuffix(version, "\n") {
		version = strings.TrimSuffix(version, "\n")
		version = strings.TrimSuffix(version, "\r")
	}
	if version == "" || version != strings.TrimSpace(version) || strings.ContainsAny(version, "\r\n") {
		return "", fmt.Errorf("version pin must contain one exact version with an optional trailing newline")
	}
	if !policy.ExactArtifactVersion(version) {
		return "", fmt.Errorf("version pin %q is not an exact semantic version", version)
	}
	if err := validateReleaseArtifactVersion(artifact.Source, version); err != nil {
		return "", err
	}
	return version, nil
}

func validateReleaseArtifactVersion(source, version string) error {
	hasV := strings.HasPrefix(version, "v")
	switch source {
	case "go-module":
		if !hasV {
			return fmt.Errorf("go-module version %q must include its leading v", version)
		}
	case "go-toolchain", "node-runtime", "npm":
		if hasV {
			return fmt.Errorf("%s version %q must not include a leading v", source, version)
		}
	}
	return nil
}

func lookupReleaseArtifact(ctx context.Context, client artifactHTTPClient, repo repository.Repository, artifact policy.ReleaseArtifact, version string) (time.Time, error) {
	switch artifact.Source {
	case "github-release":
		return lookupGitHubRelease(ctx, client, artifact.Locator, artifact.TagPrefix+version)
	case "go-module":
		return lookupGoModuleRelease(ctx, client, artifact.Locator, version)
	case "go-toolchain":
		return lookupGoToolchainRelease(ctx, client, version)
	case "node-runtime":
		return lookupNodeRuntimeRelease(ctx, client, version)
	case "npm":
		return lookupNPMArtifactRelease(ctx, client, repo.Config.SupplyChain.NPMRegistryURL, artifact.Locator, version)
	default:
		return time.Time{}, fmt.Errorf("unsupported metadata source %q", artifact.Source)
	}
}

func lookupGitHubRelease(ctx context.Context, client artifactHTTPClient, repositoryName, tag string) (time.Time, error) {
	owner, name, _ := strings.Cut(repositoryName, "/")
	endpoint := "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/releases/tags/" + url.PathEscape(tag)
	var payload struct {
		TagName     string    `json:"tag_name"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := fetchArtifactMetadata(ctx, client, endpoint, &payload); err != nil {
		return time.Time{}, err
	}
	if payload.TagName != tag || payload.PublishedAt.IsZero() {
		return time.Time{}, fmt.Errorf("GitHub release metadata did not identify tag %s with a publication timestamp", tag)
	}
	return payload.PublishedAt, nil
}

func lookupGoModuleRelease(ctx context.Context, client artifactHTTPClient, module, version string) (time.Time, error) {
	endpoint := "https://proxy.golang.org/" + goProxyEscapePath(module) + "/@v/" + goProxyEscapeSegment(version) + ".info"
	var payload struct {
		Version string    `json:"Version"`
		Time    time.Time `json:"Time"`
	}
	if err := fetchArtifactMetadata(ctx, client, endpoint, &payload); err != nil {
		return time.Time{}, err
	}
	if payload.Version != version || payload.Time.IsZero() {
		return time.Time{}, fmt.Errorf("go module metadata did not identify %s@%s with a release timestamp", module, version)
	}
	return payload.Time, nil
}

func lookupGoToolchainRelease(ctx context.Context, client artifactHTTPClient, version string) (time.Time, error) {
	tag := "go" + version
	endpoint := "https://api.github.com/repos/golang/go/commits/" + url.PathEscape(tag)
	var payload struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := fetchArtifactMetadata(ctx, client, endpoint, &payload); err != nil {
		return time.Time{}, err
	}
	firstLine, _, _ := strings.Cut(payload.Commit.Message, "\n")
	fields := strings.Fields(firstLine)
	if !fullCommit.MatchString(payload.SHA) || payload.Commit.Committer.Date.IsZero() || len(fields) == 0 || fields[len(fields)-1] != tag {
		return time.Time{}, fmt.Errorf("go release tag %s did not resolve to its exact release commit", tag)
	}
	return payload.Commit.Committer.Date, nil
}

func lookupNodeRuntimeRelease(ctx context.Context, client artifactHTTPClient, version string) (time.Time, error) {
	var payload []struct {
		Version string `json:"version"`
		Date    string `json:"date"`
	}
	if err := fetchArtifactMetadata(ctx, client, "https://nodejs.org/dist/index.json", &payload); err != nil {
		return time.Time{}, err
	}
	wanted := "v" + version
	for _, release := range payload {
		if release.Version != wanted {
			continue
		}
		date, err := time.Parse("2006-01-02", release.Date)
		if err != nil {
			return time.Time{}, fmt.Errorf("node release %s has an invalid release date", wanted)
		}

		return date.AddDate(0, 0, 1), nil
	}
	return time.Time{}, fmt.Errorf("node release metadata omitted %s", wanted)
}

func lookupNPMArtifactRelease(ctx context.Context, client artifactHTTPClient, registryURL, packageName, version string) (time.Time, error) {
	endpoint := strings.TrimSuffix(registryURL, "/") + "/" + url.PathEscape(packageName)
	var payload struct {
		Time map[string]string `json:"time"`
	}
	if err := fetchArtifactMetadata(ctx, client, endpoint, &payload); err != nil {
		return time.Time{}, err
	}
	value := payload.Time[version]
	released, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("npm metadata omitted the exact timestamp for %s@%s", packageName, version)
	}
	return released, nil
}

func fetchArtifactMetadata(ctx context.Context, client artifactHTTPClient, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "code-polishy-release-age")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("release metadata request failed with HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, artifactMetadataMaximumBytes+1))
	if err != nil {
		return err
	}
	if len(data) > artifactMetadataMaximumBytes {
		return fmt.Errorf("release metadata exceeds %d bytes", artifactMetadataMaximumBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return requireOneJSONValue(decoder)
}

func goProxyEscapePath(value string) string {
	segments := strings.Split(value, "/")
	for index, segment := range segments {
		segments[index] = goProxyEscapeSegment(segment)
	}
	return strings.Join(segments, "/")
}

func goProxyEscapeSegment(value string) string {
	escaped := strings.Builder{}
	for _, character := range value {
		if character >= 'A' && character <= 'Z' {
			escaped.WriteByte('!')
			escaped.WriteRune(character + ('a' - 'A'))
			continue
		}
		escaped.WriteRune(character)
	}
	return url.PathEscape(escaped.String())
}
