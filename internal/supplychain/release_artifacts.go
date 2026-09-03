package supplychain

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
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

const (
	artifactMetadataMaximumBytes      = 16 << 20
	artifactMetadataErrorMaximumBytes = 4 << 10
	githubReleaseFeedMaximumPages     = 32
	githubReleaseFeedIDPrefix         = "tag:github.com,2008:Repository/"
	goReleaseHistoryEndpoint          = "https://go.dev/doc/devel/release"
)

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
	if !policy.ExactReleaseArtifactVersion(artifact.Source, version) {
		return "", fmt.Errorf("version pin %q is not exact for source %s", version, artifact.Source)
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
	case policy.ReleaseArtifactSourcePyPI:
		return lookupPyPIArtifactRelease(ctx, client, artifact.Locator, version)
	case policy.ReleaseArtifactSourcePythonBuildStandalone:
		_, tag, found := strings.Cut(version, "+")
		if !found || tag == "" || !policy.ExactReleaseArtifactVersion(artifact.Source, version) {
			return time.Time{}, fmt.Errorf("python-build-standalone version %q must include one release tag", version)
		}
		return lookupGitHubRelease(ctx, client, "astral-sh/python-build-standalone", tag)
	default:
		return time.Time{}, fmt.Errorf("unsupported metadata source %q", artifact.Source)
	}
}

func lookupGitHubRelease(ctx context.Context, client artifactHTTPClient, repositoryName, tag string) (time.Time, error) {
	owner, name, _ := strings.Cut(repositoryName, "/")
	baseEndpoint := "https://github.com/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/releases.atom"
	cursor := ""
	for range githubReleaseFeedMaximumPages {
		endpoint := baseEndpoint
		if cursor != "" {
			endpoint += "?after=" + url.QueryEscape(cursor)
		}
		var feed githubReleaseFeed
		if err := fetchArtifactFeed(ctx, client, endpoint, &feed); err != nil {
			return time.Time{}, err
		}
		if len(feed.Entries) == 0 {
			break
		}
		for _, entry := range feed.Entries {
			observed, err := githubReleaseFeedTag(entry.ID)
			if err != nil || entry.Updated.IsZero() {
				return time.Time{}, fmt.Errorf("GitHub release feed contained malformed release metadata")
			}
			if observed == tag {
				return entry.Updated, nil
			}
		}
		next, err := githubReleaseFeedTag(feed.Entries[len(feed.Entries)-1].ID)
		if err != nil || next == cursor {
			return time.Time{}, fmt.Errorf("GitHub release feed pagination did not advance")
		}
		cursor = next
	}
	return time.Time{}, fmt.Errorf("GitHub release feed omitted tag %s", tag)
}

type githubReleaseFeed struct {
	Entries []githubReleaseFeedEntry `xml:"entry"`
}

type githubReleaseFeedEntry struct {
	ID      string    `xml:"id"`
	Updated time.Time `xml:"updated"`
}

