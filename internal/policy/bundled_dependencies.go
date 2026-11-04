package policy

import "fmt"

func validateBundledDependencies(declarations []BundledDependencies) error {
	seen := map[string]bool{}
	for _, declaration := range declarations {
		if seen[declaration.Manifest] {
			return fmt.Errorf("scope.bundledDependencies repeats manifest %q", declaration.Manifest)
		}
		seen[declaration.Manifest] = true
	}
	return nil
}
