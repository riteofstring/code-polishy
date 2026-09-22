package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

type pythonDiscovery struct {
	request          request
	sources          map[string]inventoryEntry
	manifests        map[string]bool
	metadata         []string
	sourceManifests  map[string]string
	selectedManifest map[string]bool
}

type pythonScopeData struct {
	Manifest       string              `json:"manifest"`
	RequiresPython string              `json:"requiresPython"`
	TargetVersion  string              `json:"targetVersion"`
	SourceRoots    []string            `json:"sourceRoots"`
	EntryPoints    []projectEntryPoint `json:"entryPoints"`
	Problems       []projectProblem    `json:"problems"`
}

func discover(ctx context.Context, request request, executor projectExecutor) response {
	discovery, err := newPythonDiscovery(request)
	if err != nil {
		return response{ProtocolVersion: protocolVersion, Status: "operational-failure", Failure: boundedFailure(err)}
	}
	projects, inputs, err := executor.inspect(ctx, request, discovery.selectedProjectInputs())
	if err != nil {
		return response{ProtocolVersion: protocolVersion, Status: "operational-failure", Failure: boundedFailure(err)}
	}
	scopes, err := discovery.scopes(projects)
	if err != nil {
		return response{ProtocolVersion: protocolVersion, Status: "operational-failure", Failure: boundedFailure(err)}
	}
	return response{
		ProtocolVersion: protocolVersion,
		Status:          "pass",
		Evidence:        []string{"static Python discovery grouped governed source by its nearest pyproject.toml"},
		Discovery:       &discoveryResult{Scopes: scopes},
		Inputs:          inputs,
	}
}

func newPythonDiscovery(request request) (pythonDiscovery, error) {
	value := pythonDiscovery{
		request: request, sources: map[string]inventoryEntry{}, manifests: map[string]bool{},
		sourceManifests: map[string]string{}, selectedManifest: map[string]bool{},
	}
	for _, entry := range request.Inventory {
		if entry.Source && entry.Language == "python" && entry.Owner == request.Provider {
			value.sources[entry.Path] = entry
		}
		if entry.Metadata {
			value.metadata = append(value.metadata, entry.Path)
			if path.Base(entry.Path) == "pyproject.toml" {
				value.manifests[entry.Path] = true
			}
		}
	}
	if len(value.sources) == 0 {
		return pythonDiscovery{}, errors.New("discovery found no provider-owned Python source")
	}
	value.assignSources()
	if err := value.selectManifests(); err != nil {
		return pythonDiscovery{}, err
	}
	return value, nil
}

func (value *pythonDiscovery) assignSources() {
	for source := range value.sources {
		if manifest := nearestManifest(source, value.manifests); manifest != "" {
			value.sourceManifests[source] = manifest
		}
	}
}

func (value *pythonDiscovery) selectManifests() error {
	for _, file := range value.request.Files {
		if _, found := value.sources[file]; !found {
			return fmt.Errorf("selected source %s is not provider-owned Python", file)
		}
		manifest := value.sourceManifests[file]
		if manifest == "" {
			return fmt.Errorf("selected source %s has no contained pyproject.toml", file)
		}
		value.selectedManifest[manifest] = true
	}
	for _, selected := range value.request.Selection.Paths {
		if value.manifests[selected] {
			value.selectedManifest[selected] = true
		}
	}
	if len(value.selectedManifest) == 0 {
		for _, manifest := range value.sourceManifests {
			value.selectedManifest[manifest] = true
		}
	}
	return nil
}

func (value pythonDiscovery) selectedProjectInputs() []projectInput {
	inputs := []projectInput{}
	for manifest := range value.selectedManifest {
		inputs = append(inputs, projectInput{Path: manifest, Manifest: manifest, Kind: "manifest"})
		for _, metadata := range value.metadata {
			if metadata == manifest || nearestManifest(metadata, value.manifests) != manifest {
				continue
			}
			if slices.Contains([]string{"ruff.toml", ".ruff.toml"}, path.Base(metadata)) {
				inputs = append(inputs, projectInput{Path: metadata, Manifest: manifest, Kind: "ruff"})
			}
		}
	}
	sort.Slice(inputs, func(left, right int) bool {
		return inputs[left].Path+"\x00"+inputs[left].Kind < inputs[right].Path+"\x00"+inputs[right].Kind
	})
	return inputs
}

