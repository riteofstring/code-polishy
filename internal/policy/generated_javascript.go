package policy

import (
	"fmt"
)

func validateSourceContext(declarations []SourceContext) error {
	seen := map[string]bool{}
	for index, declaration := range declarations {
		label := fmt.Sprintf("scope.sourceContexts[%d]", index)
		if err := rejectUniversalPatterns(declaration.Paths, label+".paths"); err != nil {
			return err
		}
		for _, pattern := range declaration.Paths {
			identity := pattern + "\x00" + declaration.Context
			if seen[identity] {
				return fmt.Errorf("%s duplicates source-context ownership for %q", label, pattern)
			}
			seen[identity] = true
		}
	}
	return nil
}
