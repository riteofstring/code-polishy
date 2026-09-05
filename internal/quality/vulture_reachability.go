package quality

import (
	"sort"

	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func validatePythonReachabilityEvidence(files []string, references []pythonVultureReference, response pythonVultureResponse) error {
	inputs := []pythonfacts.ReachabilityInput{}
	for _, reference := range references {
		if reference.Contract != nil {
			inputs = append(inputs, *reference.Contract)
		}
	}
	return pythonfacts.ValidateReachabilityEvidence(files, inputs, response.Resolved, response.Reachability)
}

func pythonVultureReferences(repo repository.Repository, project repository.PythonProject) ([]pythonVultureReference, map[string]pythonVultureReferenceOrigin) {
	references := []pythonVultureReference{}
	origins := map[string]pythonVultureReferenceOrigin{}
	configPath := pythonVultureConfigPath(repo)
	for _, input := range repo.PythonReachabilityInputs(project) {
		references = append(references, pythonVultureReference{ID: input.ID, Contract: &input})
		origins[input.ID] = pythonVultureReferenceOrigin{Path: configPath, Subject: input.ID, Check: "policy.pythonReachability", Message: "Python reachability consumer cannot resolve exactly: "}
	}
	for _, reference := range project.DynamicReferences {
		id := pythonVultureManifestReferenceID(project, reference)
		references = append(references, pythonVultureReference{ID: id, Module: reference.Module, Symbol: reference.Symbol})
		origins[id] = pythonVultureReferenceOrigin{
			Path: project.Manifest, Line: reference.Line, Subject: id, Check: "policy.pythonDynamicReference",
			Message: "Python dynamic reference cannot resolve exactly: ",
		}
	}
	sort.Slice(references, func(left, right int) bool { return references[left].ID < references[right].ID })
	return references, origins
}
