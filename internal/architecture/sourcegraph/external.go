package sourcegraph

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

var externalDistributionName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var externalPythonName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*$`)

func normalizeExternalCompositions(edges []ExternalComposition, nodes map[string]Node) ([]ExternalComposition, error) {
	if len(edges) > MaximumEdges {
		return nil, fmt.Errorf("external composition exceeds the graph edge boundary")
	}
	result := make([]ExternalComposition, 0, len(edges))
	seen := map[string]bool{}
	for _, edge := range edges {
		normalized, err := normalizeExternalComposition(edge, nodes)
		if err != nil {
			return nil, err
		}
		identity := externalCompositionIdentity(normalized)
		if seen[identity] {
			return nil, fmt.Errorf("external composition repeats a loader callsite")
		}
		seen[identity] = true
		result = append(result, normalized)
	}
	slices.SortFunc(result, func(left, right ExternalComposition) int {
		return strings.Compare(externalCompositionIdentity(left), externalCompositionIdentity(right))
	})
	return result, nil
}

func normalizeExternalComposition(edge ExternalComposition, nodes map[string]Node) (ExternalComposition, error) {
	source, err := normalizePath(edge.Source, false)
	if err != nil {
		return ExternalComposition{}, err
	}
	node, found := nodes[source]
	if !found || node.Language != "python" || edge.Line <= 0 || edge.Column <= 0 {
		return ExternalComposition{}, fmt.Errorf("external composition has no exact Python source location")
	}
	edge.Source = source
	edge.SourceResolution = strings.ReplaceAll(edge.SourceResolution, "\\", "/")
	if edge.SourceResolution != node.Resolution {
		return ExternalComposition{}, fmt.Errorf("external composition source resolution is stale")
	}
	if err := validateExternalDependency(edge.Dependency, node); err != nil {
		return ExternalComposition{}, err
	}
	if err := validateExternalContract(edge.Contract); err != nil {
		return ExternalComposition{}, err
	}
	return edge, nil
}

func validateExternalDependency(dependency ExternalDependency, node Node) error {
	if dependency.Project != factProjectManifest(node.Root) || dependency.Lock != path.Join(node.Root, "uv.lock") {
		return fmt.Errorf("external composition dependency belongs to another project or lock")
	}
	if dependency.Kind != "registry" && dependency.Kind != "git" {
		return fmt.Errorf("external composition has an unsupported dependency source kind")
	}
	if !factDigest(dependency.ManifestSHA256) || !factDigest(dependency.LockSHA256) {
		return fmt.Errorf("external composition dependency source evidence is missing")
	}
	return validateExternalDependencyIdentity(dependency)
}

func validateExternalDependencyIdentity(dependency ExternalDependency) error {
	if len(dependency.Distribution) > 255 || !externalDistributionName.MatchString(dependency.Distribution) || !externalPythonName.MatchString(dependency.Namespace) {
		return fmt.Errorf("external composition has an invalid distribution or namespace")
	}
	for _, value := range []string{dependency.Distribution, dependency.Version, dependency.Source, dependency.Namespace} {
		if !boundedAtom(value) {
			return fmt.Errorf("external composition has invalid dependency identity")
		}
	}
	return nil
}

func validateExternalContract(contract ExternalContract) error {
	if contract.InputGrammar != "python-module-object/v1" || !slices.Contains([]string{"isinstance", "issubclass", "validator-call"}, contract.CheckKind) {
		return fmt.Errorf("external composition has an unsupported grammar or runtime check")
	}
	if !boundedAtom(contract.Protocol) || !boundedAtom(contract.RuntimeType) || contract.CheckLine <= 0 || contract.CheckColumn <= 0 {
		return fmt.Errorf("external composition omits its exact runtime type and check site")
	}
	if !externalPythonName.MatchString(contract.Protocol) || !externalPythonName.MatchString(contract.RuntimeType) {
		return fmt.Errorf("external composition runtime type must be a qualified Python identifier")
	}
	for _, digest := range []string{contract.SourceSHA256, contract.RuntimeSHA256, contract.InputSHA256} {
		if !factDigest(digest) {
			return fmt.Errorf("external composition has stale or missing runtime or input evidence")
		}
	}
	return nil
}

func externalCompositionIdentity(edge ExternalComposition) string {
	return fmt.Sprintf("%s\x00%09d\x00%09d", edge.Source, edge.Line, edge.Column)
}
