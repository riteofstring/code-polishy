package supplychain

import (
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

func checkResolvedNodeReleaseAge(ctx context.Context, repo repository.Repository, manifest nodeManifest) []policy.Finding {
	packages, err := resolvedNodePackages(ctx, repo, manifest)
	if err != nil {
		return []policy.Finding{{
			Check: "supplyChain.releaseAge", Path: manifest.Path, Subject: manifest.Manager + ":resolved-graph",
			Message: err.Error(),
		}}
	}
	owner := "package.json"
	if manifest.Root != "." {
		owner = manifest.Root + "/package.json"
	}
	if exactVersion.MatchString(manifest.ManagerVersion) {
		packages = append(packages, resolvedPackage{
			Ecosystem: manifest.Manager, Name: manifest.Manager,
			Version: manifest.ManagerVersion, Scope: owner,
		})
	}
	return releaseAgeFindings(ctx, repo, packages, time.Now().UTC())
}

func checkResolvedPythonReleaseAge(ctx context.Context, repo repository.Repository, projectPath string) []policy.Finding {
	root := filepathDirectory(projectPath)
	lockPath := "uv.lock"
	if root != "." {
		lockPath = root + "/uv.lock"
	}
	data, err := repo.Read(lockPath)
	if err != nil {
		return []policy.Finding{{
			Check: "supplyChain.releaseAge", Path: projectPath, Subject: "uv:resolved-graph",
			Message: err.Error(),
		}}
	}
	packages, err := parseUVLock(data, lockPath)
	if err != nil {
		return []policy.Finding{{
			Check: "supplyChain.releaseAge", Path: lockPath, Subject: "uv:resolved-graph",
			Message: err.Error(),
		}}
	}
	findings := pythonGitSourceCoverageFindings(packages)
	findings = append(findings, releaseAgeFindings(ctx, repo, packages, time.Now().UTC())...)
	return uniqueFindings(findings)
}

func filepathDirectory(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[:index]
	}
	return "."
}

type releaseObservation struct {
	Package  resolvedPackage
	Released time.Time
	Err      error
}

func releaseAgeFindings(ctx context.Context, repo repository.Repository, packages []resolvedPackage, now time.Time) []policy.Finding {
	observations := observeReleases(ctx, repo, registryReleasePackages(packages))
	cutoff := now.AddDate(0, 0, -repo.Config.SupplyChain.MinimumReleaseAgeDays)
	findings := []policy.Finding{}
	for _, observation := range observations {
		item := observation.Package
		subject := item.Name + "@" + item.Version
		if observation.Err != nil {
			findings = append(findings, policy.Finding{
				Check: "supplyChain.releaseAge", Path: item.Scope, Subject: subject,
				Message: observation.Err.Error(),
			})
			continue
		}
		if !observation.Released.After(cutoff) {
			continue
		}
		identity := policy.ReleaseAgeIdentity{
			Ecosystem: item.Ecosystem, Package: item.Name, Version: item.Version, Scope: item.Scope,
			Released: observation.Released, Eligible: observation.Released.AddDate(0, 0, repo.Config.SupplyChain.MinimumReleaseAgeDays),
		}
		message := fmt.Sprintf(
			"package was released %s and has not reached the %d-day hard minimum (eligible %s)",
			observation.Released.Format(time.RFC3339), repo.Config.SupplyChain.MinimumReleaseAgeDays,
			identity.Eligible.Format(time.RFC3339),
		)
		findings = append(findings, policy.Finding{
			Check: "supplyChain.releaseAge", Path: item.Scope, Subject: policy.ReleaseAgeSubject(identity),
			Message: message, ReleaseAge: &identity,
		})
	}
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Path+"\x00"+findings[left].Subject < findings[right].Path+"\x00"+findings[right].Subject
	})
	return uniqueFindings(findings)
}

func registryReleasePackages(packages []resolvedPackage) []resolvedPackage {
	registry := []resolvedPackage{}
	for _, item := range packages {
		if item.Source.Kind != "" && item.Source.Kind != "registry" {
			continue
		}
		registry = append(registry, item)
	}
	return registry
}

func pythonGitSourceCoverageFindings(packages []resolvedPackage) []policy.Finding {
	findings := []policy.Finding{}
	for _, item := range packages {
		if item.Source.Kind != "git" {
			continue
		}
		subject := item.Name + "@" + item.Source.Git.Identity()
		findings = append(findings,
			policy.Finding{
				Check: "supplyChain.releaseAgeCoverage", Path: item.Scope, Subject: subject,
				Message: "Git source has no registry release timestamp, so release-age coverage is unavailable",
			},
			policy.Finding{
				Check: "policy.securityScanner", Path: item.Scope, Subject: subject,
				Message: "Git source vulnerability coverage is unavailable without a scanner that can assess the resolved repository commit",
			},
		)
	}
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Check+"\x00"+findings[left].Path+"\x00"+findings[left].Subject <
			findings[right].Check+"\x00"+findings[right].Path+"\x00"+findings[right].Subject
	})
	return uniqueFindings(findings)
}