func (value pythonDiscovery) scopes(projects map[string]projectFact) ([]discoveredScope, error) {
	manifests := make([]string, 0, len(value.selectedManifest))
	for manifest := range value.selectedManifest {
		manifests = append(manifests, manifest)
	}
	sort.Strings(manifests)
	scopes := make([]discoveredScope, 0, len(manifests))
	for _, manifest := range manifests {
		scope, err := value.scope(manifest, projects[manifest])
		if err != nil {
			return nil, err
		}
		scopes = append(scopes, scope)
	}
	if len(scopes) == 0 {
		return nil, errors.New("discovery found no selected Python project")
	}
	return scopes, nil
}

func (value pythonDiscovery) scope(manifest string, project projectFact) (discoveredScope, error) {
	members := []string{}
	for source, owner := range value.sourceManifests {
		if owner == manifest {
			members = append(members, source)
		}
	}
	members = uniqueSorted(members)
	if len(members) == 0 {
		return discoveredScope{}, fmt.Errorf("selected Python project %s has no provider-owned source", manifest)
	}
	selected := []string{}
	for _, file := range value.request.Files {
		if slices.Contains(members, file) {
			selected = append(selected, file)
		}
	}
	context := append(slices.Clone(members), manifest)
	for _, metadata := range value.metadata {
		if nearestManifest(metadata, value.manifests) == manifest {
			context = append(context, metadata)
		}
	}
	if project.Manifest != manifest {
		return discoveredScope{}, fmt.Errorf("python project facts omitted %s", manifest)
	}
	sourceRoots := pythonSourceRoots(projectRoot(manifest), members, project.BackendPaths)
	problems := slices.Clone(project.Problems)
	if len(problems) == 0 {
		problems = pythonModuleProblems(sourceRoots, members)
	}
	data, err := json.Marshal(pythonScopeData{
		Manifest: manifest, RequiresPython: project.RequiresPython, TargetVersion: project.TargetVersion,
		SourceRoots: sourceRoots, EntryPoints: project.EntryPoints, Problems: problems,
	})
	if err != nil {
		return discoveredScope{}, err
	}
	return discoveredScope{
		ID: "python:" + manifest, Language: "python", Root: projectRoot(manifest), Members: members,
		EntryFiles: pythonEntryFiles(members, selected), Context: uniqueSorted(context), Selected: uniqueSorted(selected), Data: data,
	}, nil
}

func pythonModuleProblems(sourceRoots, members []string) []projectProblem {
	for _, member := range members {
		if _, err := pythonModuleName(sourceRoots, member); err != nil {
			return []projectProblem{{Path: member, Message: "Python project layout is unsupported: " + err.Error()}}
		}
	}
	return []projectProblem{}
}

func pythonSourceRoots(root string, members, backendPaths []string) []string {
	roots := []string{root}
	candidates := append(slices.Clone(backendPaths), "src")
	for _, candidate := range candidates {
		contained := candidate
		if root != "." {
			contained = path.Join(root, candidate)
		}
		if candidate == "." {
			contained = root
		}
		if slices.ContainsFunc(members, func(member string) bool {
			return member == contained || strings.HasPrefix(member, contained+"/")
		}) {
			roots = append(roots, contained)
		}
	}
	return uniqueSorted(roots)
}

func decodePythonScopeData(data json.RawMessage) (pythonScopeData, error) {
	value := pythonScopeData{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return pythonScopeData{}, fmt.Errorf("decode python scope data: %w", err)
	}
	if err := requireEnd(decoder); err != nil {
		return pythonScopeData{}, err
	}
	if value.Manifest == "" || len(value.SourceRoots) == 0 || value.EntryPoints == nil || value.Problems == nil {
		return pythonScopeData{}, errors.New("python scope data is incomplete")
	}
	if err := validateProjectEntryPoints(value.EntryPoints); err != nil {
		return pythonScopeData{}, err
	}
	return value, nil
}

func nearestManifest(file string, manifests map[string]bool) string {
	directory := path.Dir(file)
	if path.Base(file) == "pyproject.toml" && manifests[file] {
		return file
	}
	for {
		candidate := "pyproject.toml"
		if directory != "." {
			candidate = path.Join(directory, "pyproject.toml")
		}
		if manifests[candidate] {
			return candidate
		}
		if directory == "." {
			return ""
		}
		directory = path.Dir(directory)
	}
}

func projectRoot(manifest string) string {
	root := path.Dir(manifest)
	if root == "" {
		return "."
	}
	return root
}

func pythonEntryFiles(members, selected []string) []string {
	entries := slices.Clone(selected)
	for _, member := range members {
		if path.Base(member) == "__main__.py" {
			entries = append(entries, member)
		}
	}
	return uniqueSorted(entries)
}