func githubReleaseFeedTag(id string) (string, error) {
	remainder, found := strings.CutPrefix(id, githubReleaseFeedIDPrefix)
	if !found {
		return "", fmt.Errorf("unrecognized GitHub release feed identity")
	}
	repositoryID, tag, found := strings.Cut(remainder, "/")
	if !found || repositoryID == "" || tag == "" || tag != strings.TrimSpace(tag) {
		return "", fmt.Errorf("malformed GitHub release feed identity")
	}
	for _, character := range repositoryID {
		if character < '0' || character > '9' {
			return "", fmt.Errorf("malformed GitHub release feed repository identity")
		}
	}
	return tag, nil
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
	data, err := fetchArtifactDocument(ctx, client, goReleaseHistoryEndpoint, "text/html")
	if err != nil {
		return time.Time{}, err
	}
	marker := []byte(`<p id="go` + version + `">`)
	if bytes.Count(data, marker) != 1 {
		return time.Time{}, fmt.Errorf("go release history did not identify go%s exactly once", version)
	}
	section := data[bytes.Index(data, marker)+len(marker):]
	end := bytes.Index(section, []byte("</p>"))
	if end < 0 {
		return time.Time{}, fmt.Errorf("go release history entry for go%s was incomplete", version)
	}
	section = section[:end]
	releaseMarker := []byte("(released ")
	if bytes.Count(section, releaseMarker) != 1 {
		return time.Time{}, fmt.Errorf("go release history entry for go%s did not contain one release date", version)
	}
	remainder := section[bytes.Index(section, releaseMarker)+len(releaseMarker):]
	if len(remainder) < len("2006-01-02)") || remainder[len("2006-01-02")] != ')' {
		return time.Time{}, fmt.Errorf("go release history entry for go%s had a malformed release date", version)
	}
	released, err := time.Parse("2006-01-02", string(remainder[:len("2006-01-02")]))
	if err != nil {
		return time.Time{}, fmt.Errorf("go release history entry for go%s had a malformed release date", version)
	}
	return released.AddDate(0, 0, 1), nil
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

func lookupPyPIArtifactRelease(ctx context.Context, client artifactHTTPClient, packageName, version string) (time.Time, error) {
	endpoint := "https://pypi.org/pypi/" + url.PathEscape(packageName) + "/" + url.PathEscape(version) + "/json"
	var payload struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
		URLs []struct {
			UploadTime string `json:"upload_time_iso_8601"`
		} `json:"urls"`
	}
	if err := fetchArtifactMetadata(ctx, client, endpoint, &payload); err != nil {
		return time.Time{}, err
	}
	if payload.Info.Version != version || len(payload.URLs) == 0 {
		return time.Time{}, fmt.Errorf("PyPI metadata did not identify %s@%s", packageName, version)
	}
	return latestPyPIUpload(payload.URLs, packageName, version)
}

func latestPyPIUpload(urls []struct {
	UploadTime string `json:"upload_time_iso_8601"`
}, packageName, version string) (time.Time, error) {
	latest := time.Time{}
	for _, artifact := range urls {
		released, err := time.Parse(time.RFC3339, artifact.UploadTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("PyPI metadata has an invalid upload time for %s@%s", packageName, version)
		}
		if released.After(latest) {
			latest = released
		}
	}
	return latest, nil
}

func fetchArtifactMetadata(ctx context.Context, client artifactHTTPClient, endpoint string, target any) error {
	data, err := fetchArtifactDocument(ctx, client, endpoint, "application/json")
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return requireOneJSONValue(decoder)
}

func fetchArtifactFeed(ctx context.Context, client artifactHTTPClient, endpoint string, target any) error {
	data, err := fetchArtifactDocument(ctx, client, endpoint, "application/atom+xml")
	if err != nil {
		return err
	}
	return xml.Unmarshal(data, target)
}

func fetchArtifactDocument(ctx context.Context, client artifactHTTPClient, endpoint, accept string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "code-polishy-release-age")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, artifactMetadataStatusError(endpoint, response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, artifactMetadataMaximumBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > artifactMetadataMaximumBytes {
		return nil, fmt.Errorf("release metadata exceeds %d bytes", artifactMetadataMaximumBytes)
	}
	return data, nil
}

func artifactMetadataStatusError(endpoint string, response *http.Response) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" {
		return fmt.Errorf("release metadata request failed with HTTP %d", response.StatusCode)
	}
	details := []string{}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, artifactMetadataErrorMaximumBytes+1))
	if readErr == nil && len(data) <= artifactMetadataErrorMaximumBytes {
		var payload struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &payload) == nil && boundedPrintableHTTPDetail(payload.Message, 512) {
			details = append(details, fmt.Sprintf("GitHub message %q", payload.Message))
		}
	}
	for _, header := range []struct {
		Name  string
		Label string
	}{
		{Name: "X-RateLimit-Remaining", Label: "rate-limit remaining"},
		{Name: "Retry-After", Label: "retry after"},
	} {
		value := response.Header.Get(header.Name)
		if boundedPrintableHTTPDetail(value, 64) {
			details = append(details, header.Label+" "+value)
		}
	}
	if len(details) == 0 {
		return fmt.Errorf("release metadata request failed with HTTP %d", response.StatusCode)
	}
	return fmt.Errorf("release metadata request failed with HTTP %d (%s)", response.StatusCode, strings.Join(details, "; "))
}

func boundedPrintableHTTPDetail(value string, maximum int) bool {
	if value == "" || len(value) > maximum || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if character < ' ' || character > '~' {
			return false
		}
	}
	return true
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
