package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
)

type pythonReachabilityStateInput struct {
	Admission PythonPluginDependencyFacts
	Sources   map[string]string
}

func (repo Repository) PythonReachabilityStateSHA256() (string, error) {
	selected := map[string]map[string]bool{}
	for _, declaration := range repo.Config.Scope.PythonDynamicReferences {
		if declaration.Consumer.Kind == "callsite" {
			continue
		}
		if selected[declaration.Project] == nil {
			selected[declaration.Project] = map[string]bool{}
		}
		selected[declaration.Project][declaration.Consumer.Distribution] = true
	}
	if len(selected) == 0 {
		return "", nil
	}
	inputs := []pythonReachabilityStateInput{}
	for _, manifest := range slices.Sorted(maps.Keys(selected)) {
		project := PythonProject{Manifest: manifest, Root: path.Dir(manifest), Venv: path.Join(path.Dir(manifest), ".venv")}
		facts, err := repo.PythonPluginDependencies(project, slices.Sorted(maps.Keys(selected[manifest])))
		if err != nil {
			return "", err
		}
		state, err := repo.pythonReachabilityDistributionState(project, facts)
		if err != nil {
			return "", err
		}
		inputs = append(inputs, pythonReachabilityStateInput{Admission: facts, Sources: state})
	}
	data, err := json.Marshal(inputs)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (repo Repository) pythonReachabilityDistributionState(project PythonProject, facts PythonPluginDependencyFacts) (map[string]string, error) {
	inputs := map[string]string{}
	for _, dependency := range facts.Dependencies {
		if dependency.Error != "" {
			return nil, fmt.Errorf("external reachability dependency is not admitted: %s", dependency.Error)
		}
		snapshot, err := repo.ReadPythonDistributionSources(project, dependency)
		if err != nil {
			return nil, err
		}
		if err := validatePythonDistributionOrigin(snapshot, dependency); err != nil {
			return nil, err
		}
		inputs[dependency.Distribution] = snapshot.Identity
	}
	return inputs, nil
}
