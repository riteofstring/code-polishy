package policy

import (
	"fmt"
	pathpkg "path"
)

func validateGeneratedJavaScript(declarations []GeneratedJavaScript) error {
	seen := map[string]bool{}
	for index, declaration := range declarations {
		label := fmt.Sprintf("scope.generatedJavaScript[%d]", index)
		if len(declaration.Paths) == 0 {
			return fmt.Errorf("%s.paths must not be empty", label)
		}
		if err := validatePatterns(declaration.Paths, label+".paths", true); err != nil {
			return err
		}
		if err := concreteRepositoryPath(declaration.SourcePackage, label+".sourcePackage"); err != nil {
			return err
		}
		if pathpkg.Base(declaration.SourcePackage) != "package.json" {
			return fmt.Errorf("%s.sourcePackage must name an exact package.json file", label)
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
