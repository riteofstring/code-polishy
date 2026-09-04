package policy

import (
	"fmt"
	"strings"
)

func validateTestArtifacts(artifacts []TestArtifact, suiteLabel string) error {
	seen := map[string]bool{}
	for index, artifact := range artifacts {
		label := fmt.Sprintf("%s.artifacts[%d]", suiteLabel, index)
		if artifact.Path == "." || strings.HasPrefix(artifact.Path, ".code-polishy-") {
			return fmt.Errorf("%s.path must name a contained output below the execution artifact directory", label)
		}
		if seen[artifact.Path] {
			return fmt.Errorf("%s.path duplicates artifact output %q", label, artifact.Path)
		}
		seen[artifact.Path] = true
	}
	return nil
}
