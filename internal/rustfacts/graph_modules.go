package rustfacts

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func (inventory *moduleInventory) readModuleFile(root *rustModule, ancestry []string) {
	if len(ancestry) >= 128 || slices.Contains(ancestry, root.file) {
		root.crate.findings = append(root.crate.findings, moduleCoverage(root.file, SourceGraphFact{}, "module source inclusion is recursive or exceeds 128 levels"))
		return
	}
	source, found := inventory.sources[root.file]
	if !found {
		root.crate.findings = append(root.crate.findings, moduleCoverage(root.file, SourceGraphFact{}, "governed module source has no complete parsed syntax facts"))
		return
	}
	if !inventory.repo.NativeAnalysis(root.file, "architecture") {
		root.crate.findings = append(root.crate.findings, moduleCoverage(root.file, SourceGraphFact{}, "one Cargo target's module closure is split between analysis providers"))
		return
	}
	scopes := map[string]*rustModule{"": root}
	for _, fact := range source.Graph {
		module := scopes[strings.Join(fact.Scope, "::")]
		if module == nil {
			continue
		}
		module.facts = append(module.facts, fact)
		if err := inventory.addModuleFact(module, fact, scopes, append(ancestry, root.file)); err != nil {
			root.crate.findings = append(root.crate.findings, moduleCoverage(root.file, fact, err.Error()))
		}
	}
}

func (inventory *moduleInventory) addModuleFact(module *rustModule, fact SourceGraphFact, scopes map[string]*rustModule, ancestry []string) error {
	switch fact.Kind {
	case "module":
		child, err := inventory.moduleChild(module, fact)
		if err != nil {
			return err
		}
		if err := bindRustName(module, fact.Name, rustBinding{module: child}); err != nil {
			return err
		}
		if fact.Inline {
			key := strings.Join(append(slices.Clone(fact.Scope), fact.Name), "::")
			scopes[key] = child
		} else {
			inventory.readModuleFile(child, ancestry)
		}
	case "item":
		return bindRustName(module, fact.Name, rustBinding{file: module.file, scope: module})
	case "use":
		binding := rustBinding{alias: fact.Path, scope: module}
		if fact.Alias == "*" {
			module.globs = append(module.globs, binding)
		} else if fact.Alias != "_" {
			return bindRustName(module, fact.Alias, binding)
		}
	}
	return nil
}

func bindRustName(module *rustModule, name string, binding rustBinding) error {
	if previous, found := module.bindings[name]; found {
		if previous.file == binding.file && binding.file != "" {
			return nil
		}
		return fmt.Errorf("binding %q has ambiguous written-source definitions", name)
	}
	module.bindings[name] = binding
	return nil
}

func (inventory *moduleInventory) moduleChild(parent *rustModule, fact SourceGraphFact) (*rustModule, error) {
	directory := filepath.ToSlash(filepath.Join(parent.directory, fact.Name))
	if fact.Inline {
		if fact.File != "" {
			return nil, errors.New("inline module path overrides need resolved directory evidence")
		}
		return newRustModule(parent.crate, parent, parent.file, directory, true), nil
	}
	candidates := []string{directory + ".rs", directory + "/mod.rs"}
	if fact.File != "" {
		base := filepath.Dir(parent.file)
		if parent.inline {
			base = parent.directory
		}
		if filepath.IsAbs(fact.File) || strings.ContainsAny(fact.File, "\\\x00") {
			return nil, errors.New("module path is not a contained portable relative path")
		}
		candidates = []string{filepath.ToSlash(filepath.Join(base, fact.File))}
	}
	path, err := inventory.uniqueModuleFile(candidates)
	if err != nil {
		return nil, err
	}
	childDirectory := filepath.ToSlash(filepath.Dir(path))
	if filepath.Base(path) != "mod.rs" {
		childDirectory = strings.TrimSuffix(path, ".rs")
	}
	return newRustModule(parent.crate, parent, path, childDirectory, false), nil
}

func (inventory *moduleInventory) uniqueModuleFile(candidates []string) (string, error) {
	found := ""
	for _, candidate := range candidates {
		if candidate == ".." || strings.HasPrefix(candidate, "../") || filepath.Ext(candidate) != ".rs" {
			return "", errors.New("module path escapes the checkout or is not Rust source")
		}
		if !inventory.available[candidate] {
			continue
		}
		if found != "" {
			return "", errors.New("module declaration has multiple governed source candidates")
		}
		found = candidate
	}
	if found == "" {
		return "", fmt.Errorf("module declaration has no governed source among %s", strings.Join(candidates, ", "))
	}
	return found, nil
}
