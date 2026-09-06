package policy

import (
	"fmt"
	"regexp"
	"strings"
)

type PythonContract struct {
	Project         string          `json:"project"`
	Kind            string          `json:"kind"`
	Target          string          `json:"target"`
	Members         []string        `json:"members,omitempty"`
	Attributes      []string        `json:"attributes,omitempty"`
	Decorators      []string        `json:"decorators,omitempty"`
	AnnotatedFields bool            `json:"annotatedFields,omitempty"`
	Keywords        map[string]bool `json:"keywords,omitempty"`
	Reason          string          `json:"reason"`
}

func validatePythonContracts(contracts []PythonContract) error {
	seen := map[string]bool{}
	for index, contract := range contracts {
		label := fmt.Sprintf("scope.pythonContracts[%d]", index)
		if err := validatePythonContract(contract); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		identity := contract.Project + ":" + contract.Kind + ":" + contract.Target
		if seen[identity] {
			return fmt.Errorf("%s duplicates %s", label, identity)
		}
		seen[identity] = true
	}
	return nil
}

func validatePythonContract(contract PythonContract) error {
	if strings.TrimSpace(contract.Reason) == "" {
		return fmt.Errorf("reason is required")
	}
	if err := validatePythonContractTarget(contract); err != nil {
		return err
	}
	if len(contract.Keywords) > 0 && contract.Kind != "decorator" {
		return fmt.Errorf("only decorator contracts support literal keyword constraints")
	}
	return validatePythonContractMembers(contract)
}

func validatePythonContractTarget(contract PythonContract) error {
	chain := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	target := contract.Target
	if contract.Kind == "entry-point" {
		module, symbol, found := strings.Cut(target, ":")
		if !found || !chain.MatchString(module) || !chain.MatchString(symbol) {
			return fmt.Errorf("entry-point target must be module:attribute, including optional nested attributes")
		}
	} else if !chain.MatchString(target) {
		return fmt.Errorf("target must be an exact qualified Python name")
	}
	return nil
}

func validatePythonContractMembers(contract PythonContract) error {
	if contract.Kind != "type" && contract.hasTypeMembers() {
		return fmt.Errorf("only type contracts describe attributes, decorators, or annotated fields")
	}
	if contract.Kind == "decorator" && len(contract.Members) > 0 {
		return fmt.Errorf("decorator contracts preserve only the decorated definition")
	}
	if contract.Kind == "module-binding" && len(contract.Members) == 0 {
		return fmt.Errorf("module-binding contract requires exact binding names")
	}
	if contract.Kind == "type" && len(contract.Members) == 0 && !contract.hasTypeMembers() {
		return fmt.Errorf("type contract must describe consumed members")
	}
	return nil
}

func (contract PythonContract) hasTypeMembers() bool {
	return len(contract.Attributes)+len(contract.Decorators) > 0 || contract.AnnotatedFields
}
