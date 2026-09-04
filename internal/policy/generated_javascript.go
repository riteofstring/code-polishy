package policy

import (
	"fmt"
)

func validateGeneratedJavaScript(declarations []GeneratedJavaScript) error {
	seen := map[string]bool{}
	for index, declaration := range declarations {
		label := fmt.Sprintf("scope.generatedJavaScript[%d]", index)
		if err := rejectUniversalPatterns(declaration.Paths, label+".paths"); err != nil {
			return err
		}
		for _, pattern := range declaration.Paths {
			identity := pattern + "\x00" + declaration.SourcePackage
			if seen[identity] {
				return fmt.Errorf("%s duplicates generated JavaScript ownership for %q", label, pattern)
			}
			seen[identity] = true
		}
	}
	return nil
}
