package pack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type Status struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Digest   string `json:"digest"`
	State    string `json:"state"`
	Selected bool   `json:"selected"`
	Reason   string `json:"reason,omitempty"`
}

func List(dataRoot string, selected []policy.PackSelection, engineVersion string) ([]Status, error) {
	statuses, err := installedStatuses(dataRoot, engineVersion)
	if err != nil {
		return nil, err
	}
	for _, selection := range selected {
		index := slices.IndexFunc(statuses, func(status Status) bool {
			return status.Name == selection.Name && status.Version == selection.Version && status.Digest == selection.Digest
		})
		if index < 0 {
			statuses = append(statuses, Status{Name: selection.Name, Version: selection.Version, Digest: selection.Digest, State: "missing", Selected: true, Reason: "the exact selected identity is not installed"})
			continue
		}
		statuses[index].Selected = true
		if statuses[index].State == "installed" {
			statuses[index].State = "selected"
		}
	}
	sortStatuses(statuses)
	return statuses, nil
}

func installedStatuses(dataRoot, engineVersion string) ([]Status, error) {
	entries, err := os.ReadDir(dataRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []Status{}, nil
	}
	if err != nil {
		return nil, err
	}
	statuses := []Status{}
	for _, nameEntry := range entries {
		if !nameEntry.IsDir() || !identifierPattern.MatchString(nameEntry.Name()) {
			continue
		}
		versions, err := os.ReadDir(filepath.Join(dataRoot, nameEntry.Name()))
		if err != nil {
			return nil, err
		}
		for _, versionEntry := range versions {
			if !versionEntry.IsDir() || !semanticVersionPattern.MatchString(versionEntry.Name()) {
				continue
			}
			digests, err := os.ReadDir(filepath.Join(dataRoot, nameEntry.Name(), versionEntry.Name()))
			if err != nil {
				return nil, err
			}
			for _, digestEntry := range digests {
				if !digestEntry.IsDir() || !validDigest(digestEntry.Name()) {
					continue
				}
				statuses = append(statuses, installedStatus(dataRoot, nameEntry.Name(), versionEntry.Name(), digestEntry.Name(), engineVersion))
			}
		}
	}
	sortStatuses(statuses)
	return statuses, nil
}

func installedStatus(dataRoot, name, version, digest, engineVersion string) Status {
	status := Status{Name: name, Version: version, Digest: digest, State: "installed"}
	root := InstalledRoot(dataRoot, name, version, digest)
	receipt, err := VerifyInstalled(root)
	if err != nil || receipt.Name != name || receipt.Version != version || receipt.Digest != digest {
		status.State = "corrupt"
		status.Reason = boundedLifecycleReason(err, "installation receipt does not match its path")
		return status
	}
	manifest, incompatible, err := installedManifest(root, engineVersion)
	if err != nil {
		status.State = "corrupt"
		status.Reason = boundedLifecycleReason(err, "installed manifest is unavailable")
		return status
	}
	if incompatible || !slices.Contains(manifest.Platforms, CurrentPlatform()) {
		status.State = "incompatible"
		status.Reason = fmt.Sprintf("requires engine %s, manifest %d, protocol %d, and platform %s", engineVersion, ManifestVersion, ProtocolVersion, CurrentPlatform())
	}
	return status
}

func installedManifest(root, engineVersion string) (Manifest, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, ManifestFilename))
	if err != nil {
		return Manifest{}, false, err
	}
	header := struct {
		ManifestVersion int      `json:"manifestVersion"`
		ProtocolVersion int      `json:"protocolVersion"`
		EngineVersion   string   `json:"engineVersion"`
		Platforms       []string `json:"platforms"`
	}{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&header); err != nil {
		return Manifest{}, false, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Manifest{}, false, errors.New("installed manifest contains more than one JSON value")
	}
	if header.ManifestVersion > 0 && header.ProtocolVersion > 0 && (header.ManifestVersion != ManifestVersion || header.ProtocolVersion != ProtocolVersion || header.EngineVersion != engineVersion) {
		return Manifest{ManifestVersion: header.ManifestVersion, ProtocolVersion: header.ProtocolVersion, EngineVersion: header.EngineVersion, Platforms: header.Platforms}, true, nil
	}
	manifest, err := ParseManifest(data, filepath.Join(root, ManifestFilename))
	return manifest, false, err
}

func boundedLifecycleReason(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	value := err.Error()
	if len(value) > 512 {
		return value[:512]
	}
	return value
}

func sortStatuses(statuses []Status) {
	sort.Slice(statuses, func(left, right int) bool {
		return statuses[left].Name+"\x00"+statuses[left].Version+"\x00"+statuses[left].Digest < statuses[right].Name+"\x00"+statuses[right].Version+"\x00"+statuses[right].Digest
	})
}

func Remove(dataRoot, name, version, digest string) (Identity, error) {
	if !identifierPattern.MatchString(name) || !semanticVersionPattern.MatchString(version) || digest != "" && !validDigest(digest) {
		return Identity{}, errors.New("remove requires an exact pack name, version, and optional digest")
	}
	statuses, err := installedStatuses(dataRoot, "")
	if err != nil {
		return Identity{}, err
	}
	matches := []Status{}
	for _, status := range statuses {
		if status.Name == name && status.Version == version && (digest == "" || status.Digest == digest) {
			matches = append(matches, status)
		}
	}
	if len(matches) == 0 {
		return Identity{}, fmt.Errorf("pack %s@%s is not installed", name, version)
	}
	if len(matches) > 1 {
		return Identity{}, fmt.Errorf("pack %s@%s has multiple installed digests; select one with --digest", name, version)
	}
	identity := Identity{Name: name, Version: version, Digest: matches[0].Digest}
	target := InstalledRoot(dataRoot, identity.Name, identity.Version, identity.Digest)
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Identity{}, errors.New("installed pack target is not a removable directory")
	}
	makeWritable(target)
	if err := os.RemoveAll(target); err != nil {
		return Identity{}, err
	}
	removeEmptyPackParents(dataRoot, identity)
	return identity, nil
}

func removeEmptyPackParents(dataRoot string, identity Identity) {
	for _, directory := range []string{filepath.Join(dataRoot, identity.Name, identity.Version), filepath.Join(dataRoot, identity.Name)} {
		entries, err := os.ReadDir(directory)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(directory)
		}
	}
}
