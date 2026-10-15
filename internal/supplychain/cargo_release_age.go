package supplychain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func cargoReleaseAgeFindings(ctx context.Context, repo repository.Repository, files []string, resolved []repository.CargoLock) []policy.Finding {
	locks, findings := cargoAgeLocks(repo, files, resolved)
	packages := []resolvedPackage{}
	for _, lock := range locks {
		locked, problems := resolvedCargoPackages(lock)
		findings = append(findings, problems...)
		if gitLockPackages(locked) {
			if len(problems) != 0 && repo.Config.SupplyChain.GitEvidence.Required {
				findings = append(findings, gitEvidenceFinding(lock.Path, gitEvidenceFailure("coverage", "Cargo Git evidence requires a complete supported lock inventory")))
			} else {
				assessment, _ := verifyGitLockEvidence(repo, lock.Path, locked, time.Now().UTC())
				findings = append(findings, assessment...)
			}
		}
		packages = append(packages, registryReleasePackages(locked)...)
	}
	return append(findings, releaseAgeFindings(ctx, repo, packages, time.Now().UTC())...)
}

func cargoAgeLocks(repo repository.Repository, files []string, resolved []repository.CargoLock) ([]repository.CargoLock, []policy.Finding) {
	locks := map[string]repository.CargoLock{}
	for _, lock := range resolved {
		locks[lock.Path] = lock
	}
	findings := []policy.Finding{}
	for _, path := range files {
		if _, exists := locks[path]; exists || filepath.Base(path) != "Cargo.lock" {
			continue
		}
		lock, err := repo.ReadCargoLock(path)
		if err != nil {
			findings = append(findings, policy.Finding{Check: "supplyChain.releaseAge", Path: path, Subject: "cargo:locked-inventory", Message: err.Error()})
			continue
		}
		locks[path] = lock
	}
	ordered := make([]repository.CargoLock, 0, len(locks))
	for _, path := range slices.Sorted(maps.Keys(locks)) {
		ordered = append(ordered, locks[path])
	}
	return ordered, findings
}

func lookupCargoReleaseGroup(ctx context.Context, client *http.Client, group releaseGroup) (map[string]time.Time, error) {
	query := url.Values{}
	versions := []string{}
	for _, item := range group.Items {
		versions = append(versions, item.Version)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("crate release lookup requires exact requested versions")
	}
	slices.Sort(versions)
	for _, version := range slices.Compact(versions) {
		query.Add("nums[]", version)
	}
	endpoint := "https://crates.io/api/v1/crates/" + url.PathEscape(group.Name) + "/versions?" + query.Encode()
	if len(endpoint) > 8192 {
		return nil, fmt.Errorf("crate release metadata request exceeds 8192 bytes")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Code-Polishy (https://github.com/riteofstring/code-polishy)")
	response, err := cargoMetadataRequests.do(client, request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("crate release metadata request failed with HTTP %d", response.StatusCode)
	}
	return decodeCargoReleaseTimes(response.Body, group.Name)
}

func decodeCargoReleaseTimes(reader io.Reader, name string) (map[string]time.Time, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 8<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 8<<20 {
		return nil, fmt.Errorf("crate release metadata exceeds 8388608 bytes")
	}
	var payload struct {
		Versions []struct {
			Name     string    `json:"crate"`
			Version  string    `json:"num"`
			Released time.Time `json:"created_at"`
		} `json:"versions"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	if err := requireOneJSONValue(decoder); err != nil {
		return nil, err
	}
	times := map[string]time.Time{}
	for _, version := range payload.Versions {
		if !strings.EqualFold(version.Name, name) || version.Version == "" || version.Released.IsZero() {
			return nil, fmt.Errorf("crate release metadata omitted exact package identity or publication time")
		}
		if _, exists := times[version.Version]; exists {
			return nil, fmt.Errorf("crate release metadata repeats version %q", version.Version)
		}
		times[version.Version] = version.Released
	}
	return times, nil
}
