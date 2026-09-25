package rustfacts

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func (inventory *moduleInventory) resolvePath(module *rustModule, names []string, importing bool, depth int) (rustBinding, error) {
	if len(names) == 0 || depth >= 64 {
		return rustBinding{}, errors.New("rust import path is empty or its alias resolution is recursive")
	}
	binding, err := inventory.pathAnchor(module, names[0], importing, depth)
	if err != nil {
		return binding, err
	}
	for _, name := range names[1:] {
		if binding.module == nil {
			return binding, nil
		}
		binding, err = inventory.pathMember(binding.module, name, depth+1)
		if err != nil {
			return binding, err
		}
	}
	return binding, nil
}

func (inventory *moduleInventory) pathAnchor(module *rustModule, first string, importing bool, depth int) (rustBinding, error) {
	var binding rustBinding
	var err error
	switch first {
	case "crate":
		binding.module = module.crate.root
	case "self":
		binding.module = containingModule(module)
	case "super":
		module = containingModule(module)
		if module.parent == nil {
			return binding, errors.New("rust super path has no containing module")
		}
		binding.module = containingModule(module.parent)
	case "Self":
		binding.file = module.file
	default:
		if importing && module.crate.target.Edition == "2015" {
			module = module.crate.root
		}
		binding, err = inventory.resolveName(module, first, depth+1)
	}
	return binding, err
}

func (inventory *moduleInventory) pathMember(module *rustModule, name string, depth int) (rustBinding, error) {
	if name != "super" {
		binding, found, err := inventory.moduleName(module, name, depth)
		if err != nil || found {
			return binding, err
		}
		return binding, fmt.Errorf("module has no resolved written member %q", name)
	}
	if module.parent == nil {
		return rustBinding{}, errors.New("rust super path escapes its crate")
	}
	return rustBinding{module: containingModule(module.parent)}, nil
}

func (inventory *moduleInventory) resolveName(module *rustModule, name string, depth int) (rustBinding, error) {
	for current := module; current != nil; current = lexicalParent(current) {
		binding, found, err := inventory.moduleName(current, name, depth)
		if err != nil || found {
			return binding, err
		}
	}
	return inventory.externalCrate(module, name)
}

func containingModule(module *rustModule) *rustModule {
	for module.lexical {
		module = module.parent
	}
	return module
}

func lexicalParent(module *rustModule) *rustModule {
	if module.lexical {
		return module.parent
	}
	return nil
}

func (inventory *moduleInventory) moduleName(module *rustModule, name string, depth int) (rustBinding, bool, error) {
	if depth >= 64 {
		return rustBinding{}, false, errors.New("rust name resolution exceeds 64 alias/glob steps")
	}
	if binding, found := module.bindings[name]; found {
		if len(binding.alias) == 0 {
			return binding, true, nil
		}
		resolved, err := inventory.resolveAlias(binding, name, depth)
		return resolved, true, err
	}
	return inventory.resolveGlobName(module, name, depth+1)
}

func (inventory *moduleInventory) resolveAlias(binding rustBinding, name string, depth int) (rustBinding, error) {
	if binding.extern {
		if binding.alias[0] == "self" {
			return rustBinding{module: binding.scope.crate.root}, nil
		}
		return inventory.declaredCrate(binding.scope, binding.alias[0])
	}
	if len(binding.alias) == 1 && binding.alias[0] == name {
		return inventory.externalCrate(binding.scope, name)
	}
	return inventory.resolvePath(binding.scope, binding.alias, true, depth+1)
}

func (inventory *moduleInventory) resolveGlobName(module *rustModule, name string, depth int) (rustBinding, bool, error) {
	var result rustBinding
	found := false
	for _, glob := range module.globs {
		target, err := inventory.resolvePath(glob.scope, glob.alias, true, depth+1)
		if err != nil {
			return result, false, err
		}
		if target.module == nil {
			return result, false, errors.New("external glob imports need resolved binding evidence")
		}
		candidate, exists, err := inventory.moduleName(target.module, name, depth+1)
		if err != nil {
			return result, false, err
		}
		if !exists {
			continue
		}
		if found && (result.file != candidate.file || result.module != candidate.module || result.external != candidate.external) {
			return result, false, fmt.Errorf("glob imports supply ambiguous binding %q", name)
		}
		result, found = candidate, true
	}
	return result, found, nil
}

func (inventory *moduleInventory) externalCrate(module *rustModule, name string) (rustBinding, error) {
	if slices.Contains([]string{"bool", "char", "str", "f32", "f64", "i8", "i16", "i32", "i64", "i128", "isize", "u8", "u16", "u32", "u64", "u128", "usize"}, name) {
		return rustBinding{external: true}, nil
	}
	if implicitPrelude(module) {
		binding, err := inventory.declaredCrate(module, name)
		if err == nil && !(binding.standard && name == "std" && module.crate.noStd) {
			return binding, nil
		}
	}
	if preludeItem(module, name) {
		return rustBinding{external: true}, nil
	}
	return rustBinding{}, fmt.Errorf("path prefix %q has no resolved written module, item or Cargo dependency", name)
}

func (inventory *moduleInventory) declaredCrate(module *rustModule, name string) (rustBinding, error) {
	for _, crate := range inventory.crates {
		if crate.pkg.Manifest == module.crate.pkg.Manifest && crate.target.Name == name && libraryTarget(crate.target) {
			return rustBinding{module: crate.root}, nil
		}
	}
	for _, dependency := range module.crate.pkg.Dependencies {
		alias := dependency.Rename
		if alias == "" {
			alias = dependency.Name
		}
		if strings.ReplaceAll(alias, "-", "_") != name {
			continue
		}
		return inventory.dependencyBinding(dependency, name)
	}
	if slices.Contains([]string{"std", "core", "alloc", "proc_macro"}, name) {
		return rustBinding{external: true, standard: true}, nil
	}
	return rustBinding{}, fmt.Errorf("path prefix %q has no resolved written module, item or Cargo dependency", name)
}

func (inventory *moduleInventory) dependencyBinding(dependency Dependency, name string) (rustBinding, error) {
	if dependency.Path == "" {
		return rustBinding{external: true}, nil
	}
	for _, crate := range inventory.crates {
		if crate.pkg.Manifest == filepath.ToSlash(filepath.Join(dependency.Path, "Cargo.toml")) && libraryTarget(crate.target) {
			return rustBinding{module: crate.root}, nil
		}
	}
	return rustBinding{}, fmt.Errorf("local dependency %q has no discovered Cargo library target", name)
}

func libraryTarget(target Target) bool {
	return slices.Contains(target.Kinds, "lib") || slices.Contains(target.Kinds, "proc-macro")
}
