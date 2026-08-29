package policy

import (
	"errors"
	"fmt"
	"strings"
)

func validateModules(config *Config) error {
	if len(config.Modules) == 0 {
		return errors.New("modules must contain at least one named module")
	}
	config.ModuleByName = make(map[string]int, len(config.Modules))
	for index, module := range config.Modules {
		if err := validateModuleDefinition(module, index, config.ModuleByName); err != nil {
			return err
		}
		config.ModuleByName[module.Name] = index
	}
	if err := validateModuleDependencies(config); err != nil {
		return err
	}
	if err := validateArtifactTargetModules(config); err != nil {
		return err
	}
	if cycle := moduleCycle(*config); len(cycle) > 0 {
		return fmt.Errorf("module dependency graph must be acyclic: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

func validateModuleDefinition(module Module, index int, existing map[string]int) error {
	label := fmt.Sprintf("modules[%d]", index)
	if err := identifier(module.Name, label+".name"); err != nil {
		return err
	}
	if _, exists := existing[module.Name]; exists {
		return fmt.Errorf("duplicate module name %q", module.Name)
	}
	if len(module.Paths) == 0 {
		return fmt.Errorf("%s.paths must not be empty", label)
	}
	if err := validatePatterns(module.Paths, label+".paths", false); err != nil {
		return err
	}
	if err := validateUniqueStrings(module.DependsOn, label+".dependsOn", true); err != nil {
		return err
	}
	return validateUniqueStrings(module.Capabilities, label+".capabilities", true)
}

func validateArtifactTargetModules(config *Config) error {
	for _, target := range config.SupplyChain.ArtifactSecurity.Targets {
		if target.Module == "" {
			continue
		}
		if _, exists := config.ModuleByName[target.Module]; !exists {
			return fmt.Errorf("artifact security target %q references unknown module %q", target.Name, target.Module)
		}
	}
	return nil
}

func validateModuleDependencies(config *Config) error {
	for _, module := range config.Modules {
		for _, dependency := range module.DependsOn {
			if dependency == module.Name {
				return fmt.Errorf("module %q cannot depend on itself", module.Name)
			}
			if _, exists := config.ModuleByName[dependency]; !exists {
				return fmt.Errorf("module %q depends on unknown module %q", module.Name, dependency)
			}
		}
	}
	return nil
}

func moduleCycle(config Config) []string {
	state := map[string]int{}
	stack := []string{}
	var visit func(string) []string
	visit = func(name string) []string {
		if state[name] == 1 {
			for index, item := range stack {
				if item == name {
					return append(append([]string{}, stack[index:]...), name)
				}
			}
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		stack = append(stack, name)
		module := config.Modules[config.ModuleByName[name]]
		for _, dependency := range module.DependsOn {
			if cycle := visit(dependency); len(cycle) > 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		return nil
	}
	for _, module := range config.Modules {
		if cycle := visit(module.Name); len(cycle) > 0 {
			return cycle
		}
	}
	return nil
}
