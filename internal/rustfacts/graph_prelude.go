package rustfacts

import (
	"errors"
	"fmt"
	"slices"
)

func implicitPrelude(module *rustModule) bool {
	for current := module; current != nil; current = current.parent {
		if current.noPrelude {
			return false
		}
	}
	return true
}

func preludeItem(module *rustModule, name string) bool {
	if !implicitPrelude(module) {
		return false
	}
	if slices.Contains([]string{"Clone", "Copy", "Send", "Sized", "Sync", "Unpin", "Drop", "Fn", "FnMut", "FnOnce", "AsyncFn", "AsyncFnMut", "AsyncFnOnce", "AsRef", "AsMut", "From", "Into", "DoubleEndedIterator", "ExactSizeIterator", "Extend", "IntoIterator", "Iterator", "Option", "Some", "None", "Result", "Ok", "Err", "Default", "Eq", "Ord", "PartialEq", "PartialOrd"}, name) {
		return true
	}
	if !module.crate.noStd && slices.Contains([]string{"Box", "String", "Vec", "ToOwned", "ToString"}, name) {
		return true
	}
	if slices.Contains([]string{"2021", "2024"}, module.crate.target.Edition) && slices.Contains([]string{"TryFrom", "TryInto", "FromIterator"}, name) {
		return true
	}
	return module.crate.target.Edition == "2024" && slices.Contains([]string{"Future", "IntoFuture"}, name)
}

func (inventory *moduleInventory) resolveMacro(module *rustModule, path []string, namespace string, depth int) error {
	if depth >= 64 {
		return errors.New("macro alias resolution exceeds 64 levels")
	}
	if len(path) == 1 {
		return inventory.preludeMacro(module, path[0], namespace, depth)
	}
	if !standardMacroPath(path, namespace) {
		return errors.New("macro path is not a resolved standard-library macro")
	}
	binding, err := inventory.resolveName(module, path[0], depth+1)
	if err != nil || !binding.standard {
		return errors.New("standard-library macro namespace is shadowed")
	}
	return nil
}

func standardMacroPath(path []string, namespace string) bool {
	if len(path) < 2 || !(path[0] == "std" || path[0] == namespace) {
		return false
	}
	if len(path) == 2 {
		return true
	}
	if len(path) != 3 || namespace != "core" {
		return false
	}
	modules := map[string]string{"Clone": "clone", "Copy": "marker", "Debug": "fmt", "Default": "default", "Eq": "cmp", "PartialEq": "cmp", "Ord": "cmp", "PartialOrd": "cmp", "Hash": "hash"}
	return modules[path[2]] == path[1]
}

func (inventory *moduleInventory) preludeMacro(module *rustModule, name, namespace string, depth int) error {
	for current := module; current != nil; current = lexicalParent(current) {
		if binding, found := current.bindings[name]; found && len(binding.alias) > 0 {
			if binding.alias[len(binding.alias)-1] != name {
				return errors.New("macro alias changes the standard macro's syntax identity")
			}
			return inventory.resolveMacro(binding.scope, binding.alias, namespace, depth+1)
		}
		if _, found, err := inventory.resolveGlobName(current, name, depth+1); err != nil || found {
			return fmt.Errorf("macro %q has an unresolved glob binding", name)
		}
	}
	if !implicitPrelude(module) || module.crate.noStd && namespace != "core" {
		return fmt.Errorf("macro %q is not available in this crate's implicit prelude", name)
	}
	return nil
}
