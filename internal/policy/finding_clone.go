package policy

import (
	"maps"
	"slices"
)

func (finding Finding) Clone() Finding {
	finding.SelectionEvidence = slices.Clone(finding.SelectionEvidence)
	finding.Related = slices.Clone(finding.Related)
	finding.Fields = maps.Clone(finding.Fields)
	finding.SemanticIdentity = slices.Clone(finding.SemanticIdentity)
	finding.Remediation.Configuration = slices.Clone(finding.Remediation.Configuration)
	if generation := finding.Remediation.Generation; generation != nil {
		copy := *generation
		copy.Inputs = slices.Clone(generation.Inputs)
		copy.Generate = generation.Generate.Clone()
		copy.Verify = generation.Verify.Clone()
		copy.Prerequisites = slices.Clone(generation.Prerequisites)
		finding.Remediation.Generation = &copy
	}
	if command := finding.Remediation.NextCommand; command != nil {
		copy := *command
		copy.Argv = slices.Clone(command.Argv)
		finding.Remediation.NextCommand = &copy
	}
	if component := finding.DependencyComponent; component != nil {
		copy := *component
		copy.Members = slices.Clone(component.Members)
		copy.Edges = slices.Clone(component.Edges)
		copy.Witness = slices.Clone(component.Witness)
		finding.DependencyComponent = &copy
	}
	if vulnerability := finding.Vulnerability; vulnerability != nil {
		copy := *vulnerability
		copy.Aliases = slices.Clone(vulnerability.Aliases)
		finding.Vulnerability = &copy
	}
	if age := finding.ReleaseAge; age != nil {
		copy := *age
		finding.ReleaseAge = &copy
	}
	return finding
}
