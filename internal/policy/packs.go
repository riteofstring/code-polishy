package policy

import "fmt"

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
	if seen[selected.Name] {
		return fmt.Errorf("duplicate selected pack name %q", selected.Name)
	}
	seen[selected.Name] = true
	return nil
}
