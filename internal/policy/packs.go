package policy

import (
	"fmt"
	"regexp"
)

var semanticVersionPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validatePacks(packs []PackSelection) error {
	seen := map[string]bool{}
	for index, selected := range packs {
		label := fmt.Sprintf("packs[%d]", index)
		if err := validatePackSelection(selected, label, seen); err != nil {
			return err
		}
	}
	return nil
}

func validatePackSelection(selected PackSelection, label string, seen map[string]bool) error {
	if err := identifier(selected.Name, label+".name"); err != nil {
		return err
	}
	if !semanticVersionPattern.MatchString(selected.Version) {
		return fmt.Errorf("%s.version must be an exact semantic version", label)
	}
	if !sha256Pattern.MatchString(selected.Digest) {
		return fmt.Errorf("%s.digest must be an exact lowercase SHA-256 digest", label)
	}
	if seen[selected.Name] {
		return fmt.Errorf("duplicate selected pack name %q", selected.Name)
	}
	seen[selected.Name] = true
	return nil
}
