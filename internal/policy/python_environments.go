package policy

import "fmt"

func validatePythonTypecheckEnvironments(environments []PythonTypecheckEnvironment) error {
	seen := map[string]bool{}
	for _, environment := range environments {
		for _, path := range environment.Paths {
			if seen[path] {
				return fmt.Errorf("scope.pythonTypecheckEnvironments assigns %q more than once", path)
			}
			seen[path] = true
		}
	}
	return nil
}
