package pythonfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

func ValidateReachabilityEvidence(files []string, inputs []ReachabilityInput, resolved []string, results []ReachabilityEvidence) error {
	expected, err := expectedReachabilityInputs(inputs, resolved)
	if err != nil {
		return err
	}
	for _, evidence := range results {
		input, found := expected[evidence.ID]
		if !found {
			return fmt.Errorf("reachability evidence repeats or names an unresolved consumer")
		}
		delete(expected, evidence.ID)
		dependencyIdentity := ""
		if input.Dependency != nil {
			dependencyIdentity = input.Dependency.Identity
		}
		if evidence.DependencyIdentity != dependencyIdentity {
			return fmt.Errorf("reachability evidence has stale dependency inputs")
		}
		digest := sha256.Sum256([]byte(input.Registry))
		if evidence.RegistrySHA256 != hex.EncodeToString(digest[:]) || input.Error != "" {
			return fmt.Errorf("reachability evidence has stale or invalid registry input")
		}
		if err := validateReachabilityTargetInput(input, evidence); err != nil {
			return err
		}
		if err := validatePythonReachabilityTargets(files, evidence); err != nil {
			return err
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("reachability evidence omits a resolved consumer")
	}
	return nil
}

func expectedReachabilityInputs(inputs []ReachabilityInput, resolved []string) (map[string]ReachabilityInput, error) {
	all := map[string]ReachabilityInput{}
	for _, input := range inputs {
		if _, found := all[input.ID]; found || input.ID == "" {
			return nil, fmt.Errorf("reachability input identity is empty or duplicated")
		}
		all[input.ID] = input
	}
	expected := map[string]ReachabilityInput{}
	for _, id := range resolved {
		if input, found := all[id]; found {
			if _, duplicate := expected[id]; duplicate {
				return nil, fmt.Errorf("reachability consumer was resolved more than once")
			}
			expected[id] = input
		}
	}
	return expected, nil
}

func validateReachabilityTargetInput(input ReachabilityInput, evidence ReachabilityEvidence) error {
	var declaration struct {
		Kind   string `json:"kind"`
		Target struct {
			Module string `json:"module"`
			Symbol string `json:"symbol"`
		} `json:"target"`
	}
	if err := json.Unmarshal(input.Declaration, &declaration); err != nil {
		return fmt.Errorf("reachability declaration is invalid: %w", err)
	}
	switch declaration.Kind {
	case "target":
		if input.Registry != "" || len(evidence.Targets) != 1 || evidence.Targets[0].Module != declaration.Target.Module || evidence.Targets[0].Symbol != declaration.Target.Symbol {
			return fmt.Errorf("reachability evidence does not match its exact target")
		}
	case "registry":
		if input.Registry == "" {
			return fmt.Errorf("reachability registry input is missing")
		}
	default:
		return fmt.Errorf("reachability declaration kind is unsupported")
	}
	return nil
}

func validatePythonReachabilityTargets(files []string, evidence ReachabilityEvidence) error {
	if len(evidence.Identity) != 64 || evidence.Identity != strings.ToLower(evidence.Identity) {
		return fmt.Errorf("reachability evidence has no canonical identity")
	}
	if _, err := hex.DecodeString(evidence.Identity); err != nil {
		return fmt.Errorf("reachability evidence has an invalid identity")
	}
	if len(evidence.Targets) == 0 {
		return fmt.Errorf("reachability evidence omits resolved targets")
	}
	seen := map[string]bool{}
	for _, target := range evidence.Targets {
		identity := target.Module + ":" + target.Symbol
		if target.Module == "" || target.Symbol == "" || seen[identity] || len(target.Definitions) == 0 {
			return fmt.Errorf("reachability evidence has an empty or duplicate target")
		}
		seen[identity] = true
		if err := validatePythonReachabilityDefinitions(files, target.Definitions); err != nil {
			return err
		}
	}
	return nil
}

func validatePythonReachabilityDefinitions(files []string, definitions []ReachabilityDefinition) error {
	seen := map[ReachabilityDefinition]bool{}
	for _, definition := range definitions {
		if !slices.Contains(files, definition.Path) || definition.Name == "" || definition.Line < 1 || definition.End < definition.Line || seen[definition] {
			return fmt.Errorf("reachability evidence has an invalid target definition")
		}
		seen[definition] = true
	}
	return nil
}
