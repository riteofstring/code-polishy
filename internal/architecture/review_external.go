package architecture

import (
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
)

type ReviewExternal struct {
	SourceResolution string `json:"sourceResolutionUnit"`
	Distribution     string `json:"distribution"`
	Version          string `json:"version"`
	Kind             string `json:"kind"`
	DependencySource string `json:"dependencySource"`
	Namespace        string `json:"namespace"`
	InputGrammar     string `json:"inputGrammar"`
	CheckKind        string `json:"checkKind"`
	Protocol         string `json:"protocol"`
	RuntimeType      string `json:"runtimeType"`
}

func reviewExternalCompositions(edges []sourcegraph.ExternalComposition) []ReviewExternal {
	result := make([]ReviewExternal, 0, len(edges))
	for _, edge := range edges {
		result = append(result, ReviewExternal{SourceResolution: edge.SourceResolution, Distribution: edge.Dependency.Distribution, Version: edge.Dependency.Version, Kind: edge.Dependency.Kind, DependencySource: edge.Dependency.Source, Namespace: edge.Dependency.Namespace, InputGrammar: edge.Contract.InputGrammar, CheckKind: edge.Contract.CheckKind, Protocol: edge.Contract.Protocol, RuntimeType: edge.Contract.RuntimeType})
	}
	slices.SortFunc(result, func(left, right ReviewExternal) int {
		return strings.Compare(reviewJSONKey(left), reviewJSONKey(right))
	})
	return slices.Compact(result)
}

func reviewExternalDifference(candidates, baseline []ReviewExternal) []ReviewExternal {
	known := map[ReviewExternal]bool{}
	for _, edge := range baseline {
		known[edge] = true
	}
	result := []ReviewExternal{}
	for _, edge := range candidates {
		if !known[edge] {
			result = append(result, edge)
		}
	}
	return result
}
