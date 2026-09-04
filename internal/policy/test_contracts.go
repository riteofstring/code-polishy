package policy

import (
	"fmt"
	pathpkg "path"
	"strings"
)

func validateTestArtifacts(artifacts []TestArtifact, suiteLabel string) error {
	seen := map[string]bool{}
	for index, artifact := range artifacts {
		label := fmt.Sprintf("%s.artifacts[%d]", suiteLabel, index)
		if err := concreteRepositoryPath(artifact.Path, label+".path"); err != nil {
			return err
		}
		if artifact.Path == "." || strings.HasPrefix(artifact.Path, ".code-polishy-") {
			return fmt.Errorf("%s.path must name a contained output below the execution artifact directory", label)
		}
		if seen[artifact.Path] {
			return fmt.Errorf("%s.path duplicates artifact output %q", label, artifact.Path)
		}
		seen[artifact.Path] = true
		if err := allowedValues([]string{artifact.Type}, []string{"cobertura", "junit"}, label+".type"); err != nil {
			return err
		}
		if strings.ToLower(pathpkg.Ext(artifact.Path)) != ".xml" {
			return fmt.Errorf("%s.path must name an XML file", label)
		}
	}
	return nil
}
