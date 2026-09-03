package repository

import (
	"fmt"
	"path/filepath"
	"strings"
)

func PythonModuleName(project PythonProject, source string) (module, packageName string) {
	base := pythonPackageCandidateBase(project, source)
	relative, err := filepath.Rel(filepath.FromSlash(base), filepath.FromSlash(source))
	if err != nil {
		return "", ""
	}
	relative = filepath.ToSlash(relative)
	extension := filepath.Ext(relative)
	if extension != ".py" && extension != ".pyi" {
		return "", ""
	}
	relative = strings.TrimSuffix(relative, extension)
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || parts[0] == ".." {
		return "", ""
	}
	packageParts := parts[:len(parts)-1]
	if parts[len(parts)-1] == "__init__" {
		parts = packageParts
	}
	return strings.Join(parts, "."), strings.Join(packageParts, ".")
}

func pythonProjectModuleLayoutProblem(project PythonProject) *PythonInventoryProblem {
	for _, source := range project.Files {
		module, packageName := PythonModuleName(project, source)
		if (module == "" || validPythonEntryPointIdentifierChain(module)) && (packageName == "" || validPythonEntryPointIdentifierChain(packageName)) {
			continue
		}
		problem := pythonInventoryProblem(
			PythonUnsupportedLayoutProblem,
			project.Manifest,
			source,
			fmt.Sprintf("Python project layout is unsupported: %s resolves to invalid module %q; move the manifest or place the source below a project src or build-system.backend-path root", source, module),
		)
		return &problem
	}
	return nil
}
