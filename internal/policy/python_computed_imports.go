package policy

import (
	"fmt"
	pathpkg "path"
	"sort"
	"strings"
)

func validatePythonComputedImports(imports []PythonComputedImport) error {
	seen := map[string]bool{}
	for index := range imports {
		label := fmt.Sprintf("scope.pythonComputedImports[%d]", index)
		if err := validatePythonComputedImport(&imports[index], label); err != nil {
			return err
		}
		identity := pythonComputedImportIdentity(imports[index])
		if seen[identity] {
			return fmt.Errorf("%s duplicates a computed import callsite", label)
		}
		seen[identity] = true
	}
	sort.Slice(imports, func(left, right int) bool {
		return pythonComputedImportIdentity(imports[left]) < pythonComputedImportIdentity(imports[right])
	})
	return nil
}

func validatePythonComputedImport(item *PythonComputedImport, label string) error {
	if item.Callee == "pkgutil.resolve_name" && (item.EntryPointGroup != "" || len(item.Targets) != 0 || len(item.Configuration) != 1 || item.Shape != "module-object-call/v1") {
		return fmt.Errorf("%s object loader requires one registry input and module-object-call/v1 without a duplicate target inventory", label)
	}
	projectRoot := pathpkg.Dir(item.Project)
	if projectRoot != "." && item.Importer != projectRoot && !strings.HasPrefix(item.Importer, projectRoot+"/") {
		return fmt.Errorf("%s.importer must be contained by its project", label)
	}
	if item.Namespace != "" {
		return validatePythonComputedNamespace(item, label)
	}
	return nil
}

func validatePythonComputedNamespace(item *PythonComputedImport, label string) error {
	if !strings.Contains(item.Namespace, ".") {
		return fmt.Errorf("%s.namespace must be narrower than a top-level package", label)
	}
	if len(item.Targets) == 0 && len(item.Configuration) == 0 {
		return fmt.Errorf("%s namespace target must declare exact targets or governed configuration", label)
	}
	for index, target := range item.Targets {
		if target != item.Namespace && !strings.HasPrefix(target, item.Namespace+".") {
			return fmt.Errorf("%s.targets[%d] must be contained by namespace %q", label, index, item.Namespace)
		}
	}
	sort.Strings(item.Targets)
	seenInputs := map[string]bool{}
	for index, input := range item.Configuration {
		inputLabel := fmt.Sprintf("%s.configuration[%d]", label, index)
		identity := input.Path + "\x00" + input.JSONPointer
		if seenInputs[identity] {
			return fmt.Errorf("%s duplicates a configuration input", inputLabel)
		}
		seenInputs[identity] = true
	}
	sort.Slice(item.Configuration, func(left, right int) bool {
		return item.Configuration[left].Path+"\x00"+item.Configuration[left].JSONPointer <
			item.Configuration[right].Path+"\x00"+item.Configuration[right].JSONPointer
	})
	return nil
}

func pythonComputedImportIdentity(item PythonComputedImport) string {
	return fmt.Sprintf("%s\x00%s\x00%09d\x00%09d", item.Project, item.Importer, item.Line, item.Column)
}
