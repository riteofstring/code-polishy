package rustfacts

import (
	"errors"
	"slices"
	"strings"
)

type SourceGraphFact struct {
	SourceLocation
	Scope  []string `json:"scope"`
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	Path   []string `json:"path"`
	Alias  string   `json:"alias"`
	Inline bool     `json:"inline"`
	File   string   `json:"file"`
	Public bool     `json:"public"`
}

func validateSourceGraphFacts(facts []SourceGraphFact, coordinates sourceCoordinates) error {
	for _, fact := range facts {
		if err := validateSourceLocation(fact.SourceLocation, coordinates); err != nil {
			return err
		}
		if fact.Scope == nil || fact.Path == nil || len(fact.Scope) > 128 || !validRustNames(fact.Scope) || !validRustNames(fact.Path) {
			return errors.New("rust graph fact omitted its bounded lexical identity")
		}
		if !validGraphFactIdentity(fact) {
			return errors.New("rust graph fact omitted its parsed dependency identity")
		}
	}
	return nil
}

func validGraphFactIdentity(fact SourceGraphFact) bool {
	switch fact.Kind {
	case "module", "item", "unsupported", "scope":
		return fact.Name != ""
	case "use", "extern":
		return len(fact.Path) > 0 && fact.Alias != ""
	case "reference":
		return len(fact.Path) > 0
	case "macro":
		return len(fact.Path) > 0 && slices.Contains([]string{"std", "core", "alloc"}, fact.Name)
	case "prelude":
		return fact.Name == "no_std" || fact.Name == "no_implicit_prelude"
	}
	return false
}

func validRustNames(names []string) bool {
	return !slices.ContainsFunc(names, func(name string) bool {
		return name == "" || strings.ContainsAny(name, "/\\:\x00\n\r")
	})
}