type releaseGroup struct {
	Kind  string
	Name  string
	Items []resolvedPackage
}

type releaseGroupResult struct {
	Group releaseGroup
	Times map[string]time.Time
	Err   error
}

func observeReleases(ctx context.Context, repo repository.Repository, packages []resolvedPackage) []releaseObservation {
	groups := groupReleasePackages(uniqueResolvedPackages(packages))
	if len(groups) == 0 {
		return nil
	}
	workers := 8
	if len(groups) < workers {
		workers = len(groups)
	}
	tasks := make(chan releaseGroup)
	results := make(chan releaseGroupResult, len(groups))
	client := registryClient()
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for group := range tasks {
				times, err := lookupReleaseGroup(ctx, client, repo, group)
				results <- releaseGroupResult{Group: group, Times: times, Err: err}
			}
		}()
	}
	go func() {
		for _, group := range groups {
			tasks <- group
		}
		close(tasks)
		wait.Wait()
		close(results)
	}()
	observations := []releaseObservation{}
	for result := range results {
		for _, item := range result.Group.Items {
			released, exists := result.Times[item.Version]
			err := result.Err
			if err == nil && !exists {
				err = errorsNewReleaseTime(item)
			}
			observations = append(observations, releaseObservation{Package: item, Released: released, Err: err})
		}
	}
	sort.Slice(observations, func(left, right int) bool {
		return resolvedPackageKey(observations[left].Package) < resolvedPackageKey(observations[right].Package)
	})
	return observations
}

func groupReleasePackages(packages []resolvedPackage) []releaseGroup {
	groups := map[string]*releaseGroup{}
	for _, item := range packages {
		kind := "node"
		if item.Ecosystem == "pypi" {
			kind = "pypi"
		}
		key := kind + "\x00" + strings.ToLower(item.Name)
		if groups[key] == nil {
			groups[key] = &releaseGroup{Kind: kind, Name: item.Name}
		}
		groups[key].Items = append(groups[key].Items, item)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]releaseGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, *groups[key])
	}
	return result
}

func lookupReleaseGroup(ctx context.Context, client *http.Client, repo repository.Repository, group releaseGroup) (map[string]time.Time, error) {
	endpoint := ""
	switch group.Kind {
	case "node":
		endpoint = strings.TrimSuffix(repo.Config.SupplyChain.NPMRegistryURL, "/") + "/" + url.PathEscape(group.Name)
	case "pypi":
		endpoint = "https://pypi.org/pypi/" + url.PathEscape(group.Name) + "/json"
	default:
		return nil, fmt.Errorf("unsupported release metadata ecosystem %q", group.Kind)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release metadata request failed with HTTP %d", response.StatusCode)
	}
	if group.Kind == "node" {
		return decodeNodeReleaseTimes(response.Body)
	}
	return decodePyPIReleaseTimes(response.Body)
}

func decodeNodeReleaseTimes(reader io.Reader) (map[string]time.Time, error) {
	var payload struct {
		Time map[string]string `json:"time"`
	}
	decoder := json.NewDecoder(io.LimitReader(reader, 64<<20))
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	if err := requireOneJSONValue(decoder); err != nil {
		return nil, err
	}
	times := map[string]time.Time{}
	for version, value := range payload.Time {
		released, err := time.Parse(time.RFC3339, value)
		if err == nil {
			times[version] = released
		}
	}
	return times, nil
}

func decodePyPIReleaseTimes(reader io.Reader) (map[string]time.Time, error) {
	var payload struct {
		Releases map[string][]struct {
			UploadTime time.Time `json:"upload_time_iso_8601"`
		} `json:"releases"`
	}
	decoder := json.NewDecoder(io.LimitReader(reader, 64<<20))
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	if err := requireOneJSONValue(decoder); err != nil {
		return nil, err
	}
	times := map[string]time.Time{}
	for version, files := range payload.Releases {
		for _, file := range files {
			if file.UploadTime.IsZero() || !times[version].IsZero() && !file.UploadTime.Before(times[version]) {
				continue
			}
			times[version] = file.UploadTime
		}
	}
	return times, nil
}

func errorsNewReleaseTime(item resolvedPackage) error {
	return fmt.Errorf("release metadata omitted the exact timestamp for %s@%s", item.Name, item.Version)
}
